package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ensureRootDirectoryTree(root *os.Root, workspace, rel string, mode os.FileMode) error {
	if rel == "." || rel == "" {
		return nil
	}
	normalized, err := ValidateRelativePath(rel, PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows})
	if err != nil {
		return err
	}
	segments := strings.Split(normalized, "/")
	current := ""
	parent := "."
	for _, segment := range segments {
		if current == "" {
			current = segment
		} else {
			current += "/" + segment
		}
		if err := ensureRootDirectory(root, workspace, current, mode, parent); err != nil {
			return err
		}
		parent = current
	}
	return nil
}

func copyVerifiedRootFile(root *os.Root, workspace, sourceRel, destinationRel string, expectedSize int64, expectedSHA256 string, mode os.FileMode) (bool, error) {
	if !validSHA256(expectedSHA256) || expectedSize < 0 {
		return false, errors.New("copy requires expected size and SHA-256")
	}
	sourceInfo, err := root.Lstat(filepath.FromSlash(sourceRel))
	if err != nil {
		return false, err
	}
	if !sourceInfo.Mode().IsRegular() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("source %s is not a regular file", sourceRel)
	}
	if destinationInfo, err := root.Lstat(filepath.FromSlash(destinationRel)); err == nil {
		if !destinationInfo.Mode().IsRegular() || destinationInfo.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("destination %s is not a regular file", destinationRel)
		}
		size, hash, changed, hashErr := hashBoundPreviewFile(root, destinationRel, destinationInfo)
		if hashErr != nil {
			return false, hashErr
		}
		if changed || size != expectedSize || hash != expectedSHA256 {
			return false, fmt.Errorf("destination %s conflicts: expected %s got %s", destinationRel, expectedSHA256, hash)
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	source, before, err := openBoundRegularFile(root, sourceRel, sourceInfo)
	if err != nil {
		return false, err
	}
	tempRel, err := siblingTempName(root, destinationRel)
	if err != nil {
		_ = source.Close()
		return false, err
	}
	file, err := root.OpenFile(filepath.FromSlash(tempRel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		_ = source.Close()
		return false, err
	}
	visible := false
	defer func() {
		_ = source.Close()
		_ = file.Close()
		if !visible {
			_ = root.Remove(filepath.FromSlash(tempRel))
		}
	}()
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hasher), source)
	after, statErr := source.Stat()
	closeSourceErr := source.Close()
	if copyErr != nil {
		return false, copyErr
	}
	if statErr != nil {
		return false, statErr
	}
	if closeSourceErr != nil {
		return false, closeSourceErr
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || size != expectedSize || actualHash != expectedSHA256 {
		return false, fmt.Errorf("source %s changed during copy: expected %s got %s", sourceRel, expectedSHA256, actualHash)
	}
	current, err := root.Lstat(filepath.FromSlash(sourceRel))
	if err != nil || !os.SameFile(before, current) {
		return false, fmt.Errorf("source %s identity changed after copy: %w", sourceRel, err)
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return false, err
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	if err := root.Rename(filepath.FromSlash(tempRel), filepath.FromSlash(destinationRel)); err != nil {
		return false, err
	}
	visible = true
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(destinationRel)))
	if err := syncRootDirectory(root, workspace, parent); err != nil {
		return true, err
	}
	destinationInfo, err := root.Lstat(filepath.FromSlash(destinationRel))
	if err != nil {
		return true, err
	}
	verifiedSize, verifiedHash, changed, err := hashBoundPreviewFile(root, destinationRel, destinationInfo)
	if err != nil || changed || verifiedSize != expectedSize || verifiedHash != expectedSHA256 || destinationInfo.Mode().Perm() != mode.Perm() {
		return true, fmt.Errorf("published destination %s failed verification: size=%d hash=%s changed=%t: %w", destinationRel, verifiedSize, verifiedHash, changed, err)
	}
	return true, nil
}
