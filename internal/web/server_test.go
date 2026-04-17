package web

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(text, "match_type") {
		t.Error("missing match_type documentation")
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
	if !strings.Contains(text, "Search Ranking") {
		t.Error("missing search ranking section")
	}
	if !strings.Contains(text, "match_type") {
		t.Error("missing match_type in response format")
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

func TestStaticAssetContentType(t *testing.T) {
	mux := http.NewServeMux()
	staticFS, _ := fs.Sub(webAssets, "static")
	etag := computeStaticETag()
	mux.Handle("/static/", staticCacheHandler(etag,
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	))

	tests := []struct {
		path        string
		contentType string
	}{
		{"/static/docs.css", "text/css"},
		{"/static/app.js", "text/javascript"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		ct := w.Result().Header.Get("Content-Type")
		if !strings.HasPrefix(ct, tt.contentType) {
			t.Errorf("%s: expected Content-Type starting with %q, got %q", tt.path, tt.contentType, ct)
		}
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
	if !strings.Contains(text, "p-list__item") {
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

func TestBrowsePagination(t *testing.T) {
	srv, cfg := testServer(t)

	// Create 250 manpage files to trigger pagination (page size = 25).
	manDir := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man1")
	for i := 0; i < 250; i++ {
		name := fmt.Sprintf("cmd%03d.1.html", i)
		if err := os.WriteFile(filepath.Join(manDir, name), []byte("<p>test</p>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 250 + 1 (ls.1.html from testServer) = 251 files, ceil(251/25) = 11 pages.
	t.Run("default page is 1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		// Page 1 should have the active page link.
		if !strings.Contains(text, `is-active`) {
			t.Error("expected active page indicator")
		}
		if !strings.Contains(text, "p-pagination") {
			t.Error("expected pagination controls")
		}
		if !strings.Contains(text, "cmd000") {
			t.Error("expected cmd000 on page 1")
		}
		// Should show total count.
		if !strings.Contains(text, "251 manpages") {
			t.Error("expected total file count")
		}
	})

	t.Run("page 2", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/?page=2", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		if !strings.Contains(text, "p-pagination") {
			t.Error("expected pagination controls on page 2")
		}
	})

	t.Run("page beyond max clamps to last page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/?page=999", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		// Last page (11) should be the active link.
		if !strings.Contains(text, `aria-current="page"`) {
			t.Error("expected current page indicator on last page")
		}
	})

	t.Run("negative page defaults to 1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/?page=-5", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		if !strings.Contains(text, "cmd000") {
			t.Error("expected first-page content when negative page given")
		}
	})

	t.Run("no pagination when few files", func(t *testing.T) {
		man9 := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man9")
		if err := os.MkdirAll(man9, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			name := fmt.Sprintf("few%d.9.html", i)
			if err := os.WriteFile(filepath.Join(man9, name), []byte("<p>test</p>"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man9/", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		if strings.Contains(text, "p-pagination__items") {
			t.Error("expected no pagination for small file list")
		}
	})

	t.Run("custom per_page", func(t *testing.T) {
		// With per_page=50, 251 files → ceil(251/50) = 6 pages.
		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/?per_page=50", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		// The per_page select should have 50 selected.
		if !strings.Contains(text, `value="50" selected`) {
			t.Error("expected per_page=50 to be selected")
		}
		// Page links should preserve per_page.
		if !strings.Contains(text, "per_page=50") {
			t.Error("expected page links to include per_page=50")
		}
	})

	t.Run("invalid per_page falls back to default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manpages/noble/man1/?per_page=99", nil)
		w := httptest.NewRecorder()
		srv.handleManpages(w, req)

		body, _ := io.ReadAll(w.Result().Body)
		text := string(body)

		// Should fall back to default 25, so 251/25 = 11 pages.
		if !strings.Contains(text, `value="25" selected`) {
			t.Error("expected default per_page=25 to be selected for invalid value")
		}
	})
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
	// Releases are sorted ascending by version (oldest first).
	releases := []indexRelease{
		{Name: "jammy", Label: "22.04 LTS"},
		{Name: "noble", Label: "24.04 LTS"},
	}
	results := []search.Result{
		{Title: "ls - list directory contents", Path: "/manpages/noble/man1/ls.1.html", Distro: "noble", Section: 1},
		{Title: "ls - list directory contents", Path: "/manpages/jammy/man1/ls.1.html", Distro: "jammy", Section: 1},
		{Title: "lsblk - list block devices", Path: "/manpages/noble/man8/lsblk.8.html", Distro: "noble", Section: 8},
	}

	groups, _ := groupSearchResults(results, releases, "")

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// Descending release order: noble (newest) first.
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

func TestEnglishLanguagePrefixRedirect(t *testing.T) {
	srv, _ := testServer(t)

	tests := []struct {
		name    string
		path    string
		wantLoc string
	}{
		{
			name:    "manpage with en prefix",
			path:    "/manpages/noble/en/man1/ls.1.html",
			wantLoc: "/manpages/noble/man1/ls.1.html",
		},
		{
			name:    "directory with en prefix",
			path:    "/manpages/noble/en/man1/",
			wantLoc: "/manpages/noble/man1/",
		},
		{
			name:    "bare en directory",
			path:    "/manpages/noble/en/",
			wantLoc: "/manpages/noble/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			srv.handleManpages(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusMovedPermanently {
				t.Fatalf("expected 301, got %d", resp.StatusCode)
			}
			loc := resp.Header.Get("Location")
			if loc != tt.wantLoc {
				t.Errorf("expected redirect to %s, got: %s", tt.wantLoc, loc)
			}
		})
	}
}

func TestGroupSearchResultsUnknownDistro(t *testing.T) {
	results := []search.Result{
		{Title: "foo", Path: "/manpages/unknown/man1/foo.1.html", Distro: "unknown", Section: 1},
	}

	groups, _ := groupSearchResults(results, nil, "")

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Label != "unknown" {
		t.Errorf("expected distro name as fallback label, got %q", groups[0].Label)
	}
}

func TestHandleIndexRendersLandingPage(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Must contain brochure layout elements, not the docs layout.
	for _, want := range []string{
		"Ubuntu Manpage Repository",
		"Browse by release",
		"Find a manpage",
		`action="/search"`,
		"Other resources",
		"noble",
		"What's included",
		"is-fixed-width p-rule",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected landing page to contain %q", want)
		}
	}

	// Must NOT contain docs layout markers.
	if strings.Contains(html, "l-docs__sidebar") {
		t.Error("landing page should not use the docs sidebar layout")
	}
}

func TestHandleSearchAPI_MatchType(t *testing.T) {
	srv, _ := testServer(t)

	// "ls" should produce an exact match.
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=ls&release=noble", nil)
	w := httptest.NewRecorder()
	srv.handleSearch(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(text, `"match_type":"exact"`) {
		t.Errorf("expected match_type exact in JSON response, got: %s", text)
	}
}

func TestHandleSearchPageFuzzySection(t *testing.T) {
	srv, cfg := testServer(t)

	// Create a "grep" manpage so "grpe" (transposition) fuzzy-matches it.
	manDir := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man1")
	fragment := `<!--META:{"title":"grep","description":"print lines that match patterns"}-->` + "\n" + `<p>content</p>`
	if err := os.WriteFile(filepath.Join(manDir, "grep.1.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	// Recreate the server so the searcher picks up the new file.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv = NewServer(cfg, logger)

	req := httptest.NewRequest(http.MethodGet, "/search?q=grpe", nil)
	w := httptest.NewRecorder()
	srv.handleSearchPage(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(text, "Similar matches") {
		t.Error("expected 'Similar matches' section for fuzzy results")
	}
	if !strings.Contains(text, "grep") {
		t.Error("expected grep to appear in fuzzy results for query 'grpe'")
	}
}

func TestSplitByMatchType(t *testing.T) {
	results := []search.Result{
		{Title: "ls", MatchType: search.MatchExact, Distro: "noble"},
		{Title: "lsblk", MatchType: search.MatchPrefix, Distro: "noble"},
		{Title: "false", MatchType: search.MatchContains, Distro: "noble"},
		{Title: "sl", MatchType: search.MatchFuzzy, Distro: "noble"},
	}

	groups, hasFuzzy := groupSearchResults(results, nil, "")

	if !hasFuzzy {
		t.Error("expected hasFuzzy to be true")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.Count != 3 {
		t.Errorf("expected 3 primary results, got %d", g.Count)
	}
	if g.FuzzyCount != 1 {
		t.Errorf("expected 1 fuzzy result, got %d", g.FuzzyCount)
	}
	if g.FuzzyResults[0].Name != "sl" {
		t.Errorf("expected fuzzy result 'sl', got %q", g.FuzzyResults[0].Name)
	}
}

func TestHandleSearchPageReleaseParam(t *testing.T) {
	srv, cfg := testServer(t)

	// Add a second release so we can verify switching.
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

	// Recreate server so the searcher picks up both releases.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv = NewServer(cfg, logger)

	// Without release param: defaults to newest (noble).
	req := httptest.NewRequest(http.MethodGet, "/search?q=ls", nil)
	w := httptest.NewRecorder()
	srv.handleSearchPage(w, req)
	body, _ := io.ReadAll(w.Result().Body)
	text := string(body)

	if !strings.Contains(text, "/manpages/noble/") {
		t.Error("default search should return noble results")
	}

	// With release=jammy: should return jammy results.
	req = httptest.NewRequest(http.MethodGet, "/search?q=ls&release=jammy", nil)
	w = httptest.NewRecorder()
	srv.handleSearchPage(w, req)
	body, _ = io.ReadAll(w.Result().Body)
	text = string(body)

	if !strings.Contains(text, "/manpages/jammy/") {
		t.Error("release=jammy should return jammy results")
	}
}

func TestHandleSearchPageNoMatchesShowsTabs(t *testing.T) {
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/search?q=zzzznonexistent", nil)
	w := httptest.NewRecorder()
	srv.handleSearchPage(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(text, "No results found.") {
		t.Error("expected 'No results found.' message")
	}
	// Tabs should still be rendered so the user can switch releases.
	if !strings.Contains(text, "p-tabs__link") {
		t.Error("expected release tabs even with no results")
	}
}

func TestHandleReindex(t *testing.T) {
	srv, cfg := testServer(t)

	// Add a new file after the server (and its index) was created, simulating
	// a file written by ingest after the server started.
	manDir := filepath.Join(cfg.PublicHTMLDir, "manpages", "noble", "man1")
	fragment := `<!--META:{"title":"htop","description":"interactive process viewer"}-->` + "\n" + `<p>content</p>`
	if err := os.WriteFile(filepath.Join(manDir, "htop.1.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	// The index is stale — htop should not be found yet.
	ctx := t.Context()
	results, err := srv.search.Search(ctx, "htop", "noble", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Results) != 0 {
		t.Fatalf("expected 0 results before reindex, got %d", len(results.Results))
	}

	// POST /_/reindex should return 202 Accepted.
	req := httptest.NewRequest(http.MethodPost, "/_/reindex", nil)
	w := httptest.NewRecorder()
	srv.handleReindex(w, req)

	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Result().StatusCode)
	}

	// Rebuild runs in a goroutine; poll briefly until the index is updated.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		results, err = srv.search.Search(ctx, "htop", "noble", "", 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results.Results) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(results.Results) == 0 {
		t.Error("expected htop to appear in search results after reindex")
	}
}

func TestHandleRegenerateSitemaps(t *testing.T) {
	srv, cfg := testServer(t)

	sitemapDir := filepath.Join(cfg.PublicHTMLDir, "sitemaps")

	// No sitemaps should exist yet.
	if _, err := os.Stat(sitemapDir); !os.IsNotExist(err) {
		t.Fatal("expected sitemaps dir to not exist before regeneration")
	}

	// POST /_/regenerate-sitemaps should return 202 Accepted.
	req := httptest.NewRequest(http.MethodPost, "/_/regenerate-sitemaps", nil)
	w := httptest.NewRecorder()
	srv.handleRegenerateSitemaps(w, req)

	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Result().StatusCode)
	}

	// Generation runs in a goroutine; poll briefly until the sitemap appears.
	indexPath := filepath.Join(sitemapDir, "sitemap-index.xml")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(indexPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("sitemap index not generated: %v", err)
	}

	// Verify URLs use the configured site URL, not a raw IP.
	content := string(data)
	if !strings.Contains(content, "https://manpages.ubuntu.com/sitemaps/") {
		t.Error("sitemap index should contain the configured site URL")
	}
}
