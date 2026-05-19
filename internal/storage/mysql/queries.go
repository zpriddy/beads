package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// GetReadyWork returns issues that are ready to work on (not blocked).
func (s *MySQLStore) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkInTx(ctx, tx, filter, s.computeBlockedIDsForReadyWork)
		return err
	})
	return result, err
}

// GetBlockedIssues returns issues that are blocked by other issues.
func (s *MySQLStore) GetBlockedIssues(ctx context.Context, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	var result []*types.BlockedIssue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetBlockedIssuesInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

// GetEpicsEligibleForClosure returns epics whose children are all closed.
func (s *MySQLStore) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	var result []*types.EpicStatus
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetEpicsEligibleForClosureInTx(ctx, tx)
		return err
	})
	return result, err
}

// GetStaleIssues returns issues that haven't been updated recently.
func (s *MySQLStore) GetStaleIssues(ctx context.Context, filter types.StaleFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetStaleIssuesInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

// GetStatistics returns summary statistics.
func (s *MySQLStore) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	stats := &types.Statistics{}
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return issueops.ScanIssueCountsInTx(ctx, tx, stats)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %w", err)
	}

	// Blocked count via cached computeBlockedIDs.
	blockedIDs, err := s.computeBlockedIDs(ctx, true)
	blockedCount := 0
	if err == nil {
		blockedCount = len(blockedIDs)
	}
	stats.BlockedIssues = blockedCount
	stats.ReadyIssues = stats.OpenIssues - blockedCount
	if stats.ReadyIssues < 0 {
		stats.ReadyIssues = 0
	}
	return stats, nil
}

