package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"errors"
	"path/filepath"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/fetcher"
	"github.com/canonical/ubuntu-manpages-operator/internal/launchpad"
	"github.com/canonical/ubuntu-manpages-operator/internal/logging"
	"github.com/canonical/ubuntu-manpages-operator/internal/pipeline"
	"github.com/canonical/ubuntu-manpages-operator/internal/storage"
)

func main() {
	cfg := config.Load()
	logger := logging.BuildLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	if err := ingest(logger, cfg); err != nil {
		logger.Error("ingest failed", "error", err)
		os.Exit(1)
	}
}

func ingest(logger *slog.Logger, cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	lp := launchpad.NewHTTPClient(nil)
	versions, err := lp.ReleaseMap(cfg.Releases)
	if err != nil {
		return fmt.Errorf("resolve release versions: %w", err)
	}
	cfg.ReleaseVersions = versions

	workDir, err := os.MkdirTemp("", "manpages-ingest-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()
	logger.Info("using work directory", "path", workDir)

	pkgFetcher := fetcher.New(
		cfg.Archive,
		cfg.Repos,
		[]string{cfg.Arch},
		nil,
		workDir,
	)
	pkgFetcher.Logger = logger
	converter := pipeline.NewConverter("")
	extractor := pipeline.NewDebExtractor(workDir)
	storage := storage.NewFSStorage(cfg.PublicHTMLDir)

	runner := &pipeline.Runner{
		Fetcher:      pkgFetcher,
		Extractor:    extractor,
		Converter:    converter,
		Storage:      storage,
		Logger:       logger,
		FailuresDir:  cfg.PublicHTMLDir,
		ForceProcess: cfg.Force,
		StoragePath:  filepath.Join(cfg.PublicHTMLDir, "manpages"),
	}

	ctx := context.Background()
	if err := runner.Run(ctx, cfg.ReleaseKeys()); err != nil {
		if errors.Is(err, pipeline.ErrDiskFull) {
			logger.Error("skipping reindex and sitemap generation due to low disk space")
		}
		return err
	}
	notifyReindex(logger, cfg.AdminAddr)
	notifyRegenerateSitemaps(logger, cfg.AdminAddr)
	return nil
}

func notifyReindex(logger *slog.Logger, adminAddr string) {
	notifyAdmin(logger, adminAddr, "/_/reindex", "reindex")
}

func notifyRegenerateSitemaps(logger *slog.Logger, adminAddr string) {
	notifyAdmin(logger, adminAddr, "/_/regenerate-sitemaps", "sitemap regeneration")
}

func notifyAdmin(logger *slog.Logger, adminAddr, path, action string) {
	url := "http://" + adminAddr + path
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "", nil)
	if err != nil {
		logger.Warn("failed to notify server of "+action, "error", err)
		return
	}
	_ = resp.Body.Close()
	logger.Info("server notified of "+action, "status", resp.StatusCode)
}
