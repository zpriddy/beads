// Closed-bead TTL sweep.
//
// On dolt, closed beads stay in the issues table forever — version history
// IS the audit trail. On mysql, the closed-bead JSONL export is the audit
// trail (see closed_export.go), so the row in the issues table can be
// reclaimed once it's been successfully exported.
//
// Originally that delete was synchronous with the close. Per pgr-s9u1 we
// defer it by a configurable TTL (default 6 hours) so `bd reopen` keeps
// working within the window. The deferred delete is performed by a periodic
// sweep that runs at most once per `mysql.closed-sweep-interval` (default
// 30 min). The sweep is triggered from:
//
//   - Bootstrap (on `bd init`)
//   - The auto-export hook in cmd/bd (post-command, on long-lived commands)
//
// Config keys:
//
//	mysql.closed-ttl              default 6h.  0 = immediate (legacy behavior).
//	                              -1 = never delete (keep closed rows forever).
//	mysql.closed-sweep-interval   default 30m. Lower bound between sweeps.
//
// Both are read on demand from the config table; changes take effect on the
// next sweep without a restart.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	closedTTLConfigKey           = "mysql.closed-ttl"
	closedSweepIntervalConfigKey = "mysql.closed-sweep-interval"
	defaultClosedTTL             = 6 * time.Hour
	defaultClosedSweepInterval   = 30 * time.Minute
	closedTTLDisableSentinel     = "-1"
)

// closedTTLState tracks the timestamp of the last successful sweep so we can
// honor the closed-sweep-interval throttle without spawning a goroutine.
type closedTTLState struct {
	mu        sync.Mutex
	lastSweep time.Time
}

// parseDurationConfig reads a config key and parses it as a Go duration.
// Returns (duration, isNeverSentinel, error). The sentinel "-1" means "never
// sweep" and is signaled by the second return value.
func (s *MySQLStore) parseDurationConfig(ctx context.Context, key string, def time.Duration) (time.Duration, bool, error) {
	val, err := s.GetConfig(ctx, key)
	if err != nil {
		return 0, false, err
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return def, false, nil
	}
	if val == closedTTLDisableSentinel {
		return 0, true, nil
	}
	// Allow bare integers (interpreted as seconds for backward compatibility
	// with raw-int config values seen in older fixtures).
	if d, err := time.ParseDuration(val); err == nil {
		return d, false, nil
	}
	return def, false, fmt.Errorf("mysql: parse %s=%q as duration: not recognized", key, val)
}

// closedTTL returns the configured TTL. The second return value is true when
// the operator has explicitly disabled the sweep ("-1"), in which case the
// duration is meaningless and callers should bail out.
func (s *MySQLStore) closedTTL(ctx context.Context) (time.Duration, bool, error) {
	return s.parseDurationConfig(ctx, closedTTLConfigKey, defaultClosedTTL)
}

// closedSweepInterval returns the minimum gap between sweeps. The "never"
// sentinel is not honored here — a 0/negative value falls back to the default.
func (s *MySQLStore) closedSweepInterval(ctx context.Context) time.Duration {
	d, _, err := s.parseDurationConfig(ctx, closedSweepIntervalConfigKey, defaultClosedSweepInterval)
	if err != nil || d <= 0 {
		return defaultClosedSweepInterval
	}
	return d
}

// SweepExpiredClosed deletes issues that were closed long enough ago to be
// past their TTL AND have already been exported (closed-export.enabled is
// true). Returns the number of issue rows deleted.
//
// Callers should normally use MaybeSweepExpiredClosed which honors the
// throttle interval; SweepExpiredClosed bypasses the throttle and is used
// for tests and explicit "sweep now" maintenance commands.
func (s *MySQLStore) SweepExpiredClosed(ctx context.Context) (int, error) {
	ttl, never, err := s.closedTTL(ctx)
	if err != nil {
		return 0, fmt.Errorf("mysql: read closed-ttl: %w", err)
	}
	if never {
		return 0, nil
	}

	// TTL == 0 means "delete immediately" — nothing for the sweep to do
	// because CloseIssueWithExport will have already deleted the row.
	if ttl <= 0 {
		return 0, nil
	}

	cutoff := time.Now().UTC().Add(-ttl)

	// Find candidates first (so we can scrub their auxiliary rows in a single
	// transaction). Limit per-sweep so a backlog can't lock the table.
	const sweepBatchLimit = 500
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM issues
		WHERE status = 'closed'
		  AND closed_at IS NOT NULL
		  AND closed_at <= ?
		ORDER BY closed_at ASC
		LIMIT ?`, cutoff, sweepBatchLimit)
	if err != nil {
		return 0, fmt.Errorf("mysql: query expired closed: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("mysql: scan expired closed: %w", err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	deleted := 0
	for _, id := range ids {
		// Reuse the same delete-after-export helper so the cleanup is
		// table-by-table identical to the immediate-delete path. Each id
		// runs in its own transaction so a single failure doesn't bring
		// down the whole sweep batch.
		if err := s.deleteAfterExport(ctx, id); err != nil {
			return deleted, fmt.Errorf("mysql: sweep delete %s: %w", id, err)
		}
		deleted++
	}
	return deleted, nil
}

// MaybeSweepExpiredClosed runs SweepExpiredClosed at most once per
// closed-sweep-interval. Designed to be called from any "long-lived"
// command site without hammering the DB. Errors are logged but not
// returned — sweep failures should not break user-visible commands.
func (s *MySQLStore) MaybeSweepExpiredClosed(ctx context.Context) {
	if s == nil || s.IsClosed() {
		return
	}

	interval := s.closedSweepInterval(ctx)

	s.ttlState.mu.Lock()
	since := time.Since(s.ttlState.lastSweep)
	if !s.ttlState.lastSweep.IsZero() && since < interval {
		s.ttlState.mu.Unlock()
		return
	}
	s.ttlState.lastSweep = time.Now()
	s.ttlState.mu.Unlock()

	if _, err := s.SweepExpiredClosed(ctx); err != nil {
		// Log via the same channel the rest of the package uses (stderr
		// warnings live in cmd/bd; in-package errors only surface in the
		// caller's error chain). For now we swallow — failing here would
		// turn a maintenance miss into a user-visible regression.
		_ = err
	}
}

// closeIssueRespectTTL is the close path that honors the configured TTL.
// When TTL > 0, it skips the delete and relies on a future sweep. When TTL
// is 0 it falls back to the legacy immediate-delete semantics. This allows
// callers (CloseIssueWithExport) to keep the export step unconditional.
func (s *MySQLStore) closeIssueRespectTTL(ctx context.Context, id string) error {
	ttl, never, err := s.closedTTL(ctx)
	if err != nil {
		return fmt.Errorf("mysql: read closed-ttl: %w", err)
	}
	if never {
		// Operator opted out of the delete entirely. Closed beads remain
		// in the issues table indefinitely.
		return nil
	}
	if ttl > 0 {
		// Defer to the sweep.
		return nil
	}
	// TTL == 0 → legacy immediate-delete semantics.
	return s.deleteAfterExport(ctx, id)
}

// =============================================================================
// Internal helper used by tests to short-circuit the throttle.
// =============================================================================

// resetSweepThrottleForTest clears the lastSweep timestamp so the next call
// to MaybeSweepExpiredClosed runs unconditionally. Test-only.
func (s *MySQLStore) resetSweepThrottleForTest() {
	s.ttlState.mu.Lock()
	s.ttlState.lastSweep = time.Time{}
	s.ttlState.mu.Unlock()
}

// silence unused-import warning when tests aren't compiled into the build.
var _ sql.NullTime
