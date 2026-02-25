package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// indexEntry is a single manpage stored in the in-memory search index.
type indexEntry struct {
	lower    string // lowercased command name (e.g. "ls")
	filename string // original filename (e.g. "ls.1.html")
	section  int    // man section (1-9)
}

// FSSearcher searches for manpages using an in-memory index of filenames.
// The index is built at construction by scanning the filesystem once, and
// can be refreshed with Rebuild. Searches with a language filter fall back
// to filesystem scanning since translated manpages are rare.
type FSSearcher struct {
	root     string
	releases []string

	mu    sync.RWMutex
	index map[string][]indexEntry // release → entries (default language only)
}

// NewFSSearcher creates a new filesystem-based searcher and eagerly builds
// the in-memory filename index by scanning all configured releases.
func NewFSSearcher(root string, releases []string) *FSSearcher {
	s := &FSSearcher{root: root, releases: releases}
	s.index = s.buildIndex()
	return s
}

// Rebuild rescans the filesystem and replaces the in-memory index. It is
// safe to call concurrently with Search.
func (s *FSSearcher) Rebuild() {
	idx := s.buildIndex()
	s.mu.Lock()
	s.index = idx
	s.mu.Unlock()
}

// buildIndex scans all release directories for HTML manpage files and
// returns the index map.
func (s *FSSearcher) buildIndex() map[string][]indexEntry {
	start := time.Now()

	idx := make(map[string][]indexEntry, len(s.releases))
	for _, rel := range s.releases {
		var entries []indexEntry
		for section := 1; section <= 9; section++ {
			dir := sectionDir(s.root, rel, "", section)
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".html") {
					continue
				}
				entries = append(entries, indexEntry{
					lower:    strings.ToLower(commandName(f.Name())),
					filename: f.Name(),
					section:  section,
				})
			}
		}
		idx[rel] = entries
	}

	var totalEntries int
	for _, entries := range idx {
		totalEntries += len(entries)
	}
	slog.Info("search index built",
		"entries", totalEntries,
		"releases", len(idx),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return idx
}

// Close is a no-op for the filesystem searcher.
func (s *FSSearcher) Close() error { return nil }

// scoredResult wraps a Result with a Damerau-Levenshtein distance used for
// sorting fuzzy matches before the distance is discarded.
type scoredResult struct {
	Result
	distance int
}

// matchBuckets collects results into the four match tiers during a search.
type matchBuckets struct {
	exact, prefix, contains []Result
	fuzzy                   []scoredResult
}

// classify determines the match tier for cmdLower against queryLower and
// appends the result to the appropriate bucket. It handles exact, prefix,
// contains, and fuzzy (including fuzzy prefix) matching.
func (b *matchBuckets) classify(cmdLower, queryLower string, threshold int, r Result) {
	switch {
	case cmdLower == queryLower:
		r.MatchType = MatchExact
		b.exact = append(b.exact, r)
	case strings.HasPrefix(cmdLower, queryLower):
		r.MatchType = MatchPrefix
		b.prefix = append(b.prefix, r)
	case strings.Contains(cmdLower, queryLower):
		r.MatchType = MatchContains
		b.contains = append(b.contains, r)
	default:
		if threshold == 0 {
			return
		}
		dist := damerauLevenshteinBounded(cmdLower, queryLower, threshold)
		// If the full-string distance is too large but the command is
		// longer than the query, try a fuzzy prefix match: compare the
		// query against prefixes of the command name around the query
		// length (±threshold). Require at least 3 chars to avoid noise.
		if dist > threshold && len(queryLower) >= 3 && len(cmdLower) > len(queryLower) {
			lo := len(queryLower) - threshold
			if lo < 1 {
				lo = 1
			}
			hi := len(queryLower) + threshold
			if hi > len(cmdLower) {
				hi = len(cmdLower)
			}
			for pl := lo; pl <= hi; pl++ {
				if d := damerauLevenshteinBounded(cmdLower[:pl], queryLower, threshold); d < dist {
					dist = d
				}
			}
		}
		if dist <= threshold {
			r.MatchType = MatchFuzzy
			b.fuzzy = append(b.fuzzy, scoredResult{Result: r, distance: dist})
		}
	}
}

