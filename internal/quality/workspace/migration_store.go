package workspace

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"denova/internal/durablefs"
)

const migrationRecordFileName = "record.json"

func migrationRecordRelativePath(migrationID string) string {
	return MigrationRootRelativePath + "/" + migrationID + "/" + migrationRecordFileName
}

func loadMigrationRecord(workspace, migrationID string) (MigrationRecord, []byte, bool, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, "", "workspace root cannot be opened", err)
	}
	defer root.Close()

	if err := rejectMigrationIDCollision(root, migrationID); err != nil {
		return MigrationRecord{}, nil, false, err
	}
	rel := migrationRecordRelativePath(migrationID)
	info, err := root.Lstat(filepath.FromSlash(rel))
	if errors.Is(err, os.ErrNotExist) {
		migrationDir := MigrationRootRelativePath + "/" + migrationID
		if dirInfo, dirErr := root.Lstat(filepath.FromSlash(migrationDir)); dirErr == nil {
			if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
				return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, migrationDir, "migration path is not a safe directory", nil)
			}
			return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, rel, "migration directory exists without its recovery record", nil)
		} else if !errors.Is(dirErr, os.ErrNotExist) {
			return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, migrationDir, "migration directory cannot be inspected", dirErr)
		}
		return MigrationRecord{}, nil, false, nil
	}
	if err != nil {
		return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, rel, "migration record cannot be inspected", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, rel, "migration record is not a regular file", nil)
	}
	raw, err := readBoundedRootFile(root, rel, info, maxMigrationRecordBytes)
	if err != nil {
		return MigrationRecord{}, nil, false, migrationStoreError(migrationID, MigrationStepLoadRecord, rel, "migration record cannot be read through its bound handle", err)
	}
	record, err := decodeMigrationRecord(raw)
	if err != nil {
		return MigrationRecord{}, raw, true, err
	}
	if record.MigrationID != migrationID {
		return MigrationRecord{}, raw, true, migrationStoreError(migrationID, MigrationStepLoadRecord, rel, "record migration ID does not match its directory", nil)
	}
	return record, raw, true, nil
}

func persistMigrationRecord(workspace string, record MigrationRecord) (bool, error) {
	raw, err := encodeMigrationRecord(record)
	if err != nil {
		return false, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return false, migrationStoreError(record.MigrationID, MigrationStepLoadRecord, "", "workspace root cannot be opened", err)
	}
	defer root.Close()
	if err := rejectMigrationIDCollision(root, record.MigrationID); err != nil {
		return false, err
	}
	if err := ensureMigrationDirectory(root, workspace, record.MigrationID); err != nil {
		return false, err
	}
	return durableRootWrite(root, workspace, migrationRecordRelativePath(record.MigrationID), raw, 0o600)
}

func rejectMigrationIDCollision(root *os.Root, migrationID string) error {
	info, err := root.Lstat(MigrationRootRelativePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, MigrationRootRelativePath, "migration root cannot be inspected", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, MigrationRootRelativePath, "migration root is not a safe directory", nil)
	}
	directory, err := root.Open(MigrationRootRelativePath)
	if err != nil {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, MigrationRootRelativePath, "migration root cannot be opened", err)
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, MigrationRootRelativePath, "migration IDs cannot be enumerated", readErr)
	}
	if closeErr != nil {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, MigrationRootRelativePath, "migration root handle cannot be closed", closeErr)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == migrationID {
			continue
		}
		collisions := DetectPortablePathCollisions([]string{name, migrationID})
		if len(collisions) != 0 {
			return &MigrationError{
				Code:        CodeMigrationIDCollision,
				MigrationID: migrationID,
				Step:        MigrationStepLoadRecord,
				Path:        MigrationRootRelativePath + "/" + name,
				Durability:  DurabilityNotStarted,
				Recovery:    RecoveryRequired,
				NextAction:  MigrationNextManualRecovery,
				Message:     fmt.Sprintf("migration ID collides portably with existing ID %q", name),
			}
		}
	}
	return nil
}

func ensureMigrationDirectory(root *os.Root, workspace, migrationID string) error {
	if err := ensureRootDirectory(root, workspace, MigrationRootRelativePath, 0o700, "."); err != nil {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, MigrationRootRelativePath, "migration root cannot be durably created", err)
	}
	migrationDir := MigrationRootRelativePath + "/" + migrationID
	if err := ensureRootDirectory(root, workspace, migrationDir, 0o700, MigrationRootRelativePath); err != nil {
		return migrationStoreError(migrationID, MigrationStepLoadRecord, migrationDir, "migration directory cannot be durably created", err)
	}
	return nil
}

