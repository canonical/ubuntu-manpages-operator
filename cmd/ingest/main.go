package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/fetcher"
	"github.com/canonical/ubuntu-manpages-operator/internal/launchpad"
	"github.com/canonical/ubuntu-manpages-operator/internal/logging"
	"github.com/canonical/ubuntu-manpages-operator/internal/pipeline"
	"github.com/canonical/ubuntu-manpages-operator/internal/sitemap"
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

	sitemapGen := &sitemap.SitemapGenerator{
		Root:    cfg.PublicHTMLDir,
		SiteURL: strings.TrimRight(cfg.Site, "/"),
		Logger:  logger,
	}

	runner := &pipeline.Runner{
		Fetcher:          pkgFetcher,
		Extractor:        extractor,
		Converter:        converter,
		Storage:          storage,
		SitemapGenerator: sitemapGen,
		Logger:           logger,
		FailuresDir:      cfg.PublicHTMLDir,
		ForceProcess:     cfg.Force,
	}

	ctx := context.Background()
	if err := runner.Run(ctx, cfg.ReleaseKeys()); err != nil {
		return err
	}
	notifyReindex(logger, cfg.AdminAddr)
	return nil
}

func notifyReindex(logger *slog.Logger, adminAddr string) {
	url := "http://" + adminAddr + "/_/reindex"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "", nil)
	if err != nil {
		logger.Warn("failed to notify server of reindex", "error", err)
		return
	}
	_ = resp.Body.Close()
	logger.Info("server notified of reindex", "status", resp.StatusCode)
}
