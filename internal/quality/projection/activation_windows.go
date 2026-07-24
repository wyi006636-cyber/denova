//go:build windows

package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func replaceProjectionFile(staged, final string) error {
	from, err := windows.UTF16PtrFromString(staged)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(final)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

type projectionFileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

// renameProjectionDiagnostic pins both names to the already-open data-root
// handle, renames through the source file handle, and flushes that handle
// before reporting success. This is the Windows durability boundary replacing
// directory fsync, which Windows does not expose for ordinary directory handles.
func renameProjectionDiagnostic(root *os.Root, _ string, source, destination string, expected os.FileInfo) (moved bool, resultErr error) {
	if filepath.Base(source) != source || filepath.Base(destination) != destination || source == "." || destination == "." {
		return false, errors.New("Projection diagnostic rename requires root-relative base names")
	}
	directory, err := root.Open(".")
	if err != nil {
		return false, fmt.Errorf("open Projection diagnostic root handle: %w", err)
	}
	defer func() {
		if closeErr := directory.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close Projection diagnostic root handle: %w", closeErr)
		}
	}()

	sourceName, err := windows.NewNTUnicodeString(source)
	if err != nil {
		return false, fmt.Errorf("encode Projection diagnostic source: %w", err)
	}
	attributes := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(directory.Fd()),
		ObjectName:    sourceName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	var sourceHandle windows.Handle
	var status windows.IO_STATUS_BLOCK
	var allocationSize int64
	err = windows.NtCreateFile(
		&sourceHandle,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
		attributes,
		&status,
		&allocationSize,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return false, fmt.Errorf("open Projection diagnostic source handle: %w", err)
	}
	sourceFile := os.NewFile(uintptr(sourceHandle), source)
	if sourceFile == nil {
		_ = windows.CloseHandle(sourceHandle)
		return false, errors.New("wrap Projection diagnostic source handle")
	}
	defer func() {
		if closeErr := sourceFile.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close Projection diagnostic source handle: %w", closeErr)
		}
	}()
	handleInfo, err := sourceFile.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect Projection diagnostic source handle: %w", err)
	}
	if handleInfo.Mode()&os.ModeSymlink != 0 || !handleInfo.Mode().IsRegular() || !os.SameFile(expected, handleInfo) {
		return false, errors.New("Projection diagnostic source identity changed before rename")
	}

	destinationUTF16, err := windows.UTF16FromString(destination)
	if err != nil {
		return false, fmt.Errorf("encode Projection diagnostic destination: %w", err)
	}
	fileNameBytes := len(destinationUTF16)*2 - 2
	var layout projectionFileRenameInformation
	bufferSize := int(unsafe.Offsetof(layout.FileName)) + fileNameBytes
	buffer := make([]byte, bufferSize)
	renameInfo := (*projectionFileRenameInformation)(unsafe.Pointer(&buffer[0]))
	renameInfo.RootDirectory = windows.Handle(directory.Fd())
	renameInfo.FileNameLength = uint32(fileNameBytes)
	copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&renameInfo.FileName[0]))[:fileNameBytes/2:fileNameBytes/2], destinationUTF16)
	if err := windows.NtSetInformationFile(sourceHandle, &status, &buffer[0], uint32(bufferSize), windows.FileRenameInformation); err != nil {
		return false, fmt.Errorf("rename Projection diagnostic entry: %w", err)
	}
	moved = true
	if err := sourceFile.Sync(); err != nil {
		return true, fmt.Errorf("flush renamed Projection diagnostic entry: %w", err)
	}
	return true, nil
}
