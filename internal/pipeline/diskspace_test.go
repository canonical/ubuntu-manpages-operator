package pipeline

import (
	"os"
	"testing"
)

func TestDiskFull_NormalFilesystem(t *testing.T) {
	// A normal filesystem should have well over 100 MiB free.
	if DiskFull(os.TempDir()) {
		t.Error("DiskFull reported true on a normal filesystem")
	}
}

func TestDiskFull_NonExistentPath(t *testing.T) {
	// Non-existent path should return false (assume OK).
	if DiskFull("/nonexistent/path/that/does/not/exist") {
		t.Error("DiskFull reported true for a non-existent path")
	}
}

func TestCheckDiskSpace_ReportsReason(t *testing.T) {
	// A normal filesystem should report ok with no reason.
	ok, reason := CheckDiskSpace(os.TempDir())
	if !ok {
		t.Errorf("CheckDiskSpace reported not-ok on normal fs: %q", reason)
	}
	if reason != "" {
		t.Errorf("CheckDiskSpace returned reason %q on ok fs", reason)
	}
}
