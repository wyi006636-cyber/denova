//go:build windows

package workspace

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockRecordRepositoryFile(file *os.File) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{}); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return ErrRecordLocked
		}
		return fmt.Errorf("lock record repository: %w", err)
	}
	return nil
}

func unlockRecordRepositoryFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}
