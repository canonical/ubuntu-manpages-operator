package config

import "testing"

func TestLatestRelease(t *testing.T) {
	cfg := &Config{
		ReleaseVersions: map[string]string{
			"jammy":    "22.04",
			"noble":    "24.04",
			"questing": "25.10",
		},
	}

	got := cfg.LatestRelease()
	if got != "questing" {
		t.Errorf("LatestRelease() = %q, want %q", got, "questing")
	}
}

func TestLatestReleaseSingle(t *testing.T) {
	cfg := &Config{
		ReleaseVersions: map[string]string{
			"noble": "24.04",
		},
	}

	got := cfg.LatestRelease()
	if got != "noble" {
		t.Errorf("LatestRelease() = %q, want %q", got, "noble")
	}
}

func TestLatestReleaseEmpty(t *testing.T) {
	cfg := &Config{
		ReleaseVersions: map[string]string{},
	}

	got := cfg.LatestRelease()
	if got != "" {
		t.Errorf("LatestRelease() = %q, want empty string", got)
	}
}

func TestLatestLTSRelease(t *testing.T) {
	cfg := &Config{
		ReleaseVersions: map[string]string{
			"jammy":    "22.04",
			"noble":    "24.04",
			"oracular": "24.10",
			"plucky":   "25.04",
			"questing": "25.10",
		},
	}

	got := cfg.LatestLTSRelease()
	if got != "noble" {
		t.Errorf("LatestLTSRelease() = %q, want %q", got, "noble")
	}
}

func TestLatestLTSReleaseNoLTS(t *testing.T) {
	cfg := &Config{
		ReleaseVersions: map[string]string{
			"oracular": "24.10",
			"questing": "25.10",
		},
	}

	got := cfg.LatestLTSRelease()
	if got != "" {
		t.Errorf("LatestLTSRelease() = %q, want empty string", got)
	}
}

func TestLatestLTSReleaseSkipsOddMajor(t *testing.T) {
	cfg := &Config{
		ReleaseVersions: map[string]string{
			"jammy":  "22.04",
			"noble":  "24.04",
			"plucky": "25.04", // odd major — not LTS
		},
	}

	got := cfg.LatestLTSRelease()
	if got != "noble" {
		t.Errorf("LatestLTSRelease() = %q, want %q", got, "noble")
	}
}
