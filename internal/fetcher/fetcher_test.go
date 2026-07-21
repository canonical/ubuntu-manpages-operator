package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParsePackages_HashFallback(t *testing.T) {
	// Some archives (e.g. newer Ubuntu releases) only publish a subset of
	// checksum fields. parsePackages must not silently drop packages that
	// lack SHA1 as long as some checksum is present, and must prefer
	// SHA512 over SHA256 over SHA1 over MD5sum when several are present.
	input := "Package: sha512-only\n" +
		"Version: 1.0-1\n" +
		"Filename: pool/s/sha512-only.deb\n" +
		"SHA512: deadbeef\n" +
		"\n" +
		"Package: sha1-and-sha256\n" +
		"Version: 1.0-1\n" +
		"Filename: pool/s/sha1-and-sha256.deb\n" +
		"SHA1: aaa\n" +
		"SHA256: bbb\n" +
		"\n" +
		"Package: all-hashes\n" +
		"Version: 1.0-1\n" +
		"Filename: pool/a/all-hashes.deb\n" +
		"MD5sum: ccc\n" +
		"SHA1: ddd\n" +
		"SHA256: eee\n" +
		"SHA512: fff\n" +
		"\n" +
		"Package: no-checksum\n" +
		"Version: 1.0-1\n" +
		"Filename: pool/n/no-checksum.deb\n" +
		"\n"

	got, err := parsePackages(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parsePackages: %v", err)
	}

	want := map[string]string{
		"sha512-only":     "deadbeef",
		"sha1-and-sha256": "bbb",
		"all-hashes":      "fff",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d packages (no-checksum should be dropped), got %d: %+v", len(want), len(got), got)
	}
	for _, pkg := range got {
		hash, ok := want[pkg.Name]
		if !ok {
			t.Fatalf("unexpected package %q in results", pkg.Name)
		}
		if pkg.Hash != hash {
			t.Errorf("package %q: expected hash %q, got %q", pkg.Name, hash, pkg.Hash)
		}
	}
}

func TestFetchDeb_RetriesOnConnectionReset(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// Hijack and immediately close to simulate connection reset.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server doesn't support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("deb-content"))
	}))
	defer server.Close()

	workDir := t.TempDir()
	fetcher := &Fetcher{
		Archive: server.URL,
		WorkDir: workDir,
		Client:  server.Client(),
	}

	path, err := fetcher.FetchDeb(context.Background(), "pool/test.deb")
	if err != nil {
		t.Fatalf("FetchDeb failed: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestFetchDeb_FailsAfterAllRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Always reset the connection.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server doesn't support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.(*net.TCPConn).SetLinger(0)
		_ = conn.Close()
	}))
	defer server.Close()

	workDir := t.TempDir()
	fetcher := &Fetcher{
		Archive: server.URL,
		WorkDir: workDir,
		Client:  server.Client(),
	}

	_, err := fetcher.FetchDeb(context.Background(), "pool/test.deb")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestFetchPackages_RetriesOnConnectionReset(t *testing.T) {
	var attempts atomic.Int32

	entries := []Package{{Name: "foo", Version: "1.0-1", Filename: "pool/f/foo.deb", Hash: "aaa"}}
	var body bytes.Buffer
	for _, p := range entries {
		fmt.Fprintf(&body, "Package: %s\nVersion: %s\nFilename: %s\nSHA1: %s\n\n",
			p.Name, p.Version, p.Filename, p.Hash)
	}
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(body.Bytes())
	_ = gw.Close()
	payload := gz.Bytes()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server doesn't support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	fetcher := &Fetcher{
		Archive: server.URL,
		Repos:   []string{"main"},
		Archs:   []string{"amd64"},
		Pockets: []string{""},
		WorkDir: t.TempDir(),
		Client:  server.Client(),
	}

	pkgs, err := fetcher.FetchPackages(context.Background(), "test")
	if err != nil {
		t.Fatalf("FetchPackages: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "foo" {
		t.Fatalf("unexpected pkgs: %+v", pkgs)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestFetcher_MaxConcurrent(t *testing.T) {
	var inFlight atomic.Int32
	var maxSeen atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			prev := maxSeen.Load()
			if cur <= prev || maxSeen.CompareAndSwap(prev, cur) {
				break
			}
		}
		// Hold briefly so overlap is measurable.
		<-time.After(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		var gz bytes.Buffer
		gw := gzip.NewWriter(&gz)
		_ = gw.Close()
		_, _ = w.Write(gz.Bytes())
	}))
	defer server.Close()

	fetcher := &Fetcher{
		Archive:       server.URL,
		Repos:         []string{"main", "universe", "multiverse", "restricted"},
		Archs:         []string{"amd64"},
		Pockets:       []string{"", "-updates", "-security"},
		WorkDir:       t.TempDir(),
		Client:        server.Client(),
		MaxConcurrent: 3,
	}

	if _, err := fetcher.FetchPackages(context.Background(), "test"); err != nil {
		t.Fatalf("FetchPackages: %v", err)
	}
	if got := maxSeen.Load(); got > 3 {
		t.Fatalf("expected at most 3 in-flight, saw %d", got)
	}
}

