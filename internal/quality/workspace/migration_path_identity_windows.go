//go:build windows

package workspace

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func platformFileIdentity(file *os.File) (FilesystemIdentity, error) {
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &information); err != nil {
		return FilesystemIdentity{}, err
	}
	fileID := uint64(information.FileIndexHigh)<<32 | uint64(information.FileIndexLow)
	return FilesystemIdentity{
		Volume:       fmt.Sprint(information.VolumeSerialNumber),
		FileID:       fmt.Sprint(fileID),
		ReparsePoint: information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0,
	}, nil
}
