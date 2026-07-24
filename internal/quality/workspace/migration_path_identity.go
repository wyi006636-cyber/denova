package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

func pathFilesystemIdentity(path string) (FilesystemIdentity, error) {
	file, err := os.Open(path)
	if err != nil {
		return FilesystemIdentity{}, err
	}
	defer file.Close()
	identity, err := platformFileIdentity(file)
	if err != nil {
		return FilesystemIdentity{}, err
	}
	if identity.Volume == "" || identity.FileID == "" || identity.ReparsePoint {
		return FilesystemIdentity{}, fmt.Errorf("path identity is unavailable or reparse-backed: %s", path)
	}
	return identity, nil
}

func rootEntryFilesystemIdentity(root *os.Root, rel string, expected os.FileInfo) (FilesystemIdentity, error) {
	file, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return FilesystemIdentity{}, err
	}
	defer file.Close()
	observed, err := file.Stat()
	if err != nil {
		return FilesystemIdentity{}, err
	}
	if expected != nil && !os.SameFile(expected, observed) {
		return FilesystemIdentity{}, fmt.Errorf("path identity changed before handle open: %s", rel)
	}
	identity, err := platformFileIdentity(file)
	if err != nil {
		return FilesystemIdentity{}, err
	}
	if identity.Volume == "" || identity.FileID == "" || identity.ReparsePoint {
		return FilesystemIdentity{}, fmt.Errorf("path is reparse-backed or has no stable identity: %s", rel)
	}
	return identity, nil
}

func validFilesystemIdentity(identity FilesystemIdentity) bool {
	return identity.Volume != "" && identity.FileID != "" && !identity.ReparsePoint
}
