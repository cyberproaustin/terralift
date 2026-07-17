// Package core holds the provider-agnostic run framework: run context, config,
// artifact paths, logging (checkpoint/retry land here too as we port them).
package core

import (
	"path/filepath"
	"time"

	"github.com/cyberproaustin/terralift/internal/model"
)

// Run is the per-invocation context threaded through every phase.
type Run struct {
	ID     string
	Cloud  string
	Scope  model.Scope
	Config *Config
	Paths  Paths
	DryRun bool
	Log    *Logger
}

// Config holds run settings (defaults + flag/file overrides). Cloud-specific
// settings live under Provider keyed by cloud name.
type Config struct {
	HCLOnly             bool
	Migration           bool
	ResourceTypeExclude []string
	Provider            map[string]any
}

// DefaultConfig returns baseline settings.
func DefaultConfig() *Config {
	return &Config{
		ResourceTypeExclude: []string{},
		Provider:            map[string]any{},
	}
}

// Paths are the on-disk artifact locations for a run (the resumable contract:
// each phase reads/writes a well-defined artifact and checkpoints).
type Paths struct {
	Root        string
	Manifest    string
	Inventory   string
	Export      string
	Mappings    string
	Repo        string
	Reports     string
	Checkpoints string
	Package     string
	Log         string
}

// NewPaths builds the artifact path set for a run id under artifactRoot.
func NewPaths(artifactRoot, runID string) Paths {
	root := filepath.Join(artifactRoot, runID)
	return Paths{
		Root:        root,
		Manifest:    filepath.Join(root, "manifest.json"),
		Inventory:   filepath.Join(root, "inventory.json"),
		Export:      filepath.Join(root, "export"),
		Mappings:    filepath.Join(root, "mappings"),
		Repo:        filepath.Join(root, "repo"),
		Reports:     filepath.Join(root, "reports"),
		Checkpoints: filepath.Join(root, "checkpoints"),
		Package:     filepath.Join(root, "package"),
		Log:         filepath.Join(root, "run.jsonl"),
	}
}

// NewRunID returns a timestamped, sortable run id.
func NewRunID(now time.Time) string {
	return "run-" + now.UTC().Format("20060102-150405")
}
