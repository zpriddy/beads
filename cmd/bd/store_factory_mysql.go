package main

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/mysql"
)

// openMySQLStoreFromConfig opens a MySQL-backed store using the persisted
// metadata.json plus per-host overrides from configfile / env. Returns the
// store typed as storage.DoltStorage so the cmd/bd factory contract stays
// the same — dolt-only methods on the returned store are stubbed with a
// uniform "not supported on the mysql backend" error (see
// internal/storage/mysql/dolt_stubs.go).
func openMySQLStoreFromConfig(ctx context.Context, beadsDir string, cfg *configfile.Config) (storage.DoltStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("openMySQLStoreFromConfig: nil config")
	}
	database := cfg.Database
	if database == "" {
		database = configfile.DefaultDoltDatabase
	}

	mc := &mysql.Config{
		Database: database,
		BeadsDir: beadsDir,
	}
	store, err := mysql.Open(ctx, mc)
	if err != nil {
		return nil, fmt.Errorf("mysql open: %w", err)
	}
	return store, nil
}
