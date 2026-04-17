package web

import (
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"context"

	"github.com/canonical/ubuntu-manpages-operator/internal/config"
	"github.com/canonical/ubuntu-manpages-operator/internal/pipeline"
	"github.com/canonical/ubuntu-manpages-operator/internal/search"
	"github.com/canonical/ubuntu-manpages-operator/internal/sitemap"
	"github.com/canonical/ubuntu-manpages-operator/internal/transform"
)

//go:embed templates/base.html templates/base-landing.html templates/head.html templates/nav.html templates/footer.html templates/search-form.html templates/index.html templates/search.html templates/browse.html templates/manpage.html templates/404.html static/docs.css static/app.js static/search.js
var webAssets embed.FS

const (
	metaPrefix = "<!--META:"
	metaSuffix = "-->"
)

type Server struct {
	cfg         *config.Config
	basePath    string
	logger      *slog.Logger
	index       *template.Template
	searchPage  *template.Template
	browsePage  *template.Template
	manpagePage *template.Template
	notFound    *template.Template
	search      search.Searcher
	sitemapGen  *sitemap.SitemapGenerator
}

type manpageView struct {
	ActiveNav    string
	Title        string
	Description  string
	Body         template.HTML
	TOC          template.HTML
	Package      string
	PackageURL   template.URL
	Source       string
	SourceURL    template.URL
	BugURL       template.URL
	Releases     []indexRelease
	PathSuffix   string
	Breadcrumbs  []breadcrumb
	GzipHref     string
	GzipName     string
	SiteURL      string
	CanonicalURL string
	JSONLD       template.HTML
}

type searchView struct {
	ActiveNav     string
	Releases      []indexRelease
	ActiveRelease string
	SiteURL       string
	JSONLD        template.HTML
	Query         string
	Total         uint64
	ResultGroups  []searchGroup
	HasFuzzy      bool
	SearchError   bool
	Limit         int
}

type searchGroup struct {
	Distro       string
	Label        string
	Count        int
	Results      []searchResultView
	FuzzyResults []searchResultView
	FuzzyCount   int
}

type searchResultView struct {
	Name      string
	Desc      string
	Path      string
	Section   int
	MatchType search.MatchType
}

var vcsRevision = readVCSRevision()

func readVCSRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return s.Value[:7]
		}
	}
	return ""
}

func NewServer(cfg *config.Config, logger *slog.Logger) *Server {
	basePath := cfg.BasePath()
	funcMap := template.FuncMap{
		"commitHash": func() string { return vcsRevision },
		"sub":        func(a, b int) int { return a - b },
		"add":        func(a, b int) int { return a + b },
		"even":       func(i int) bool { return i%2 == 0 },
		"urlFor":     func(path string) string { return basePath + path },
		"reverseReleases": func(rs []indexRelease) []indexRelease {
			out := make([]indexRelease, len(rs))
			for i, r := range rs {
				out[len(rs)-1-i] = r
			}
			return out
		},
		"pageURL": func(path string, page, perPage int) string {
			if page <= 1 && perPage == defaultBrowsePageSize {
				return path
			}
			var params []string
			if page > 1 {
				params = append(params, fmt.Sprintf("page=%d", page))
			}
			if perPage != defaultBrowsePageSize {
				params = append(params, fmt.Sprintf("per_page=%d", perPage))
			}
			if len(params) == 0 {
				return path
			}
			return path + "?" + strings.Join(params, "&")
		},
		"pageNums": func(current, total int) []int {
			if total <= 7 {
				nums := make([]int, total)
				for i := range nums {
					nums[i] = i + 1
				}
				return nums
			}
			// Use 0 as a sentinel for truncation ellipsis.
			var nums []int
			nums = append(nums, 1)
			if current > 3 {
				nums = append(nums, 0)
			}
			for p := current - 1; p <= current+1; p++ {
				if p > 1 && p < total {
					nums = append(nums, p)
				}
			}
			if current < total-2 {
				nums = append(nums, 0)
			}
			nums = append(nums, total)
			return nums
		},
	}
	parse := func(files ...string) *template.Template {
		return template.Must(template.New("").Funcs(funcMap).ParseFS(webAssets, files...))
	}
	partials := []string{"templates/head.html", "templates/nav.html", "templates/footer.html", "templates/search-form.html"}
	index := parse(append(partials, "templates/base-landing.html", "templates/index.html")...)
	searchPage := parse(append(partials, "templates/base.html", "templates/search.html")...)
	browsePage := parse(append(partials, "templates/base.html", "templates/browse.html")...)
	manpagePage := parse(append(partials, "templates/base.html", "templates/manpage.html")...)
	notFound := parse(append(partials, "templates/base.html", "templates/404.html")...)
	searcher := search.NewFSSearcher(cfg.PublicHTMLDir, cfg.ReleaseKeys())
	sitemapGen := &sitemap.SitemapGenerator{
		Root:    cfg.PublicHTMLDir,
		SiteURL: cfg.SiteURL(),
		Logger:  logger,
	}
	return &Server{
		cfg:         cfg,
		basePath:    basePath,
		logger:      logger,
		index:       index,
		searchPage:  searchPage,
		browsePage:  browsePage,
		manpagePage: manpagePage,
		notFound:    notFound,
		search:      searcher,
		sitemapGen:  sitemapGen,
	}
}

