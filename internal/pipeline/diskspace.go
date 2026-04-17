package pipeline

import (
	"errors"
	"syscall"
)

const (
	minFreeBytes  = 100 << 20 // 100 MiB
	minFreeInodes = 1000
)

// ErrDiskFull is returned when the storage filesystem has insufficient space or inodes.
var ErrDiskFull = errors.New("low disk space on manpages storage")

// DiskFull reports whether the filesystem containing path has less than
// 100 MiB available or fewer than 1000 free inodes.
func DiskFull(path string) bool {
	_, reason := CheckDiskSpace(path)
	return reason != ""
}

// CheckDiskSpace returns (ok, reason). If ok is true, reason is empty.
// Otherwise reason describes the problem (low space or low inodes).
func CheckDiskSpace(path string) (bool, string) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return true, "" // if we can't check, assume OK
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	if avail < minFreeBytes {
		return false, "low disk space on manpages storage"
	}
	// Filesystems with dynamic inode allocation (btrfs, zfs, xfs in some
	// configurations) report Files == 0 to signal that no fixed inode count
	// exists. Skip the inode check entirely in that case — Ffree is also 0
	// and would otherwise trip the threshold on every call.
	if stat.Files > 0 && stat.Ffree < minFreeInodes {
		return false, "no inodes available on manpages storage"
	}
	return true, ""
}
