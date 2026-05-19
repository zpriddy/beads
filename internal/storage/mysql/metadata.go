package mysql

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/storage/issueops"
)

// SetMetadata sets a metadata value.
func (s *MySQLStore) SetMetadata(ctx context.Context, key, value string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.SetMetadataInTx(ctx, tx, key, value)
	})
}

// GetMetadata retrieves a metadata value.
func (s *MySQLStore) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		value, err = issueops.GetMetadataInTx(ctx, tx, key)
		return err
	})
	return value, err
}

// SetLocalMetadata sets a value in the local_metadata table. The MySQL backend
// has no dolt_ignore equivalent, so local_metadata is a regular table.
func (s *MySQLStore) SetLocalMetadata(ctx context.Context, key, value string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.SetLocalMetadataInTx(ctx, tx, key, value)
	})
}

// GetLocalMetadata retrieves a value from the local_metadata table.
func (s *MySQLStore) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		value, err = issueops.GetLocalMetadataInTx(ctx, tx, key)
		return err
	})
	return value, err
}