func (s *Server) ListenAndServe(addr, adminAddr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/robots.txt", s.handleRobotsTxt)
	mux.HandleFunc("/llms.txt", s.handleLlmsTxt)
	mux.HandleFunc("/llms-full.txt", s.handleLlmsFullTxt)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/search", s.handleSearchPage)
	mux.HandleFunc("/", s.handleIndex)
	staticFS, _ := fs.Sub(webAssets, "static")
	staticETag := computeStaticETag()
	mux.Handle("/static/", staticCacheHandler(staticETag,
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	))
	fileServer := http.FileServer(http.Dir(s.cfg.PublicHTMLDir))
	mux.HandleFunc("/manpages/", s.handleManpages)
	mux.Handle("/manpages.gz/", fileServer)
	mux.Handle("/assets/", fileServer)
	mux.Handle("/functions.js", fileServer)
	sitemapDir := filepath.Join(s.cfg.PublicHTMLDir, "sitemaps")
	mux.Handle("/sitemaps/", http.StripPrefix("/sitemaps/", http.FileServer(http.Dir(sitemapDir))))

	pubSrv := &http.Server{
		Handler:           s.logRequests(securityHeaders(gzipHandler(mux))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /_/healthz", s.handleAdminHealth)
	adminMux.HandleFunc("POST /_/reindex", s.handleReindex)
	adminMux.HandleFunc("POST /_/regenerate-sitemaps", s.handleRegenerateSitemaps)
	adminSrv := &http.Server{
		Handler:           adminMux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	pubLn, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	adminLn, err := net.Listen("tcp", adminAddr)
	if err != nil {
		return err
	}

	s.logger.Info("listening", "addr", addr, "admin_addr", adminAddr)

	errc := make(chan error, 2)
	go func() { errc <- pubSrv.Serve(pubLn) }()
	go func() { errc <- adminSrv.Serve(adminLn) }()
	return <-errc
}

func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	go s.search.Rebuild()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRegenerateSitemaps(w http.ResponseWriter, r *http.Request) {
	go s.RegenerateSitemaps()
	w.WriteHeader(http.StatusAccepted)
}

// RegenerateSitemaps rebuilds all sitemap XML files using the current site URL.
func (s *Server) RegenerateSitemaps() {
	if err := s.sitemapGen.Generate(context.Background(), s.cfg.ReleaseKeys()); err != nil {
		s.logger.Error("sitemap generation failed", "error", err)
		return
	}
	s.logger.Info("sitemaps regenerated")
}

func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	idx := buildIndexView(s.cfg)

	// Determine the active release: use the query param if valid,
	// otherwise default to the newest release (last in ascending list).
	activeRelease := r.URL.Query().Get("release")
	valid := false
	for _, rel := range idx.Releases {
		if rel.Name == activeRelease {
			valid = true
			break
		}
	}
	if !valid && len(idx.Releases) > 0 {
		activeRelease = idx.Releases[len(idx.Releases)-1].Name
	}

	limit := parseIntQuery(r, "limit", 20)
	switch limit {
	case 20, 50, 100, 200:
	default:
		limit = 20
	}

	view := searchView{
		ActiveNav:     "search",
		Releases:      idx.Releases,
		ActiveRelease: activeRelease,
		SiteURL:       idx.SiteURL,
		Query:         r.URL.Query().Get("q"),
		Limit:         limit,
	}

	if view.Query != "" {
		if s.search == nil {
			view.SearchError = true
		} else {
			results, err := s.search.Search(r.Context(), view.Query, activeRelease, "", limit, 0)
			if err != nil {
				view.SearchError = true
			} else {
				view.ResultGroups, view.HasFuzzy = groupSearchResults(results.Results, idx.Releases, s.basePath)
				for _, g := range view.ResultGroups {
					view.Total += uint64(g.Count)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.searchPage.ExecuteTemplate(w, "base", view); err != nil {
		s.logger.Error("render error", "template", "search", "error", err)
	}
}

func groupSearchResults(results []search.Result, releases []indexRelease, basePath string) ([]searchGroup, bool) {
	labelMap := make(map[string]string, len(releases))
	for _, r := range releases {
		labelMap[r.Name] = r.Label
	}

	var order []string
	var hasFuzzy bool
	groups := map[string]*searchGroup{}
	for _, r := range results {
		g, ok := groups[r.Distro]
		if !ok {
			label := labelMap[r.Distro]
			if label == "" {
				label = r.Distro
			}
			g = &searchGroup{Distro: r.Distro, Label: label}
			groups[r.Distro] = g
			order = append(order, r.Distro)
		}

		name, desc := transform.SplitManpageTitle(r.Title)
		srv := searchResultView{
			Name:      name,
			Desc:      desc,
			Path:      basePath + r.Path,
			Section:   r.Section,
			MatchType: r.MatchType,
		}
		if r.MatchType == search.MatchFuzzy {
			g.FuzzyResults = append(g.FuzzyResults, srv)
			g.FuzzyCount++
			hasFuzzy = true
		} else {
			g.Results = append(g.Results, srv)
			g.Count++
		}
	}

	// Emit groups in descending release order (newest first).
	// Walk the releases list backwards; append any distros not in releases at the end.
	seen := make(map[string]bool, len(releases))
	out := make([]searchGroup, 0, len(groups))
	for i := len(releases) - 1; i >= 0; i-- {
		name := releases[i].Name
		if g, ok := groups[name]; ok {
			out = append(out, *g)
			seen[name] = true
		}
	}
	for _, distro := range order {
		if !seen[distro] {
			out = append(out, *groups[distro])
		}
	}
	return out, hasFuzzy
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		s.renderNotFound(w, r)
		return
	}

	view := buildIndexView(s.cfg)
	view.ActiveNav = "home"
	view.JSONLD = buildIndexJSONLD(view.SiteURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.index.ExecuteTemplate(w, "base-landing", view); err != nil {
		s.logger.Error("render error", "template", "index", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	storagePath := filepath.Join(s.cfg.PublicHTMLDir, "manpages")
	if pipeline.DiskFull(storagePath) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "low disk space on manpages storage",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if s.search == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "search index unavailable",
		})
		return
	}

	query := r.URL.Query().Get("q")
	distro := r.URL.Query().Get("release")
	lang := r.URL.Query().Get("lang")
	limit := parseIntQuery(r, "limit", 20)
	offset := parseIntQuery(r, "offset", 0)

	results, err := s.search.Search(r.Context(), query, distro, lang, limit, offset)
	if err != nil {
		s.logger.Error("search failed", "error", err, "query", query)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "search failed",
		})
		return
	}

	if s.basePath != "" {
		for i := range results.Results {
			results.Results[i].Path = s.basePath + results.Results[i].Path
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

func (s *Server) renderNotFound(w http.ResponseWriter, r *http.Request) {
	view := buildIndexView(s.cfg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := s.notFound.ExecuteTemplate(w, "base", view); err != nil {
		s.logger.Error("render error", "template", "404", "error", err)
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher, delegating to the underlying writer.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(rw, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", filepath.Clean(r.URL.Path),
			"status", rw.statusCode,
			"duration", time.Since(start),
		)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; script-src 'self'; "+
				"connect-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://assets.ubuntu.com; "+
				"img-src 'self' data: https://assets.ubuntu.com; "+
				"font-src 'self' https://assets.ubuntu.com https://fonts.gstatic.com; "+
				"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func computeStaticETag() string {
	h := sha256.New()
	entries, _ := webAssets.ReadDir("static")
	for _, entry := range entries {
		data, _ := webAssets.ReadFile("static/" + entry.Name())
		h.Write([]byte(entry.Name()))
		h.Write(data)
	}
	return `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
}

func staticCacheHandler(etag string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("ETag", etag)

		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Explicitly set Content-Type from the file extension so that
		// minimal container images without /etc/mime.types still serve
		// CSS/JS with the correct MIME type.
		if ct := mime.TypeByExtension(filepath.Ext(r.URL.Path)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}

		next.ServeHTTP(w, r)
	})
}

// gzipResponseWriter conditionally compresses responses for compressible content types.
type gzipResponseWriter struct {
	http.ResponseWriter
	gw      *gzip.Writer
	sniffed bool
}

func (grw *gzipResponseWriter) WriteHeader(code int) {
	if code != http.StatusNotModified {
		grw.sniff()
	}
	grw.ResponseWriter.WriteHeader(code)
}

func (grw *gzipResponseWriter) Write(b []byte) (int, error) {
	grw.sniff()
	if grw.gw != nil {
		return grw.gw.Write(b)
	}
	return grw.ResponseWriter.Write(b)
}

func (grw *gzipResponseWriter) sniff() {
	if grw.sniffed {
		return
	}
	grw.sniffed = true

	ct := grw.ResponseWriter.Header().Get("Content-Type")
	if strings.HasPrefix(ct, "text/") ||
		strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/javascript") {
		grw.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		grw.ResponseWriter.Header().Del("Content-Length")
	} else {
		grw.gw = nil
	}
}

func (grw *gzipResponseWriter) Flush() {
	if grw.gw != nil {
		_ = grw.gw.Flush()
	}
	if f, ok := grw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func gzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gw := gzip.NewWriter(w)
		grw := &gzipResponseWriter{ResponseWriter: w, gw: gw}
		next.ServeHTTP(grw, r)
		if grw.gw != nil {
			_ = grw.gw.Close()
		}
	})
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

type indexRelease struct {
	Name  string
	Label string
}

type indexView struct {
	ActiveNav string
	Releases  []indexRelease
	SiteURL   string
	JSONLD    template.HTML
}

type browseEntry struct {
	Name string
	Href template.URL
}

type breadcrumb struct {
	Label string
	Href  string
}

type browseView struct {
	ActiveNav      string
	Title          string
	Releases       []indexRelease
	Breadcrumbs    []breadcrumb
	Sections       []browseEntry
	Dirs           []browseEntry
	Files          []browseEntry
	FileCount      int
	Page           int
	TotalPages     int
	PerPage        int
	PerPageOptions []int
	PagePath       string
	SiteURL        string
	CanonicalURL   string
	JSONLD         template.HTML
}

func (s *Server) handleManpages(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean(r.URL.Path)

	// Redirect "latest" and "lts" aliases to the actual release codename.
	parts := strings.SplitN(clean, "/", 4) // ["", "manpages", alias, ...]
	if len(parts) >= 3 {
		var target string
		switch parts[2] {
		case "latest":
			target = s.cfg.LatestRelease()
		case "lts":
			target = s.cfg.LatestLTSRelease()
		}
		if target != "" {
			parts[2] = target
			dest := s.basePath + strings.Join(parts, "/")
			if strings.HasSuffix(r.URL.Path, "/") && !strings.HasSuffix(dest, "/") {
				dest += "/"
			}
			http.Redirect(w, r, dest, http.StatusFound)
			return
		}
	}

	// Redirect old /en/ language prefix to the canonical (unprefixed) path.
	// English manpages are stored without a language directory.
	if len(parts) >= 4 && (parts[3] == "en" || strings.HasPrefix(parts[3], "en/")) {
		rest := strings.TrimPrefix(parts[3], "en")
		dest := s.basePath + "/manpages/" + parts[2] + rest
		if strings.HasSuffix(r.URL.Path, "/") && !strings.HasSuffix(dest, "/") {
			dest += "/"
		}
		http.Redirect(w, r, dest, http.StatusMovedPermanently)
		return
	}

	fsPath := filepath.Join(s.cfg.PublicHTMLDir, clean)

	// Serve plain text version of manpages for LLM consumption.
	if strings.HasSuffix(clean, ".txt") {
		htmlPath := strings.TrimSuffix(fsPath, ".txt") + ".html"
		s.serveManpageText(w, r, htmlPath)
		return
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		// Cross-reference links like SSL_connect(3) produce .3.html but
		// the actual file may be .3ssl.html. Try finding a suffixed variant.
		if redirect := findSuffixedVariant(clean, fsPath); redirect != "" {
			http.Redirect(w, r, s.basePath+redirect, http.StatusMovedPermanently)
			return
		}
		s.renderNotFound(w, r)
		return
	}

	// Render manpage fragments through the template.
	if !info.IsDir() {
		s.serveManpage(w, r, fsPath)
		return
	}

	// Redirect to trailing slash for directories.
	if !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, s.basePath+r.URL.Path+"/", http.StatusMovedPermanently)
		return
	}

	entries, err := os.ReadDir(fsPath)
	if err != nil {
		s.renderNotFound(w, r)
		return
	}

	var sections, dirs, files []browseEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		href := s.basePath + filepath.Join(r.URL.Path, name)
		if e.IsDir() {
			entry := browseEntry{Name: name, Href: template.URL(href + "/")}
			if isManSection(name) {
				sections = append(sections, entry)
			} else {
				dirs = append(dirs, entry)
			}
		} else {
			display := strings.TrimSuffix(name, ".html")
			files = append(files, browseEntry{Name: display, Href: template.URL(href)})
		}
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Name < sections[j].Name })
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	// Build breadcrumbs from path segments.
	segments := strings.Split(strings.Trim(clean, "/"), "/")
	crumbs := []breadcrumb{{Label: "Manpages", Href: s.basePath + "/manpages/"}}
	for i := 1; i < len(segments); i++ {
		href := s.basePath + "/" + strings.Join(segments[:i+1], "/") + "/"
		label := segments[i]
		if i == 1 {
			if ver, ok := s.cfg.ReleaseVersions[label]; ok {
				label = label + " (" + ver + ")"
			}
		} else if strings.HasPrefix(label, "man") {
			label = "man(" + strings.TrimPrefix(label, "man") + ")"
		}
		if i == len(segments)-1 {
			href = "" // current page, no link
		}
		crumbs = append(crumbs, breadcrumb{Label: label, Href: href})
	}

	title := "Browse"
	if len(segments) > 1 {
		title = segments[len(segments)-1]
		if strings.HasPrefix(title, "man") {
			title = "man(" + strings.TrimPrefix(title, "man") + ")"
		}
	}

	// Paginate files.
	perPage := parseBrowsePerPage(r)
	totalFiles := len(files)
	totalPages := 1
	page := 1
	if totalFiles > perPage {
		totalPages = (totalFiles + perPage - 1) / perPage
		page = parseIntQuery(r, "page", 1)
		if page < 1 {
			page = 1
		}
		if page > totalPages {
			page = totalPages
		}
		start := (page - 1) * perPage
		end := start + perPage
		if end > totalFiles {
			end = totalFiles
		}
		files = files[start:end]
	}

	siteURL := s.cfg.SiteURL()
	view := browseView{
		ActiveNav:      "browse",
		Title:          title,
		Releases:       buildIndexView(s.cfg).Releases,
		Breadcrumbs:    crumbs,
		Sections:       sections,
		Dirs:           dirs,
		Files:          files,
		FileCount:      totalFiles,
		Page:           page,
		TotalPages:     totalPages,
		PerPage:        perPage,
		PerPageOptions: browsePerPageOptions,
		PagePath:       s.basePath + r.URL.Path,
		SiteURL:        siteURL,
		CanonicalURL:   siteURL + clean,
		JSONLD:         buildBrowseJSONLD(siteURL, crumbs),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.browsePage.ExecuteTemplate(w, "base", view); err != nil {
		s.logger.Error("render error", "template", "browse", "error", err)
	}
}

func isManSection(name string) bool {
	return len(name) == 4 && strings.HasPrefix(name, "man") && name[3] >= '1' && name[3] <= '9'
}

const defaultBrowsePageSize = 25

var browsePerPageOptions = []int{25, 50, 100, 200}

// parseBrowsePerPage reads the per_page query parameter and returns
// a validated page size. Only values in browsePerPageOptions are accepted;
// anything else falls back to the default.
func parseBrowsePerPage(r *http.Request) int {
	v := parseIntQuery(r, "per_page", defaultBrowsePageSize)
	for _, opt := range browsePerPageOptions {
		if v == opt {
			return v
		}
	}
	return defaultBrowsePageSize
}

// findSuffixedVariant handles cross-reference section suffix mismatches.
// For example, .Xr SSL_connect(3) generates a link to SSL_connect.3.html
// but the actual file is SSL_connect.3ssl.html. This scans the directory
// for a file that matches the prefix and returns a redirect URL, or "".
func findSuffixedVariant(urlPath, fsPath string) string {
	if !strings.HasSuffix(fsPath, ".html") {
		return ""
	}
	dir := filepath.Dir(fsPath)
	base := filepath.Base(fsPath)
	prefix := strings.TrimSuffix(base, ".html") // e.g. "SSL_connect.3"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == base || !strings.HasSuffix(name, ".html") {
			continue
		}
		// Match files like SSL_connect.3ssl.html for prefix SSL_connect.3
		if strings.HasPrefix(name, prefix) {
			return filepath.Join(filepath.Dir(urlPath), name)
		}
	}
	return ""
}

func (s *Server) serveManpage(w http.ResponseWriter, r *http.Request, fsPath string) {
	raw, err := os.ReadFile(fsPath)
	if err != nil {
		s.renderNotFound(w, r)
		return
	}

	siteURL := s.cfg.SiteURL()
	content := string(raw)
	view := manpageView{
		SiteURL:      siteURL,
		CanonicalURL: siteURL + filepath.Clean(r.URL.Path),
	}

	// Parse <!--META:{...}--> header if present.
	if strings.HasPrefix(content, metaPrefix) {
		if end := strings.Index(content, metaSuffix); end != -1 {
			jsonStr := content[len(metaPrefix):end]
			content = strings.TrimPrefix(content[end+len(metaSuffix):], "\n")

			var meta struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Package     string `json:"package"`
				PackageURL  string `json:"packageURL"`
				Source      string `json:"source"`
				SourceURL   string `json:"sourceURL"`
				BugURL      string `json:"bugURL"`
				TOC         string `json:"toc"`
			}
			if err := json.Unmarshal([]byte(jsonStr), &meta); err == nil {
				view.Title = meta.Title
				view.Description = meta.Description
				view.Package = meta.Package
				view.PackageURL = template.URL(meta.PackageURL)
				view.Source = meta.Source
				view.SourceURL = template.URL(meta.SourceURL)
				view.BugURL = template.URL(meta.BugURL)
				view.TOC = template.HTML(meta.TOC)
			}
		}
	}

	if view.Title == "" {
		view.Title = filepath.Base(fsPath)
	}
	view.Body = template.HTML(content)

	// Build path suffix, breadcrumbs, and gzip link from the URL.
	clean := filepath.Clean(r.URL.Path)
	segments := strings.Split(strings.Trim(clean, "/"), "/")
	// segments: [manpages, {release}, man{N}, file.html]
	if len(segments) >= 3 {
		view.PathSuffix = strings.Join(segments[2:], "/")
	}

	// Only show "Other versions" links for releases where this manpage exists.
	allReleases := buildIndexView(s.cfg).Releases
	if view.PathSuffix != "" {
		for _, rel := range allReleases {
			otherPath := filepath.Join(s.cfg.PublicHTMLDir, "manpages", rel.Name, view.PathSuffix)
			if _, err := os.Stat(otherPath); err == nil {
				view.Releases = append(view.Releases, rel)
			}
		}
	} else {
		view.Releases = allReleases
	}

	view.Breadcrumbs = s.buildManpageBreadcrumbs(segments)
	if len(segments) >= 4 {
		gzPath := strings.Replace(clean, "/manpages/", "/manpages.gz/", 1)
		gzPath = strings.TrimSuffix(gzPath, ".html") + ".gz"
		view.GzipHref = s.basePath + gzPath
		view.GzipName = filepath.Base(gzPath)
	}
	view.JSONLD = buildManpageJSONLD(view.SiteURL, view.CanonicalURL, view.Title, view.Description, view.Breadcrumbs)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.manpagePage.ExecuteTemplate(w, "base", view); err != nil {
		s.logger.Error("render error", "template", "manpage", "error", err)
	}
}

func (s *Server) buildManpageBreadcrumbs(segments []string) []breadcrumb {
	if len(segments) < 2 {
		return nil
	}
	var crumbs []breadcrumb
	// segments[1] is the release name
	distro := segments[1]
	crumbs = append(crumbs, breadcrumb{Label: distro, Href: s.basePath + "/manpages/" + distro})
	// segments[2] is e.g. "man1"
	if len(segments) >= 3 {
		section := strings.TrimPrefix(segments[2], "man")
		crumbs = append(crumbs, breadcrumb{
			Label: "man(" + section + ")",
			Href:  s.basePath + "/manpages/" + distro + "/" + segments[2],
		})
	}
	return crumbs
}

func buildIndexView(cfg *config.Config) indexView {
	keys := cfg.ReleaseKeys()
	labels := make([]indexRelease, 0, len(keys))

	for _, key := range keys {
		label := cfg.ReleaseVersions[key]
		parts := strings.Split(label, ".")
		maj, _ := strconv.Atoi(parts[0])
		if len(parts) == 2 && parts[1] == "04" && maj%2 == 0 {
			label += " LTS"
		}
		labels = append(labels, indexRelease{Name: key, Label: label})
	}

	// Sort releases ascending by version number (e.g. 14.04, 16.04, …, 25.10).
	sort.Slice(labels, func(i, j int) bool {
		vi := cfg.ReleaseVersions[labels[i].Name]
		vj := cfg.ReleaseVersions[labels[j].Name]
		majI, minI := parseVersionParts(vi)
		majJ, minJ := parseVersionParts(vj)
		if majI != majJ {
			return majI < majJ
		}
		return minI < minJ
	})

	return indexView{
		Releases: labels,
		SiteURL:  cfg.SiteURL(),
	}
}

func parseVersionParts(ver string) (maj, min int) {
	parts := strings.Split(ver, ".")
	if len(parts) == 2 {
		maj, _ = strconv.Atoi(parts[0])
		min, _ = strconv.Atoi(parts[1])
	}
	return
}

func (s *Server) handleRobotsTxt(w http.ResponseWriter, _ *http.Request) {
	siteURL := s.cfg.SiteURL()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, `User-agent: *
Allow: /
Disallow: %[1]s/api/
Disallow: %[1]s/healthz
Disallow: %[1]s/manpages.gz/

Sitemap: %[2]s/sitemaps/sitemap-index.xml
`, s.basePath, siteURL)
}

func (s *Server) handleLlmsTxt(w http.ResponseWriter, _ *http.Request) {
	siteURL := s.cfg.SiteURL()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	_, _ = fmt.Fprintf(w, `# Ubuntu Manpages

> A repository of hundreds of thousands of manpages from every supported Ubuntu release, rendered as browsable HTML.

This site provides web-accessible Unix/Linux manual pages (manpages) extracted from Ubuntu packages.

## Content Structure

- %[1]s/manpages/{release}/man{section}/{name}.{section}.html — Individual manpage
- %[1]s/manpages/{release}/ — Browse manpages by release
- %[1]s/search?q={query} — Search across all manpages

## Plain Text

Append .txt to any manpage URL for plain text output suitable for LLM consumption:
- %[1]s/manpages/{release}/man{section}/{name}.{section}.txt

## Releases

`, siteURL)

	for _, key := range s.cfg.ReleaseKeys() {
		_, _ = fmt.Fprintf(w, "- %s (%s)\n", key, s.cfg.ReleaseVersions[key])
	}

	_, _ = fmt.Fprintf(w, `
## Man Sections

- man1: User commands
- man2: System calls
- man3: Library functions
- man4: Special files
- man5: File formats
- man6: Games
- man7: Miscellaneous
- man8: System administration
- man9: Kernel routines

## API

- GET %[1]s/api/search?q={query}&release={release}&lang={lang}&limit={n}&offset={n}
  Returns JSON with fields: total, results (array of {title, path, distro, section, match_type})
  match_type is one of: exact, prefix, contains, fuzzy (Damerau-Levenshtein)
  Results are ranked: exact > prefix > contains > fuzzy
`, siteURL)
}

func (s *Server) handleLlmsFullTxt(w http.ResponseWriter, _ *http.Request) {
	siteURL := s.cfg.SiteURL()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	_, _ = fmt.Fprintf(w, `# Ubuntu Manpages — Full Documentation

> A repository of hundreds of thousands of manpages from every supported Ubuntu release, rendered as browsable HTML.

This site provides web-accessible Unix/Linux manual pages (manpages) extracted from Ubuntu packages across all supported Ubuntu releases. Manpages are refreshed from the official Ubuntu archive.

## Content Structure

Individual manpage: %[1]s/manpages/{release}/man{section}/{name}.{section}.html
Browse by release:  %[1]s/manpages/{release}/
Browse by section:  %[1]s/manpages/{release}/man{section}/
Homepage:           %[1]s/
Search:             %[1]s/search?q={query}

## Plain Text Access

Any manpage URL can be accessed in plain text by changing .html to .txt:
  %[1]s/manpages/{release}/man{section}/{name}.{section}.txt

This strips all HTML markup and returns the content as text/plain, suitable for LLM consumption.

## Releases

`, siteURL)

	for _, key := range s.cfg.ReleaseKeys() {
		_, _ = fmt.Fprintf(w, "- %s (%s)\n", key, s.cfg.ReleaseVersions[key])
	}

	_, _ = fmt.Fprintf(w, `
## Man Sections

- man1: Executable programs or shell commands
- man2: System calls (functions provided by the kernel)
- man3: Library calls (functions within program libraries)
- man4: Special files (usually found in /dev)
- man5: File formats and conventions (e.g. /etc/passwd)
- man6: Games
- man7: Miscellaneous (including macro packages and conventions)
- man8: System administration commands (usually only for root)
- man9: Kernel routines (non-standard)

## Search API

### Endpoint

GET %[1]s/api/search

### Parameters

- q (required): Search query string
- release (optional): Filter by Ubuntu release codename (e.g. "noble")
- lang (optional): Language filter (default: "en")
- limit (optional): Maximum results to return (default: 20)
- offset (optional): Pagination offset (default: 0)

### Response Format

{
  "total": 42,
  "results": [
    {
      "title": "ls - list directory contents",
      "path": "/manpages/noble/man1/ls.1.html",
      "distro": "noble",
      "section": 1,
      "match_type": "exact"
    }
  ]
}

### Search Ranking

Results are returned in four ranked tiers:
1. exact — case-insensitive exact match on the command name
2. prefix — query is a prefix of the command name
3. contains — query appears as a substring within the command name
4. fuzzy — within Damerau-Levenshtein edit distance (typo tolerance)

Each result carries a "match_type" field indicating which tier it matched.
Fuzzy matching is disabled for queries of 2 characters or fewer. For queries
of 3-4 characters the max edit distance is 1; for 5+ characters it is 2.
Fuzzy results are capped at 10.

### Example Requests

Search for "systemctl":
  GET %[1]s/api/search?q=systemctl

Search within noble release only:
  GET %[1]s/api/search?q=systemctl&release=noble

Paginated results:
  GET %[1]s/api/search?q=config&limit=20&offset=20

## Language Subdirectories

Some manpages are available in translated form under language subdirectories:
  %[1]s/manpages/{release}/{lang}/man{section}/{name}.{section}.html

For example:
  %[1]s/manpages/noble/de/man1/ls.1.html (German)
  %[1]s/manpages/noble/fr/man1/ls.1.html (French)

## Example URLs for Popular Commands

`, siteURL)

	examples := []string{"ls.1", "grep.1", "systemctl.1", "ssh.1", "apt.8", "nginx.8", "crontab.5", "bash.1"}
	for _, ex := range examples {
		section := "man" + string(ex[len(ex)-1])
		_, _ = fmt.Fprintf(w, "- %s/manpages/noble/%s/%s.html\n", siteURL, section, ex)
	}
}

func (s *Server) serveManpageText(w http.ResponseWriter, r *http.Request, htmlPath string) {
	raw, err := os.ReadFile(htmlPath)
	if err != nil {
		s.renderNotFound(w, r)
		return
	}

	content := string(raw)
	if strings.HasPrefix(content, metaPrefix) {
		if end := strings.Index(content, metaSuffix); end != -1 {
			content = strings.TrimPrefix(content[end+len(metaSuffix):], "\n")
		}
	}

	text := transform.StripHTMLTags(content)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(text))
}

func buildJSONLD(data any) template.HTML {
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return template.HTML(`<script type="application/ld+json">` + string(b) + `</script>`)
}

func buildManpageJSONLD(siteURL, canonicalURL, title, description string, breadcrumbs []breadcrumb) template.HTML {
	items := []any{
		buildBreadcrumbJSONLD(siteURL, breadcrumbs),
		map[string]any{
			"@context":    "https://schema.org",
			"@type":       "TechArticle",
			"name":        title,
			"description": description,
			"url":         canonicalURL,
			"isPartOf": map[string]any{
				"@type": "WebSite",
				"name":  "Ubuntu Manpages",
				"url":   siteURL,
			},
		},
	}
	b, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return template.HTML(`<script type="application/ld+json">` + string(b) + `</script>`)
}

func buildBreadcrumbJSONLD(siteURL string, breadcrumbs []breadcrumb) map[string]any {
	items := make([]map[string]any, 0, len(breadcrumbs))
	for i, crumb := range breadcrumbs {
		href := crumb.Href
		if href == "" {
			continue
		}
		items = append(items, map[string]any{
			"@type":    "ListItem",
			"position": i + 1,
			"name":     crumb.Label,
			"item":     siteURL + href,
		})
	}
	return map[string]any{
		"@context":        "https://schema.org",
		"@type":           "BreadcrumbList",
		"itemListElement": items,
	}
}

func buildIndexJSONLD(siteURL string) template.HTML {
	return buildJSONLD(map[string]any{
		"@context": "https://schema.org",
		"@type":    "WebSite",
		"name":     "Ubuntu Manpages",
		"url":      siteURL,
		"potentialAction": map[string]any{
			"@type":       "SearchAction",
			"target":      siteURL + "/search?q={search_term_string}",
			"query-input": "required name=search_term_string",
		},
	})
}

func buildBrowseJSONLD(siteURL string, breadcrumbs []breadcrumb) template.HTML {
	return buildJSONLD(buildBreadcrumbJSONLD(siteURL, breadcrumbs))
}
