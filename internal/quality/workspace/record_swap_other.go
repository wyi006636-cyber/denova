//go:build !darwin && !linux && !windows

package workspace

import (
	"errors"
	"os"
)

func replaceRecordAtomically(_ *os.Root, _ string, _, _ string) (string, error) {
	return "", errors.New("atomic record CAS replacement is unsupported on this platform")
}

func restoreRecordAtomically(_ *os.Root, _ string, _, _, _ string) error {
	return errors.New("atomic record CAS restoration is unsupported on this platform")
}
