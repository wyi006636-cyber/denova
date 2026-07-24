//go:build darwin || linux

package workspace

import (
	"fmt"
	"os"
	"syscall"
)

func platformAvailableBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func platformFilesystemIdentity(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("filesystem identity unavailable for %s", path)
	}
	return fmt.Sprint(stat.Dev), nil
}

func platformAtomicNamespaceRenameSupported() bool { return true }
func platformLongPathsSupported() bool             { return true }
