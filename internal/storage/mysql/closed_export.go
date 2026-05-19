// Package mysql closed-bead JSONL export.
//
// Closed beads are exported to a per-month JSONL file under .beads/closed/
// then deleted from the MySQL database. This trades version-history retention
// for InnoDB performance: the audit trail survives in the JSONL files, the
// hot table stays small, and JSONL is grep-friendly.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/steveyegge/beads/internal/types"
)

// closedExportRecord is the JSON document appended to the per-month JSONL file.
// It bundles the issue snapshot with its recent event timeline so future
// readers can reconstruct the close history without a database round-trip.
type closedExportRecord struct {
	SchemaVersion int            `json:"schema_version"`
	ExportedAt    time.Time      `json:"exported_at"`
	Issue         *types.Issue   `json:"issue"`
	Events        []*types.Event `json:"events,omitempty"`
}

const closedExportSchemaVersion = 1

// closedExportConfigKeys.
const (
	closedExportEnabledKey = "closed-export.enabled"
	closedExportPathKey    = "closed-export.path"
	closedExportDefaultDir = "closed"
)

// CloseIssue overrides the raw close to add JSONL export + delete-after-export.
// When closed-export.enabled is false, behaves like closeIssueRaw.
func (s *MySQLStore) CloseIssueWithExport(ctx context.Context, id, reason, actor, session string) error {
	// Step 1: perform the in-DB close.
	if err := s.closeIssueRaw(ctx, id, reason, actor, session); err != nil {
		return err
	}

	// Step 2: check whether the export is enabled.
	enabled, err := s.closedExportEnabled(ctx)
	if err != nil {
		return fmt.Errorf("close issue: read closed-export.enabled: %w", err)
	}
	if !enabled {
		return nil
	}

	// Step 3: snapshot the issue + recent events.
	issue, err := s.GetIssue(ctx, id)
	if err != nil {
		// If the issue isn't there, treat as already-exported / nothing to do.
		return nil
	}
	events, err := s.GetEvents(ctx, id, 100)
	if err != nil {
		return fmt.Errorf("close issue: get events for export: %w", err)
	}

	// Step 4: append to the per-month JSONL.
	if err := s.appendClosedExport(ctx, issue, events); err != nil {
		return fmt.Errorf("close issue: append closed export: %w", err)
	}

	// Step 5: delete the issue + auxiliary rows. Idempotent — if the delete
	// fails after a successful export, the next close re-appends; JSONL
	// consumers tolerate dupes (they can dedupe by issue.ID + closed_at).
	if err := s.deleteAfterExport(ctx, id); err != nil {
		return fmt.Errorf("close issue: delete after export: %w", err)
	}
	return nil
}

// closedExportEnabled reads closed-export.enabled. Default: true.
func (s *MySQLStore) closedExportEnabled(ctx context.Context) (bool, error) {
	val, err := s.GetConfig(ctx, closedExportEnabledKey)
	if err != nil {
		return false, err
	}
	if val == "" {
		return true, nil // default
	}
	return val == "true" || val == "1" || val == "yes" || val == "on", nil
}

// closedExportDir returns the directory where the JSONL files live. Default:
// <beadsDir>/closed/
func (s *MySQLStore) closedExportDir(ctx context.Context) (string, error) {
	val, err := s.GetConfig(ctx, closedExportPathKey)
	if err != nil {
		return "", err
	}
	if val == "" {
		// No beadsDir means we cannot export; the caller must fall through.
		if s.beadsDir == "" {
			return "", nil
		}
		return filepath.Join(s.beadsDir, closedExportDefaultDir), nil
	}
	if filepath.IsAbs(val) {
		return val, nil
	}
	if s.beadsDir == "" {
		return val, nil
	}
	return filepath.Join(s.beadsDir, val), nil
}

// appendClosedExport writes a single record to the per-month JSONL using a
// file lock so concurrent processes serialize their writes.
func (s *MySQLStore) appendClosedExport(ctx context.Context, issue *types.Issue, events []*types.Event) error {
	dir, err := s.closedExportDir(ctx)
	if err != nil {
		return err
	}
	if dir == "" {
		// No place to write — silently skip; the record stays in the DB.
		// Operators that care about durability set closed-export.path.
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	now := time.Now().UTC()
	stamp := issueClosedAt(issue, now)
	monthFile := filepath.Join(dir, stamp.Format("2006-01")+".jsonl")
	lockFile := monthFile + ".lock"

	rec := closedExportRecord{
		SchemaVersion: closedExportSchemaVersion,
		ExportedAt:    now,
		Issue:         issue,
		Events:        events,
	}
	encoded, err := json.Marshal(&rec)
	if err != nil {
		return fmt.Errorf("marshal closed export record: %w", err)
	}

	lock := flock.New(lockFile)
	locked, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire export lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("acquire export lock: timed out on %s", lockFile)
	}
	defer func() { _ = lock.Unlock() }()

	// #nosec G304 G302 — monthFile is derived from configured export dir + a
	// time.Format pattern, not from user input. 0o600 keeps the JSONL
	// readable only by the owner; export consumers run as the same user.
	f, err := os.OpenFile(monthFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", monthFile, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", monthFile, err)
	}
	return nil
}

// issueClosedAt returns the issue's ClosedAt if set, else fallback.
func issueClosedAt(issue *types.Issue, fallback time.Time) time.Time {
	if issue == nil {
		return fallback
	}
	if issue.ClosedAt != nil && !issue.ClosedAt.IsZero() {
		return *issue.ClosedAt
	}
	if !issue.UpdatedAt.IsZero() {
		return issue.UpdatedAt
	}
	return fallback
}

// deleteAfterExport removes the issue + its rows in dependencies/labels/events/
// comments after the JSONL append succeeded.
func (s *MySQLStore) deleteAfterExport(ctx context.Context, id string) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// Determine which tables to scrub. Wisp closes are unusual but possible
		// (close + export of an ephemeral); pick the matching set.
		isWisp := false
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM wisps WHERE id = ? LIMIT 1", id).Scan(new(int)); err == nil {
			isWisp = true
		}

		var tables []string
		var issueTable string
		if isWisp {
			issueTable = "wisps"
			tables = []string{"wisp_labels", "wisp_dependencies", "wisp_events", "wisp_comments"}
		} else {
			issueTable = "issues"
			tables = []string{"labels", "dependencies", "events", "comments"}
		}

		for _, table := range tables {
			//nolint:gosec // table is a hardcoded constant from the slice above
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE issue_id = ?", table), id); err != nil {
				if isTableNotExistError(err) {
					continue
				}
				return wrapExecError("delete after export: "+table, err)
			}
		}

		//nolint:gosec // issueTable is a hardcoded constant ("issues" or "wisps")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", issueTable), id); err != nil {
			return wrapExecError("delete after export: "+issueTable, err)
		}
		return nil
	})
}
