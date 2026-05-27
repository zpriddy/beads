package schema

import (
	"os"
	"regexp"
	"testing"
)

// be-725: the defer_until / updated_at indexes must be wired into the
// embedded Dolt/schema migration set and create the three indexes bd's hot
// polling queries depend on, with a matching down migration. A regression
// here (file misnamed so it is not embedded, a dropped CREATE INDEX, or a
// down that does not undo the up) silently reintroduces the full-table scans
// documented in w-gc-005 or leaves the rollback incomplete.
//
// The schema package's unit tests are sqlmock-based (no live Dolt), so this
// verifies the migration through the package's own discovery + embedding path
// rather than by applying it. Applying the migration against a real Dolt
// instance is the manual validation step in the work item.
func TestMigration0048DeferUntilIndexes(t *testing.T) {
	const (
		version    = 48
		upFile     = "0048_add_defer_until_updated_at_indexes.up.sql"
		downFile   = "0048_add_defer_until_updated_at_indexes.down.sql"
		migrateDir = "migrations"
	)

	var found *migrationFile
	for _, mf := range mainSource.list() {
		if mf.version == version {
			mf := mf
			found = &mf
			break
		}
	}
	if found == nil {
		t.Fatalf("migration version %d not discovered in embedded schema set", version)
	}
	if found.name != upFile {
		t.Errorf("migration %d filename = %q, want %q", version, found.name, upFile)
	}

	data, err := mainSource.files.ReadFile(migrateDir + "/" + found.name)
	if err != nil {
		t.Fatalf("read %s: %v", found.name, err)
	}
	up := string(data)

	upIndexes := []struct {
		name string
		re   *regexp.Regexp
	}{
		{"idx_issues_defer_until", regexp.MustCompile(`CREATE INDEX (?:IF NOT EXISTS )?idx_issues_defer_until ON issues\s*\(\s*defer_until\s*\)`)},
		{"idx_issues_updated_at", regexp.MustCompile(`CREATE INDEX (?:IF NOT EXISTS )?idx_issues_updated_at ON issues\s*\(\s*updated_at\s*\)`)},
		{"idx_wisps_defer_until", regexp.MustCompile(`CREATE INDEX (?:IF NOT EXISTS )?idx_wisps_defer_until ON wisps\s*\(\s*defer_until\s*\)`)},
	}
	for _, idx := range upIndexes {
		if !idx.re.MatchString(up) {
			t.Errorf("up migration %d missing CREATE INDEX for %s", version, idx.name)
		}
	}

	// wisps is dolt-ignored (0019): its index must be guarded by a
	// table-existence check so the migration is safe where wisps is absent
	// (mirrors 0031's wisp_events guard).
	if !regexp.MustCompile(`INFORMATION_SCHEMA\.TABLES`).MatchString(up) {
		t.Errorf("up migration %d must guard the wisps index on table existence (INFORMATION_SCHEMA.TABLES)", version)
	}

	// The paired down migration must drop all three indexes.
	downData, err := os.ReadFile(migrateDir + "/" + downFile)
	if err != nil {
		t.Fatalf("read %s: %v", downFile, err)
	}
	down := string(downData)
	for _, drop := range []struct {
		name string
		re   *regexp.Regexp
	}{
		{"idx_issues_defer_until", regexp.MustCompile(`DROP INDEX idx_issues_defer_until ON issues`)},
		{"idx_issues_updated_at", regexp.MustCompile(`DROP INDEX idx_issues_updated_at ON issues`)},
		{"idx_wisps_defer_until", regexp.MustCompile(`DROP INDEX idx_wisps_defer_until ON wisps`)},
	} {
		if !drop.re.MatchString(down) {
			t.Errorf("down migration %d missing DROP INDEX for %s", version, drop.name)
		}
	}
}