func ensureRootDirectory(root *os.Root, workspace, rel string, mode os.FileMode, parent string) error {
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is not a safe directory", rel)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Mkdir(filepath.FromSlash(rel), mode); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		info, statErr := root.Lstat(filepath.FromSlash(rel))
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("concurrent unsafe directory at %s: %w", rel, statErr)
		}
	}
	return syncRootDirectory(root, workspace, parent)
}

type durableWriteEvent string

const (
	durableWriteContentsWritten durableWriteEvent = "contents_written"
	durableWriteFileSynced      durableWriteEvent = "file_synced"
	durableWriteFileClosed      durableWriteEvent = "file_closed"
	durableWriteRenamed         durableWriteEvent = "renamed"
	durableWriteParentSynced    durableWriteEvent = "parent_synced"
)

func durableRootWrite(root *os.Root, workspace, rel string, raw []byte, mode os.FileMode) (bool, error) {
	return durableRootWriteObserved(root, workspace, rel, raw, mode, nil)
}

func durableRootWriteObserved(root *os.Root, workspace, rel string, raw []byte, mode os.FileMode, observe func(durableWriteEvent)) (bool, error) {
	if _, err := ValidateRelativePath(rel, PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}); err != nil {
		return false, err
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if parent == "." {
		parent = "."
	}
	if info, err := root.Lstat(filepath.FromSlash(rel)); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("destination %s is not a regular file", rel)
		}
		existing, readErr := readBoundedRootFile(root, rel, info, maxMigrationManifestBytes)
		if readErr != nil {
			return false, readErr
		}
		if bytes.Equal(existing, raw) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	tempRel, err := siblingTempName(root, rel)
	if err != nil {
		return false, err
	}
	file, err := root.OpenFile(filepath.FromSlash(tempRel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return false, err
	}
	visible := false
	cleanup := func() {
		_ = file.Close()
		if !visible {
			_ = root.Remove(filepath.FromSlash(tempRel))
		}
	}
	defer cleanup()
	if _, err := file.Write(raw); err != nil {
		return false, err
	}
	emitDurableWriteEvent(observe, durableWriteContentsWritten)
	if err := file.Sync(); err != nil {
		return false, err
	}
	emitDurableWriteEvent(observe, durableWriteFileSynced)
	if err := file.Close(); err != nil {
		return false, err
	}
	emitDurableWriteEvent(observe, durableWriteFileClosed)
	if err := root.Rename(filepath.FromSlash(tempRel), filepath.FromSlash(rel)); err != nil {
		return false, err
	}
	visible = true
	emitDurableWriteEvent(observe, durableWriteRenamed)
	if err := syncRootDirectory(root, workspace, parent); err != nil {
		return true, err
	}
	emitDurableWriteEvent(observe, durableWriteParentSynced)
	return true, nil
}

func emitDurableWriteEvent(observe func(durableWriteEvent), event durableWriteEvent) {
	if observe != nil {
		observe(event)
	}
}

func siblingTempName(root *os.Root, rel string) (string, error) {
	return siblingNameWithMarker(root, rel, ".tmp-")
}

func siblingNameWithMarker(root *os.Root, rel, marker string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		candidate := rel + marker + hex.EncodeToString(random[:])
		if _, err := root.Lstat(filepath.FromSlash(candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not allocate a unique sibling temporary file")
}

func syncRootDirectory(root *os.Root, workspace, rel string) error {
	directory, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	if err := durablefs.SyncDirectory(directory); err != nil {
		_ = directory.Close()
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	_ = workspace // retained in the signature for structured call-site evidence.
	return nil
}

func readBoundedRootFile(root *os.Root, rel string, expected os.FileInfo, limit int64) ([]byte, error) {
	file, before, err := openBoundRegularFile(root, rel, expected)
	if err != nil {
		return nil, err
	}
	reader := io.LimitReader(file, limit+1)
	raw, readErr := io.ReadAll(reader)
	after, statErr := file.Stat()
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if statErr != nil {
		return nil, statErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	if before.Size() != after.Size() || before.ModTime() != after.ModTime() || before.Mode() != after.Mode() {
		return nil, errors.New("file changed during bound read")
	}
	current, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil || !os.SameFile(before, current) {
		return nil, errors.New("file identity changed after bound read")
	}
	return raw, nil
}

func migrationStoreError(migrationID string, step MigrationStep, path, message string, err error) *MigrationError {
	return &MigrationError{
		Code:        CodeMigrationRecordInvalid,
		MigrationID: migrationID,
		Step:        step,
		Path:        strings.TrimPrefix(filepath.ToSlash(path), "./"),
		Durability:  DurabilityNotStarted,
		Recovery:    RecoveryRequired,
		NextAction:  MigrationNextManualRecovery,
		Message:     message,
		Err:         err,
	}
}
