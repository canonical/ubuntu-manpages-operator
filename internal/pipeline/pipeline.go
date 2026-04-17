package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/canonical/ubuntu-manpages-operator/internal/fetcher"
	"github.com/canonical/ubuntu-manpages-operator/internal/sitemap"
	"github.com/canonical/ubuntu-manpages-operator/internal/storage"
	"github.com/canonical/ubuntu-manpages-operator/internal/transform"
)

type Runner struct {
	Fetcher          *fetcher.Fetcher
	Extractor        *DebExtractor
	Converter        *Converter
	Storage          *storage.FSStorage
	SitemapGenerator *sitemap.SitemapGenerator
	Logger           *slog.Logger
	FailuresDir      string
	ForceProcess     bool
	StoragePath      string // checked for available disk space per package

	mu              sync.Mutex
	statuses        []ReleaseStatus
	releaseFailures [][]string
}

func (r *Runner) Run(ctx context.Context, releases []string) error {
	if r.Fetcher == nil || r.Extractor == nil || r.Converter == nil || r.Storage == nil {
		return errors.New("pipeline runner missing dependencies")
	}

	r.statuses = make([]ReleaseStatus, len(releases))
	r.releaseFailures = make([][]string, len(releases))
	for i, rel := range releases {
		r.statuses[i] = ReleaseStatus{Release: rel, Stage: "waiting"}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for i, release := range releases {
		wg.Add(1)
		go func(idx int, rel string) {
			defer wg.Done()
			if err := r.runRelease(ctx, idx, rel); err != nil {
				errOnce.Do(func() { firstErr = err })
				if errors.Is(err, ErrDiskFull) {
					cancel()
				}
				r.mu.Lock()
				r.statuses[idx].Stage = "error"
				r.mu.Unlock()
			}
		}(i, release)
	}

	wg.Wait()

	if r.SitemapGenerator != nil {
		if err := r.SitemapGenerator.Generate(ctx, releases); err != nil {
			r.Logger.Error("sitemap generation failed", "error", err)
			// Non-fatal: don't fail the entire ingest for a sitemap error.
		}
	}

	var totalFailures int
	for _, failures := range r.releaseFailures {
		totalFailures += len(failures)
	}
	if totalFailures > 0 && r.Logger != nil {
		r.Logger.Warn("ingest completed with failures", "count", totalFailures)
	}

	return firstErr
}

func (r *Runner) runRelease(ctx context.Context, idx int, release string) error {
	// Create a per-release work subdirectory for downloads and extraction.
	releaseDir := filepath.Join(r.Fetcher.WorkDir, release)
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return fmt.Errorf("create release work dir: %w", err)
	}
	relFetcher := *r.Fetcher
	relFetcher.WorkDir = releaseDir
	extractor := NewDebExtractor(releaseDir)

	r.mu.Lock()
	if r.FailuresDir != "" {
		r.statuses[idx].FailuresPath = filepath.Join(r.FailuresDir, release+"-failures.log")
	}
	failPath := r.statuses[idx].FailuresPath
	r.mu.Unlock()

	// Create the failure log up front so users can tail it during processing.
	if failPath != "" {
		_ = os.MkdirAll(filepath.Dir(failPath), 0o755)
		_ = os.WriteFile(failPath, nil, 0o644)
	}

	if r.Logger != nil {
		r.Logger.Info("fetching package list", "release", release)
	}

	packages, err := relFetcher.FetchPackages(ctx, release)
	if err != nil {
		return fmt.Errorf("fetch packages for %s: %w", release, err)
	}

	r.mu.Lock()
	r.statuses[idx].Stage = "processing"
	r.statuses[idx].Total = len(packages)
	r.mu.Unlock()

	for i, pkg := range packages {
		if ctx.Err() != nil {
			r.Logger.Error("release cancelled", "release", release, "remaining", len(packages)-i)
			return ctx.Err()
		}
		if r.StoragePath != "" {
			if ok, reason := CheckDiskSpace(r.StoragePath); !ok {
				r.Logger.Error("disk full, stopping ingest", "release", release, "remaining", len(packages)-i, "reason", reason)
				return fmt.Errorf("ingest %s: %s: %w", release, reason, ErrDiskFull)
			}
		}
		if err := r.processPackage(ctx, idx, release, pkg, &relFetcher, extractor); err != nil {
			r.recordFailure(idx, "package", pkg.Name, err)
		}
		r.mu.Lock()
		r.statuses[idx].Done++
		r.mu.Unlock()
	}

	r.mu.Lock()
	s := r.statuses[idx]
	r.mu.Unlock()
	if r.Logger != nil {
		r.Logger.Info("release done", "release", release, "total", s.Total, "skipped", s.Skipped, "errors", s.Errors)
	}
	return nil
}

