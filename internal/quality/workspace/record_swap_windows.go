//go:build windows

package workspace

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

type recordFileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

// ReplaceFileW is the Windows primitive that atomically publishes the staged
// bytes while moving whichever target existed at the namespace boundary to a
// known backup sibling. Every directory component is held without delete
// sharing and checked against the os.Root view first, so the absolute strings
// required by ReplaceFileW cannot be redirected through a renamed directory or
// junction during the operation.
func replaceRecordAtomically(root *os.Root, workspace, targetRel, replacementRel string) (string, error) {
	backupRel, err := siblingNameWithMarker(root, targetRel, ".displaced-")
	if err != nil {
		return "", err
	}
	if err := replaceWindowsRecord(root, workspace, targetRel, replacementRel, backupRel); err != nil {
		return "", err
	}
	return backupRel, nil
}

func restoreRecordAtomically(root *os.Root, workspace, targetRel, displacedRel, cleanupRel string) error {
	return replaceWindowsRecord(root, workspace, targetRel, displacedRel, cleanupRel)
}

func replaceWindowsRecord(root *os.Root, workspace, targetRel, replacementRel, backupRel string) error {
	parent := path.Dir(targetRel)
	if parent != path.Dir(replacementRel) || parent != path.Dir(backupRel) {
		return errors.New("Windows record replacement requires sibling paths")
	}
	for _, rel := range []string{targetRel, replacementRel, backupRel} {
		validated, err := ValidateRelativePath(filepath.ToSlash(rel), PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows})
		if err != nil || validated != filepath.ToSlash(rel) {
			return fmt.Errorf("validate Windows record replacement path %q: %w", rel, err)
		}
	}
	targetBefore, err := strictWindowsRecordInfo(root, targetRel)
	if err != nil {
		return err
	}
	replacementBefore, err := strictWindowsRecordInfo(root, replacementRel)
	if err != nil {
		return err
	}
	if _, err := root.Lstat(filepath.FromSlash(backupRel)); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return ErrRecordConflict
		}
		return err
	}

	pinned, err := pinWindowsRecordParent(root, workspace, parent)
	if err != nil {
		return err
	}
	defer pinned.close()
	callErr := callReplaceFileW(
		filepath.Join(pinned.absolute, filepath.Base(filepath.FromSlash(targetRel))),
		filepath.Join(pinned.absolute, filepath.Base(filepath.FromSlash(replacementRel))),
		filepath.Join(pinned.absolute, filepath.Base(filepath.FromSlash(backupRel))),
	)
	return reconcileWindowsReplacement(root, workspace, parent, targetRel, replacementRel, backupRel, targetBefore, replacementBefore, callErr)
}

