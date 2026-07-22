//go:build !windows

package evaluation

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type reviewLock struct {
	file *os.File
}

func acquireReviewLock(runRoot, runID string) (*reviewLock, error) {
	runDir, err := RunDirectory(runRoot, runID)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(runDir, "private", "reviews")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := protectPrivateEvidence(directory, true); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, ".review.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := protectPrivateEvidence(file.Name(), false); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &reviewLock{file: file}, nil
}

func (lock *reviewLock) Close() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	_ = lock.file.Close()
}
