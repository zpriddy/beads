package mysql

import (
	"regexp"
	"testing"
)

// be-725: the defer_until / updated_at indexes must be wired into the
// embedded MySQL migration set and create the three indexes bd's hot polling
// queries depend on. A regression here (file misnamed so it is not embedded,
// a dropped CREATE INDEX, or a lost idempotency guard) silently reintroduces
// the full-table scans documented in w-gc-005.
//
// The mysql package has no live-DB unit-test harness (tests are pure-Go /
// sqlmock), so this verifies the migration through the package's own
// discovery + embedding path rather than by applying it. Applying the
// migration against a real MySQL/Dolt instance is the manual validation step
// in the work item.
func TestMigration0049DeferUntilIndexes(t *testing.T) {
	const (
		version  = 49
		filename = "0049_add_defer_until_updated_at_indexes.up.sql"
	)

	files, err := listMigrationFiles()
	if err != nil {
		t.Fatalf("listMigrationFiles: %v", err)
	}

	var found *migrationFile
	for i := range files {
		if files[i].version == version {
			found = &files[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("migration version %d not discovered in embedded MySQL set", version)
	}
	if found.name != filename {
		t.Errorf("migration %d filename = %q, want %q", version, found.name, filename)
	}

	data, err := upMigrations.ReadFile(migrationsDir + "/" + found.name)
	if err != nil {
		t.Fatalf("read %s: %v", found.name, err)
	}
	sql := string(data)

	// Each index must be created on the right table + column (spacing-tolerant).
	indexes := []struct {
		name string
		re   *regexp.Regexp
	}{
		{"idx_issues_defer_until", regexp.MustCompile(`CREATE INDEX idx_issues_defer_until ON issues\s*\(\s*defer_until\s*\)`)},
		{"idx_issues_updated_at", regexp.MustCompile(`CREATE INDEX idx_issues_updated_at ON issues\s*\(\s*updated_at\s*\)`)},
		{"idx_wisps_defer_until", regexp.MustCompile(`CREATE INDEX idx_wisps_defer_until ON wisps\s*\(\s*defer_until\s*\)`)},
	}
	for _, idx := range indexes {
		if !idx.re.MatchString(sql) {
			t.Errorf("migration %d missing CREATE INDEX for %s", version, idx.name)
		}
	}

	// MySQL has no CREATE INDEX IF NOT EXISTS; idempotency depends on an
	// INFORMATION_SCHEMA.STATISTICS existence guard per index. Without it,
	// re-applying (or applying to a DB that already has an index) errors.
	if got := len(regexp.MustCompile(`INFORMATION_SCHEMA\.STATISTICS`).FindAllString(sql, -1)); got < len(indexes) {
		t.Errorf("migration %d has %d INFORMATION_SCHEMA.STATISTICS guards, want >= %d (one per index for idempotency)", version, got, len(indexes))
	}
}
