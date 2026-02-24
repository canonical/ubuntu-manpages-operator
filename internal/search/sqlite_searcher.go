package search

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Result struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	Distro  string `json:"distro"`
	Section int    `json:"section"`
}

type SearchResponse struct {
	Total   uint64   `json:"total"`
	Results []Result `json:"results"`
}

type SQLiteSearcher struct {
	db *sql.DB
}

func NewSQLiteSearcher(path string) (*SQLiteSearcher, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	return &SQLiteSearcher{db: db}, nil
}

func (s *SQLiteSearcher) Close() error {
	return s.db.Close()
}

func (s *SQLiteSearcher) Search(ctx context.Context, queryString string, distro string, language string, limit int, offset int) (SearchResponse, error) {
	queryString = sanitizeQuery(queryString)
	if queryString == "" {
		return SearchResponse{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `SELECT m.title, m.path, m.distro, m.section, COUNT(*) OVER() AS total
		 FROM manpages_fts f
		 JOIN manpages m ON m.rowid = f.rowid
		 WHERE manpages_fts MATCH ?
		   AND m.language = ?`
	args := []any{queryString, language}

	if distro != "" {
		query += ` AND m.distro = ?`
		args = append(args, distro)
	}

	query += ` ORDER BY f.rank LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("search query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var resp SearchResponse
	resp.Results = make([]Result, 0)

	for rows.Next() {
		var r Result
		var total uint64
		if err := rows.Scan(&r.Title, &r.Path, &r.Distro, &r.Section, &total); err != nil {
			return SearchResponse{}, fmt.Errorf("scan result: %w", err)
		}
		resp.Total = total
		resp.Results = append(resp.Results, r)
	}
	if err := rows.Err(); err != nil {
		return SearchResponse{}, fmt.Errorf("iterate results: %w", err)
	}

	return resp, nil
}

func sanitizeQuery(q string) string {
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
			r == ' ', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	q = strings.TrimSpace(b.String())
	if q == "" {
		return ""
	}

	terms := strings.Fields(q)
	for i, t := range terms {
		upper := strings.ToUpper(t)
		if upper == "AND" || upper == "OR" || upper == "NOT" {
			terms[i] = ""
			continue
		}
		terms[i] = `"` + t + `"` + "*"
	}

	var filtered []string
	for _, t := range terms {
		if t != "" {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, " ")
}
