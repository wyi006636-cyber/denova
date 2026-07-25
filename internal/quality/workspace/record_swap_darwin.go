//go:build darwin

package workspace

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func replaceRecordAtomically(root *os.Root, _ string, targetRel, replacementRel string) (string, error) {
	if err := swapRecordSiblings(root, targetRel, replacementRel); err != nil {
		return "", err
	}
	return replacementRel, nil
}

func restoreRecordAtomically(root *os.Root, _ string, targetRel, displacedRel, _ string) error {
	return swapRecordSiblings(root, targetRel, displacedRel)
}

func swapRecordSiblings(root *os.Root, leftRel, rightRel string) error {
	parent := path.Dir(leftRel)
	if parent != path.Dir(rightRel) {
		return errors.New("record atomic exchange requires sibling paths")
	}
	directory, err := root.Open(filepath.FromSlash(parent))
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := unix.RenameatxNp(int(directory.Fd()), path.Base(leftRel), int(directory.Fd()), path.Base(rightRel), unix.RENAME_SWAP); err != nil {
		return fmt.Errorf("atomic record exchange: %w", err)
	}
	return nil
}
