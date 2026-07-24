//go:build darwin || linux

package workspace

import (
	"fmt"
	"os"
	"syscall"
)

func platformFileIdentity(file *os.File) (FilesystemIdentity, error) {
	info, err := file.Stat()
	if err != nil {
		return FilesystemIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FilesystemIdentity{}, fmt.Errorf("Unix file identity unavailable for %s", file.Name())
	}
	return FilesystemIdentity{Volume: fmt.Sprint(stat.Dev), FileID: fmt.Sprint(stat.Ino)}, nil
}
