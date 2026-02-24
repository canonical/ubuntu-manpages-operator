package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeManpage is a test helper that creates a manpage HTML file with a META header.
func writeManpage(t *testing.T, root, release, lang string, section int, filename, title, desc string) {
	t.Helper()
	var dir string
	if lang == "" {
		dir = filepath.Join(root, "manpages", release, sectionDirName(section))
	} else {
		dir = filepath.Join(root, "manpages", release, lang, sectionDirName(section))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `<!--META:{"title":"` + title + `","description":"` + desc + `"}-->` + "\n<p>content</p>"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sectionDirName(section int) string {
	return "man" + string(rune('0'+section))
}

func TestFSSearcher_ExactMatch(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")
	writeManpage(t, root, "noble", "", 1, "lsblk.1.html", "lsblk", "list block devices")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "ls", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total < 1 {
		t.Fatal("expected at least 1 result")
	}
	// Exact match should be first.
	if resp.Results[0].Title != "ls - list directory contents" {
		t.Errorf("expected first result to be exact match 'ls', got %q", resp.Results[0].Title)
	}
	if resp.Results[0].Section != 1 {
		t.Errorf("expected section 1, got %d", resp.Results[0].Section)
	}
}

func TestFSSearcher_PrefixMatch(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")
	writeManpage(t, root, "noble", "", 1, "lsblk.1.html", "lsblk", "list block devices")
	writeManpage(t, root, "noble", "", 8, "lsof.8.html", "lsof", "list open files")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "ls", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 3 {
		t.Fatalf("expected 3 results, got %d", resp.Total)
	}
	// Exact match first, then prefix matches.
	if resp.Results[0].Title != "ls - list directory contents" {
		t.Errorf("expected exact match first, got %q", resp.Results[0].Title)
	}
}

func TestFSSearcher_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "LS", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 result for case-insensitive match, got %d", resp.Total)
	}
	if resp.Results[0].Title != "ls - list directory contents" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
}

func TestFSSearcher_DistroFilter(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")
	writeManpage(t, root, "jammy", "", 1, "ls.1.html", "ls", "list directory contents")

	s := NewFSSearcher(root, []string{"noble", "jammy"})

	// Without filter: both releases.
	resp, err := s.Search(context.Background(), "ls", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected 2 results without filter, got %d", resp.Total)
	}

	// With filter: only noble.
	resp, err = s.Search(context.Background(), "ls", "noble", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 result with noble filter, got %d", resp.Total)
	}
	if resp.Results[0].Distro != "noble" {
		t.Errorf("expected distro noble, got %q", resp.Results[0].Distro)
	}
}

func TestFSSearcher_LanguageFilter(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")
	writeManpage(t, root, "noble", "de", 1, "ls.1.html", "ls", "Verzeichnisinhalte auflisten")

	s := NewFSSearcher(root, []string{"noble"})

	// Default language (English).
	resp, err := s.Search(context.Background(), "ls", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 English result, got %d", resp.Total)
	}

	// German.
	resp, err = s.Search(context.Background(), "ls", "", "de", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 German result, got %d", resp.Total)
	}
	if resp.Results[0].Title != "ls - Verzeichnisinhalte auflisten" {
		t.Errorf("unexpected German title: %q", resp.Results[0].Title)
	}
}

func TestFSSearcher_Pagination(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "aa.1.html", "aa", "first")
	writeManpage(t, root, "noble", "", 1, "ab.1.html", "ab", "second")
	writeManpage(t, root, "noble", "", 1, "ac.1.html", "ac", "third")

	s := NewFSSearcher(root, []string{"noble"})

	// Limit 2.
	resp, err := s.Search(context.Background(), "a", "", "", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 3 {
		t.Fatalf("expected total=3, got %d", resp.Total)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results on first page, got %d", len(resp.Results))
	}

	// Offset 2.
	resp, err = s.Search(context.Background(), "a", "", "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result on second page, got %d", len(resp.Results))
	}
}

func TestFSSearcher_MetaEnrichment(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "grep.1.html", "grep", "print lines that match patterns")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "grep", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatal("expected 1 result")
	}
	if resp.Results[0].Title != "grep - print lines that match patterns" {
		t.Errorf("expected enriched title, got %q", resp.Results[0].Title)
	}
}

func TestFSSearcher_EmptyQuery(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0 results for empty query, got %d", resp.Total)
	}
}

func TestFSSearcher_NoMatchingFiles(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "nonexistent", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0 results, got %d", resp.Total)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected empty results slice, got %d", len(resp.Results))
	}
}

func TestFSSearcher_SectionSuffix(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 3, "SSL_connect.3ssl.html", "SSL_connect", "initiate TLS/SSL handshake")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "SSL_connect", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 result for section-suffixed file, got %d", resp.Total)
	}
	if resp.Results[0].Section != 3 {
		t.Errorf("expected section 3, got %d", resp.Results[0].Section)
	}
}

func TestFSSearcher_ExactMatchSortedBySection(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 3, "printf.3.html", "printf", "formatted output conversion")
	writeManpage(t, root, "noble", "", 1, "printf.1.html", "printf", "format and print data")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "printf", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected 2 results, got %d", resp.Total)
	}
	// Section 1 should come before section 3.
	if resp.Results[0].Section != 1 {
		t.Errorf("expected section 1 first, got %d", resp.Results[0].Section)
	}
	if resp.Results[1].Section != 3 {
		t.Errorf("expected section 3 second, got %d", resp.Results[1].Section)
	}
}

func TestCommandName(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"ls.1.html", "ls"},
		{"SSL_connect.3ssl.html", "SSL_connect"},
		{"apt-file.1.html", "apt-file"},
		{"printf.3.html", "printf"},
		{"voro++.1.html", "voro++"},
	}
	for _, tc := range tests {
		got := commandName(tc.filename)
		if got != tc.want {
			t.Errorf("commandName(%q) = %q, want %q", tc.filename, got, tc.want)
		}
	}
}

func TestCleanQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ls", "ls"},
		{"  ls  ", "ls"},
		{"apt-file", "apt-file"},
		{"SSL_connect", "SSL_connect"},
		{"foo(bar)", "foo bar"},
		{"", ""},
		{"   ", ""},
		{"voro++", "voro++"},
	}
	for _, tc := range tests {
		got := cleanQuery(tc.input)
		if got != tc.want {
			t.Errorf("cleanQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
