package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	recordLockParent = ".denova/quality/runs"
	recordLockPath   = recordLockParent + "/record-repositories.lock"
)

type recordRepositoryLock struct {
	file *os.File
}

func acquireRecordRepositoryLock(root *os.Root, workspace string) (*recordRepositoryLock, error) {
	info, err := root.Lstat(filepath.FromSlash(recordLockPath))
	created := false
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := root.OpenFile(filepath.FromSlash(recordLockPath), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if createErr != nil {
			return nil, createErr
		}
		created = true
		info, err = file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return nil, err
		}
		if err := syncRootDirectory(root, workspace, recordLockParent); err != nil {
			file.Close()
			return nil, err
		}
		if err := lockRecordRepositoryFile(file); err != nil {
			file.Close()
			return nil, err
		}
		return &recordRepositoryLock{file: file}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("record repository lock is not a regular file")
	}
	file, err := root.OpenFile(filepath.FromSlash(recordLockPath), os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	handleInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, handleInfo) {
		file.Close()
		if err == nil {
			err = errors.New("lock identity changed")
		}
		return nil, fmt.Errorf("verify record repository lock: %w", err)
	}
	if err := lockRecordRepositoryFile(file); err != nil {
		file.Close()
		return nil, err
	}
	_ = created
	return &recordRepositoryLock{file: file}, nil
}

func (lock *recordRepositoryLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	result := unlockRecordRepositoryFile(lock.file)
	if err := lock.file.Close(); result == nil {
		result = err
	}
	lock.file = nil
	return result
}
