package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// isCrossPrefixDep returns true if the two bead IDs have different prefixes.
func isCrossPrefixDep(sourceID, targetID string) bool {
	return types.ExtractPrefix(sourceID) != types.ExtractPrefix(targetID)
}

// AddDependency adds a dependency between two issues.
func (s *MySQLStore) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	isCrossPrefix := isCrossPrefixDep(dep.IssueID, dep.DependsOnID)

	sourceTable := "issues"
	writeTable := "dependencies"
	if s.isActiveWisp(ctx, dep.IssueID) {
		sourceTable = "wisps"
		writeTable = "wisp_dependencies"
	}

	targetTable := "issues"
	kind := issueops.DepTargetIssue
	switch {
	case isCrossPrefix, strings.HasPrefix(dep.DependsOnID, "external:"):
		kind = issueops.DepTargetExternal
	default:
		if s.isActiveWisp(ctx, dep.DependsOnID) {
			targetTable = "wisps"
			kind = issueops.DepTargetWisp
		}
	}

	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		opts := issueops.AddDependencyOpts{
			SourceTable:   sourceTable,
			TargetTable:   targetTable,
			WriteTable:    writeTable,
			IsCrossPrefix: isCrossPrefix,
			TargetKind:    &kind,
		}
		return issueops.AddDependencyInTx(ctx, tx, dep, actor, opts)
	}); err != nil {
		return err
	}
	s.invalidateBlockedIDsCache()
	return nil
}

// RemoveDependency removes a dependency between two issues.
func (s *MySQLStore) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.RemoveDependencyInTx(ctx, tx, issueID, dependsOnID)
	}); err != nil {
		return err
	}
	s.invalidateBlockedIDsCache()
	return nil
}

// GetDependencies retrieves issues that this issue depends on.
func (s *MySQLStore) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependenciesInTx(ctx, tx, issueID)
		return err
	})
	return result, err
}

// GetDependents retrieves issues that depend on this issue.
func (s *MySQLStore) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependentsInTx(ctx, tx, issueID)
		return err
	})
	return result, err
}

// GetDependenciesWithMetadata returns dependencies with metadata.
func (s *MySQLStore) GetDependenciesWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	var result []*types.IssueWithDependencyMetadata
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependenciesWithMetadataInTx(ctx, tx, issueID)
		return err
	})
	return result, err
}

// GetDependentsWithMetadata returns dependents with metadata.
func (s *MySQLStore) GetDependentsWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	var result []*types.IssueWithDependencyMetadata
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependentsWithMetadataInTx(ctx, tx, issueID)
		return err
	})
	return result, err
}

