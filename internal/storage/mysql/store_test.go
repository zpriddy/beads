package mysql

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

// TestMySQLStore_InterfaceAssertion is a compile-time check that
// MySQLStore satisfies storage.Storage. The actual var-declaration in
// store.go would fail the build if this regressed; this test gives the
// reader an explicit, name-able signal.
func TestMySQLStore_InterfaceAssertion(t *testing.T) {
	var _ storage.Storage = (*MySQLStore)(nil)
}

// TestMySQLStore_RawDBAccessor verifies the type-assertion path callers
// use to reach the underlying *sql.DB.
func TestMySQLStore_RawDBAccessor(t *testing.T) {
	var s any = &MySQLStore{}
	if _, ok := s.(storage.RawDBAccessor); !ok {
		t.Error("*MySQLStore does not satisfy storage.RawDBAccessor")
	}
}

// TestMySQLStore_LifecycleManager verifies the type-assertion path callers
// use for IsClosed checks.
func TestMySQLStore_LifecycleManager(t *testing.T) {
	var s any = &MySQLStore{}
	if _, ok := s.(storage.LifecycleManager); !ok {
		t.Error("*MySQLStore does not satisfy storage.LifecycleManager")
	}
}

// TestOpen_NilConfigRejected ensures Open guards against nil configs.
func TestOpen_NilConfigRejected(t *testing.T) {
	if _, err := Open(context.Background(), nil); err == nil {
		t.Error("Open(nil) should error")
	}
}

// TestBootstrap_NilConfigRejected ensures Bootstrap guards against nil configs.
func TestBootstrap_NilConfigRejected(t *testing.T) {
	if _, err := Bootstrap(context.Background(), nil); err == nil {
		t.Error("Bootstrap(nil) should error")
	}
}

// TestMySQLStore_CloseIdempotent verifies Close on a freshly-created (but
// never-Opened) store is a no-op rather than a panic. The actual pool isn't
// initialized so we can't exercise the full close path here, but we can
// confirm the closed flag flips.
func TestMySQLStore_CloseIdempotent(t *testing.T) {
	s := &MySQLStore{}
	if s.IsClosed() {
		t.Error("fresh store should not report closed")
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if !s.IsClosed() {
		t.Error("after Close, IsClosed should be true")
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v (must be idempotent)", err)
	}
}

// TestMySQLStore_Path returns the DSN.
func TestMySQLStore_Path(t *testing.T) {
	s := &MySQLStore{dsn: "user:pass@tcp(127.0.0.1:3306)/beads"}
	if got := s.Path(); got != s.dsn {
		t.Errorf("Path() = %q, want %q", got, s.dsn)
	}
}
