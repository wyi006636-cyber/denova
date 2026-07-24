//go:build !windows

package projection

import (
	"errors"
	"os"
)

func replaceProjectionFile(staged, final string) error {
	return os.Rename(staged, final)
}

func renameProjectionDiagnostic(root *os.Root, _ string, source, destination string, expected os.FileInfo) (bool, error) {
	current, err := root.Lstat(source)
	if err != nil {
		return false, err
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		return false, errors.New("Projection diagnostic source identity changed before rename")
	}
	if err := root.Rename(source, destination); err != nil {
		return false, err
	}
	return true, nil
}
