package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotEnvBasic(t *testing.T) {
	input := "KEY1=value1\nKEY2=value2\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY1"] != "value1" {
		t.Errorf("KEY1 = %q, want %q", vars["KEY1"], "value1")
	}
	if vars["KEY2"] != "value2" {
		t.Errorf("KEY2 = %q, want %q", vars["KEY2"], "value2")
	}
}

func TestParseDotEnvCommentsAndBlanks(t *testing.T) {
	input := "# this is a comment\n\nKEY=val\n  # indented comment\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 1 {
		t.Errorf("got %d vars, want 1", len(vars))
	}
	if vars["KEY"] != "val" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "val")
	}
}

func TestParseDotEnvDoubleQuotes(t *testing.T) {
	input := `KEY="hello world"` + "\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY"] != "hello world" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "hello world")
	}
}

func TestParseDotEnvSingleQuotes(t *testing.T) {
	input := `KEY='hello world'` + "\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY"] != "hello world" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "hello world")
	}
}

func TestParseDotEnvEqualsInValue(t *testing.T) {
	input := "KEY=a=b=c\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY"] != "a=b=c" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "a=b=c")
	}
}

func TestParseDotEnvExportPrefix(t *testing.T) {
	input := "export KEY=value\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "value")
	}
}

func TestParseDotEnvWhitespaceTrimming(t *testing.T) {
	input := "  KEY  =  value  \n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", vars["KEY"], "value")
	}
}

func TestParseDotEnvEmptyValue(t *testing.T) {
	input := "KEY=\n"
	vars, err := parseDotEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if vars["KEY"] != "" {
		t.Errorf("KEY = %q, want empty string", vars["KEY"])
	}
}

func TestLoadDotEnvOverridesEnv(t *testing.T) {
	// Create a temp directory with a .env file and chdir into it.
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	err := os.WriteFile(envFile, []byte("MANPAGES_ARCH=arm64\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Set a real env var that should be overridden by .env.
	t.Setenv("MANPAGES_ARCH", "s390x")

	cfg := Load()
	if cfg.Arch != "arm64" {
		t.Errorf("Arch = %q, want %q (.env should override real env)", cfg.Arch, "arm64")
	}
}

func TestLoadWithoutDotEnv(t *testing.T) {
	// Ensure Load works fine when no .env exists.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("MANPAGES_ARCH", "riscv64")

	cfg := Load()
	if cfg.Arch != "riscv64" {
		t.Errorf("Arch = %q, want %q (should use real env when no .env)", cfg.Arch, "riscv64")
	}
}

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

func TestLogLevelDefault(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("MANPAGES_LOG_LEVEL", "")

	cfg := Load()
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLogLevelFromEnv(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("MANPAGES_LOG_LEVEL", "debug")

	cfg := Load()
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestForceDefault(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("MANPAGES_FORCE", "")

	cfg := Load()
	if cfg.Force {
		t.Error("Force = true, want false")
	}
}

func TestForceFromEnv(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	for _, val := range []string{"true", "1", "TRUE"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("MANPAGES_FORCE", val)

			cfg := Load()
			if !cfg.Force {
				t.Errorf("Force = false with MANPAGES_FORCE=%q, want true", val)
			}
		})
	}
}
