package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// SetConfig sets a configuration value.
func (s *MySQLStore) SetConfig(ctx context.Context, key, value string) error {
	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		if err := issueops.SetConfigInTx(ctx, tx, key, value); err != nil {
			return err
		}
		switch key {
		case "status.custom":
			if err := issueops.SyncCustomStatusesTable(ctx, tx, value); err != nil {
				return fmt.Errorf("syncing custom_statuses table: %w", err)
			}
		case "types.custom":
			if err := issueops.SyncCustomTypesTable(ctx, tx, value); err != nil {
				return fmt.Errorf("syncing custom_types table: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	s.cacheMu.Lock()
	switch key {
	case "status.custom":
		s.customStatusCached = false
		s.customStatusCache = nil
	case "types.custom":
		s.customTypeCached = false
		s.customTypeCache = nil
	case "types.infra":
		s.infraTypeCached = false
		s.infraTypeCache = nil
	}
	s.cacheMu.Unlock()

	return nil
}

// GetConfig retrieves a configuration value.
func (s *MySQLStore) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		value, err = issueops.GetConfigInTx(ctx, tx, key)
		return err
	})
	return value, err
}

// GetAllConfig retrieves all configuration values.
func (s *MySQLStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	var result map[string]string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetAllConfigInTx(ctx, tx)
		return err
	})
	return result, err
}

// DeleteConfig removes a configuration value.
func (s *MySQLStore) DeleteConfig(ctx context.Context, key string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.DeleteConfigInTx(ctx, tx, key)
	})
}

// (Metadata + LocalMetadata methods live in metadata.go.)

// GetCustomStatuses returns custom status name strings from config.
func (s *MySQLStore) GetCustomStatuses(ctx context.Context) ([]string, error) {
	s.cacheMu.Lock()
	if s.customStatusCached {
		result := s.customStatusCache
		s.cacheMu.Unlock()
		return result, nil
	}
	s.cacheMu.Unlock()

	detailed, err := s.GetCustomStatusesDetailed(ctx)
	if err != nil {
		return nil, err
	}
	return types.CustomStatusNames(detailed), nil
}

// GetCustomStatusesDetailed returns typed custom statuses with category information.
func (s *MySQLStore) GetCustomStatusesDetailed(ctx context.Context) ([]types.CustomStatus, error) {
	var detailed []types.CustomStatus
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var txErr error
		detailed, txErr = issueops.ResolveCustomStatusesDetailedInTx(ctx, tx)
		return txErr
	})
	if err != nil {
		log.Printf("warning: failed to resolve custom statuses: %v", err)
		if yamlStatuses := config.GetCustomStatusesFromYAML(); len(yamlStatuses) > 0 {
			return issueops.ParseStatusFallback(yamlStatuses), nil
		}
		return nil, nil
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if !s.customStatusCached {
		s.customStatusCache = types.CustomStatusNames(detailed)
		s.customStatusCached = true
	}
	return detailed, nil
}

// GetCustomTypes returns custom issue type values from config.
func (s *MySQLStore) GetCustomTypes(ctx context.Context) ([]string, error) {
	s.cacheMu.Lock()
	if s.customTypeCached {
		result := s.customTypeCache
		s.cacheMu.Unlock()
		return result, nil
	}
	s.cacheMu.Unlock()

	var result []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var txErr error
		result, txErr = issueops.ResolveCustomTypesInTx(ctx, tx)
		return txErr
	})
	if err != nil {
		if yamlTypes := config.GetCustomTypesFromYAML(); len(yamlTypes) > 0 {
			return yamlTypes, nil
		}
		return nil, err
	}

	s.cacheMu.Lock()
	s.customTypeCache = result
	s.customTypeCached = true
	s.cacheMu.Unlock()
	return result, nil
}

// (GetInfraTypes lives in helpers.go so Phase 2 can compile without config.go.
//  Phase 3's config.go layers on the YAML fallback / extended caching.)
