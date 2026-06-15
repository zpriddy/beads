package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// mysqlTransaction implements storage.Transaction for the MySQL backend. Unlike
// the dolt transaction (regularTx + ignoredTx + dirty tracker + DOLT_COMMIT),
// this is a single std *sql.Tx — InnoDB's atomic commit is the only durability
// barrier we need.
type mysqlTransaction struct {
	tx    *sql.Tx
	store *MySQLStore
}

func (t *mysqlTransaction) isActiveWisp(ctx context.Context, id string) bool {
	var exists int
	err := t.tx.QueryRowContext(ctx, "SELECT 1 FROM wisps WHERE id = ? LIMIT 1", id).Scan(&exists)
	return err == nil
}

// CreateIssueImport delegates to CreateIssue. The mysql backend does not
// enforce prefix validation at the storage layer.
func (t *mysqlTransaction) CreateIssueImport(ctx context.Context, issue *types.Issue, actor string, skipPrefixValidation bool) error {
	return t.CreateIssue(ctx, issue, actor)
}

// RunInTransaction executes fn within a single MySQL transaction.
// commitMsg is accepted for interface parity but ignored — there is no
// version-control commit in MySQL.
func (s *MySQLStore) RunInTransaction(ctx context.Context, commitMsg string, fn func(tx storage.Transaction) error) error {
	if s.closed.Load() {
		return ErrStoreClosed
	}

	var lastErr error
	for attempt := 0; attempt < maxTxRetries; attempt++ {
		err := s.runMySQLTransaction(ctx, fn)
		if err == nil {
			return nil
		}
		if !isSerializationError(err) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (s *MySQLStore) runMySQLTransaction(ctx context.Context, fn func(tx storage.Transaction) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}

	mtx := &mysqlTransaction{tx: tx, store: s}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	if err := fn(mtx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return wrapTransactionError("commit", err)
	}
	return nil
}

// =============================================================================
// Transaction methods that mirror storage.Transaction.
// =============================================================================

// CreateIssue creates an issue within the transaction.
func (t *mysqlTransaction) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	bc, err := issueops.NewBatchContext(ctx, t.tx, storage.BatchCreateOptions{
		SkipPrefixValidation: true,
	})
	if err != nil {
		return err
	}
	return issueops.CreateIssueInTx(ctx, t.tx, bc, issue, actor)
}

// CreateIssues creates multiple issues within the transaction.
func (t *mysqlTransaction) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return issueops.CreateIssuesInTx(ctx, t.tx, issues, actor, storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: false,
	})
}

// GetIssue retrieves an issue within the transaction.
func (t *mysqlTransaction) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return issueops.GetIssueInTx(ctx, t.tx, id)
}

// SearchIssues searches for issues within the transaction.
func (t *mysqlTransaction) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return issueops.SearchIssuesInTx(ctx, t.tx, query, filter)
}

// UpdateIssue updates an issue within the transaction.
func (t *mysqlTransaction) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if rawMeta, ok := updates["metadata"]; ok {
		metadataStr, err := storage.NormalizeMetadataValue(rawMeta)
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		updates["metadata"] = metadataStr
		if err := validateMetadataIfConfigured(json.RawMessage(metadataStr)); err != nil {
			return err
		}
	}
	_, err := issueops.UpdateIssueInTx(ctx, t.tx, id, updates, actor)
	return err
}

// CloseIssue closes an issue within the transaction.
func (t *mysqlTransaction) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	_, err := issueops.CloseIssueInTx(ctx, t.tx, id, reason, actor, session)
	return err
}

// DeleteIssue deletes an issue within the transaction.
func (t *mysqlTransaction) DeleteIssue(ctx context.Context, id string) error {
	return issueops.DeleteIssueInTx(ctx, t.tx, id)
}

// AddDependency adds a dependency within the transaction.
func (t *mysqlTransaction) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return t.AddDependencyWithOptions(ctx, dep, actor, storage.DependencyAddOptions{})
}

// AddDependencyWithOptions adds a dependency with options.
func (t *mysqlTransaction) AddDependencyWithOptions(ctx context.Context, dep *types.Dependency, actor string, addOpts storage.DependencyAddOptions) error {
	sourceTable := "issues"
	writeTable := "dependencies"
	if t.isActiveWisp(ctx, dep.IssueID) {
		sourceTable = "wisps"
		writeTable = "wisp_dependencies"
	}

	isCrossPrefix := isCrossPrefixDep(dep.IssueID, dep.DependsOnID)
	targetTable := "issues"
	kind := issueops.DepTargetIssue
	switch {
	case isCrossPrefix, strings.HasPrefix(dep.DependsOnID, "external:"):
		kind = issueops.DepTargetExternal
	default:
		if t.isActiveWisp(ctx, dep.DependsOnID) {
			targetTable = "wisps"
			kind = issueops.DepTargetWisp
		}
	}

	opts := issueops.AddDependencyOpts{
		SourceTable:    sourceTable,
		TargetTable:    targetTable,
		WriteTable:     writeTable,
		IsCrossPrefix:  isCrossPrefix,
		SkipCycleCheck: addOpts.SkipCycleCheck,
		TargetKind:     &kind,
	}
	if err := issueops.AddDependencyInTx(ctx, t.tx, dep, actor, opts); err != nil {
		return err
	}
	t.store.invalidateBlockedIDsCache()
	return nil
}

