package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// Re-exports from issueops to keep signatures tidy in the mysql package.
// =============================================================================

// issueSelectColumns is the canonical column list for issue rows.
const issueSelectColumns = issueops.IssueSelectColumns

// scanIssueFrom delegates to the shared issueops scanner.
var scanIssueFrom = issueops.ScanIssueFrom

// parseTimeString delegates to issueops.ParseTimeString.
var parseTimeString = issueops.ParseTimeString

// =============================================================================
// Filter table aliases — wisp routing keeps the same pair of filter table maps
// the dolt backend uses, just without the dolt_ignore semantics.
// =============================================================================

var (
	issuesFilterTables = issueops.IssuesFilterTables
	wispsFilterTables  = issueops.WispsFilterTables
)

// =============================================================================
// SQL helpers
// =============================================================================

// buildSQLInClause builds a parameterized IN clause for SQL queries.
func buildSQLInClause(ids []string) (string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
}

// =============================================================================
// Wisp routing — kept so ListWisps and per-issue wisp lookup behave the same
// as the dolt backend. The mysql backend writes wisps to the same database;
// there is no dolt_ignore equivalent, but the table-routing contract is the
// same.
// =============================================================================

// IsEphemeralID returns true if the ID belongs to an ephemeral issue.
func IsEphemeralID(id string) bool {
	return strings.Contains(id, "-wisp-")
}

// allEphemeral returns true if all IDs in the slice are ephemeral.
func allEphemeral(ids []string) bool {
	for _, id := range ids {
		if !IsEphemeralID(id) {
			return false
		}
	}
	return len(ids) > 0
}

// IsInfraTypeCtx reports whether the issue type is infrastructure (routed to
// wisps table). Mirrors the dolt backend.
func (s *MySQLStore) IsInfraTypeCtx(ctx context.Context, t types.IssueType) bool {
	return s.GetInfraTypes(ctx)[string(t)]
}

// GetInfraTypes returns infrastructure type names from config. The shipped
// implementation lives in config.go (Phase 3); this Phase 2 placeholder reads
// the same path so CreateIssue's wisp-routing test compiles in isolation.
//
// When config.go is layered in (Phase 3), it shadows this method via the
// receiver — the Phase 3 version handles caching and YAML fallback. Phase 2
// keeps the minimal path: read DB config, fall back to defaults.
func (s *MySQLStore) GetInfraTypes(ctx context.Context) map[string]bool {
	s.cacheMu.Lock()
	if s.infraTypeCached {
		result := s.infraTypeCache
		s.cacheMu.Unlock()
		return result
	}
	s.cacheMu.Unlock()

	var result map[string]bool
	_ = s.withReadTx(ctx, func(tx *sql.Tx) error {
		result = issueops.ResolveInfraTypesInTx(ctx, tx)
		return nil
	})
	if result == nil {
		typeList := domain.DefaultInfraTypes()
		result = make(map[string]bool, len(typeList))
		for _, t := range typeList {
			result[t] = true
		}
	}

	s.cacheMu.Lock()
	s.infraTypeCache = result
	s.infraTypeCached = true
	s.cacheMu.Unlock()
	return result
}

// isActiveWisp checks if an issue ID exists in the wisps table.
func (s *MySQLStore) isActiveWisp(ctx context.Context, id string) bool {
	if IsEphemeralID(id) {
		wisp, _ := s.getWisp(ctx, id)
		return wisp != nil
	}
	return s.wispExists(ctx, id)
}

// wispExists is a lightweight existence check on the wisps table.
func (s *MySQLStore) wispExists(ctx context.Context, id string) bool {
	if s.closed.Load() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM wisps WHERE id = ? LIMIT 1", id).Scan(&exists)
	return err == nil
}

