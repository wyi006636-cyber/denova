package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func schemaV1PreviewOptions() PreviewOptions {
	return PreviewOptions{
		Inspector: InspectorOptions{
			ApplicationVersion: "1.7.0",
			SupportedFeatures: map[string]string{
				"quality_harness": ">=1.0.0 <2.0.0",
			},
		},
		TargetFeatures: map[string]FeatureContract{
			"quality_harness": {Version: "1.1.0", Required: true},
		},
	}
}

func workspaceTreeDigest(t *testing.T, workspace string) []string {
	t.Helper()
	result := make([]string, 0)
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == workspace {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hash := sha256.Sum256([]byte(target))
			result = append(result, fmt.Sprintf("L %s %s", rel, hex.EncodeToString(hash[:])))
		case info.IsDir():
			result = append(result, "D "+rel)
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(content)
			result = append(result, fmt.Sprintf("F %s %d %s", rel, len(content), hex.EncodeToString(hash[:])))
		default:
			result = append(result, fmt.Sprintf("O %s %s", rel, info.Mode().Type()))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(result)
	return result
}

func previewEntryBySource(t *testing.T, preview MigrationPreview, source string) PreviewEntry {
	t.Helper()
	for _, entry := range preview.Entries {
		if entry.Source == source {
			return entry
		}
	}
	t.Fatalf("preview entries %#v do not contain source %q", preview.Entries, source)
	return PreviewEntry{}
}

func requirePreviewConflict(t *testing.T, preview MigrationPreview, code ErrorCode) PreviewConflict {
	t.Helper()
	for _, conflict := range preview.Conflicts {
		if conflict.Code == code {
			return conflict
		}
	}
	t.Fatalf("preview conflicts %#v do not contain %q", preview.Conflicts, code)
	return PreviewConflict{}
}
