// dolt-only API stubs.
//
// The mysql backend cannot satisfy DoltStorage's full surface area natively
// — version-control / federation / history / sync / compaction / backup are
// all dolt-specific by design. We need MySQLStore to satisfy DoltStorage so
// the cmd/bd store factory contract stays the same; the methods below return
// a uniform "not supported on the mysql backend" error and never touch the
// database. cmd/bd consumers should short-circuit on cfg.GetBackend() before
// invoking these (see cmd/bd/dolt.go's PersistentPreRunE pattern).
package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// notSupported returns a stable error for dolt-only operations on the mysql
// backend. The string is grep-able and matches the dolt-cmd guard wording so
// users see consistent messaging.
func notSupported(op string) error {
	return fmt.Errorf("%q is not supported on the mysql backend", op)
}

// =============================================================================
// VersionControl — bd dolt commit / branch / checkout / merge / log / status.
// =============================================================================

func (s *MySQLStore) Branch(_ context.Context, _ string) error   { return notSupported("Branch") }
func (s *MySQLStore) Checkout(_ context.Context, _ string) error { return notSupported("Checkout") }
func (s *MySQLStore) Commit(_ context.Context, _ string) error   { return notSupported("Commit") }
func (s *MySQLStore) CommitWithConfig(_ context.Context, _ string) error {
	return notSupported("CommitWithConfig")
}
func (s *MySQLStore) CommitPending(_ context.Context, _ string) (bool, error) {
	return false, notSupported("CommitPending")
}
func (s *MySQLStore) CommitExists(_ context.Context, _ string) (bool, error) {
	return false, notSupported("CommitExists")
}
func (s *MySQLStore) CurrentBranch(_ context.Context) (string, error) {
	return "", notSupported("CurrentBranch")
}
func (s *MySQLStore) DeleteBranch(_ context.Context, _ string) error {
	return notSupported("DeleteBranch")
}
func (s *MySQLStore) ListBranches(_ context.Context) ([]string, error) {
	return nil, notSupported("ListBranches")
}
func (s *MySQLStore) Status(_ context.Context) (*storage.Status, error) {
	return nil, notSupported("Status")
}
func (s *MySQLStore) Log(_ context.Context, _ int) ([]storage.CommitInfo, error) {
	return nil, notSupported("Log")
}
func (s *MySQLStore) Merge(_ context.Context, _ string) ([]storage.Conflict, error) {
	return nil, notSupported("Merge")
}
func (s *MySQLStore) GetConflicts(_ context.Context) ([]storage.Conflict, error) {
	return nil, notSupported("GetConflicts")
}
func (s *MySQLStore) ResolveConflicts(_ context.Context, _ string, _ string) error {
	return notSupported("ResolveConflicts")
}

// =============================================================================
// HistoryViewer — bd show --history, bd diff, --as-of reads.
// =============================================================================
// History() is implemented in versioned.go (events-table-backed). AsOf and
// Diff remain stubbed: InnoDB has no row-level snapshot history, so true
// time-travel reads aren't expressible.

func (s *MySQLStore) AsOf(_ context.Context, _ string, _ string) (*types.Issue, error) {
	return nil, notSupported("AsOf")
}
func (s *MySQLStore) Diff(_ context.Context, _ string, _ string) ([]*storage.DiffEntry, error) {
	return nil, notSupported("Diff")
}

// =============================================================================
// RemoteStore — bd dolt push / pull / fetch / remote add/remove/list.
// =============================================================================

func (s *MySQLStore) AddRemote(_ context.Context, _ string, _ string) error {
	return notSupported("AddRemote")
}
func (s *MySQLStore) RemoveRemote(_ context.Context, _ string) error {
	return notSupported("RemoveRemote")
}
func (s *MySQLStore) HasRemote(_ context.Context, _ string) (bool, error) {
	return false, notSupported("HasRemote")
}
func (s *MySQLStore) ListRemotes(_ context.Context) ([]storage.RemoteInfo, error) {
	return nil, notSupported("ListRemotes")
}
func (s *MySQLStore) Push(_ context.Context) error { return notSupported("Push") }
func (s *MySQLStore) Pull(_ context.Context) error { return notSupported("Pull") }
func (s *MySQLStore) Fetch(_ context.Context, _ string) error {
	return notSupported("Fetch")
}
func (s *MySQLStore) ForcePush(_ context.Context) error { return notSupported("ForcePush") }
func (s *MySQLStore) PushRemote(_ context.Context, _ string, _ bool) error {
	return notSupported("PushRemote")
}
func (s *MySQLStore) PullRemote(_ context.Context, _ string) error {
	return notSupported("PullRemote")
}
func (s *MySQLStore) PushTo(_ context.Context, _ string) error {
	return notSupported("PushTo")
}
func (s *MySQLStore) PullFrom(_ context.Context, _ string) ([]storage.Conflict, error) {
	return nil, notSupported("PullFrom")
}

