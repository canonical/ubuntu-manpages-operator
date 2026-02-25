package search

import (
	"context"
	"fmt"
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
	if resp.Results[0].MatchType != MatchExact {
		t.Errorf("expected MatchExact, got %q", resp.Results[0].MatchType)
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
	if resp.Results[0].MatchType != MatchExact {
		t.Errorf("expected MatchExact for first result, got %q", resp.Results[0].MatchType)
	}
	if resp.Results[1].MatchType != MatchPrefix {
		t.Errorf("expected MatchPrefix for second result, got %q", resp.Results[1].MatchType)
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

func TestFSSearcher_ContainsMatch(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 3, "SSL_connect.3ssl.html", "SSL_connect", "initiate TLS/SSL handshake")
	writeManpage(t, root, "noble", "", 1, "connect-proxy.1.html", "connect-proxy", "establish connection")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "connect", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	// "connect-proxy" is a prefix match; "SSL_connect" is a contains match.
	if resp.Total < 2 {
		t.Fatalf("expected at least 2 results, got %d", resp.Total)
	}

	// Find the SSL_connect result.
	var found bool
	for _, r := range resp.Results {
		if r.MatchType == MatchContains && r.Section == 3 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SSL_connect to appear as a contains match")
	}
}

func TestFSSearcher_FuzzyMatch(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "grep.1.html", "grep", "print lines that match patterns")

	s := NewFSSearcher(root, []string{"noble"})

	// Transposition typo: "grpe" (4 chars, threshold 1) → should fuzzy-match "grep" (distance 1).
	resp, err := s.Search(context.Background(), "grpe", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 fuzzy result for 'grpe', got %d", resp.Total)
	}
	if resp.Results[0].MatchType != MatchFuzzy {
		t.Errorf("expected MatchFuzzy, got %q", resp.Results[0].MatchType)
	}
	if resp.Results[0].Title != "grep - print lines that match patterns" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
}

func TestFSSearcher_FuzzyPrefixMatch(t *testing.T) {
	root := t.TempDir()
	// "sed" — exact match for the typo correction.
	writeManpage(t, root, "noble", "", 1, "sed.1.html", "sed", "stream editor")
	// "sed-opal-unlocker" — starts with "sed", should be found via fuzzy prefix.
	writeManpage(t, root, "noble", "", 8, "sed-opal-unlocker.8.html", "sed-opal-unlocker", "manage opal disks")
	// "sedlex" — starts with "sed", should also be found via fuzzy prefix.
	writeManpage(t, root, "noble", "", 1, "sedlex.1.html", "sedlex", "some tool")
	// "unrelated" — should NOT appear.
	writeManpage(t, root, "noble", "", 1, "unrelated.1.html", "unrelated", "nope")

	s := NewFSSearcher(root, []string{"noble"})

	// Typo "sde" (transposition of "sed"): should find sed, sed-opal-unlocker,
	// and sedlex via fuzzy + fuzzy prefix matching.
	resp, err := s.Search(context.Background(), "sde", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, r := range resp.Results {
		found[r.Path] = true
		if r.MatchType != MatchFuzzy {
			t.Errorf("expected MatchFuzzy for %s, got %s", r.Path, r.MatchType)
		}
	}

	if !found["/manpages/noble/man1/sed.1.html"] {
		t.Error("expected fuzzy match for 'sed'")
	}
	if !found["/manpages/noble/man8/sed-opal-unlocker.8.html"] {
		t.Error("expected fuzzy prefix match for 'sed-opal-unlocker'")
	}
	if !found["/manpages/noble/man1/sedlex.1.html"] {
		t.Error("expected fuzzy prefix match for 'sedlex'")
	}
	if found["/manpages/noble/man1/unrelated.1.html"] {
		t.Error("'unrelated' should not match 'sde'")
	}
}

func TestFSSearcher_FuzzyPrefixMatchTypo(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")
	writeManpage(t, root, "noble", "", 1, "lsblk.1.html", "lsblk", "list block devices")
	writeManpage(t, root, "noble", "", 1, "lsb_release.1.html", "lsb_release", "print distribution info")
	writeManpage(t, root, "noble", "", 1, "lsof.1.html", "lsof", "list open files")
	writeManpage(t, root, "noble", "", 1, "unrelated.1.html", "unrelated", "nope")

	s := NewFSSearcher(root, []string{"noble"})

	// 2-char query "sl" now has threshold 0, so no fuzzy matches.
	// Only exact/prefix/contains matches apply.
	resp, err := s.Search(context.Background(), "sl", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range resp.Results {
		if r.MatchType == MatchFuzzy {
			t.Errorf("2-char query should not produce fuzzy matches, got fuzzy for %s", r.Path)
		}
	}
}

func TestFSSearcher_FuzzyNoNoise(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")
	writeManpage(t, root, "noble", "", 1, "ab.1.html", "ab", "something else")

	s := NewFSSearcher(root, []string{"noble"})

	// "ls" (2 chars) has threshold 0, so no fuzzy matches at all.
	resp, err := s.Search(context.Background(), "ls", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resp.Results {
		if r.MatchType == MatchFuzzy {
			t.Errorf("2-char query should not produce fuzzy matches, got fuzzy for %s", r.Path)
		}
	}
}

func TestFSSearcher_MatchOrdering(t *testing.T) {
	root := t.TempDir()
	// Exact match.
	writeManpage(t, root, "noble", "", 1, "cat.1.html", "cat", "concatenate files")
	// Prefix match.
	writeManpage(t, root, "noble", "", 1, "catman.1.html", "catman", "create or update pre-formatted manual pages")
	// Contains match.
	writeManpage(t, root, "noble", "", 1, "bobcat.1.html", "bobcat", "a fictional command")
	// Fuzzy match (distance 1: "cat" → "bat").
	writeManpage(t, root, "noble", "", 1, "bat.1.html", "bat", "a cat alternative")

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "cat", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total < 4 {
		t.Fatalf("expected at least 4 results, got %d", resp.Total)
	}

	// Verify ordering: exact → prefix → contains → fuzzy.
	wantOrder := []MatchType{MatchExact, MatchPrefix, MatchContains, MatchFuzzy}
	for i, want := range wantOrder {
		if i >= len(resp.Results) {
			t.Fatalf("not enough results: got %d, need %d", len(resp.Results), i+1)
		}
		if resp.Results[i].MatchType != want {
			t.Errorf("result[%d]: expected %s, got %s (path=%s)", i, want, resp.Results[i].MatchType, resp.Results[i].Path)
		}
	}
}

func TestFSSearcher_Rebuild(t *testing.T) {
	root := t.TempDir()
	writeManpage(t, root, "noble", "", 1, "ls.1.html", "ls", "list directory contents")

	s := NewFSSearcher(root, []string{"noble"})

	// Initial search finds ls.
	resp, err := s.Search(context.Background(), "grep", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Fatalf("expected 0 results before adding grep, got %d", resp.Total)
	}

	// Add grep manpage and rebuild the index.
	writeManpage(t, root, "noble", "", 1, "grep.1.html", "grep", "print lines that match patterns")
	s.Rebuild()

	// Now grep should be found.
	resp, err = s.Search(context.Background(), "grep", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected 1 result after rebuild, got %d", resp.Total)
	}
	if resp.Results[0].Title != "grep - print lines that match patterns" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
}

func TestFSSearcher_FuzzyCap(t *testing.T) {
	root := t.TempDir()
	// Create 30 commands that are all within fuzzy distance 1 of "test"
	// by appending to the name (fuzzy prefix will match).
	base := "tset" // transposition of "test", distance 1
	writeManpage(t, root, "noble", "", 1, base+".1.html", base, "desc")
	// Create many variants like "tset-aaa", "tset-bbb", etc. that will
	// fuzzy prefix match "test" (prefix "tset" is distance 1 from "test").
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("tset-%03d", i)
		writeManpage(t, root, "noble", "", 1, name+".1.html", name, "desc")
	}

	s := NewFSSearcher(root, []string{"noble"})
	resp, err := s.Search(context.Background(), "test", "", "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}

	var fuzzyCount int
	for _, r := range resp.Results {
		if r.MatchType == MatchFuzzy {
			fuzzyCount++
		}
	}

	// Fuzzy results should be capped at 10.
	if fuzzyCount > 10 {
		t.Errorf("expected at most 10 fuzzy results, got %d", fuzzyCount)
	}
}

func BenchmarkFSSearcher_Search(b *testing.B) {
	root := b.TempDir()
	// Create a realistic number of manpages across sections.
	names := []string{
		"ls", "cat", "grep", "sed", "awk", "find", "sort", "cut", "head", "tail",
		"wc", "tr", "uniq", "diff", "patch", "tar", "gzip", "curl", "wget", "ssh",
		"scp", "rsync", "chmod", "chown", "mkdir", "rmdir", "cp", "mv", "rm", "ln",
		"echo", "printf", "test", "true", "false", "yes", "date", "cal", "sleep", "kill",
		"ps", "top", "free", "df", "du", "mount", "umount", "fdisk", "mkfs", "fsck",
	}
	for _, name := range names {
		for section := 1; section <= 9; section++ {
			writeManpageBench(b, root, "noble", section,
				name+"."+string(rune('0'+section))+".html", name, "description of "+name)
		}
	}

	s := NewFSSearcher(root, []string{"noble"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(context.Background(), "grpe", "", "", 50, 0)
	}
}

func writeManpageBench(b *testing.B, root, release string, section int, filename, title, desc string) {
	b.Helper()
	dir := filepath.Join(root, "manpages", release, sectionDirName(section))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	meta := `<!--META:{"title":"` + title + `","description":"` + desc + `"}-->` + "\n<p>content</p>"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(meta), 0o644); err != nil {
		b.Fatal(err)
	}
}
