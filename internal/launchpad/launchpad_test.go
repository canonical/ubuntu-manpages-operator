package launchpad

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFakeClientValidReleases(t *testing.T) {
	client := NewFakeClient()
	result, err := client.ReleaseMap([]string{"noble", "jammy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["noble"] != "24.04" {
		t.Errorf("expected noble=24.04, got %s", result["noble"])
	}
	if result["jammy"] != "22.04" {
		t.Errorf("expected jammy=22.04, got %s", result["jammy"])
	}
}

func TestFakeClientUnknownRelease(t *testing.T) {
	client := NewFakeClient()
	_, err := client.ReleaseMap([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown release")
	}
}

func TestHTTPClientSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ubuntu/noble":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "24.04"})
		case "/ubuntu/jammy":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "22.04"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.Client())
	client.BaseURL = srv.URL

	result, err := client.ReleaseMap([]string{"noble", "jammy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["noble"] != "24.04" {
		t.Errorf("expected noble=24.04, got %s", result["noble"])
	}
	if result["jammy"] != "22.04" {
		t.Errorf("expected jammy=22.04, got %s", result["jammy"])
	}
}

func TestHTTPClientNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	client := NewHTTPClient(srv.Client())
	client.BaseURL = srv.URL

	_, err := client.ReleaseMap([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown release")
	}
}

func TestHTTPClientEmptyVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": ""})
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.Client())
	client.BaseURL = srv.URL

	_, err := client.ReleaseMap([]string{"broken"})
	if err == nil {
		t.Fatal("expected error for empty version")
	}
}
