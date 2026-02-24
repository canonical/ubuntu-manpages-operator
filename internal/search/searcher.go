package search

import (
	"context"
	"strings"
)

// Searcher abstracts search so the web server does not depend on a specific
// search implementation.
type Searcher interface {
	Search(ctx context.Context, query, distro, language string, limit, offset int) (SearchResponse, error)
	Close() error
}

// Result represents a single search result.
type Result struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	Distro  string `json:"distro"`
	Section int    `json:"section"`
}

// SearchResponse is the paginated response returned by a Searcher.
type SearchResponse struct {
	Total   uint64   `json:"total"`
	Results []Result `json:"results"`
}

// cleanQuery strips characters that are not useful for filename matching and
// returns the remaining terms joined by spaces. It is intentionally lenient
// so that searches like "apt-file" or "SSL_connect" work.
func cleanQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range q {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '+':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.TrimSpace(b.String())
}