func reconcileWindowsReplacement(root *os.Root, workspace, parent, targetRel, replacementRel, backupRel string, targetBefore, replacementBefore os.FileInfo, callErr error) error {
	targetAfter, targetErr := root.Lstat(filepath.FromSlash(targetRel))
	replacementAfter, replacementErr := root.Lstat(filepath.FromSlash(replacementRel))
	backupAfter, backupErr := root.Lstat(filepath.FromSlash(backupRel))

	published := targetErr == nil && os.SameFile(targetAfter, replacementBefore)
	priorPreserved := backupErr == nil && os.SameFile(backupAfter, targetBefore)
	replacementGone := errors.Is(replacementErr, os.ErrNotExist)
	if published && backupErr == nil && !priorPreserved {
		if err := replaceWindowsRecord(root, workspace, targetRel, backupRel, replacementRel); err != nil {
			return errors.Join(ErrRecordRecoveryRequired, ErrRecordConflict, replacementStateError(callErr, "restore concurrently substituted target"), err)
		}
		return errors.Join(ErrRecordRecoveryRequired, ErrRecordConflict, replacementStateError(callErr, "concurrently substituted target was restored"))
	}
	if published && priorPreserved {
		if !replacementGone {
			if replacementErr != nil || !os.SameFile(replacementAfter, replacementBefore) {
				return errors.Join(ErrRecordRecoveryRequired, replacementStateError(callErr, "published replacement retained an unknown staged path"))
			}
			if err := root.Remove(filepath.FromSlash(replacementRel)); err != nil {
				return errors.Join(ErrRecordRecoveryRequired, fmt.Errorf("remove duplicate replacement name: %w", err))
			}
			if err := syncRootDirectory(root, workspace, parent); err != nil {
				return errors.Join(ErrRecordRecoveryRequired, fmt.Errorf("sync duplicate replacement cleanup: %w", err))
			}
		}
		return nil
	}
	foreignPublished := targetErr == nil && !os.SameFile(targetAfter, targetBefore) && !os.SameFile(targetAfter, replacementBefore) && priorPreserved && replacementGone
	if foreignPublished {
		if err := replaceWindowsRecord(root, workspace, targetRel, backupRel, replacementRel); err != nil {
			return errors.Join(ErrRecordRecoveryRequired, ErrRecordConflict, replacementStateError(callErr, "restore prior target after staged-path substitution"), err)
		}
		return errors.Join(ErrRecordRecoveryRequired, ErrRecordConflict, replacementStateError(callErr, "staged-path substitution was withdrawn"))
	}

	notPublished := targetErr == nil && os.SameFile(targetAfter, targetBefore) && replacementErr == nil && os.SameFile(replacementAfter, replacementBefore)
	if notPublished {
		if backupErr == nil {
			if !os.SameFile(backupAfter, targetBefore) {
				return errors.Join(ErrRecordRecoveryRequired, replacementStateError(callErr, "failed replacement left an unknown backup"))
			}
			if err := root.Remove(filepath.FromSlash(backupRel)); err != nil {
				return errors.Join(ErrRecordRecoveryRequired, fmt.Errorf("remove redundant replacement backup: %w", err))
			}
			if err := syncRootDirectory(root, workspace, parent); err != nil {
				return errors.Join(ErrRecordRecoveryRequired, fmt.Errorf("sync redundant backup cleanup: %w", err))
			}
		} else if !errors.Is(backupErr, os.ErrNotExist) {
			return errors.Join(ErrRecordRecoveryRequired, backupErr)
		}
		return replacementStateError(callErr, "Windows replacement made no namespace change")
	}

	if errors.Is(targetErr, os.ErrNotExist) && backupErr == nil && replacementErr == nil && os.SameFile(replacementAfter, replacementBefore) {
		if err := renameWindowsRecord(root, backupRel, targetRel); err != nil {
			return errors.Join(ErrRecordRecoveryRequired, replacementStateError(callErr, "restore absent target after partial replacement"), err)
		}
		if err := syncRootDirectory(root, workspace, parent); err != nil {
			return errors.Join(ErrRecordRecoveryRequired, replacementStateError(callErr, "sync restored target after partial replacement"), err)
		}
		if priorPreserved {
			return replacementStateError(callErr, "Windows replacement was rolled back after a partial namespace change")
		}
		return errors.Join(ErrRecordRecoveryRequired, ErrRecordConflict, replacementStateError(callErr, "concurrently substituted target was restored after a partial namespace change"))
	}

	return errors.Join(ErrRecordRecoveryRequired, replacementStateError(callErr, "Windows replacement left an unrecognized namespace state"))
}

func replacementStateError(callErr error, state string) error {
	if callErr != nil {
		return errors.Join(fmt.Errorf("%w: %s", ErrRecordDurability, state), callErr)
	}
	return fmt.Errorf("%w: %s", ErrRecordDurability, state)
}

func callReplaceFileW(target, replacement, backup string) error {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	replacementPointer, err := windows.UTF16PtrFromString(replacement)
	if err != nil {
		return err
	}
	backupPointer, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		uintptr(unsafe.Pointer(replacementPointer)),
		uintptr(unsafe.Pointer(backupPointer)),
		0,
		0,
		0,
	)
	if result != 0 {
		return nil
	}
	if callErr == windows.ERROR_SUCCESS {
		return errors.New("ReplaceFileW failed without a Windows error")
	}
	return callErr
}

type pinnedWindowsRecordParent struct {
	absolute string
	handles  []*os.File
}

