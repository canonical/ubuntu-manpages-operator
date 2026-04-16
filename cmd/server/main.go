package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/launchpad"
	"github.com/canonical/ubuntu-manpages-operator/internal/logging"
	"github.com/canonical/ubuntu-manpages-operator/internal/web"
)

func main() {
	cfg := config.Load()
	logger := logging.BuildLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	lp := launchpad.NewHTTPClient(nil)
	versions, err := lp.ReleaseMap(cfg.Releases)
	if err != nil {
		logger.Error("resolve release versions", "error", err)
		os.Exit(1)
	}
	cfg.ReleaseVersions = versions

	server := web.NewServer(cfg, logger)
	go server.RegenerateSitemaps()
	if err := server.ListenAndServe(cfg.Addr, cfg.AdminAddr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
