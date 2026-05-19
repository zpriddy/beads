// Package mysql implements bd's storage.Storage interface against a plain
// MySQL/MariaDB server (no Dolt, no version control).
//
// Compared to the dolt backend, this package sacrifices version-control
// features (push/pull/history/--as-of/diff) in exchange for the operational
// simplicity, stability, and performance of stock InnoDB. Closed beads are
// auto-exported to JSONL files so the durable audit trail survives the
// trade-off (see closed_export.go).
//
// All operations target a vanilla MySQL connection — no DOLT_* stored
// procedures, no dolt_ignore tables, no dolt_status / dolt_log / dolt_diff
// queries. Schema migrations are a filtered subset of the shared migration
// set under internal/storage/schema/migrations: 19/28/40 (dolt_ignore-only)
// are skipped entirely and 41 is patched to drop its DOLT_COMMIT prelude.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	_ "github.com/go-sql-driver/mysql" // registers "mysql" driver

	"github.com/steveyegge/beads/internal/storage"
)

// Compile-time interface checks. The mysql backend deliberately satisfies
// only the core Storage interface — version-control / federation /
// history capability sub-interfaces are out of scope (see the package
// doc and beads-mysql-implementation-plan.md).
//
// The Storage assertion is added in Phase 2 once the CRUD methods are in
// place; the auxiliary capability assertions can land now because they
// only depend on DB() / IsClosed() / Close() — all defined here.
var (
	_ storage.RawDBAccessor    = (*MySQLStore)(nil)
	_ storage.LifecycleManager = (*MySQLStore)(nil)
)

// ErrStoreClosed is returned when an operation is attempted on a closed store.
var ErrStoreClosed = errors.New("store is closed")

// MySQLStore implements storage.Storage against a vanilla MySQL server.
type MySQLStore struct {
	db       *sql.DB
	dsn      string       // DSN used to open the pool (for diagnostics; never log)
	database string       // SQL database name
	beadsDir string       // .beads directory (used for closed-bead JSONL export)
	closed   atomic.Bool  // tracks whether Close() has been called
	mu       sync.RWMutex // serializes Close vs concurrent ops

	// Per-store caches (mirror the dolt backend's invalidation contract).
	cacheMu                      sync.Mutex
	customStatusCache            []string
	customStatusCached           bool
	customTypeCache              []string
	customTypeCached             bool
	infraTypeCache               map[string]bool
	infraTypeCached              bool
	blockedIDsCache              []string
	blockedIDsCacheMap           map[string]bool
	blockedIDsCached             bool
	blockedIDsCacheIncludesWisps bool
}

// DB returns the underlying *sql.DB. Use sparingly; prefer the typed methods
// for normal operations.
func (s *MySQLStore) DB() *sql.DB { return s.db }

// UnderlyingDB returns the underlying *sql.DB. Provided so callers can use
// the storage.RawDBAccessor type-assertion path consistently with the dolt
// backend.
func (s *MySQLStore) UnderlyingDB() *sql.DB { return s.db }

// IsClosed reports whether Close() has been called.
func (s *MySQLStore) IsClosed() bool { return s.closed.Load() }

// Path returns the DSN. The mysql backend has no on-disk path; the DSN is
// the closest analog. Callers that need a filesystem path should prefer the
// dolt backend.
func (s *MySQLStore) Path() string { return s.dsn }

// Close closes the underlying database connection pool.
func (s *MySQLStore) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	if err != nil {
		return fmt.Errorf("mysql: close: %w", err)
	}
	return nil
}

// withReadTx runs fn inside a read-only-style transaction. The dolt backend
// uses RLock to allow concurrent readers; we mirror the contract here for
// caller-visible parity.
func (s *MySQLStore) withReadTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if s.closed.Load() {
		return ErrStoreClosed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mysql: begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return fn(tx)
}

// withWriteTx runs fn inside a write transaction. The transaction is
// committed if fn returns nil and rolled back otherwise.
func (s *MySQLStore) withWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if s.closed.Load() {
		return ErrStoreClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mysql: begin write tx: %w", err)
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mysql: commit write tx: %w", err)
	}
	return nil
}
