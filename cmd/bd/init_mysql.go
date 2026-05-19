package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/mysql"
	"github.com/steveyegge/beads/internal/ui"
)

// runMySQLInit handles `bd init --backend=mysql`. It is intentionally a
// minimal path: connect to MySQL, run migrations, write metadata.json with
// backend=mysql, and seed the issue prefix. Dolt server lifecycle, federation
// remotes, JSONL import, etc. are all out of scope for the mysql backend.
func runMySQLInit(ctx context.Context, prefix, database, serverHost string, serverPort int, serverUser string, quiet bool) {
	// Resolve the .beads directory the same way the dolt path does, falling
	// back to ./.beads when the workspace has no existing init.
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		beadsDir = filepath.Join(".", ".beads")
	}
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		FatalError("failed to create %s: %v", beadsDir, err)
	}

	if database == "" {
		database = configfile.DefaultDoltDatabase // "beads"
	}

	cfg := &mysql.Config{
		Host:            serverHost,
		Port:            serverPort,
		User:            serverUser,
		Database:        database,
		BeadsDir:        beadsDir,
		CreateIfMissing: true,
	}

	store, err := mysql.Bootstrap(ctx, cfg)
	if err != nil {
		FatalError("mysql bootstrap: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed issue prefix if missing — same precedence as the dolt path.
	if prefix != "" {
		existing, _ := store.GetConfig(ctx, "issue_prefix")
		if existing == "" {
			if err := store.SetConfig(ctx, "issue_prefix", prefix); err != nil {
				FatalError("failed to set issue_prefix: %v", err)
			}
		}
	}

	// Persist metadata.json with backend=mysql so subsequent invocations
	// (factory + dolt-cmd short-circuit + doctor skip) read the right path.
	metadata := &configfile.Config{
		Database: database,
		Backend:  configfile.BackendMySQL,
	}
	if err := metadata.Save(beadsDir); err != nil {
		FatalError("failed to write metadata.json: %v", err)
	}

	if !quiet {
		fmt.Fprintf(os.Stdout, "%s Initialized beads with MySQL backend\n", ui.RenderPass("✓"))
		fmt.Fprintf(os.Stdout, "  Database: %s\n", ui.RenderAccent(database))
		if serverHost != "" {
			fmt.Fprintf(os.Stdout, "  Host:     %s\n", ui.RenderAccent(serverHost))
		}
		if serverPort != 0 {
			fmt.Fprintf(os.Stdout, "  Port:     %s\n", ui.RenderAccent(fmt.Sprintf("%d", serverPort)))
		}
		fmt.Fprintf(os.Stdout, "  Beads dir: %s\n", ui.RenderAccent(beadsDir))
	}
}
