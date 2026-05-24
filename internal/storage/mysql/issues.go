package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// CreateIssue creates a new issue. Mechanically equivalent to the dolt
// implementation, minus the DOLT_COMMIT prelude — InnoDB needs no extra
// versioning step.
func (s *MySQLStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	if issue == nil {
		return fmt.Errorf("issue must not be nil")
	}

	// Route to wisps table if ephemeral, no-history, or infra type.
	useWispsTable := issue.Ephemeral || issue.NoHistory || s.IsInfraTypeCtx(ctx, issue.IssueType)
	if useWispsTable && !issue.NoHistory {
		issue.Ephemeral = true
	}

	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		bc, err := issueops.NewBatchContext(ctx, tx, storage.BatchCreateOptions{
			SkipPrefixValidation: true,
		})
		if err != nil {
			return err
		}
		return issueops.CreateIssueInTx(ctx, tx, bc, issue, actor)
	})
}

// CreateIssues creates multiple issues in a single transaction.
func (s *MySQLStore) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return s.CreateIssuesWithFullOptions(ctx, issues, actor, storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: false,
	})
}

// CreateIssuesWithFullOptions creates multiple issues with full options control.
func (s *MySQLStore) CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts storage.BatchCreateOptions) error {
	if len(issues) == 0 {
		return nil
	}

	// All-wisps fast path: separate transactions per issue.
	if issueops.AllWisps(issues) {
		for _, issue := range issues {
			if !issue.NoHistory {
				issue.Ephemeral = true
			}
			if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
				bc, err := issueops.NewBatchContext(ctx, tx, opts)
				if err != nil {
					return err
				}
				return issueops.CreateIssueInTx(ctx, tx, bc, issue, actor)
			}); err != nil {
				return err
			}
		}
		return nil
	}

	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.CreateIssuesInTx(ctx, tx, issues, actor, opts)
	})
}

// GetIssue retrieves an issue by ID. issueops handles wisp routing internally.
func (s *MySQLStore) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	var issue *types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		issue, err = issueops.GetIssueInTx(ctx, tx, id)
		return err
	})
	return issue, err
}

// GetIssueByExternalRef retrieves an issue by external reference.
func (s *MySQLStore) GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error) {
	var id string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = issueops.GetIssueByExternalRefInTx(ctx, tx, externalRef)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetIssue(ctx, id)
}

// UpdateIssue updates fields on an issue. Drops the DOLT_COMMIT step but keeps
// the metadata-validation, wisp-routing, and demotion logic.
func (s *MySQLStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if rawMeta, ok := updates["metadata"]; ok {
		metadataStr, err := storage.NormalizeMetadataValue(rawMeta)
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		if err := validateMetadataIfConfigured(json.RawMessage(metadataStr)); err != nil {
			return err
		}
	}

	// Wisp routing — issueops.UpdateIssueInTx handles wisp/issues table internally.
	_, settingNoHistory := updates["no_history"]
	_, settingWisp := updates["wisp"]
	if settingNoHistory || settingWisp {
		return s.demoteToWisp(ctx, id, updates, actor)
	}

	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		_, err := issueops.UpdateIssueInTx(ctx, tx, id, updates, actor)
		return err
	}); err != nil {
		return err
	}

	if _, hasStatus := updates["status"]; hasStatus {
		s.invalidateBlockedIDsCache()
	}
	return nil
}

// ClaimIssue atomically claims an issue using compare-and-swap semantics.
func (s *MySQLStore) ClaimIssue(ctx context.Context, id string, actor string) error {
	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		_, err := issueops.ClaimIssueInTx(ctx, tx, id, actor)
		return err
	}); err != nil {
		return err
	}
	s.invalidateBlockedIDsCache()
	return nil
}

// ClaimReadyIssue atomically claims the first ready issue matching filter.
func (s *MySQLStore) ClaimReadyIssue(ctx context.Context, filter types.WorkFilter, actor string) (*types.Issue, error) {
	var claimed *types.Issue
	err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		var err error
		claimed, err = issueops.ClaimReadyIssueInTx(ctx, tx, filter, actor)
		return err
	})
	if err != nil {
		return nil, err
	}
	if claimed != nil {
		s.invalidateBlockedIDsCache()
	}
	return claimed, nil
}

// ReopenIssue reopens a closed issue.
func (s *MySQLStore) ReopenIssue(ctx context.Context, id string, reason string, actor string) error {
	updates := map[string]interface{}{
		"status":      string(types.StatusOpen),
		"defer_until": nil,
	}
	if err := s.UpdateIssue(ctx, id, updates, actor); err != nil {
		return err
	}
	if reason != "" {
		if err := s.AddComment(ctx, id, actor, reason); err != nil {
			return fmt.Errorf("reopen comment: %w", err)
		}
	}
	return nil
}

