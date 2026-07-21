package durablefs

import (
	"os"
	"testing"
)

func TestSyncDirectorySupportsCurrentPlatform(t *testing.T) {
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open temporary directory: %v", err)
	}
	defer directory.Close()

	if err := SyncDirectory(directory); err != nil {
		t.Fatalf("sync temporary directory: %v", err)
	}
}
