package issueops

import (
	"context"
	"database/sql"
	"strings"
)

// isMySQLBackendTx detects whether the active SQL connection is a real
// MySQL server (vs dolt's go-mysql-server). We check `version_comment`
// because dolt reports something like "Dolt SQL Server" while MySQL
// reports "MySQL Community Server - GPL" or similar.
//
// Used by RecomputeIsBlockedInTx to short-circuit the upstream
// UPDATE-with-correlated-subquery templates that MySQL 9 rejects with
// Error 1093 (gs-4vz). Removable once those templates are rewritten
// to use derived-table joins.
func isMySQLBackendTx(ctx context.Context, tx *sql.Tx) bool {
	var versionComment string
	if err := tx.QueryRowContext(ctx, "SELECT @@version_comment").Scan(&versionComment); err != nil {
		return false
	}
	vc := strings.ToLower(versionComment)
	return !strings.Contains(vc, "dolt")
}
