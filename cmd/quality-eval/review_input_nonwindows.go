//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openPrivateReviewInput(inputPath, inboxPath string) (*os.File, error) {
	if !filepath.IsAbs(inputPath) {
		return nil, errors.New("invalid input")
	}
	if !strings.EqualFold(filepath.Ext(inputPath), ".json") {
		return nil, errReviewInputType
	}
	inbox, err := resolvePathForContainment(inboxPath)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(inputPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), inputPath)
	canonical, err := filepath.EvalSymlinks(inputPath)
	if err != nil || !pathContains(inbox, canonical) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("input outside inbox")
	}
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errReviewInputType
	}
	canonicalStat, err := os.Stat(canonical)
	if err != nil || !sameReviewInputFile(stat, canonicalStat) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("input identity mismatch")
	}
	return file, nil
}
