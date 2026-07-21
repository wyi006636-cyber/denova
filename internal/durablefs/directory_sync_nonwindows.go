//go:build !windows

package durablefs

import (
	"errors"
	"io"
	"os"
)

// SyncDirectory flushes namespace metadata on platforms that support syncing
// directory handles. Callers must sync regular files before invoking it.
func SyncDirectory(directory *os.File) error {
	if err := directory.Sync(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		return err
	}
	return nil
}
