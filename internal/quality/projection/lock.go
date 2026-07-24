package projection

import (
	"errors"
	"fmt"
	"os"
)

const projectionLockFileName = "index.db-rebuild.lock"

var ErrProjectionLocked = errors.New("Projection rebuild lock is held by another process")

type projectionFileLock struct {
	file *os.File
	root *projectionDataRoot
}

func acquireProjectionFileLock(workspace string, nonBlocking bool) (*projectionFileLock, error) {
	root, err := openProjectionDataRoot(workspace)
	if err != nil {
		return nil, err
	}
	var file *os.File
	created := false
	for attempt := 0; attempt < 2; attempt++ {
		pathInfo, statErr := root.data.Lstat(projectionLockFileName)
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			file, err = root.data.OpenFile(projectionLockFileName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			created = err == nil
		case statErr != nil:
			err = statErr
		case pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular():
			err = errors.New("Projection rebuild lock path is not a regular file")
		default:
			file, err = root.data.OpenFile(projectionLockFileName, os.O_RDWR, 0)
		}
		break
	}
	if err != nil || file == nil {
		root.Close()
		if err == nil {
			err = errors.New("Projection rebuild lock could not be opened")
		}
		return nil, fmt.Errorf("open Projection rebuild lock: %w", err)
	}
	pathInfo, err := root.data.Lstat(projectionLockFileName)
	if err != nil {
		file.Close()
		root.Close()
		return nil, fmt.Errorf("inspect Projection rebuild lock path: %w", err)
	}
	handleInfo, err := file.Stat()
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, handleInfo) {
		file.Close()
		root.Close()
		if err == nil {
			err = errors.New("Projection rebuild lock identity changed while opening")
		}
		return nil, fmt.Errorf("verify Projection rebuild lock: %w", err)
	}
	if err := lockProjectionFile(file, nonBlocking); err != nil {
		file.Close()
		root.Close()
		return nil, err
	}
	if created {
		if err := file.Sync(); err != nil {
			_ = unlockProjectionFile(file)
			file.Close()
			root.Close()
			return nil, fmt.Errorf("sync Projection rebuild lock: %w", err)
		}
		if err := syncProjectionDirectory(root.path, root.identity); err != nil {
			_ = unlockProjectionFile(file)
			file.Close()
			root.Close()
			return nil, fmt.Errorf("sync Projection rebuild lock parent: %w", err)
		}
	}
	return &projectionFileLock{file: file, root: root}, nil
}

func (lock *projectionFileLock) Close() error {
	if lock == nil {
		return nil
	}
	var result error
	if lock.file != nil {
		result = unlockProjectionFile(lock.file)
		if err := lock.file.Close(); result == nil {
			result = err
		}
		lock.file = nil
	}
	if lock.root != nil {
		if err := lock.root.Close(); result == nil {
			result = err
		}
		lock.root = nil
	}
	return result
}