// RemoveDependency removes a dependency within the transaction.
func (t *mysqlTransaction) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return issueops.RemoveDependencyInTx(ctx, t.tx, issueID, dependsOnID)
}

// GetDependencyRecords returns dependency records within the transaction.
func (t *mysqlTransaction) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	depTable := "dependencies"
	if t.isActiveWisp(ctx, issueID) {
		depTable = "wisp_dependencies"
	}

	//nolint:gosec
	rows, err := t.tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM %s
		WHERE issue_id = ?
	`, depTable), issueID)
	if err != nil {
		return nil, wrapQueryError("get dependency records in tx", err)
	}
	defer func() { _ = rows.Close() }()

	var deps []*types.Dependency
	for rows.Next() {
		var d types.Dependency
		var metadata, threadID sql.NullString
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &d.Type, &d.CreatedAt, &d.CreatedBy, &metadata, &threadID); err != nil {
			return nil, wrapScanError("get dependency records in tx", err)
		}
		if metadata.Valid {
			d.Metadata = metadata.String
		}
		if threadID.Valid {
			d.ThreadID = threadID.String
		}
		deps = append(deps, &d)
	}
	return deps, rows.Err()
}

// AddLabel adds a label within the transaction.
func (t *mysqlTransaction) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return issueops.AddLabelInTx(ctx, t.tx, "", "", issueID, label, actor)
}

// RemoveLabel removes a label within the transaction.
func (t *mysqlTransaction) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return issueops.RemoveLabelInTx(ctx, t.tx, "", "", issueID, label, actor)
}

// GetLabels returns labels within the transaction.
func (t *mysqlTransaction) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return issueops.GetLabelsInTx(ctx, t.tx, "", issueID)
}

// SetConfig sets a config value within the transaction.
func (t *mysqlTransaction) SetConfig(ctx context.Context, key, value string) error {
	return issueops.SetConfigInTx(ctx, t.tx, key, value)
}

// GetConfig gets a config value within the transaction.
func (t *mysqlTransaction) GetConfig(ctx context.Context, key string) (string, error) {
	return issueops.GetConfigInTx(ctx, t.tx, key)
}

// SetMetadata sets a metadata value within the transaction.
func (t *mysqlTransaction) SetMetadata(ctx context.Context, key, value string) error {
	return issueops.SetMetadataInTx(ctx, t.tx, key, value)
}

// GetMetadata gets a metadata value within the transaction.
func (t *mysqlTransaction) GetMetadata(ctx context.Context, key string) (string, error) {
	return issueops.GetMetadataInTx(ctx, t.tx, key)
}

// SetLocalMetadata sets a local-metadata value within the transaction.
func (t *mysqlTransaction) SetLocalMetadata(ctx context.Context, key, value string) error {
	return issueops.SetLocalMetadataInTx(ctx, t.tx, key, value)
}

// GetLocalMetadata gets a local-metadata value within the transaction.
func (t *mysqlTransaction) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	return issueops.GetLocalMetadataInTx(ctx, t.tx, key)
}

// AddComment adds a comment event within the transaction.
func (t *mysqlTransaction) AddComment(ctx context.Context, issueID, actor, comment string) error {
	return issueops.AddCommentEventInTx(ctx, t.tx, issueID, actor, comment)
}

// ImportIssueComment imports a comment within the transaction.
func (t *mysqlTransaction) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	_ = uuid.Nil // keep uuid import if needed; ImportIssueCommentInTx generates UUIDs internally
	return issueops.ImportIssueCommentInTx(ctx, t.tx, issueID, author, text, createdAt)
}

// GetIssueComments gets all comments for an issue within the transaction.
func (t *mysqlTransaction) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	return issueops.GetIssueCommentsInTx(ctx, t.tx, issueID)
}

// CycleThroughEdges reports a blocking cycle through one of the new edges,
// including the transaction's own uncommitted dependency writes. Mirrors
// embeddeddolt's implementation; mysql uses the same issueops helpers.
func (t *mysqlTransaction) CycleThroughEdges(ctx context.Context, edges [][2]string) (string, error) {
	graph := make(map[string][]string)
	if err := issueops.AppendBlockingGraphInTx(ctx, t.tx, []string{"dependencies", "wisp_dependencies"}, graph); err != nil {
		return "", err
	}
	return issueops.CycleThroughEdgesInGraph(graph, edges), nil
}
