package mysql

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// CheckEligibility checks if an issue is eligible for compaction at the given
// tier. Mechanically equivalent to the dolt implementation — issueops' SQL is
// portable (no dolt-specific table functions).
func (s *MySQLStore) CheckEligibility(ctx context.Context, issueID string, tier int) (bool, string, error) {
	var eligible bool
	var reason string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		eligible, reason, err = issueops.CheckEligibilityInTx(ctx, tx, issueID, tier)
		return err
	})
	return eligible, reason, err
}

// ApplyCompaction records a compaction result in the issues table. On dolt
// this is part of a Dolt-commit-history-aware flow that also squashes commits;
// on mysql it just updates the metadata columns (compaction_level,
// compacted_at, compacted_at_commit, original_size). Callers that want
// commit-history compaction should use the dolt backend.
func (s *MySQLStore) ApplyCompaction(ctx context.Context, issueID string, tier int, originalSize int, _ int, commitHash string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.ApplyCompactionInTx(ctx, tx, issueID, tier, originalSize, commitHash)
	})
}

// GetTier1Candidates returns issues eligible for tier 1 compaction.
func (s *MySQLStore) GetTier1Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	var result []*types.CompactionCandidate
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetTier1CandidatesInTx(ctx, tx)
		return err
	})
	return result, err
}

// GetTier2Candidates returns issues eligible for tier 2 compaction.
func (s *MySQLStore) GetTier2Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	var result []*types.CompactionCandidate
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetTier2CandidatesInTx(ctx, tx)
		return err
	})
	return result, err
}

// =============================================================================
// Flattener / Compactor — type-asserted-to interfaces (NOT part of DoltStorage).
//
// On dolt these squash commit history. InnoDB self-manages its tablespaces;
// there's no commit history to squash. We make these no-ops that succeed
// silently so callers (e.g., `bd flatten`, periodic compaction) can run
// unconditionally instead of backend-sniffing.
// =============================================================================

// Flatten is a no-op on the mysql backend. Returns nil so callers can invoke
// it without backend-sniffing.
func (s *MySQLStore) Flatten(_ context.Context) error { return nil }

// Compact is a no-op on the mysql backend. Parameters are accepted but ignored.
func (s *MySQLStore) Compact(_ context.Context, _, _ string, _ int, _ []string) error {
	return nil
}
