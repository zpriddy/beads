package mysql

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

// TestMySQLStore_FlattenerInterface — type-assertion check for the
// Flattener interface used by `bd flatten`. The mysql backend satisfies
// it via a no-op (InnoDB self-manages tablespaces).
func TestMySQLStore_FlattenerInterface(t *testing.T) {
	var s any = &MySQLStore{}
	if _, ok := s.(storage.Flattener); !ok {
		t.Error("*MySQLStore does not satisfy storage.Flattener")
	}
}

// TestMySQLStore_CompactorInterface — type-assertion check for the
// Compactor interface used by `bd compact`. The mysql backend satisfies
// it via a no-op.
func TestMySQLStore_CompactorInterface(t *testing.T) {
	var s any = &MySQLStore{}
	if _, ok := s.(storage.Compactor); !ok {
		t.Error("*MySQLStore does not satisfy storage.Compactor")
	}
}

// TestMySQLStore_FlattenNoOp — verifies that calling Flatten on a closed
// (or disconnected) store returns nil without touching the DB. We only
// exercise the no-op contract; the store has no DB connection.
func TestMySQLStore_FlattenNoOp(t *testing.T) {
	s := &MySQLStore{}
	if err := s.Flatten(context.Background()); err != nil {
		t.Errorf("Flatten() should be a no-op, got: %v", err)
	}
}

// TestMySQLStore_CompactNoOp — same contract as Flatten.
func TestMySQLStore_CompactNoOp(t *testing.T) {
	s := &MySQLStore{}
	if err := s.Compact(context.Background(), "init", "boundary", 5, []string{"recent1", "recent2"}); err != nil {
		t.Errorf("Compact() should be a no-op, got: %v", err)
	}
}
