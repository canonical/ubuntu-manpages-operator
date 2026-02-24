package launchpad

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

const defaultBaseURL = "https://api.launchpad.net/1.0"

// Client resolves Ubuntu release codenames to version numbers.
type Client interface {
	ReleaseMap(codenames []string) (map[string]string, error)
}

// HTTPClient queries the Launchpad REST API to resolve release versions.
type HTTPClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPClient creates a new Launchpad HTTP client. If httpClient is nil,
// http.DefaultClient is used.
func NewHTTPClient(httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &HTTPClient{
		BaseURL:    defaultBaseURL,
		HTTPClient: httpClient,
	}
}

func (c *HTTPClient) ReleaseMap(codenames []string) (map[string]string, error) {
	result := make(map[string]string, len(codenames))
	for _, name := range codenames {
		version, err := c.fetchVersion(name)
		if err != nil {
			return nil, err
		}
		result[name] = version
	}
	return sortedMap(result), nil
}

func (c *HTTPClient) fetchVersion(codename string) (string, error) {
	url := c.BaseURL + "/ubuntu/" + codename
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch release %q: %w", codename, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("release %q not found on Launchpad", codename)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch release %q: unexpected status %d", codename, resp.StatusCode)
	}

	var series struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&series); err != nil {
		return "", fmt.Errorf("parse release %q: %w", codename, err)
	}
	if series.Version == "" {
		return "", fmt.Errorf("release %q has no version on Launchpad", codename)
	}
	return series.Version, nil
}

// FakeClient is a test fake that returns hardcoded release versions.
type FakeClient struct {
	Releases map[string]string
}

// NewFakeClient creates a FakeClient pre-populated with well-known Ubuntu releases.
func NewFakeClient() *FakeClient {
	return &FakeClient{
		Releases: map[string]string{
			"trusty":   "14.04",
			"xenial":   "16.04",
			"bionic":   "18.04",
			"jammy":    "22.04",
			"noble":    "24.04",
			"oracular": "24.10",
			"plucky":   "25.04",
			"questing": "25.10",
		},
	}
}

func (f *FakeClient) ReleaseMap(codenames []string) (map[string]string, error) {
	result := make(map[string]string, len(codenames))
	for _, name := range codenames {
		version, ok := f.Releases[name]
		if !ok {
			return nil, fmt.Errorf("release %q not found on Launchpad", name)
		}
		result[name] = version
	}
	return sortedMap(result), nil
}

func sortedMap(m map[string]string) map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return m[keys[i]] < m[keys[j]]
	})
	sorted := make(map[string]string, len(m))
	for _, k := range keys {
		sorted[k] = m[k]
	}
	return sorted
}
