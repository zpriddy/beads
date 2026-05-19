package mysql

import (
	"context"
	"fmt"
)

// Bootstrap initializes a fresh MySQL backend. It creates the database if
// missing, opens a pool, and runs all pending migrations. It is the
// equivalent of "bd init" for the mysql backend.
//
// Bootstrap is intentionally thin — Open does the heavy lifting when
// CreateIfMissing is set. This wrapper exists to give cmd/bd init a
// stable, intention-revealing entry point.
func Bootstrap(ctx context.Context, cfg *Config) (*MySQLStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mysql: Bootstrap: nil config")
	}
	bcfg := *cfg
	bcfg.CreateIfMissing = true
	store, err := Open(ctx, &bcfg)
	if err != nil {
		return nil, fmt.Errorf("mysql: bootstrap: %w", err)
	}
	return store, nil
}
