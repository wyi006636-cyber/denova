//go:build windows

package durablefs

import "os"

// SyncDirectory is best-effort on Windows because File.Sync delegates to
// FlushFileBuffers, which rejects the read-only handles returned for directories.
// Callers must still sync regular files before invoking this function.
func SyncDirectory(_ *os.File) error {
	return nil
}