func TestVersionGreater(t *testing.T) {
	tests := []struct {
		left, right string
		want        bool
	}{
		{"2.0-1", "1.0-1", true},
		{"1.0-1", "2.0-1", false},
		{"1.0-1", "1.0-1", false},
		{"1:1.0-1", "2.0-1", true},
		{"1.0-1ubuntu2", "1.0-1ubuntu1", true},
		{"1.0-1ubuntu1", "1.0-1ubuntu2", false},
		{"4.3-1ubuntu2.1", "4.3-1ubuntu2", true},
		{"1.0-1", "", true},
		{"", "1.0-1", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_gt_%s", tt.left, tt.right), func(t *testing.T) {
			got := versionGreater(tt.left, tt.right)
			if got != tt.want {
				t.Errorf("versionGreater(%q, %q) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestFetchPackagesParallel(t *testing.T) {
	// Build a gzipped Packages file with two packages.
	makePackagesGz := func(entries []Package) []byte {
		var buf bytes.Buffer
		for _, p := range entries {
			fmt.Fprintf(&buf, "Package: %s\nVersion: %s\nFilename: %s\nSHA1: %s\n\n",
				p.Name, p.Version, p.Filename, p.Hash)
		}
		var gz bytes.Buffer
		w := gzip.NewWriter(&gz)
		_, _ = w.Write(buf.Bytes())
		_ = w.Close()
		return gz.Bytes()
	}

	// Two "pockets" serve overlapping packages with different versions.
	pocket1 := makePackagesGz([]Package{
		{Name: "foo", Version: "2.0-1", Filename: "pool/f/foo_2.deb", Hash: "aaa"},
		{Name: "bar", Version: "1.0-1", Filename: "pool/b/bar_1.deb", Hash: "bbb"},
	})
	pocket2 := makePackagesGz([]Package{
		{Name: "foo", Version: "1.0-1", Filename: "pool/f/foo_1.deb", Hash: "ccc"},
		{Name: "baz", Version: "3.0-1", Filename: "pool/b/baz_3.deb", Hash: "ddd"},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case contains(r.URL.Path, "test-updates"):
			w.Write(pocket1)
		case contains(r.URL.Path, "test/"):
			w.Write(pocket2)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := &Fetcher{
		Archive: server.URL,
		Repos:   []string{"main"},
		Archs:   []string{"amd64"},
		Pockets: []string{"-updates", ""},
		WorkDir: t.TempDir(),
		Client:  server.Client(),
	}

	pkgs, err := fetcher.FetchPackages(context.Background(), "test")
	if err != nil {
		t.Fatalf("FetchPackages: %v", err)
	}

	byName := make(map[string]Package)
	for _, p := range pkgs {
		byName[p.Name] = p
	}

	if len(byName) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(byName))
	}
	// foo: version 2.0-1 should win over 1.0-1
	if byName["foo"].Version != "2.0-1" {
		t.Errorf("foo version = %q, want 2.0-1", byName["foo"].Version)
	}
	if _, ok := byName["bar"]; !ok {
		t.Error("missing package bar")
	}
	if _, ok := byName["baz"]; !ok {
		t.Error("missing package baz")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && bytes.Contains([]byte(s), []byte(substr))
}
