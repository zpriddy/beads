package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

// GetCurrentCommit returns a string that consumers can compare across calls
// to detect "did anything change since last time." On the dolt backend this
// is the literal HEAD commit hash. The mysql backend has no commits, so we
// return MAX(updated_at) across the issues table as an RFC3339 timestamp.
//
// This unblocks two callers that previously logged "not supported" warnings:
//   - export-auto change detection (writes JSONL only when this value moves)
//   - auto-backup (skips when this value matches the last backup's tag)
//
// If the issues table is empty, returns the unix epoch in RFC3339. The value
// is monotonically non-decreasing under normal write activity, which is all
// the change-detection callers actually need.
func (s *MySQLStore) GetCurrentCommit(ctx context.Context) (string, error) {
	var ts sql.NullTime
	err := s.db.QueryRowContext(ctx, "SELECT MAX(updated_at) FROM issues").Scan(&ts)
	if err != nil {
		return "", fmt.Errorf("mysql: get current commit: %w", err)
	}
	if !ts.Valid {
		return time.Unix(0, 0).UTC().Format(time.RFC3339Nano), nil
	}
	return ts.Time.UTC().Format(time.RFC3339Nano), nil
}

// History returns the lifecycle event log for an issue, scanned out of the
// `events` table (or `wisp_events` if the issue is currently a wisp).
//
// On the dolt backend, History returns one HistoryEntry per dolt commit with
// the *as-of* Issue snapshot at that commit (powered by dolt_history_issues).
// The mysql backend has no row-level snapshot history, so each HistoryEntry
// here represents a single audit-trail event:
//
//   - CommitHash → event row id (UUID)
//   - Committer  → "<actor> [<event_type>]"
//   - CommitDate → event created_at (UTC)
//   - Issue      → the *current* state of the issue (NOT as-of). All entries
//     share the same Issue value — callers that need true
//     point-in-time state should use the dolt backend.
//
// If the issue doesn't exist, returns ErrNotFound (via GetIssue).
//
// Newest events first (mirrors dolt history ordering).
func (s *MySQLStore) History(ctx context.Context, issueID string) ([]*storage.HistoryEntry, error) {
	current, err := s.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}

	table := "events"
	if s.isActiveWisp(ctx, issueID) {
		table = "wisp_events"
	}

	//nolint:gosec // table is from a hardcoded two-element allowlist above.
	q := fmt.Sprintf(`
		SELECT COALESCE(id, ''), event_type, actor, created_at
		FROM %s
		WHERE issue_id = ?
		ORDER BY created_at DESC, id DESC
	`, table)

	rows, err := s.db.QueryContext(ctx, q, issueID)
	if err != nil {
		return nil, fmt.Errorf("mysql: query %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*storage.HistoryEntry
	for rows.Next() {
		var (
			id, eventType, actor string
			createdAt            time.Time
		)
		if err := rows.Scan(&id, &eventType, &actor, &createdAt); err != nil {
			return nil, fmt.Errorf("mysql: scan history row: %w", err)
		}
		// Per-event Issue snapshot is a copy of current state — there's no
		// way to reconstruct as-of state from the events table without
		// replaying every prior event, which is out of scope.
		issueCopy := *current
		entries = append(entries, &storage.HistoryEntry{
			CommitHash: id,
			Committer:  fmt.Sprintf("%s [%s]", actor, eventType),
			CommitDate: createdAt.UTC(),
			Issue:      &issueCopy,
		})
	}
	return entries, rows.Err()
}
