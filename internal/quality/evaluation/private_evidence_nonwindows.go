//go:build !windows

package evaluation

import "os"

func protectPrivateEvidence(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}
