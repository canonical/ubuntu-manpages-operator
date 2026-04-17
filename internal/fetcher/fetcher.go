package fetcher

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	debversion "pault.ag/go/debian/version"
)

type Package struct {
	Name     string
	Version  string
	Filename string
	SHA1     string
}

type Fetcher struct {
	Archive       string
	Repos         []string
	Archs         []string
	Pockets       []string
	WorkDir       string
	Client        *http.Client
	Logger        *slog.Logger
	MaxConcurrent int

	// state holds concurrency control shared across shallow copies of the
	// Fetcher (pipeline.go copies the struct to customise WorkDir per release;
	// copies retain the same pointer so the concurrency budget is global).
	state *fetcherState
}

type fetcherState struct {
	sem chan struct{}
}

const defaultMaxConcurrent = 8

// stateInit guards lazy initialisation of Fetcher.state for Fetchers that were
// constructed as a struct literal instead of via New.
var stateInit sync.Mutex

func New(archive string, repos []string, archs []string, pockets []string, workDir string) *Fetcher {
	f := &Fetcher{
		Archive: archive,
		Repos:   repos,
		Archs:   archs,
		Pockets: pockets,
		WorkDir: workDir,
		Client:  &http.Client{Timeout: 5 * time.Minute},
	}
	f.ensureState()
	return f
}

func (f *Fetcher) ensureState() {
	stateInit.Lock()
	defer stateInit.Unlock()
	if f.state != nil {
		return
	}
	n := f.MaxConcurrent
	if n <= 0 {
		n = defaultMaxConcurrent
	}
	f.state = &fetcherState{sem: make(chan struct{}, n)}
}