// UpdateIssueType changes the issue_type field of an issue.
func (s *MySQLStore) UpdateIssueType(ctx context.Context, id string, issueType string, actor string) error {
	return s.UpdateIssue(ctx, id, map[string]interface{}{"issue_type": issueType}, actor)
}

// CloseIssue closes an issue with a reason. The closed-bead JSONL export
// (see closed_export.go) wraps this method by first calling closeIssueRaw,
// then exporting + deleting when closed-export.enabled is true.
func (s *MySQLStore) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	return s.CloseIssueWithExport(ctx, id, reason, actor, session)
}

// closeIssueRaw performs the in-DB close without the JSONL export step.
func (s *MySQLStore) closeIssueRaw(ctx context.Context, id string, reason string, actor string, session string) error {
	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		_, err := issueops.CloseIssueInTx(ctx, tx, id, reason, actor, session)
		return err
	}); err != nil {
		return err
	}
	s.invalidateBlockedIDsCache()
	return nil
}

// DeleteIssue permanently removes an issue.
func (s *MySQLStore) DeleteIssue(ctx context.Context, id string) error {
	if err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return issueops.DeleteIssueInTx(ctx, tx, id)
	}); err != nil {
		return err
	}
	s.invalidateBlockedIDsCache()
	return nil
}

// DeleteIssues deletes multiple issues in a single transaction.
func (s *MySQLStore) DeleteIssues(ctx context.Context, ids []string, cascade bool, force bool, dryRun bool) (*types.DeleteIssuesResult, error) {
	if len(ids) == 0 {
		return &types.DeleteIssuesResult{}, nil
	}

	// Partition wisps from regular issues so wisp deletes can use their own batch.
	ephIDs, regularIDs := s.partitionByWispStatus(ctx, ids)
	wispDeleteCount := 0
	if !dryRun && len(ephIDs) > 0 {
		// Wisp deletion is straightforward in MySQL — no separate dolt_ignore tables;
		// just batch DELETE from wisps + auxiliary tables.
		deleted, err := s.deleteWispBatch(ctx, ephIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to batch delete wisps: %w", err)
		}
		wispDeleteCount = deleted
	} else if dryRun {
		wispDeleteCount = len(ephIDs)
	}

	if len(regularIDs) == 0 {
		return &types.DeleteIssuesResult{DeletedCount: wispDeleteCount}, nil
	}

	var result *types.DeleteIssuesResult
	if err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		r, err := issueops.DeleteIssuesInTx(ctx, tx, regularIDs, cascade, force, dryRun)
		result = r
		return err
	}); err != nil {
		if result != nil {
			result.DeletedCount += wispDeleteCount
		}
		return result, err
	}
	if result != nil {
		result.DeletedCount += wispDeleteCount
	}
	if !dryRun {
		s.invalidateBlockedIDsCache()
	}
	return result, nil
}

// SearchIssues finds issues matching query and filters.
func (s *MySQLStore) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.SearchIssuesInTx(ctx, tx, query, filter)
		return err
	})
	return result, err
}

// GetIssuesByIDs retrieves multiple issues by ID.
func (s *MySQLStore) GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetIssuesByIDsInTx(ctx, tx, ids, nil)
		return err
	})
	return result, err
}

// =============================================================================
// Wisp helpers — direct SQL (no dolt_ignore plumbing).
// =============================================================================

// deleteWispBatch deletes wisps + their auxiliary rows in batches.
func (s *MySQLStore) deleteWispBatch(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	deleted := 0
	for start := 0; start < len(ids); start += deleteBatchSize {
		end := start + deleteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders, args := buildSQLInClause(batch)

		err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
			for _, table := range []string{
				"wisp_labels", "wisp_dependencies", "wisp_events", "wisp_comments",
			} {
				//nolint:gosec
				if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE issue_id IN (%s)", table, placeholders), args...); err != nil {
					if isTableNotExistError(err) {
						continue
					}
					return wrapExecError("delete wisp aux from "+table, err)
				}
			}
			//nolint:gosec
			res, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM wisps WHERE id IN (%s)", placeholders), args...)
			if err != nil {
				return wrapExecError("delete wisps", err)
			}
			n, _ := res.RowsAffected()
			deleted += int(n)
			return nil
		})
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// demoteToWisp moves an issue from issues to wisps + copies aux data.
// Mirrors the dolt backend (without the DOLT_COMMIT step).
func (s *MySQLStore) demoteToWisp(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Read the issue from the issues table.
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM issues WHERE id = ?", issueSelectColumns), id)
	issue, err := scanIssueFrom(row)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: issue %s", storage.ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("failed to scan issue for demotion: %w", err)
	}

	applyUpdatesToIssueStruct(issue, updates)

	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := issueops.InsertIssueIntoTable(ctx, tx, "wisps", issue); err != nil {
			return fmt.Errorf("failed to insert into wisps: %w", err)
		}
		for _, copy := range []struct{ from, to, cols string }{
			{"labels", "wisp_labels", "issue_id, label"},
			{"dependencies", "wisp_dependencies", "issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id"},
			{"events", "wisp_events", "issue_id, event_type, actor, old_value, new_value, comment, created_at"},
			{"comments", "wisp_comments", "issue_id, author, text, created_at"},
		} {
			//nolint:gosec
			if _, err := tx.ExecContext(ctx,
				fmt.Sprintf("INSERT IGNORE INTO %s (%s) SELECT %s FROM %s WHERE issue_id = ?", copy.to, copy.cols, copy.cols, copy.from),
				id); err != nil {
				// Best-effort copy; log handled by caller via wrapExecError if it returns.
				return wrapExecError("demote: copy "+copy.from, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO wisp_events (issue_id, event_type, actor, old_value, new_value) VALUES (?, ?, ?, ?, ?)",
			id, types.EventUpdated, actor, "", "demoted to wisp"); err != nil {
			return wrapExecError("demote: record event", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM issues WHERE id = ?", id); err != nil {
			return wrapExecError("demote: delete from issues", err)
		}
		return nil
	})
}

