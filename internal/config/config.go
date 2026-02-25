package config

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	Site            string
	Archive         string
	PublicHTMLDir   string
	Releases        []string
	ReleaseVersions map[string]string
	Repos           []string
	Arch            string
}

// Load reads configuration from environment variables, applying defaults
// for any that are unset. If a .env file exists in the current working
// directory, its values are loaded first and override the real environment.
func Load() *Config {
	loadDotEnv()
	cfg := &Config{
		Site:          envOrDefault("MANPAGES_SITE", "https://manpages.ubuntu.com"),
		Archive:       envOrDefault("MANPAGES_ARCHIVE", "https://archive.ubuntu.com/ubuntu"),
		PublicHTMLDir: envOrDefault("MANPAGES_PUBLIC_HTML_DIR", "/app/www"),
		Releases:      splitCSV(envOrDefault("MANPAGES_RELEASES", "trusty, xenial, bionic, jammy, noble, plucky, questing")),
		Repos:         splitCSV(envOrDefault("MANPAGES_REPOS", "main, restricted, universe, multiverse")),
		Arch:          envOrDefault("MANPAGES_ARCH", "amd64"),
	}
	return cfg
}

func (c *Config) Validate() error {
	if c.Site == "" {
		return errors.New("config: site is required")
	}
	if c.Archive == "" {
		return errors.New("config: archive is required")
	}
	if c.PublicHTMLDir == "" {
		return errors.New("config: public_html_dir is required")
	}
	if len(c.Releases) == 0 {
		return errors.New("config: releases is required")
	}
	if len(c.Repos) == 0 {
		return errors.New("config: repos is required")
	}
	if c.Arch == "" {
		return errors.New("config: arch is required")
	}
	return nil
}

func (c *Config) IndexPath() string {
	return filepath.Join(c.PublicHTMLDir, "search.db")
}

func (c *Config) SiteURL() string {
	return strings.TrimRight(c.Site, "/")
}

func (c *Config) ReleaseKeys() []string {
	keys := make([]string, len(c.Releases))
	copy(keys, c.Releases)
	sort.Strings(keys)
	return keys
}

// LatestRelease returns the codename of the release with the highest version.
func (c *Config) LatestRelease() string {
	var best string
	var bestMaj, bestMin int
	for name, ver := range c.ReleaseVersions {
		maj, min := parseVersion(ver)
		if best == "" || maj > bestMaj || (maj == bestMaj && min > bestMin) {
			best, bestMaj, bestMin = name, maj, min
		}
	}
	return best
}

// LatestLTSRelease returns the codename of the most recent LTS release.
// LTS releases have an even major version and minor version "04".
func (c *Config) LatestLTSRelease() string {
	var best string
	var bestMaj int
	for name, ver := range c.ReleaseVersions {
		maj, min := parseVersion(ver)
		if min != 4 || maj%2 != 0 {
			continue
		}
		if best == "" || maj > bestMaj {
			best, bestMaj = name, maj
		}
	}
	return best
}

func parseVersion(ver string) (maj, min int) {
	parts := strings.Split(ver, ".")
	if len(parts) == 2 {
		maj, _ = strconv.Atoi(parts[0])
		min, _ = strconv.Atoi(parts[1])
	}
	return
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads a .env file from the current working directory and
// sets each key-value pair into the process environment via os.Setenv.
// If the file does not exist, the function returns silently.
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	vars, err := parseDotEnv(f)
	if err != nil {
		return
	}
	for k, v := range vars {
		os.Setenv(k, v)
	}
}

// parseDotEnv parses a .env-formatted stream into a map of key-value pairs.
// It supports:
//   - Blank lines and lines starting with # (comments)
//   - KEY=VALUE (whitespace around key and value is trimmed)
//   - Optional "export " prefix
//   - Double-quoted and single-quoted values
//   - Values containing '=' (only the first '=' is significant)
//   - Empty values (KEY= or KEY)
func parseDotEnv(r io.Reader) (map[string]string, error) {
	vars := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		key, value, hasEquals := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		if hasEquals {
			value = strings.TrimSpace(value)
			value = unquote(value)
		}

		vars[key] = value
	}
	return vars, scanner.Err()
}

// unquote removes matching surrounding single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
