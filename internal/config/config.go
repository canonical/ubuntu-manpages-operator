package config

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
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
// for any that are unset.
func Load() *Config {
	cfg := &Config{
		Site:          envOrDefault("MANPAGES_SITE", "https://manpages.ubuntu.com"),
		Archive:       envOrDefault("MANPAGES_ARCHIVE", "http://archive.ubuntu.com/ubuntu"),
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

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