// applyUpdatesToIssueStruct copies an updates map into an Issue struct in
// memory. Used by demoteToWisp so the wisps row reflects all field changes
// alongside the routing flag.
func applyUpdatesToIssueStruct(issue *types.Issue, updates map[string]interface{}) {
	for key, value := range updates {
		switch key {
		case "status":
			if v, ok := value.(string); ok {
				issue.Status = types.Status(v)
			}
		case "title":
			if v, ok := value.(string); ok {
				issue.Title = v
			}
		case "description":
			if v, ok := value.(string); ok {
				issue.Description = v
			}
		case "design":
			if v, ok := value.(string); ok {
				issue.Design = v
			}
		case "notes":
			if v, ok := value.(string); ok {
				issue.Notes = v
			}
		case "assignee":
			if v, ok := value.(string); ok {
				issue.Assignee = v
			}
		case "priority":
			if v, ok := value.(int); ok {
				issue.Priority = v
			}
		case "issue_type":
			if v, ok := value.(string); ok {
				issue.IssueType = types.IssueType(v)
			}
		case "wisp":
			if v, ok := value.(bool); ok {
				issue.Ephemeral = v
			}
		case "no_history":
			if v, ok := value.(bool); ok {
				issue.NoHistory = v
			}
		}
	}
}

// =============================================================================
// withRetryTx — std-tx with limited retry on serialization errors.
// =============================================================================

const maxTxRetries = 5

func (s *MySQLStore) withRetryTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	var err error
	for attempt := 0; attempt < maxTxRetries; attempt++ {
		err = s.withWriteTx(ctx, fn)
		if err == nil {
			return nil
		}
		if !isSerializationError(err) {
			return err
		}
	}
	return err
}

// invalidateBlockedIDsCache clears the blocked IDs cache.
func (s *MySQLStore) invalidateBlockedIDsCache() {
	s.cacheMu.Lock()
	s.blockedIDsCached = false
	s.blockedIDsCache = nil
	s.blockedIDsCacheMap = nil
	s.blockedIDsCacheIncludesWisps = false
	s.cacheMu.Unlock()
}

// =============================================================================
// Compile-time check that MySQLStore satisfies storage.Storage.
// (Asserted at end of issues.go since most methods are defined here / siblings.)
// =============================================================================
// The full assertion lives in store.go after all methods are in place.

// CountIssues returns the number of issues matching query and filter.
// Filter.Limit and Filter.Offset are ignored; all other fields apply.
// Mirrors dolt.CountIssues (be-7daa14).
func (s *MySQLStore) CountIssues(ctx context.Context, query string, filter types.IssueFilter) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		whereClauses, args, err := issueops.BuildIssueFilterClauses(query, filter, issuesFilterTables)
		if err != nil {
			return err
		}
		where := ""
		if len(whereClauses) > 0 {
			where = " WHERE " + strings.Join(whereClauses, " AND ")
		}
		//nolint:gosec // table name is a static constant; placeholders are bound
		q := fmt.Sprintf(`SELECT count(*) FROM issues%s`, where)
		return tx.QueryRowContext(ctx, q, args...).Scan(&n)
	})
	return n, err
}

// CountIssueComments returns the number of comments on an issue.
// Mirrors dolt.CountIssueComments (be-7daa14).
func (s *MySQLStore) CountIssueComments(ctx context.Context, issueID string) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM comments WHERE issue_id = ?`, issueID).Scan(&n)
	})
	return n, err
}