// getWisp retrieves an issue from the wisps table.
func (s *MySQLStore) getWisp(ctx context.Context, id string) (*types.Issue, error) {
	if s.closed.Load() {
		return nil, ErrStoreClosed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM wisps
		WHERE id = ?
	`, issueSelectColumns), id)
	issue, err := scanIssueFrom(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: wisp %s", storage.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wisp: %w", err)
	}
	return issue, nil
}

// partitionByWispStatus splits IDs into (wispIDs, permIDs).
func (s *MySQLStore) partitionByWispStatus(ctx context.Context, ids []string) (wispIDs, permIDs []string) {
	if len(ids) == 0 {
		return nil, nil
	}

	var patternWispIDs []string
	for _, id := range ids {
		if IsEphemeralID(id) {
			patternWispIDs = append(patternWispIDs, id)
		} else {
			permIDs = append(permIDs, id)
		}
	}

	if len(patternWispIDs) > 0 {
		activeSet := s.batchWispExists(ctx, patternWispIDs)
		for _, id := range patternWispIDs {
			if activeSet[id] {
				wispIDs = append(wispIDs, id)
			} else {
				permIDs = append(permIDs, id)
			}
		}
	}

	if len(permIDs) == 0 {
		return
	}

	activeSet := s.batchWispExists(ctx, permIDs)
	if len(activeSet) == 0 {
		return
	}

	var realPerm []string
	for _, id := range permIDs {
		if activeSet[id] {
			wispIDs = append(wispIDs, id)
		} else {
			realPerm = append(realPerm, id)
		}
	}
	permIDs = realPerm
	return
}

// batchWispExists returns the set of IDs that exist in the wisps table.
func (s *MySQLStore) batchWispExists(ctx context.Context, ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	if s.closed.Load() {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]bool)
	for start := 0; start < len(ids); start += queryBatchSize {
		end := start + queryBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders, args := buildSQLInClause(batch)
		rows, err := s.db.QueryContext(ctx,
			fmt.Sprintf("SELECT id FROM wisps WHERE id IN (%s)", placeholders), //nolint:gosec
			args...)
		if err != nil {
			return nil
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				result[id] = true
			}
		}
		_ = rows.Close()
	}
	return result
}

// =============================================================================
// ID generation helpers (table-aware) — mirror dolt's wisps.go logic, sans
// dolt-only specifics.
// =============================================================================

// wispPrefix returns the ID prefix for wisp ID generation.
func wispPrefix(configPrefix string, issue *types.Issue) string {
	if issue.PrefixOverride != "" {
		return issue.PrefixOverride
	}
	if issue.IDPrefix != "" {
		return configPrefix + "-" + issue.IDPrefix
	}
	return configPrefix + "-wisp"
}

// isCounterModeTx checks whether issue_id_mode=counter is configured.
func isCounterModeTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var idMode string
	err := tx.QueryRowContext(ctx, "SELECT value FROM config WHERE `key` = ?", "issue_id_mode").Scan(&idMode)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to read issue_id_mode config: %w", err)
	}
	return idMode == "counter", nil
}

// =============================================================================
// Constants — kept identical to dolt for behavioral parity.
// =============================================================================

const (
	deleteBatchSize = 50
	queryBatchSize  = 200
)

// =============================================================================
// Metadata schema validation. The dolt and mysql backends share the same YAML
// schema mechanism; this is a copy of the dolt helper that keeps the mysql
// package self-contained.
// =============================================================================

func validateMetadataIfConfigured(metadata json.RawMessage) error {
	schema := loadMetadataSchema()
	if schema.Mode == "none" {
		return nil
	}
	errs := storage.ValidateMetadataSchema(metadata, schema)
	if len(errs) == 0 {
		return nil
	}
	if schema.Mode == "warn" {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "warning: %s\n", e.Error())
		}
		return nil
	}
	return fmt.Errorf("metadata schema violation: %s", errs[0].Error())
}

func loadMetadataSchema() storage.MetadataSchemaConfig {
	mode := config.MetadataValidationMode()
	if mode == "none" {
		return storage.MetadataSchemaConfig{Mode: "none"}
	}
	rawFields := config.MetadataSchemaFields()
	if rawFields == nil {
		return storage.MetadataSchemaConfig{Mode: "none"}
	}
	fields := make(map[string]storage.MetadataFieldSchema)
	for name, raw := range rawFields {
		fieldMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fields[name] = parseFieldSchema(fieldMap)
	}
	if len(fields) == 0 {
		return storage.MetadataSchemaConfig{Mode: "none"}
	}
	return storage.MetadataSchemaConfig{Mode: mode, Fields: fields}
}

func parseFieldSchema(m map[string]interface{}) storage.MetadataFieldSchema {
	schema := storage.MetadataFieldSchema{}
	if t, ok := m["type"].(string); ok {
		schema.Type = storage.MetadataFieldType(t)
	}
	if req, ok := m["required"].(bool); ok {
		schema.Required = req
	}
	if vals, ok := m["values"]; ok {
		switch v := vals.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					schema.Values = append(schema.Values, s)
				}
			}
		case string:
			for _, s := range strings.Split(v, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					schema.Values = append(schema.Values, s)
				}
			}
		}
	}
	if min, ok := toFloat64(m["min"]); ok {
		schema.Min = &min
	}
	if max, ok := toFloat64(m["max"]); ok {
		schema.Max = &max
	}
	return schema
}

func toFloat64(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
