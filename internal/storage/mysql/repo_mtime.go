package mysql

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/storage/issueops"
)

// GetRepoMtime returns the cached mtime (nanoseconds) for a repo path.
// On dolt this lives in a dolt_ignored local-state table; on mysql it's just
// the regular repo_mtimes table (no clone-local-state distinction). Returns
// 0 if no entry exists.
func (s *MySQLStore) GetRepoMtime(ctx context.Context, repoPath string) (int64, error) {
	var result int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetRepoMtimeInTx(ctx, tx, repoPath)
		return err
	})
	return result, err
}

// SetRepoMtime upserts the mtime cache for a repo path.
func (s *MySQLStore) SetRepoMtime(ctx context.Context, repoPath, jsonlPath string, mtimeNs int64) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.SetRepoMtimeInTx(ctx, tx, repoPath, jsonlPath, mtimeNs)
	})
}

// ClearRepoMtime removes the mtime cache entry for a repo path.
func (s *MySQLStore) ClearRepoMtime(ctx context.Context, repoPath string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.ClearRepoMtimeInTx(ctx, tx, repoPath)
	})
}
