package mysql

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Migrations are a filtered, MySQL-only copy of the shared migration set
// under internal/storage/schema/migrations:
//   - 0019, 0028, 0040 are skipped entirely (dolt_ignore-only)
//   - 0041 is patched to drop its DOLT_COMMIT prelude (see the file's
//     header comment)
//
// Every other migration is reused verbatim. The cursor table mirrors the
// dolt backend's name (`schema_migrations`) so debuggers can read either
// backend with the same query.
//
//go:embed migrations/*.up.sql
var upMigrations embed.FS

const migrationsDir = "migrations"

// schemaCursorTable tracks applied migration versions. Mirrors the name
// used by the dolt backend.
const schemaCursorTable = "schema_migrations"

type migrationFile struct {
	version int
	name    string
}

// runMigrations applies all pending migrations on the store. It is
// idempotent and safe to call repeatedly.
//
// The migrator does NOT use MySQL named locks (GET_LOCK / RELEASE_LOCK)
// because the mysql backend's intended deployment is a single bd process
// per database — multi-writer is the dolt path. If we ever need
// multi-writer here, this is the spot to add it.
func (s *MySQLStore) runMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, bootstrapCursorTableSQL()); err != nil {
		return fmt.Errorf("mysql: create %s: %w", schemaCursorTable, err)
	}

	files, err := listMigrationFiles()
	if err != nil {
		return err
	}

	var current int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM "+schemaCursorTable,
	).Scan(&current); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("mysql: read %s version: %w", schemaCursorTable, err)
	}

	for _, mf := range files {
		if mf.version <= current {
			continue
		}
		data, err := upMigrations.ReadFile(migrationsDir + "/" + mf.name)
		if err != nil {
			return fmt.Errorf("mysql: read migration %s: %w", mf.name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(data)); err != nil {
			return fmt.Errorf("mysql: apply migration %s: %w", mf.name, err)
		}
		if _, err := s.db.ExecContext(ctx,
			"INSERT IGNORE INTO "+schemaCursorTable+" (version) VALUES (?)",
			mf.version,
		); err != nil {
			return fmt.Errorf("mysql: record migration %s: %w", mf.name, err)
		}
	}
	return nil
}

func bootstrapCursorTableSQL() string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	version INT PRIMARY KEY,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, schemaCursorTable)
}

func listMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(upMigrations, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("mysql: read embedded migrations: %w", err)
	}
	files := make([]migrationFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, err := parseMigrationVersion(e.Name())
		if err != nil {
			return nil, fmt.Errorf("mysql: invalid migration filename %q: %w", e.Name(), err)
		}
		files = append(files, migrationFile{version: v, name: e.Name()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})
	return files, nil
}

func parseMigrationVersion(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("no version prefix")
	}
	return strconv.Atoi(parts[0])
}