func (f *Fetcher) acquire(ctx context.Context) error {
	select {
	case f.state.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Fetcher) release() {
	<-f.state.sem
}

// doWithRetry runs fn up to 3 times with linear backoff, returning the last
// error. Context cancellation aborts immediately.
func (f *Fetcher) doWithRetry(ctx context.Context, label, url string, fn func() error) error {
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			if f.Logger != nil {
				f.Logger.Warn("retrying "+label, "url", url, "attempt", attempt+1, "error", lastErr)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

func (f *Fetcher) FetchPackages(ctx context.Context, release string) ([]Package, error) {
	if len(f.Repos) == 0 || len(f.Archs) == 0 {
		return nil, errors.New("fetcher requires repos and archs")
	}
	if len(f.Pockets) == 0 {
		f.Pockets = []string{"-updates", "-security", ""}
	}
	f.ensureState()

	type fetchResult struct {
		candidates []Package
		dist       string
		repo       string
		arch       string
		err        error
	}

	// Build work items preserving pocket priority order.
	type workItem struct {
		index int
		dist  string
		repo  string
		arch  string
	}
	var items []workItem
	for _, pocket := range f.Pockets {
		dist := release + pocket
		for _, repo := range f.Repos {
			for _, arch := range f.Archs {
				items = append(items, workItem{len(items), dist, repo, arch})
			}
		}
	}

	results := make([]fetchResult, len(items))
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		go func(it workItem) {
			defer wg.Done()
			if err := f.acquire(ctx); err != nil {
				results[it.index] = fetchResult{err: fmt.Errorf("open packages %s %s %s: %w", it.dist, it.repo, it.arch, err)}
				return
			}
			defer f.release()
			if f.Logger != nil {
				f.Logger.Info("fetching packages", "dist", it.dist, "repo", it.repo, "arch", it.arch)
			}
			var candidates []Package
			err := f.doWithRetry(ctx, "packages fetch", f.packagesURL(it.dist, it.repo, it.arch), func() error {
				reader, err := f.openPackages(ctx, it.dist, it.repo, it.arch)
				if err != nil {
					return err
				}
				candidates, err = parsePackages(reader)
				_ = reader.Close()
				return err
			})
			if err != nil {
				results[it.index] = fetchResult{err: fmt.Errorf("open packages %s %s %s: %w", it.dist, it.repo, it.arch, err)}
				return
			}
			if f.Logger != nil {
				f.Logger.Info("parsed packages", "count", len(candidates), "dist", it.dist, "repo", it.repo, "arch", it.arch)
			}
			results[it.index] = fetchResult{candidates: candidates, dist: it.dist, repo: it.repo, arch: it.arch}
		}(item)
	}
	wg.Wait()

	// Merge in pocket-priority order.
	packages := make(map[string]Package)
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for _, candidate := range r.candidates {
			current, ok := packages[candidate.Name]
			if !ok || versionGreater(candidate.Version, current.Version) {
				packages[candidate.Name] = candidate
			}
		}
	}

	return mapToSlice(packages), nil
}

// FetchDeb downloads a .deb package and returns the path to the downloaded file.
func (f *Fetcher) FetchDeb(ctx context.Context, debURL string) (string, error) {
	if f.WorkDir == "" {
		f.WorkDir = os.TempDir()
	}

	src := strings.TrimSuffix(f.Archive, "/") + "/" + strings.TrimPrefix(debURL, "/")
	fileName := filepath.Base(debURL)
	destPath := filepath.Join(f.WorkDir, fileName)

	if f.Logger != nil {
		f.Logger.Debug("downloading deb", "url", src)
	}

	if err := os.MkdirAll(f.WorkDir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	err := f.doWithRetry(ctx, "download", src, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}

		resp, err := f.Client.Do(req)
		if err != nil {
			return fmt.Errorf("download deb: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("download deb: status %s", resp.Status)
		}

		tmp, err := os.CreateTemp(f.WorkDir, ".deb-*")
		if err != nil {
			return fmt.Errorf("create temp deb file: %w", err)
		}
		tmpPath := tmp.Name()

		const maxDebSize = 1024 * 1024 * 1024 // 1024 MB
		if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDebSize)); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("write deb file: %w", err)
		}
		_ = tmp.Close()

		if err := os.Rename(tmpPath, destPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("rename deb file: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return destPath, nil
}

func (f *Fetcher) packagesURL(dist, repo, arch string) string {
	return strings.TrimSuffix(f.Archive, "/") + "/dists/" + dist + "/" + repo + "/binary-" + arch + "/Packages.gz"
}

func (f *Fetcher) openPackages(ctx context.Context, dist string, repo string, arch string) (io.ReadCloser, error) {
	url := f.packagesURL(dist, repo, arch)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build packages request: %w", err)
	}

	resp, err := f.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download packages: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download packages: status %s", resp.Status)
	}

	return wrapGzipReader(resp.Body)
}

func wrapGzipReader(r io.ReadCloser) (io.ReadCloser, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	return &gzipReadCloser{ReadCloser: r, Reader: gz}, nil
}

type gzipReadCloser struct {
	io.ReadCloser
	Reader *gzip.Reader
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.Reader.Read(p)
}

func (g *gzipReadCloser) Close() error {
	_ = g.Reader.Close()
	return g.ReadCloser.Close()
}

func parsePackages(reader io.Reader) ([]Package, error) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	fields := map[string]string{}
	var results []Package

	flush := func() {
		if fields["Package"] == "" || fields["Filename"] == "" || fields["SHA1"] == "" {
			fields = map[string]string{}
			return
		}
		results = append(results, Package{
			Name:     fields["Package"],
			Version:  fields["Version"],
			Filename: fields["Filename"],
			SHA1:     fields["SHA1"],
		})
		fields = map[string]string{}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		switch key {
		case "Package", "Version", "Filename", "SHA1":
			fields[key] = value
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan packages: %w", err)
	}

	return results, nil
}

func mapToSlice(pkgs map[string]Package) []Package {
	results := make([]Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		results = append(results, pkg)
	}
	return results
}

func versionGreater(left string, right string) bool {
	if right == "" {
		return true
	}
	l, err := debversion.Parse(left)
	if err != nil {
		return false
	}
	r, err := debversion.Parse(right)
	if err != nil {
		return false
	}
	return debversion.Compare(l, r) > 0
}
