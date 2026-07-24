//go:build windows

package projection

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockProjectionFile(file *os.File, nonBlocking bool) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if nonBlocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if err != nil {
		if nonBlocking && errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return ErrProjectionLocked
		}
		return fmt.Errorf("lock Projection rebuild file: %w", err)
	}
	return nil
}

func unlockProjectionFile(file *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{}); err != nil {
		return fmt.Errorf("unlock Projection rebuild file: %w", err)
	}
	return nil
}
