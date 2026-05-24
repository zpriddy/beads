package mysql

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// Iter*** methods provide streaming access to query results. The current
// implementations buffer the full result set in memory and return a slice
// iterator — same shape as dolt's iter_stubs.go (be-yinl4d-iter). They will
// be replaced with truly streaming queries once a shared streaming abstraction
// lands across both backends.

func (s *MySQLStore) IterIssueComments(ctx context.Context, issueID string) (storage.Iter[types.Comment], error) {
	cs, err := s.GetIssueComments(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(cs), nil
}

func (s *MySQLStore) IterEvents(ctx context.Context, issueID string, limit int) (storage.Iter[types.Event], error) {
	ev, err := s.GetEvents(ctx, issueID, limit)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(ev), nil
}

func (s *MySQLStore) IterAllEventsSince(ctx context.Context, since time.Time) (storage.Iter[types.Event], error) {
	ev, err := s.GetAllEventsSince(ctx, since)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(ev), nil
}

func (s *MySQLStore) IterReadyWork(ctx context.Context, filter types.WorkFilter) (storage.Iter[types.Issue], error) {
	is, err := s.GetReadyWork(ctx, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(is), nil
}

func (s *MySQLStore) IterBlockedIssues(ctx context.Context, filter types.WorkFilter) (storage.Iter[types.BlockedIssue], error) {
	bs, err := s.GetBlockedIssues(ctx, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(bs), nil
}

func (s *MySQLStore) IterWisps(ctx context.Context, filter types.WispFilter) (storage.Iter[types.Issue], error) {
	ws, err := s.ListWisps(ctx, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(ws), nil
}

func (s *MySQLStore) IterAllDependencyRecords(ctx context.Context) (storage.Iter[types.Dependency], error) {
	all, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return nil, err
	}
	// Flatten map[string][]*types.Dependency into a single slice.
	var flat []*types.Dependency
	for _, deps := range all {
		flat = append(flat, deps...)
	}
	return storage.NewSliceIter(flat), nil
}

func (s *MySQLStore) IterIssues(ctx context.Context, query string, filter types.IssueFilter) (storage.Iter[types.Issue], error) {
	is, err := s.SearchIssues(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(is), nil
}

func (s *MySQLStore) IterDependenciesWithMetadata(ctx context.Context, issueID string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	deps, err := s.GetDependenciesWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(deps), nil
}

func (s *MySQLStore) IterDependentsWithMetadata(ctx context.Context, issueID string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	deps, err := s.GetDependentsWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(deps), nil
}