func pinWindowsRecordParent(root *os.Root, workspace, parent string) (*pinnedWindowsRecordParent, error) {
	pinned := &pinnedWindowsRecordParent{}
	prefixes := []string{"."}
	if parent != "." {
		current := ""
		for _, segment := range splitPortablePath(parent) {
			current = path.Join(current, segment)
			prefixes = append(prefixes, current)
		}
	}
	for _, prefix := range prefixes {
		rootDirectory, err := root.Open(filepath.FromSlash(prefix))
		if err != nil {
			pinned.close()
			return nil, err
		}
		rootInfo, statErr := rootDirectory.Stat()
		_ = rootDirectory.Close()
		if statErr != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
			pinned.close()
			return nil, fmt.Errorf("inspect root-relative Windows record directory %q: %w", prefix, statErr)
		}
		absolute := workspace
		if prefix != "." {
			absolute = filepath.Join(workspace, filepath.FromSlash(prefix))
		}
		handle, err := openPinnedWindowsDirectory(absolute)
		if err != nil {
			pinned.close()
			return nil, err
		}
		handleInfo, statErr := handle.Stat()
		if statErr != nil || !handleInfo.IsDir() || handleInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(rootInfo, handleInfo) {
			_ = handle.Close()
			pinned.close()
			return nil, ErrRecordConflict
		}
		pinned.handles = append(pinned.handles, handle)
		pinned.absolute = absolute
	}
	return pinned, nil
}

func splitPortablePath(value string) []string {
	if value == "." || value == "" {
		return nil
	}
	var result []string
	for value != "." && value != "" {
		directory, base := path.Split(value)
		result = append([]string{base}, result...)
		value = path.Clean(strings.TrimSuffix(directory, "/"))
	}
	return result
}

func openPinnedWindowsDirectory(absolute string) (*os.File, error) {
	pointer, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), absolute)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("wrap pinned Windows record directory handle")
	}
	return file, nil
}

func (pinned *pinnedWindowsRecordParent) close() {
	for index := len(pinned.handles) - 1; index >= 0; index-- {
		_ = pinned.handles[index].Close()
	}
	pinned.handles = nil
}

func strictWindowsRecordInfo(root *os.Root, rel string) (os.FileInfo, error) {
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("Windows record is not a strict regular file: %w", err)
	}
	return info, nil
}

// renameWindowsRecord moves an exact source handle to an absent sibling name.
// Both names are single components relative to a parent opened through os.Root.
func renameWindowsRecord(root *os.Root, sourceRel, destinationRel string) (resultErr error) {
	parent := path.Dir(sourceRel)
	if parent != path.Dir(destinationRel) {
		return errors.New("Windows record rename requires sibling paths")
	}
	directory, err := root.Open(filepath.FromSlash(parent))
	if err != nil {
		return fmt.Errorf("open root-relative record parent: %w", err)
	}
	defer directory.Close()

	sourceName, err := windows.NewNTUnicodeString(filepath.Base(filepath.FromSlash(sourceRel)))
	if err != nil {
		return fmt.Errorf("encode record source: %w", err)
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
		return fmt.Errorf("open root-relative record source: %w", err)
	}
	sourceFile := os.NewFile(uintptr(sourceHandle), sourceRel)
	if sourceFile == nil {
		_ = windows.CloseHandle(sourceHandle)
		return errors.New("wrap root-relative record source handle")
	}
	defer func() {
		if closeErr := sourceFile.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close root-relative record source handle: %w", closeErr)
		}
	}()
	info, err := sourceFile.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("record rename source is not a strict regular file: %w", err)
	}

	destinationUTF16, err := windows.UTF16FromString(filepath.Base(filepath.FromSlash(destinationRel)))
	if err != nil {
		return fmt.Errorf("encode record destination: %w", err)
	}
	fileNameBytes := len(destinationUTF16)*2 - 2
	var layout recordFileRenameInformation
	bufferSize := int(unsafe.Offsetof(layout.FileName)) + fileNameBytes
	buffer := make([]byte, bufferSize)
	renameInfo := (*recordFileRenameInformation)(unsafe.Pointer(&buffer[0]))
	renameInfo.RootDirectory = windows.Handle(directory.Fd())
	renameInfo.FileNameLength = uint32(fileNameBytes)
	copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&renameInfo.FileName[0]))[:fileNameBytes/2:fileNameBytes/2], destinationUTF16)
	if err := windows.NtSetInformationFile(sourceHandle, &status, &buffer[0], uint32(bufferSize), windows.FileRenameInformation); err != nil {
		return fmt.Errorf("rename root-relative record entry: %w", err)
	}
	if err := sourceFile.Sync(); err != nil {
		return fmt.Errorf("flush root-relative renamed record: %w", err)
	}
	return nil
}
