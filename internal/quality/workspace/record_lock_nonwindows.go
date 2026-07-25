//go:build !windows

package workspace

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockRecordRepositoryFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return ErrRecordLocked
		}
		return fmt.Errorf("lock record repository: %w", err)
	}
	return nil
}

func unlockRecordRepositoryFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
