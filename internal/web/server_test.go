package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/search"
	"github.com/canonical/ubuntu-manpages-operator/internal/transform"
)

func testServer(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	dir := t.TempDir()

	// Create a minimal manpage HTML fragment.
	manDir := filepath.Join(dir, "manpages", "noble", "man1")
	if err := os.MkdirAll(manDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fragment := `<!--META:{"title":"ls","description":"list directory contents","package":"coreutils"}-->` + "\n" + `<h2>NAME</h2><p>ls - list directory contents</p>`
	if err := os.WriteFile(filepath.Join(manDir, "ls.1.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Site:            "https://manpages.ubuntu.com",
		Archive:         "http://archive.ubuntu.com/ubuntu",
		PublicHTMLDir:   dir,
		Releases:        []string{"noble"},
		ReleaseVersions: map[string]string{"noble": "24.04"},
		Repos:           []string{"main"},
		Arch:            "amd64",
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(cfg, logger)
	return srv, cfg
}

func TestHandleRobotsTxt(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()
	srv.handleRobotsTxt(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("unexpected content type: %s", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(text, "User-agent: *") {
		t.Error("missing User-agent line")
	}
	if !strings.Contains(text, "Disallow: /api/") {
		t.Error("missing Disallow /api/")
	}
	if !strings.Contains(text, "Sitemap: https://manpages.ubuntu.com/sitemaps/sitemap-index.xml") {
		t.Errorf("missing or incorrect Sitemap line, got:\n%s", text)
	}
}

func TestHandleLlmsTxt(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
	w := httptest.NewRecorder()
	srv.handleLlmsTxt(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("unexpected content type: %s", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(text, "# Ubuntu Manpages") {
		t.Error("missing title")
	}
	if !strings.Contains(text, "noble (24.04)") {
		t.Error("missing release listing")
	}
	if !strings.Contains(text, "/api/search") {
		t.Error("missing API documentation")
	}
	if !strings.Contains(text, ".txt") {
		t.Error("missing plain text endpoint documentation")
	}
}

func TestHandleLlmsFullTxt(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/llms-full.txt", nil)
	w := httptest.NewRecorder()
	srv.handleLlmsFullTxt(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, "Full Documentation") {
		t.Error("missing full documentation header")
	}
	if !strings.Contains(text, "Example Requests") {
		t.Error("missing example requests section")
	}
	if !strings.Contains(text, "noble") {
		t.Error("missing release listing")
	}
}

func TestServeManpageText(t *testing.T) {
	srv, cfg := testServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/manpages/", srv.handleManpages)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Verify the .html version works.
	htmlPath := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man1", "ls.1.html")
	if _, err := os.Stat(htmlPath); err != nil {
		t.Fatalf("test file missing: %v", err)
	}

	// Request the .txt version.
	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/ls.1.txt", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("unexpected content type: %s", resp.Header.Get("Content-Type"))
	}
	if strings.Contains(text, "<h2>") {
		t.Error("plain text output still contains HTML tags")
	}
	if !strings.Contains(text, "list directory contents") {
		t.Error("expected manpage content in plain text")
	}
}

func TestServeManpageText_NotFound(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/nonexistent.1.txt", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<p>hello</p>", "hello"},
		{"<h1>Title</h1><p>Body text</p>", "Title  Body text"},
		{"no tags here", "no tags here"},
		{"", ""},
	}
	for _, tc := range tests {
		got := transform.StripHTMLTags(tc.input)
		if got != tc.want {
			t.Errorf("stripHTMLTags(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestBuildManpageJSONLD(t *testing.T) {
	crumbs := []breadcrumb{
		{Label: "noble", Href: "/manpages/noble"},
		{Label: "man(1)", Href: "/manpages/noble/man1"},
	}
	jsonld := buildManpageJSONLD("https://manpages.ubuntu.com", "https://manpages.ubuntu.com/manpages/noble/man1/ls.1.html", "ls", "list directory contents", crumbs)
	s := string(jsonld)

	if !strings.Contains(s, `"@type":"TechArticle"`) {
		t.Error("missing TechArticle type")
	}
	if !strings.Contains(s, `"@type":"BreadcrumbList"`) {
		t.Error("missing BreadcrumbList type")
	}
	if !strings.Contains(s, `"name":"ls"`) {
		t.Error("missing title")
	}
	if !strings.Contains(s, `application/ld+json`) {
		t.Error("missing script tag")
	}
}

func TestBuildIndexJSONLD(t *testing.T) {
	jsonld := buildIndexJSONLD("https://manpages.ubuntu.com")
	s := string(jsonld)

	if !strings.Contains(s, `"@type":"WebSite"`) {
		t.Error("missing WebSite type")
	}
	if !strings.Contains(s, `"@type":"SearchAction"`) {
		t.Error("missing SearchAction")
	}
	if !strings.Contains(s, `search?q={search_term_string}`) {
		t.Error("missing search target URL")
	}
}

func TestBuildIndexViewReleasesAscending(t *testing.T) {
	cfg := &config.Config{
		Site:     "https://manpages.ubuntu.com",
		Releases: []string{"noble", "jammy", "xenial", "plucky", "trusty"},
		ReleaseVersions: map[string]string{
			"noble":  "24.04",
			"jammy":  "22.04",
			"xenial": "16.04",
			"plucky": "25.04",
			"trusty": "14.04",
		},
	}
	view := buildIndexView(cfg)

	want := []string{"trusty", "xenial", "jammy", "noble", "plucky"}
	if len(view.Releases) != len(want) {
		t.Fatalf("got %d releases, want %d", len(view.Releases), len(want))
	}
	for i, r := range view.Releases {
		if r.Name != want[i] {
			t.Errorf("release[%d] = %q, want %q", i, r.Name, want[i])
		}
	}
}

func TestLogRequestsStatus200(t *testing.T) {
	srv, _ := testServer(t)

	var buf bytes.Buffer
	srv.logger = slog.New(slog.NewTextHandler(&buf, nil))

	handler := srv.logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "status=200") {
		t.Errorf("expected status=200 in log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "duration=") {
		t.Errorf("expected duration in log, got: %s", logOutput)
	}
}

func TestLogRequestsStatus404(t *testing.T) {
	srv, _ := testServer(t)

	var buf bytes.Buffer
	srv.logger = slog.New(slog.NewTextHandler(&buf, nil))

	handler := srv.logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "status=404") {
		t.Errorf("expected status=404 in log, got: %s", logOutput)
	}
}

func TestLogRequestsImplicit200(t *testing.T) {
	srv, _ := testServer(t)

	var buf bytes.Buffer
	srv.logger = slog.New(slog.NewTextHandler(&buf, nil))

	handler := srv.logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello")) // implicit 200
	}))

	req := httptest.NewRequest(http.MethodGet, "/implicit", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "status=200") {
		t.Errorf("expected status=200 in log, got: %s", logOutput)
	}
}

func TestResponseWriterImplementsFlusher(t *testing.T) {
	rw := &responseWriter{ResponseWriter: httptest.NewRecorder()}
	if _, ok := interface{}(rw).(http.Flusher); !ok {
		t.Error("responseWriter should implement http.Flusher")
	}
}

func TestStaticAssetCacheHeaders(t *testing.T) {
	mux := http.NewServeMux()
	staticFS, _ := fs.Sub(webAssets, "static")
	etag := computeStaticETag()
	mux.Handle("/static/", staticCacheHandler(etag,
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	))

	req := httptest.NewRequest(http.MethodGet, "/static/docs.css", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("unexpected Cache-Control: %s", cc)
	}
	if got := resp.Header.Get("ETag"); got != etag {
		t.Errorf("expected ETag %s, got %s", etag, got)
	}
}

func TestStaticAssetConditionalRequest(t *testing.T) {
	mux := http.NewServeMux()
	staticFS, _ := fs.Sub(webAssets, "static")
	etag := computeStaticETag()
	mux.Handle("/static/", staticCacheHandler(etag,
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	))

	req := httptest.NewRequest(http.MethodGet, "/static/docs.css", nil)
	req.Header.Set("If-None-Match", etag)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w.Code)
	}
}

func TestComputeStaticETagDeterministic(t *testing.T) {
	etag1 := computeStaticETag()
	etag2 := computeStaticETag()
	if etag1 != etag2 {
		t.Errorf("ETag should be deterministic: %s != %s", etag1, etag2)
	}
	if !strings.HasPrefix(etag1, `"`) || !strings.HasSuffix(etag1, `"`) {
		t.Errorf("ETag should be quoted: %s", etag1)
	}
}

func TestGzipCompressesHTMLResponse(t *testing.T) {
	handler := gzipHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>hello world</body></html>"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", resp.Header.Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()
	body, _ := io.ReadAll(gr)
	if !strings.Contains(string(body), "hello world") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestGzipSkipsWithoutAcceptEncoding(t *testing.T) {
	handler := gzipHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Accept-Encoding header.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") == "gzip" {
		t.Error("should not gzip without Accept-Encoding")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestGzipCompressesJSON(t *testing.T) {
	handler := gzipHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip for JSON, got %q", resp.Header.Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()
	body, _ := io.ReadAll(gr)
	if string(body) != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestHandleSearchPageEmpty(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	w := httptest.NewRecorder()
	srv.handleSearchPage(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// Empty state should show suggestion chips.
	if !strings.Contains(text, "mp-search-empty") {
		t.Error("expected empty state with suggestions")
	}
	if !strings.Contains(text, "systemctl") {
		t.Error("expected suggestion chip for systemctl")
	}
	// No results should be rendered.
	if strings.Contains(text, "results found") {
		t.Error("should not show results summary without a query")
	}
}

func TestHandleSearchPageWithResults(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/search?q=ls", nil)
	w := httptest.NewRecorder()
	srv.handleSearchPage(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if strings.Contains(text, "Search is unavailable") {
		t.Error("filesystem search should always be available")
	}
	if !strings.Contains(text, "found.") {
		t.Error("expected search results for 'ls'")
	}
}

func TestBrowseURLWithPlusChar(t *testing.T) {
	srv, cfg := testServer(t)

	// Create a file with + in the name.
	manDir := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man1")
	fragment := `<!--META:{"title":"voro++"}-->` + "\n" + `<p>content</p>`
	if err := os.WriteFile(filepath.Join(manDir, "voro++.1.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// The file with + in the name should appear in the browse listing.
	// Go's html/template HTML-encodes + as &#43; in both href and text
	// content, which is valid HTML — browsers decode it correctly.
	if !strings.Contains(text, "voro") {
		t.Errorf("expected voro++ file in browse listing")
	}
	// The href should not be percent-encoded (%2B) which template.URL prevents.
	if strings.Contains(text, "%2B") {
		t.Errorf("href should not contain percent-encoded +, got:\n%s", text)
	}
}

func TestSuffixedVariantRedirect(t *testing.T) {
	srv, cfg := testServer(t)

	// Create a suffixed file: SSL_connect.3ssl.html
	manDir := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man3")
	if err := os.MkdirAll(manDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fragment := `<!--META:{"title":"SSL_connect"}-->` + "\n" + `<p>connect</p>`
	if err := os.WriteFile(filepath.Join(manDir, "SSL_connect.3ssl.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	// Request the unsuffixed URL that cross-references produce.
	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man3/SSL_connect.3.html", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "SSL_connect.3ssl.html") {
		t.Errorf("expected redirect to SSL_connect.3ssl.html, got: %s", loc)
	}
}

func TestSuffixedVariantNoMatch(t *testing.T) {
	srv, _ := testServer(t)

	// Request a completely nonexistent file — should 404, not redirect.
	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/nonexistent.1.html", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGroupSearchResults(t *testing.T) {
	releases := []indexRelease{
		{Name: "noble", Label: "24.04 LTS"},
		{Name: "jammy", Label: "22.04 LTS"},
	}
	results := []search.Result{
		{Title: "ls - list directory contents", Path: "/manpages/noble/man1/ls.1.html", Distro: "noble", Section: 1},
		{Title: "ls - list directory contents", Path: "/manpages/jammy/man1/ls.1.html", Distro: "jammy", Section: 1},
		{Title: "lsblk - list block devices", Path: "/manpages/noble/man8/lsblk.8.html", Distro: "noble", Section: 8},
	}

	groups := groupSearchResults(results, releases)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// First-seen order: noble first.
	if groups[0].Distro != "noble" {
		t.Errorf("expected first group to be noble, got %s", groups[0].Distro)
	}
	if groups[0].Label != "24.04 LTS" {
		t.Errorf("expected label 24.04 LTS, got %s", groups[0].Label)
	}
	if groups[0].Count != 2 {
		t.Errorf("expected 2 results in noble, got %d", groups[0].Count)
	}
	if groups[1].Distro != "jammy" {
		t.Errorf("expected second group to be jammy, got %s", groups[1].Distro)
	}
	if groups[1].Count != 1 {
		t.Errorf("expected 1 result in jammy, got %d", groups[1].Count)
	}
	// Title should be split.
	if groups[0].Results[0].Name != "ls" {
		t.Errorf("expected name 'ls', got %q", groups[0].Results[0].Name)
	}
	if groups[0].Results[0].Desc != "list directory contents" {
		t.Errorf("expected desc 'list directory contents', got %q", groups[0].Results[0].Desc)
	}
}

func TestOtherVersionsOnlyExisting(t *testing.T) {
	srv, cfg := testServer(t)

	// Add a second release to config where the manpage does NOT exist on disk.
	cfg.Releases = append(cfg.Releases, "jammy")
	cfg.ReleaseVersions["jammy"] = "22.04"

	// noble/man1/ls.1.html exists (created by testServer), but jammy does not.
	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/ls.1.html", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// The "Other versions" section should contain noble (exists on disk).
	if !strings.Contains(text, "Other versions") {
		t.Fatal("expected 'Other versions' section")
	}
	if !strings.Contains(text, `/manpages/noble/man1/ls.1.html`) {
		t.Error("expected link to noble manpage")
	}
	// jammy should NOT appear because the file doesn't exist on disk.
	if strings.Contains(text, `/manpages/jammy/man1/ls.1.html`) {
		t.Error("should not show link to jammy manpage that doesn't exist")
	}
}

func TestOtherVersionsBothExist(t *testing.T) {
	srv, cfg := testServer(t)

	// Add jammy release with the same manpage on disk.
	cfg.Releases = append(cfg.Releases, "jammy")
	cfg.ReleaseVersions["jammy"] = "22.04"
	jammyDir := filepath.Join(cfg.PublicHTMLDir, "manpages", "jammy", "man1")
	if err := os.MkdirAll(jammyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fragment := `<!--META:{"title":"ls","description":"list directory contents"}-->` + "\n" + `<p>content</p>`
	if err := os.WriteFile(filepath.Join(jammyDir, "ls.1.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/ls.1.html", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// Both should appear since both files exist.
	if !strings.Contains(text, `/manpages/noble/man1/ls.1.html`) {
		t.Error("expected link to noble manpage")
	}
	if !strings.Contains(text, `/manpages/jammy/man1/ls.1.html`) {
		t.Error("expected link to jammy manpage")
	}
}

func TestLatestRedirect(t *testing.T) {
	srv, _ := testServer(t) // single release: noble 24.04

	req := httptest.NewRequest(http.MethodGet, "/manpages/latest/man1/ls.1.html", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/manpages/noble/man1/ls.1.html" {
		t.Errorf("expected redirect to noble, got: %s", loc)
	}
}

func TestLTSRedirect(t *testing.T) {
	srv, cfg := testServer(t)

	// Add a non-LTS release with a higher version.
	cfg.Releases = append(cfg.Releases, "questing")
	cfg.ReleaseVersions["questing"] = "25.10"

	req := httptest.NewRequest(http.MethodGet, "/manpages/lts/man1/ls.1.html", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/manpages/noble/man1/ls.1.html" {
		t.Errorf("expected redirect to noble (LTS), got: %s", loc)
	}
}

func TestLatestRedirectDirectory(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/manpages/latest/", nil)
	w := httptest.NewRecorder()
	srv.handleManpages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/manpages/noble/" {
		t.Errorf("expected redirect to /manpages/noble/, got: %s", loc)
	}
}

func TestGroupSearchResultsUnknownDistro(t *testing.T) {
	results := []search.Result{
		{Title: "foo", Path: "/manpages/unknown/man1/foo.1.html", Distro: "unknown", Section: 1},
	}

	groups := groupSearchResults(results, nil)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Label != "unknown" {
		t.Errorf("expected distro name as fallback label, got %q", groups[0].Label)
	}
}
