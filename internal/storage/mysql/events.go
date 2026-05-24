package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// AddComment adds a comment event to an issue.
func (s *MySQLStore) AddComment(ctx context.Context, issueID, actor, comment string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.AddCommentEventInTx(ctx, tx, issueID, actor, comment)
	})
}

// GetEvents retrieves events for an issue.
func (s *MySQLStore) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	var result []*types.Event
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetEventsInTx(ctx, tx, issueID, limit)
		return err
	})
	return result, err
}

// GetAllEventsSince returns all events created after the given time, ordered by creation time.
func (s *MySQLStore) GetAllEventsSince(ctx context.Context, since time.Time) ([]*types.Event, error) {
	var result []*types.Event
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetAllEventsSinceInTx(ctx, tx, since)
		return err
	})
	return result, err
}

// AddIssueComment adds a comment to an issue (structured comment).
func (s *MySQLStore) AddIssueComment(ctx context.Context, issueID, author, text string) (*types.Comment, error) {
	return s.ImportIssueComment(ctx, issueID, author, text, time.Now().UTC())
}

// ImportIssueComment adds a comment during import, preserving the original timestamp.
func (s *MySQLStore) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	var result *types.Comment
	err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.ImportIssueCommentInTx(ctx, tx, issueID, author, text, createdAt)
		return err
	})
	return result, err
}

// GetIssueComments retrieves all comments for an issue.
func (s *MySQLStore) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	table := "comments"
	if s.isActiveWisp(ctx, issueID) {
		table = "wisp_comments"
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, issue_id, author, text, created_at
		FROM %s
		WHERE issue_id = ?
		ORDER BY created_at ASC, id ASC
	`, table), issueID) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanComments(rows)
}

// GetCommentsForIssues retrieves comments for multiple issues.
func (s *MySQLStore) GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error) {
	var result map[string][]*types.Comment
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetCommentsForIssuesInTx(ctx, tx, issueIDs)
		return err
	})
	return result, err
}

// GetCommentCounts returns the number of comments for each issue.
func (s *MySQLStore) GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error) {
	var result map[string]int
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetCommentCountsInTx(ctx, tx, issueIDs)
		return err
	})
	return result, err
}

func scanComments(rows *sql.Rows) ([]*types.Comment, error) {
	var comments []*types.Comment
	for rows.Next() {
		var c types.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, &c)
	}
	return comments, rows.Err()
}

// CountEvents returns the number of audit events for an issue, capped at limit
// (or unbounded if limit == 0). Mirrors dolt.CountEvents (be-7daa14).
func (s *MySQLStore) CountEvents(ctx context.Context, issueID string, limit int) (int64, error) {
	var n int64
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT count(*) FROM events WHERE issue_id = ?`, issueID).Scan(&n)
	})
	if err != nil {
		return 0, err
	}
	if limit > 0 && n > int64(limit) {
		n = int64(limit)
	}
	return n, nil
}
