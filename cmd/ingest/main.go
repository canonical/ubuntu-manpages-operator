package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/fetcher"
	"github.com/canonical/ubuntu-manpages-operator/internal/launchpad"
	"github.com/canonical/ubuntu-manpages-operator/internal/logging"
	"github.com/canonical/ubuntu-manpages-operator/internal/pipeline"
	"github.com/canonical/ubuntu-manpages-operator/internal/search"
	"github.com/canonical/ubuntu-manpages-operator/internal/sitemap"
	"github.com/canonical/ubuntu-manpages-operator/internal/storage"
)

func main() {
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	release := flag.String("release", "", "Comma-separated list of releases to ingest")
	workdir := flag.String("workdir", "", "Working directory for downloads/extraction")
	force := flag.Bool("force", false, "Force reprocessing of all packages (ignore processing cache)")
	output := flag.String("output", "", "Override public HTML output directory")
	flag.Parse()

	logger := logging.BuildLogger(*logLevel)

	if err := ingest(logger, *release, *workdir, *force, *output); err != nil {
		logger.Error("ingest failed", "error", err)
		os.Exit(1)
	}
}

func ingest(logger *slog.Logger, releaseList, workDir string, forceProcess bool, output string) error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	lp := launchpad.NewHTTPClient(nil)
	versions, err := lp.ReleaseMap(cfg.Releases)
	if err != nil {
		return fmt.Errorf("resolve release versions: %w", err)
	}
	cfg.ReleaseVersions = versions

	if output != "" {
		cfg.PublicHTMLDir = output
	}

	releases, err := resolveReleases(cfg, releaseList)
	if err != nil {
		return fmt.Errorf("invalid release list: %w", err)
	}

	if workDir == "" {
		workDir, err = os.MkdirTemp("", "manpages-ingest-")
		if err != nil {
			return fmt.Errorf("create work dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(workDir) }()
	}
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
	indexer, err := search.NewSQLiteIndexer(cfg.IndexPath())
	if err != nil {
		return err
	}

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
		Indexer:          indexer,
		SitemapGenerator: sitemapGen,
		Logger:           logger,
		FailuresDir:      cfg.PublicHTMLDir,
		ForceProcess:     forceProcess,
	}

	ctx := context.Background()
	return runner.Run(ctx, releases)
}

var errInvalidRelease = errors.New("invalid release")

func resolveReleases(cfg *config.Config, releaseList string) ([]string, error) {
	if strings.TrimSpace(releaseList) == "" {
		return cfg.ReleaseKeys(), nil
	}

	releases := strings.Split(releaseList, ",")
	for i := range releases {
		releases[i] = strings.TrimSpace(releases[i])
	}

	for _, release := range releases {
		if release == "" {
			return nil, errInvalidRelease
		}
		if !slices.Contains(cfg.Releases, release) {
			return nil, errInvalidRelease
		}
	}

	return releases, nil
}
