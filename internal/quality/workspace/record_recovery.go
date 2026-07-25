package workspace

import (
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// RecordRecoveryEntry exposes complete preserved sibling bytes after a
// publication or rollback cannot cross its durability/identity barrier. It is
// inspection-only: no source is guessed to be authoritative.
type RecordRecoveryEntry struct {
	TargetRelativePath    string
	PreservedRelativePath string
	Kind                  RecordRecoveryKind
	RawSHA256             string
	Size                  int64
}

type RecordRecoveryKind string

const (
	RecordRecoveryTemporarySibling RecordRecoveryKind = "temporary_sibling"
	RecordRecoveryDisplacedTarget  RecordRecoveryKind = "displaced_target"
	RecordRecoveryCreateConflict   RecordRecoveryKind = "create_conflict"
)

func listRecordRecovery(root *os.Root, directory string, limit int, byteLimit int64, acceptsTarget func(string) bool) ([]RecordRecoveryEntry, error) {
	info, err := root.Lstat(filepath.FromSlash(directory))
	if os.IsNotExist(err) {
		return []RecordRecoveryEntry{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("record recovery directory is not strict: %w", err)
	}
	handle, err := root.Open(filepath.FromSlash(directory))
	if err != nil {
		return nil, err
	}
	entries, readErr := handle.ReadDir(limit + 1)
	closeErr := handle.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > limit {
		return nil, ErrRecordTooLarge
	}
	result := make([]RecordRecoveryEntry, 0)
	for _, entry := range entries {
		targetName, kind, recovery := recordRecoveryTarget(entry.Name())
		if !recovery || !acceptsTarget(targetName) {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil || !entryInfo.Mode().IsRegular() || entryInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("preserved record sibling %q is not regular: %w", entry.Name(), err)
		}
		preserved := path.Join(directory, entry.Name())
		raw, err := readBoundedRootFile(root, preserved, entryInfo, byteLimit)
		if err != nil {
			return nil, err
		}
		result = append(result, RecordRecoveryEntry{
			TargetRelativePath:    path.Join(directory, targetName),
			PreservedRelativePath: preserved,
			Kind:                  kind,
			RawSHA256:             recordSHA256(raw),
			Size:                  int64(len(raw)),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PreservedRelativePath < result[j].PreservedRelativePath })
	return result, nil
}

func recordTemporaryTarget(name string) (string, bool) {
	target, _, found := recordRecoveryTarget(name)
	return target, found
}

func recordRecoveryTarget(name string) (string, RecordRecoveryKind, bool) {
	markers := []struct {
		marker string
		kind   RecordRecoveryKind
	}{
		{".tmp-", RecordRecoveryTemporarySibling},
		{".displaced-", RecordRecoveryDisplacedTarget},
		{".conflict-", RecordRecoveryCreateConflict},
	}
	for _, candidate := range markers {
		if target, ok := recordRecoveryTargetWithMarker(name, candidate.marker); ok {
			return target, candidate.kind, true
		}
	}
	return "", "", false
}

func recordRecoveryTargetWithMarker(name, marker string) (string, bool) {
	index := strings.LastIndex(name, marker)
	if index <= 0 || len(name[index+len(marker):]) != 24 {
		return "", false
	}
	if _, err := hex.DecodeString(name[index+len(marker):]); err != nil {
		return "", false
	}
	return name[:index], true
}
