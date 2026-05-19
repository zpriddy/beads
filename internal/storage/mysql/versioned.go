package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
