//go:build !windows

package projection

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockProjectionFile(file *os.File, nonBlocking bool) error {
	operation := unix.LOCK_EX
	if nonBlocking {
		operation |= unix.LOCK_NB
	}
	if err := unix.Flock(int(file.Fd()), operation); err != nil {
		if nonBlocking && (errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)) {
			return ErrProjectionLocked
		}
		return fmt.Errorf("lock Projection rebuild file: %w", err)
	}
	return nil
}

func unlockProjectionFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("unlock Projection rebuild file: %w", err)
	}
	return nil
}