// GetDependencyRecords returns raw dependency records for an issue.
func (s *MySQLStore) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	depTable := "dependencies"
	if s.isActiveWisp(ctx, issueID) {
		depTable = "wisp_dependencies"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM %s
		WHERE issue_id = ?
	`, depTable), issueID) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanDependencyRows(rows)
}

// GetAllDependencyRecords returns all dependency records.
func (s *MySQLStore) GetAllDependencyRecords(ctx context.Context) (map[string][]*types.Dependency, error) {
	var result map[string][]*types.Dependency
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetAllDependencyRecordsInTx(ctx, tx)
		return err
	})
	return result, err
}

// GetDependencyRecordsForIssues returns dependency records for specific issues.
func (s *MySQLStore) GetDependencyRecordsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Dependency, error) {
	var result map[string][]*types.Dependency
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependencyRecordsForIssuesInTx(ctx, tx, issueIDs)
		return err
	})
	return result, err
}

// GetBlockingInfoForIssues returns blocking dependency records relevant to a set of issue IDs.
func (s *MySQLStore) GetBlockingInfoForIssues(ctx context.Context, issueIDs []string) (
	blockedByMap map[string][]string,
	blocksMap map[string][]string,
	parentMap map[string]string,
	err error,
) {
	err = s.withReadTx(ctx, func(tx *sql.Tx) error {
		var txErr error
		blockedByMap, blocksMap, parentMap, txErr = issueops.GetBlockingInfoForIssuesInTx(ctx, tx, issueIDs)
		return txErr
	})
	return
}

// GetDependencyCounts returns dependency counts for multiple issues.
func (s *MySQLStore) GetDependencyCounts(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error) {
	var result map[string]*types.DependencyCounts
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependencyCountsInTx(ctx, tx, issueIDs)
		return err
	})
	return result, err
}

// GetDependencyTree returns a dependency tree for visualization.
func (s *MySQLStore) GetDependencyTree(ctx context.Context, issueID string, maxDepth int, showAllPaths bool, reverse bool) ([]*types.TreeNode, error) {
	var result []*types.TreeNode
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDependencyTreeInTx(ctx, tx, issueID, maxDepth, showAllPaths, reverse)
		return err
	})
	return result, err
}

// DetectCycles finds circular dependencies.
func (s *MySQLStore) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	var result [][]*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.DetectCyclesInTx(ctx, tx)
		return err
	})
	return result, err
}

// IsBlocked checks if an issue has open blockers.
func (s *MySQLStore) IsBlocked(ctx context.Context, issueID string) (bool, []string, error) {
	var blocked bool
	var blockers []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		blocked, blockers, err = issueops.IsBlockedInTx(ctx, tx, issueID)
		return err
	})
	return blocked, blockers, err
}

// GetNewlyUnblockedByClose finds issues that become unblocked when an issue is closed.
func (s *MySQLStore) GetNewlyUnblockedByClose(ctx context.Context, closedIssueID string) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetNewlyUnblockedByCloseInTx(ctx, tx, closedIssueID)
		return err
	})
	return result, err
}

// =============================================================================
// Helpers
// =============================================================================

func scanDependencyRows(rows *sql.Rows) ([]*types.Dependency, error) {
	var deps []*types.Dependency
	for rows.Next() {
		var d types.Dependency
		var createdAt sql.NullTime
		var metadata, threadID sql.NullString
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &d.Type, &createdAt, &d.CreatedBy, &metadata, &threadID); err != nil {
			return nil, fmt.Errorf("failed to scan dependency: %w", err)
		}
		if createdAt.Valid {
			d.CreatedAt = createdAt.Time
		}
		if metadata.Valid {
			d.Metadata = metadata.String
		}
		if threadID.Valid {
			d.ThreadID = threadID.String
		}
		deps = append(deps, &d)
	}
	return deps, rows.Err()
}

// CountDependencies returns the number of issues that issueID depends on.
// Counts both `dependencies` and `wisp_dependencies` so the total matches
// GetDependenciesWithMetadata: a wisp's outgoing edges live in wisp_dependencies,
// a permanent issue's in dependencies. Mirrors dolt.CountDependencies (be-7daa14).
func (s *MySQLStore) CountDependencies(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var perm, wisp int64
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies WHERE issue_id = ?`, issueID).Scan(&perm); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM wisp_dependencies WHERE issue_id = ?`, issueID).Scan(&wisp); err != nil {
			return err
		}
		n = perm + wisp
		return nil
	})
	return n, err
}

// CountDependents returns the number of issues that depend on issueID.
// Mirrors dolt.CountDependents (be-7daa14). On MySQL the query planner
// resolves the STORED generated column `depends_on_id` directly under
// count(*), so we don't need the COALESCE workaround that dolt uses.
func (s *MySQLStore) CountDependents(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var perm, wisp int64
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies WHERE depends_on_id = ?`, issueID).Scan(&perm); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM wisp_dependencies WHERE depends_on_id = ?`, issueID).Scan(&wisp); err != nil {
			return err
		}
		n = perm + wisp
		return nil
	})
	return n, err
}

// CountDependentsByStatus returns the number of dependents in a given status.
// Mirrors dolt.CountDependentsByStatus (be-7daa14). MySQL can use depends_on_id
// directly thanks to its real query planner.
func (s *MySQLStore) CountDependentsByStatus(ctx context.Context, issueID string, status types.Status) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var perm, wisp int64
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM dependencies d
			 JOIN issues i ON i.id = d.issue_id
			 WHERE d.depends_on_id = ? AND i.status = ?`,
			issueID, string(status)).Scan(&perm); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT count(*) FROM wisp_dependencies d
			 JOIN wisps w ON w.id = d.issue_id
			 WHERE d.depends_on_id = ? AND w.status = ?`,
			issueID, string(status)).Scan(&wisp); err != nil {
			return err
		}
		n = perm + wisp
		return nil
	})
	return n, err
}
