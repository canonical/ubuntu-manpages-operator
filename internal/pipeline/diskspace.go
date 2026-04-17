package pipeline

import (
	"errors"
	"syscall"
)

const minFreeBytes = 100 << 20 // 100 MiB

// ErrDiskFull is returned when the storage filesystem has less than 100 MiB available.
var ErrDiskFull = errors.New("low disk space on manpages storage")

// DiskFull reports whether the filesystem containing path has less than 100 MiB available.
func DiskFull(path string) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false // if we can't check, assume OK
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	return avail < minFreeBytes
}
