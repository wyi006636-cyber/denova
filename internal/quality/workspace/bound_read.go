package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

func openBoundRegularFile(root *os.Root, rel string, expected os.FileInfo) (*os.File, os.FileInfo, error) {
	nativeRel := filepath.FromSlash(rel)
	file, err := root.Open(nativeRel)
	if err != nil {
		return nil, nil, &PathError{Code: CodePathCanonical, Path: rel, Field: "source.handle", Value: rel, Message: "workspace-root-bound open failed", Err: err}
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, &PathError{Code: CodePathCanonical, Path: rel, Field: "source.handle", Value: rel, Message: "opened source identity cannot be inspected", Err: err}
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, &PathError{Code: CodePathCanonical, Path: rel, Field: "source.handle", Value: info.Mode().String(), Message: "opened source is not a regular file"}
	}
	if expected != nil && !os.SameFile(expected, info) {
		_ = file.Close()
		return nil, nil, &PathError{Code: CodePathIdentityChanged, Path: rel, Field: "source.identity", Value: rel, Message: "source identity changed before the bound handle opened"}
	}
	return file, info, nil
}

func hashBoundPreviewFile(root *os.Root, rel string, expected os.FileInfo) (int64, string, bool, error) {
	file, before, err := openBoundRegularFile(root, rel, expected)
	if err != nil {
		return 0, "", false, err
	}
	hasher := sha256.New()
	size, readErr := io.Copy(hasher, file)
	after, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil {
		return 0, "", false, readErr
	}
	if statErr != nil {
		return 0, "", false, statErr
	}
	if closeErr != nil {
		return 0, "", false, closeErr
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	changed := before.Size() != after.Size() || before.ModTime() != after.ModTime() || before.Mode() != after.Mode() || size != after.Size()
	current, err := root.Stat(filepath.FromSlash(rel))
	if err != nil {
		return size, hash, true, &PathError{Code: CodePathIdentityChanged, Path: rel, Field: "source.identity", Value: rel, Message: "source path changed after the bound read", Err: err}
	}
	if !os.SameFile(before, current) {
		return size, hash, true, &PathError{Code: CodePathIdentityChanged, Path: rel, Field: "source.identity", Value: rel, Message: "source path now resolves to a different file"}
	}
	return size, hash, changed, nil
}
