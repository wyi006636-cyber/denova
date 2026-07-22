//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
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
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(inputPath), windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), inputPath)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("reparse input")
	}
	canonical, err := finalPathByHandle(handle)
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
	return file, nil
}

func finalPathByHandle(handle windows.Handle) (string, error) {
	size := uint32(512)
	for {
		buffer := make([]uint16, size)
		count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], size, 0)
		if err != nil {
			return "", err
		}
		if count < size {
			path := windows.UTF16ToString(buffer[:count])
			return strings.TrimPrefix(path, `\\?\`), nil
		}
		size = count + 1
	}
}