// computeBlockedIDs returns the set of issue IDs that are blocked.
func (s *MySQLStore) computeBlockedIDs(ctx context.Context, includeWisps bool) ([]string, error) {
	var result []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = s.computeBlockedIDsForReadyWork(ctx, tx, includeWisps)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *MySQLStore) computeBlockedIDsForReadyWork(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, error) {
	s.cacheMu.Lock()
	if s.blockedIDsCached && (s.blockedIDsCacheIncludesWisps || !includeWisps) {
		result := s.blockedIDsCache
		s.cacheMu.Unlock()
		return result, nil
	}
	s.cacheMu.Unlock()

	result, _, err := issueops.ComputeBlockedIDsInTx(ctx, tx, includeWisps)
	if err != nil {
		return nil, err
	}
	blockedSet := make(map[string]bool, len(result))
	for _, id := range result {
		blockedSet[id] = true
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.blockedIDsCached && (s.blockedIDsCacheIncludesWisps || !includeWisps) {
		return s.blockedIDsCache, nil
	}
	s.blockedIDsCache = result
	s.blockedIDsCacheMap = blockedSet
	s.blockedIDsCached = true
	s.blockedIDsCacheIncludesWisps = includeWisps
	return result, nil
}

// GetMoleculeProgress returns progress stats for a molecule.
func (s *MySQLStore) GetMoleculeProgress(ctx context.Context, moleculeID string) (*types.MoleculeProgressStats, error) {
	stats := &types.MoleculeProgressStats{MoleculeID: moleculeID}

	issueTable := "issues"
	depTable := "dependencies"
	if s.isActiveWisp(ctx, moleculeID) {
		issueTable = "wisps"
		depTable = "wisp_dependencies"
	}

	var title sql.NullString
	//nolint:gosec
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT title FROM %s WHERE id = ?", issueTable), moleculeID).Scan(&title); err == nil && title.Valid {
		stats.MoleculeTitle = title.String
	}

	//nolint:gosec
	depRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT issue_id FROM %s
		WHERE depends_on_id = ? AND type = 'parent-child'
	`, depTable), moleculeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get molecule children: %w", err)
	}
	var childIDs []string
	for depRows.Next() {
		var id string
		if err := depRows.Scan(&id); err != nil {
			_ = depRows.Close()
			return nil, wrapScanError("get molecule progress: scan child", err)
		}
		childIDs = append(childIDs, id)
	}
	_ = depRows.Close()

	if len(childIDs) > 0 {
		statusMap := make(map[string]string)
		for start := 0; start < len(childIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(childIDs) {
				end = len(childIDs)
			}
			batch := childIDs[start:end]
			placeholders, args := buildSQLInClause(batch)
			//nolint:gosec
			sRows, err := s.db.QueryContext(ctx,
				fmt.Sprintf("SELECT id, status FROM %s WHERE id IN (%s)", issueTable, placeholders),
				args...)
			if err != nil {
				return nil, fmt.Errorf("failed to batch-fetch child statuses: %w", err)
			}
			for sRows.Next() {
				var id, status string
				if err := sRows.Scan(&id, &status); err != nil {
					_ = sRows.Close()
					return nil, wrapScanError("get molecule progress: scan status", err)
				}
				statusMap[id] = status
			}
			_ = sRows.Close()
		}
		for _, childID := range childIDs {
			status, ok := statusMap[childID]
			if !ok {
				continue
			}
			stats.Total++
			switch types.Status(status) {
			case types.StatusClosed:
				stats.Completed++
			case types.StatusInProgress:
				stats.InProgress++
				if stats.CurrentStepID == "" {
					stats.CurrentStepID = childID
				}
			}
		}
	}
	return stats, nil
}

// GetMoleculeLastActivity returns the most recent activity timestamp for a molecule.
func (s *MySQLStore) GetMoleculeLastActivity(ctx context.Context, moleculeID string) (*types.MoleculeLastActivity, error) {
	var result *types.MoleculeLastActivity
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetMoleculeLastActivityInTx(ctx, tx, moleculeID)
		return err
	})
	return result, err
}

// GetNextChildID returns the next available child ID for a parent.
func (s *MySQLStore) GetNextChildID(ctx context.Context, parentID string) (string, error) {
	var childID string
	err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		var err error
		childID, err = issueops.GetNextChildIDTx(ctx, tx, parentID)
		return err
	})
	return childID, err
}

// =============================================================================
// ListWisps
// =============================================================================

// ListWisps returns ephemeral issues matching the filter. Always queries the
// wisps table.
func (s *MySQLStore) ListWisps(ctx context.Context, filter types.WispFilter) ([]*types.Issue, error) {
	issueFilter := issueops.WispFilterToIssueFilter(filter)
	return s.searchWisps(ctx, "", issueFilter)
}

func (s *MySQLStore) searchWisps(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	whereClauses, args, err := issueops.BuildIssueFilterClauses(query, filter, wispsFilterTables)
	if err != nil {
		return nil, err
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}
	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	//nolint:gosec
	querySQL := fmt.Sprintf(`
		SELECT id FROM wisps
		%s
		ORDER BY priority ASC, created_at DESC
		%s
	`, whereSQL, limitSQL)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search wisps: %w", err)
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, wrapScanError("search wisps: scan id", err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, wrapQueryError("search wisps: rows", err)
	}

	if len(ids) == 0 {
		return nil, nil
	}
	return s.getWispsByIDs(ctx, ids)
}

// getWispsByIDs retrieves multiple wisps by ID, batching IN-clauses.
func (s *MySQLStore) getWispsByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	issueMap := make(map[string]*types.Issue, len(ids))
	for start := 0; start < len(ids); start += queryBatchSize {
		end := start + queryBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders, args := buildSQLInClause(batch)
		//nolint:gosec
		querySQL := fmt.Sprintf(`SELECT %s FROM wisps WHERE id IN (%s)`, issueSelectColumns, placeholders)
		rows, err := s.db.QueryContext(ctx, querySQL, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to get wisps by IDs: %w", err)
		}
		for rows.Next() {
			issue, err := scanIssueFrom(rows)
			if err != nil {
				_ = rows.Close()
				return nil, wrapScanError("get wisps by IDs", err)
			}
			issueMap[issue.ID] = issue
		}
		_ = rows.Close()
	}
	out := make([]*types.Issue, 0, len(ids))
	for _, id := range ids {
		if issue, ok := issueMap[id]; ok {
			out = append(out, issue)
		}
	}
	return out, nil
}
