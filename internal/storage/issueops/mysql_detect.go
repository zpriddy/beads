package issueops

import (
	"context"
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
func isMySQLBackendTx(ctx context.Context, tx DBTX) bool {
	rows, err := tx.QueryContext(ctx, "SELECT @@version_comment")
	if err != nil {
		return false
	}
	defer rows.Close()
	if !rows.Next() {
		return false
	}
	var versionComment string
	if err := rows.Scan(&versionComment); err != nil {
		return false
	}
	vc := strings.ToLower(versionComment)
	return !strings.Contains(vc, "dolt")
}