// =============================================================================
// SyncStore — bd sync.
// =============================================================================

func (s *MySQLStore) Sync(_ context.Context, _ string, _ string) (*storage.SyncResult, error) {
	return nil, notSupported("Sync")
}
func (s *MySQLStore) SyncStatus(_ context.Context, _ string) (*storage.SyncStatus, error) {
	return nil, notSupported("SyncStatus")
}

// =============================================================================
// FederationStore — federation peers.
// =============================================================================

func (s *MySQLStore) AddFederationPeer(_ context.Context, _ *storage.FederationPeer) error {
	return notSupported("AddFederationPeer")
}
func (s *MySQLStore) GetFederationPeer(_ context.Context, _ string) (*storage.FederationPeer, error) {
	return nil, notSupported("GetFederationPeer")
}
func (s *MySQLStore) ListFederationPeers(_ context.Context) ([]*storage.FederationPeer, error) {
	return nil, notSupported("ListFederationPeers")
}
func (s *MySQLStore) RemoveFederationPeer(_ context.Context, _ string) error {
	return notSupported("RemoveFederationPeer")
}

// =============================================================================
// CompactionStore — Dolt commit-history compaction.
// =============================================================================

func (s *MySQLStore) CheckEligibility(_ context.Context, _ string, _ int) (bool, string, error) {
	return false, "", notSupported("CheckEligibility")
}
func (s *MySQLStore) ApplyCompaction(_ context.Context, _ string, _ int, _ int, _ int, _ string) error {
	return notSupported("ApplyCompaction")
}
func (s *MySQLStore) GetTier1Candidates(_ context.Context) ([]*types.CompactionCandidate, error) {
	return nil, notSupported("GetTier1Candidates")
}
func (s *MySQLStore) GetTier2Candidates(_ context.Context) ([]*types.CompactionCandidate, error) {
	return nil, notSupported("GetTier2Candidates")
}

// =============================================================================
// BulkIssueStore extras — methods we already implement satisfy most of the
// interface. The few that remain are noted here.
// =============================================================================

// PromoteFromEphemeral is a no-op stub on mysql. The dolt backend uses it for
// crystallizing ephemeral wisps; mysql wisps stay in the wisps table by
// default. Returning a not-supported error matches the FederationStore
// pattern: the caller short-circuits before invoking.
func (s *MySQLStore) PromoteFromEphemeral(_ context.Context, _ string, _ string) error {
	return notSupported("PromoteFromEphemeral")
}

// UpdateIssueID is a maintenance op typically used by the dolt-shaped rename
// flow. Not supported on mysql.
func (s *MySQLStore) UpdateIssueID(_ context.Context, _ string, _ string, _ *types.Issue, _ string) error {
	return notSupported("UpdateIssueID")
}

// DeleteIssuesBySourceRepo is multi-repo-config cleanup. Stubbed for now;
// callers that need it on mysql should file a bead.
func (s *MySQLStore) DeleteIssuesBySourceRepo(_ context.Context, _ string) (int, error) {
	return 0, notSupported("DeleteIssuesBySourceRepo")
}

// =============================================================================
// AdvancedQueryStore extras — repo mtime bookkeeping.
// =============================================================================

// GetRepoMtime / SetRepoMtime / ClearRepoMtime: the repo_mtimes table mirrors
// the dolt_ignored local-state pattern; on mysql it's just a regular table.
// Implementations delegate to issueops.

func (s *MySQLStore) GetRepoMtime(_ context.Context, _ string) (int64, error) {
	return 0, notSupported("GetRepoMtime")
}

func (s *MySQLStore) SetRepoMtime(_ context.Context, _ string, _ string, _ int64) error {
	return notSupported("SetRepoMtime")
}

func (s *MySQLStore) ClearRepoMtime(_ context.Context, _ string) error {
	return notSupported("ClearRepoMtime")
}

// =============================================================================
// DependencyQueryStore extras.
// =============================================================================

// FindWispDependentsRecursive is used by the wisp-promotion flow on dolt.
// Stubbed on mysql — the wisp-promotion flow itself errors on mysql.
func (s *MySQLStore) FindWispDependentsRecursive(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, notSupported("FindWispDependentsRecursive")
}

// =============================================================================
// time import keeper — silence unused-import warnings if a future stub needs
// time.Duration / time.Time and an existing one drops it.
// =============================================================================

var _ = time.Now
