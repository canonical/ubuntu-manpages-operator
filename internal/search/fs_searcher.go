package search

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FSSearcher searches for manpages by scanning the filesystem. Because the
// HTML files are stored with predictable paths, the directory tree acts as
// the search index — no separate database is required.
type FSSearcher struct {
	root     string   // PublicHTMLDir
	releases []string // configured release codenames
}

// NewFSSearcher creates a new filesystem-based searcher rooted at the given
// public HTML directory.
func NewFSSearcher(root string, releases []string) *FSSearcher {
	return &FSSearcher{root: root, releases: releases}
}

// Close is a no-op for the filesystem searcher.
func (s *FSSearcher) Close() error { return nil }

// Search finds manpages whose command name matches the query. Exact matches
// (case-insensitive) are returned first, followed by prefix matches. Results
// are enriched with title and description read from each file's META header.
func (s *FSSearcher) Search(ctx context.Context, query, distro, language string, limit, offset int) (SearchResponse, error) {
	name := cleanQuery(query)
	if name == "" {
		return SearchResponse{Results: []Result{}}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	releases := s.releases
	if distro != "" {
		releases = []string{distro}
	}

	nameLower := strings.ToLower(name)

	var exact, prefix []Result

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

				cmdName := commandName(e.Name())
				cmdLower := strings.ToLower(cmdName)

				var match bool
				var isExact bool

				if cmdLower == nameLower {
					match = true
					isExact = true
				} else if strings.HasPrefix(cmdLower, nameLower) {
					match = true
				}

				if !match {
					continue
				}

				r := Result{
					Path:    urlPath(rel, language, section, e.Name()),
					Distro:  rel,
					Section: section,
				}

				if isExact {
					exact = append(exact, r)
				} else {
					prefix = append(prefix, r)
				}
			}
		}
	}

	// Sort exact matches by section, then prefix matches alphabetically.
	sort.Slice(exact, func(i, j int) bool {
		if exact[i].Section != exact[j].Section {
			return exact[i].Section < exact[j].Section
		}
		return exact[i].Path < exact[j].Path
	})
	sort.Slice(prefix, func(i, j int) bool {
		return prefix[i].Path < prefix[j].Path
	})

	results := append(exact, prefix...)
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