func (r *Runner) processPackage(ctx context.Context, idx int, release string, pkg fetcher.Package, f *fetcher.Fetcher, extractor *DebExtractor) error {
	if r.Logger != nil {
		r.Logger.Info("processing package", "release", release, "package", pkg.Name)
	}

	if !r.ForceProcess && pkg.Name != "" && pkg.SHA1 != "" {
		if r.Storage.CheckCache(release, pkg.Name, pkg.SHA1) {
			if r.Logger != nil {
				r.Logger.Debug("skipping unchanged package", "release", release, "package", pkg.Name)
			}
			r.mu.Lock()
			r.statuses[idx].Skipped++
			r.mu.Unlock()
			return nil
		}
	}

	debPath, err := f.FetchDeb(ctx, pkg.Filename)
	if err != nil {
		return fmt.Errorf("fetch deb %s: %w", pkg.Filename, err)
	}
	defer func() { _ = os.Remove(debPath) }()

	manpages, cleanup, err := extractor.ExtractManpages(ctx, debPath)
	if err != nil {
		return fmt.Errorf("extract manpages for %s: %w", pkg.Filename, err)
	}
	defer func() { _ = cleanup() }()

	for _, manpage := range manpages {
		if err := r.processManpage(ctx, idx, release, manpage); err != nil {
			return err
		}
	}

	if pkg.Name != "" && pkg.SHA1 != "" {
		if err := r.Storage.WriteCache(ctx, release, pkg.Name, pkg.SHA1); err != nil {
			return fmt.Errorf("write cache for %s: %w", pkg.Name, err)
		}
	}

	return nil
}

func (r *Runner) processManpage(ctx context.Context, idx int, release string, manpage ManpageFile) error {
	if r.Logger != nil {
		r.Logger.Debug("processing", "path", manpage.RelativePath, "symlink", manpage.IsSymlink)
	}
	err := ProcessSingleManpage(ctx, release, manpage, r.Converter, r.Storage)
	if err != nil {
		var ce *ConvertError
		if errors.As(err, &ce) {
			r.recordFailure(idx, "convert", manpage.Path, ce.Unwrap())
			return nil
		}
		return err
	}
	return nil
}

// ProcessSingleManpage converts and stores a single manpage file using
// the provided pipeline components. Conversion failures are returned as
// *ConvertError so callers can decide whether they are fatal.
func ProcessSingleManpage(ctx context.Context, release string, manpage ManpageFile, converter *Converter, storage *storage.FSStorage) error {
	paths, err := ParseManpagePath(release, manpage.RelativePath)
	if err != nil {
		return fmt.Errorf("parse manpage path %s: %w", manpage.RelativePath, err)
	}

	if manpage.IsSymlink {
		target := ConvertSymlinkTarget(manpage.SymlinkTarget)
		if err := storage.WriteSymlink(ctx, paths.HTMLPath, target); err != nil {
			return fmt.Errorf("write html symlink: %w", err)
		}
		if err := storage.WriteGzipSymlink(ctx, paths.GzipPath, manpage.SymlinkTarget); err != nil {
			return fmt.Errorf("write gzip symlink: %w", err)
		}
		return nil
	}

	if target, ok, err := DetectSoLink(manpage.Path); err != nil {
		return err
	} else if ok {
		soTarget := ConvertSoTarget(target)
		if err := storage.WriteSymlink(ctx, paths.HTMLPath, soTarget); err != nil {
			return fmt.Errorf("write html symlink: %w", err)
		}
		return nil
	}

	rawHTML, err := converter.ConvertManpage(ctx, manpage.Path)
	if err != nil {
		return &ConvertError{Err: fmt.Errorf("convert %s: %w", manpage.Path, err)}
	}

	manpage.Meta.Filename = filepath.Base(manpage.RelativePath)
	tdoc, err := transform.Pipeline(release, rawHTML, &manpage.Meta)
	if err != nil {
		return fmt.Errorf("transform %s: %w", manpage.Path, err)
	}

	if err := storage.WriteHTML(ctx, paths.HTMLPath, tdoc.Body); err != nil {
		return fmt.Errorf("write html %s: %w", paths.HTMLPath, err)
	}

	content, err := os.ReadFile(manpage.Path)
	if err != nil {
		return fmt.Errorf("read manpage gzip: %w", err)
	}

	if err := storage.WriteGzip(ctx, paths.GzipPath, content); err != nil {
		return fmt.Errorf("write gzip %s: %w", paths.GzipPath, err)
	}

	return nil
}

func (r *Runner) recordFailure(idx int, stage string, path string, err error) {
	message := strings.TrimSpace(fmt.Sprintf("%s %s: %v", stage, path, err))
	r.mu.Lock()
	r.releaseFailures[idx] = append(r.releaseFailures[idx], message)
	r.statuses[idx].Errors++
	failPath := r.statuses[idx].FailuresPath
	r.mu.Unlock()

	// Append to the failure log immediately so users can tail it.
	if failPath != "" {
		f, ferr := os.OpenFile(failPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr == nil {
			_, _ = fmt.Fprintln(f, message)
			_ = f.Close()
		}
	}

	if r.Logger != nil {
		r.Logger.Warn("pipeline failure", "stage", stage, "path", path, "error", err)
	}
}
