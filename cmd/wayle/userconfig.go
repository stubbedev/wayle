package main

import (
	"fmt"

	"github.com/stubbedev/wayle/config"
)

// loadUserConfig is the CLI's one-shot ConfigService::load: the user's
// config directory with its config and runtime layers, diagnostics on
// stderr, no watcher.
func loadUserConfig() (*config.Service, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return config.Load(dir, config.StderrDiagnostics), nil
}