// Search finds manpages whose command name matches the query. Results are
// returned in four tiers: exact matches (case-insensitive), prefix matches,
// substring (contains) matches, and fuzzy matches (within a Damerau-Levenshtein
// distance threshold). Each result carries a MatchType indicating how it
// matched. The results are enriched with title and description read from each
// file's META header.
//
// When language is empty, Search uses the in-memory index (no filesystem I/O
// during matching). When language is set, it falls back to scanning the
// filesystem directly.
func (s *FSSearcher) Search(ctx context.Context, query, distro, language string, limit, offset int) (SearchResponse, error) {
	name := cleanQuery(query)
	if name == "" {
		return SearchResponse{Results: []Result{}}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	// Language-filtered searches fall back to filesystem scanning because
	// translated manpages are rare and not worth indexing.
	if language != "" {
		return s.searchFilesystem(name, distro, language, limit, offset)
	}

	releases := s.releases
	if distro != "" {
		releases = []string{distro}
	}

	nameLower := strings.ToLower(name)
	threshold := fuzzyThreshold(len(nameLower))

	var buckets matchBuckets

	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()

	for _, rel := range releases {
		entries := idx[rel]
		for i := range entries {
			e := &entries[i]
			r := Result{
				Path:    urlPath(rel, "", e.section, e.filename),
				Distro:  rel,
				Section: e.section,
			}
			buckets.classify(e.lower, nameLower, threshold, r)
		}
	}

	return s.assembleResults(buckets, limit, offset)
}

// searchFilesystem performs a search by scanning the filesystem directly.
// Used for language-filtered queries that are not covered by the in-memory index.
func (s *FSSearcher) searchFilesystem(name, distro, language string, limit, offset int) (SearchResponse, error) {
	releases := s.releases
	if distro != "" {
		releases = []string{distro}
	}

	nameLower := strings.ToLower(name)
	threshold := fuzzyThreshold(len(nameLower))

	var buckets matchBuckets

	for _, rel := range releases {
		for section := 1; section <= 9; section++ {
			dir := sectionDir(s.root, rel, language, section)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}

			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
					continue
				}
				r := Result{
					Path:    urlPath(rel, language, section, e.Name()),
					Distro:  rel,
					Section: section,
				}
				buckets.classify(strings.ToLower(commandName(e.Name())), nameLower, threshold, r)
			}
		}
	}

	return s.assembleResults(buckets, limit, offset)
}

// assembleResults sorts, combines, paginates, and enriches search results.
func (s *FSSearcher) assembleResults(b matchBuckets, limit, offset int) (SearchResponse, error) {
	// Sort each tier.
	sort.Slice(b.exact, func(i, j int) bool {
		if b.exact[i].Section != b.exact[j].Section {
			return b.exact[i].Section < b.exact[j].Section
		}
		return b.exact[i].Path < b.exact[j].Path
	})
	sort.Slice(b.prefix, func(i, j int) bool {
		return b.prefix[i].Path < b.prefix[j].Path
	})
	sort.Slice(b.contains, func(i, j int) bool {
		return b.contains[i].Path < b.contains[j].Path
	})
	sort.Slice(b.fuzzy, func(i, j int) bool {
		if b.fuzzy[i].distance != b.fuzzy[j].distance {
			return b.fuzzy[i].distance < b.fuzzy[j].distance
		}
		return b.fuzzy[i].Path < b.fuzzy[j].Path
	})

	// Cap fuzzy results to avoid noise dominating the result set.
	const maxFuzzy = 10
	if len(b.fuzzy) > maxFuzzy {
		b.fuzzy = b.fuzzy[:maxFuzzy]
	}

	// Combine all tiers: exact → prefix → contains → fuzzy.
	results := make([]Result, 0, len(b.exact)+len(b.prefix)+len(b.contains)+len(b.fuzzy))
	results = append(results, b.exact...)
	results = append(results, b.prefix...)
	results = append(results, b.contains...)
	for _, sr := range b.fuzzy {
		results = append(results, sr.Result)
	}
	total := uint64(len(results))

	// Paginate.
	if offset >= len(results) {
		return SearchResponse{Total: total, Results: []Result{}}, nil
	}
	results = results[offset:]
	if len(results) > limit {
		results = results[:limit]
	}

	// Enrich the paginated results with titles from the META header.
	for i := range results {
		fsPath := filepath.Join(s.root, results[i].Path[1:]) // strip leading /
		title, desc := readMeta(fsPath)
		if title == "" {
			title = commandName(filepath.Base(results[i].Path))
		}
		if desc != "" {
			results[i].Title = title + " - " + desc
		} else {
			results[i].Title = title
		}
	}

	return SearchResponse{Total: total, Results: results}, nil
}

// sectionDir returns the filesystem path to a man section directory.
func sectionDir(root, release, language string, section int) string {
	if language == "" {
		return filepath.Join(root, "manpages", release, fmt.Sprintf("man%d", section))
	}
	return filepath.Join(root, "manpages", release, language, fmt.Sprintf("man%d", section))
}

// urlPath builds the URL path for a manpage result.
func urlPath(release, language string, section int, filename string) string {
	if language == "" {
		return fmt.Sprintf("/manpages/%s/man%d/%s", release, section, filename)
	}
	return fmt.Sprintf("/manpages/%s/%s/man%d/%s", release, language, section, filename)
}

// commandName extracts the command name from an HTML filename.
// For example "ls.1.html" → "ls", "SSL_connect.3ssl.html" → "SSL_connect".
func commandName(filename string) string {
	name := strings.TrimSuffix(filename, ".html")
	// Find the last dot followed by a digit (the section separator).
	if dot := strings.LastIndex(name, "."); dot > 0 {
		suffix := name[dot+1:]
		if len(suffix) > 0 && suffix[0] >= '1' && suffix[0] <= '9' {
			return name[:dot]
		}
	}
	return name
}

const (
	metaPrefix = "<!--META:"
	metaSuffix = "-->"
)

// readMeta reads the <!--META:{...}--> header from a manpage HTML file and
// returns the title and description. It reads at most 4 KB to avoid loading
// the entire file.
func readMeta(path string) (title, description string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	content := string(buf[:n])

	if !strings.HasPrefix(content, metaPrefix) {
		return "", ""
	}
	end := strings.Index(content, metaSuffix)
	if end == -1 {
		return "", ""
	}

	jsonStr := content[len(metaPrefix):end]
	var meta struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		return "", ""
	}
	return meta.Title, meta.Description
}
