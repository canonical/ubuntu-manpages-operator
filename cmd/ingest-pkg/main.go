package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/fetcher"
	"github.com/canonical/ubuntu-manpages-operator/internal/launchpad"
	"github.com/canonical/ubuntu-manpages-operator/internal/logging"
	"github.com/canonical/ubuntu-manpages-operator/internal/pipeline"
	"github.com/canonical/ubuntu-manpages-operator/internal/storage"
)

func main() {
	release := flag.String("release", "", "Release to ingest from (required)")
	pkgName := flag.String("package", "", "Package name to process (required)")
	flag.Parse()

	cfg := config.Load()
	logger := logging.BuildLogger(cfg.LogLevel)

	if *release == "" || *pkgName == "" {
		fmt.Fprintf(os.Stderr, "Usage: ingest-pkg -release <release> -package <name>\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if err := run(logger, cfg, *release, *pkgName); err != nil {
		logger.Error("ingest-pkg failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, cfg *config.Config, release, pkgName string) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	lp := launchpad.NewHTTPClient(nil)
	versions, err := lp.ReleaseMap(cfg.Releases)
	if err != nil {
		return fmt.Errorf("resolve release versions: %w", err)
	}
	cfg.ReleaseVersions = versions

	if !slices.Contains(cfg.Releases, release) {
		return fmt.Errorf("unknown release %q (available: %v)", release, cfg.ReleaseKeys())
	}

	workDir, err := os.MkdirTemp("", "manpages-ingest-pkg-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()
	logger.Info("using work directory", "path", workDir)

	pkgFetcher := fetcher.New(
		cfg.Archive, cfg.Repos, []string{cfg.Arch}, nil, workDir,
	)
	pkgFetcher.Logger = logger
	converter := pipeline.NewConverter("")
	extractor := pipeline.NewDebExtractor(workDir)
	storage := storage.NewFSStorage(cfg.PublicHTMLDir)

	ctx := context.Background()

	// Fetch the package list and find the named package.
	logger.Info("fetching package list", "release", release)
	packages, err := pkgFetcher.FetchPackages(ctx, release)
	if err != nil {
		return fmt.Errorf("fetch packages: %w", err)
	}

	var pkg *fetcher.Package
	for i := range packages {
		if packages[i].Name == pkgName {
			pkg = &packages[i]
			break
		}
	}
	if pkg == nil {
		return fmt.Errorf("package %q not found in release %q (%d packages searched)", pkgName, release, len(packages))
	}
	logger.Info("found package", "name", pkg.Name, "version", pkg.Version, "filename", pkg.Filename)

	// Download the .deb.
	debPath, err := pkgFetcher.FetchDeb(ctx, pkg.Filename)
	if err != nil {
		return fmt.Errorf("fetch deb: %w", err)
	}
	logger.Info("deb ready", "path", debPath)

	// Extract manpages from the .deb.
	manpages, cleanup, err := extractor.ExtractManpages(ctx, debPath)
	if err != nil {
		return fmt.Errorf("extract manpages: %w", err)
	}
	defer func() { _ = cleanup() }()
	logger.Info("extracted manpages", "count", len(manpages))

	// Process each manpage through the conversion pipeline.
	var convertErrors int
	for _, manpage := range manpages {
		logger.Debug("processing", "path", manpage.RelativePath, "symlink", manpage.IsSymlink)
		if err := pipeline.ProcessSingleManpage(ctx, release, manpage, converter, storage); err != nil {
			var ce *pipeline.ConvertError
			if errors.As(err, &ce) {
				logger.Warn("convert failed", "path", manpage.RelativePath, "error", ce.Unwrap())
				convertErrors++
				continue
			}
			return fmt.Errorf("process manpage %s: %w", manpage.RelativePath, err)
		}
	}

	// Write cache so subsequent full ingests see this package as processed.
	if pkg.SHA1 != "" {
		if err := storage.WriteCache(ctx, release, pkg.Name, pkg.SHA1); err != nil {
			return fmt.Errorf("write cache: %w", err)
		}
	}

	_ = os.Remove(debPath)

	logger.Info("done",
		"package", pkg.Name,
		"release", release,
		"manpages", len(manpages),
		"convert_errors", convertErrors,
		"output", cfg.PublicHTMLDir,
	)
	return nil
}
