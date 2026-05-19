package mysql

import (
	"context"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// MergeSlotCreate creates the merge slot bead for the current rig.
func (s *MySQLStore) MergeSlotCreate(ctx context.Context, actor string) (*types.Issue, error) {
	return storage.MergeSlotCreateImpl(ctx, s, actor)
}

// MergeSlotCheck returns the current status of the merge slot.
func (s *MySQLStore) MergeSlotCheck(ctx context.Context) (*storage.MergeSlotStatus, error) {
	return storage.MergeSlotCheckImpl(ctx, s)
}

// MergeSlotAcquire attempts to acquire the merge slot atomically.
func (s *MySQLStore) MergeSlotAcquire(ctx context.Context, holder, actor string, wait bool) (*storage.MergeSlotResult, error) {
	return storage.MergeSlotAcquireImpl(ctx, s, holder, actor, wait)
}

// MergeSlotRelease releases the merge slot.
func (s *MySQLStore) MergeSlotRelease(ctx context.Context, holder, actor string) error {
	return storage.MergeSlotReleaseImpl(ctx, s, holder, actor)
}
