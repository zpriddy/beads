package main

import (
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
)

// isMySQLBackend returns true when the active project's metadata.json
// declares backend=mysql. It is a cheap, side-effect-free check used to
// short-circuit dolt-only post-write hooks (auto-commit, auto-export,
// tip-commit) when running against the mysql backend.
//
// Returns false when no .beads dir is found or when metadata.json is
// absent / unreadable — callers should treat that as the dolt default.
func isMySQLBackend() bool {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return false
	}
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return false
	}
	return cfg.GetBackend() == configfile.BackendMySQL
}
