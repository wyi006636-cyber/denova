package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	BackupManifestVersionV1   = 1
	maxMigrationManifestBytes = 8 * 1024 * 1024
)

// BackupNodeType records both byte-bearing files and required absence.
type BackupNodeType string

const (
	BackupNodeFile    BackupNodeType = "file"
	BackupNodeMissing BackupNodeType = "missing"
)

// BackupManifestEntry is stable by Path and contains enough evidence to
// restore or prove that the original destination was absent.
type BackupManifestEntry struct {
	Path       string         `json:"path"`
	BackupPath string         `json:"backup_path,omitempty"`
	Exists     bool           `json:"exists"`
	NodeType   BackupNodeType `json:"node_type"`
	Mode       uint32         `json:"mode"`
	Size       int64          `json:"size"`
	SHA256     string         `json:"sha256,omitempty"`
}

// BackupManifest is the content-hash truth for the retained recovery input.
type BackupManifest struct {
	RecordVersion            int                   `json:"record_version"`
	MigrationID              string                `json:"migration_id"`
	CanonicalWorkspace       string                `json:"canonical_workspace"`
	SourceExpectationsSHA256 string                `json:"source_expectations_sha256"`
	Entries                  []BackupManifestEntry `json:"entries"`
}

func backupManifestRelativePath(migrationID string) string {
	return MigrationRootRelativePath + "/" + migrationID + "/backup/manifest.json"
}

func prepareMigrationBackup(preview MigrationPreview, record MigrationRecord) (BackupManifest, bool, error) {
	root, err := os.OpenRoot(preview.Workspace)
	if err != nil {
		return BackupManifest{}, false, migrationArtifactError(record, MigrationStepBackup, "", "workspace root cannot be opened for backup", err)
	}
	defer root.Close()
	backupRoot := MigrationRootRelativePath + "/" + record.MigrationID + "/backup"
	if err := ensureRootDirectoryTree(root, preview.Workspace, backupRoot, 0o700); err != nil {
		return BackupManifest{}, false, migrationArtifactError(record, MigrationStepBackup, backupRoot, "backup directory cannot be durably created", err)
	}

	entries := backupEntries(preview, record)
	mutated := false
	for index := range entries {
		entry := &entries[index]
		if !entry.Exists {
			continue
		}
		backupPath := backupRoot + "/files/" + entry.Path
		if err := ensureRootDirectoryTree(root, preview.Workspace, filepath.ToSlash(filepath.Dir(filepath.FromSlash(backupPath))), 0o700); err != nil {
			return BackupManifest{}, mutated, migrationArtifactError(record, MigrationStepBackup, backupPath, "backup parent cannot be durably created", err)
		}
		written, err := copyVerifiedRootFile(root, preview.Workspace, entry.Path, backupPath, entry.Size, entry.SHA256, os.FileMode(entry.Mode))
		mutated = mutated || written
		if err != nil {
			return BackupManifest{}, mutated, migrationArtifactError(record, MigrationStepBackup, entry.Path, "source could not be copied and verified into backup", err)
		}
		entry.BackupPath = backupPath
	}
	expectationsHash, err := canonicalSHA256(record.Sources)
	if err != nil {
		return BackupManifest{}, mutated, migrationArtifactError(record, MigrationStepBackup, "sources", "source expectations cannot be hashed", err)
	}
	manifest := BackupManifest{
		RecordVersion:            BackupManifestVersionV1,
		MigrationID:              record.MigrationID,
		CanonicalWorkspace:       record.CanonicalWorkspace,
		SourceExpectationsSHA256: expectationsHash,
		Entries:                  entries,
	}
	if err := validateBackupManifest(manifest); err != nil {
		return BackupManifest{}, mutated, err
	}
	return manifest, mutated, nil
}

func backupEntries(preview MigrationPreview, record MigrationRecord) []BackupManifestEntry {
	entries := make([]BackupManifestEntry, 0)
	expectations := make(map[string]SourceExpectation, len(record.Sources))
	for _, expectation := range record.Sources {
		expectations[expectation.Path] = expectation
	}
	switch preview.Kind {
	case WorkspaceKindCurrent:
		if preview.Compatibility.Marker.Present {
			raw := preview.Compatibility.Marker.RawBytes()
			mode := uint32(0)
			if expectation, exists := expectations[MarkerRelativePath]; exists {
				mode = expectation.Mode
			}
			entries = append(entries, BackupManifestEntry{
				Path: MarkerRelativePath, Exists: true, NodeType: BackupNodeFile,
				Mode: mode, Size: int64(len(raw)), SHA256: sha256Hex(raw),
			})
		} else {
			entries = append(entries, BackupManifestEntry{Path: MarkerRelativePath, NodeType: BackupNodeMissing})
		}
	case WorkspaceKindLegacy:
		entries = append(entries, BackupManifestEntry{Path: record.TargetRoot, NodeType: BackupNodeMissing})
		for _, previewEntry := range preview.Entries {
			if previewEntry.Source == previewEntry.Destination || !strings.HasPrefix(previewEntry.Source, ".nova/") {
				continue
			}
			expectation, exists := expectations[previewEntry.Source]
			if !exists {
				continue
			}
			entries = append(entries, BackupManifestEntry{
				Path: previewEntry.Source, Exists: true, NodeType: BackupNodeFile,
				Mode: expectation.Mode, Size: previewEntry.Size, SHA256: previewEntry.SHA256,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func publishBackupManifest(workspace string, record MigrationRecord, manifest BackupManifest) (MigrationArtifactRef, bool, error) {
	raw, err := encodeBackupManifest(manifest)
	if err != nil {
		return MigrationArtifactRef{}, false, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return MigrationArtifactRef{}, false, migrationArtifactError(record, MigrationStepBackup, "", "workspace root cannot be opened to publish backup manifest", err)
	}
	defer root.Close()
	rel := backupManifestRelativePath(record.MigrationID)
	written, err := durableRootWrite(root, workspace, rel, raw, 0o600)
	if err != nil {
		return MigrationArtifactRef{}, written, migrationArtifactError(record, MigrationStepBackup, rel, "backup manifest publication was not durable", err)
	}
	return MigrationArtifactRef{RelativePath: rel, SHA256: sha256Hex(raw)}, written, nil
}

func verifyBackupArtifact(workspace string, record MigrationRecord) error {
	if record.Backup == nil || record.Backup.RelativePath != backupManifestRelativePath(record.MigrationID) || !validSHA256(record.Backup.SHA256) {
		return migrationArtifactError(record, MigrationStepBackup, backupManifestRelativePath(record.MigrationID), "backup manifest reference is missing or invalid", nil)
	}
	raw, err := readMigrationArtifact(workspace, record.Backup.RelativePath, maxMigrationManifestBytes)
	if err != nil {
		return migrationArtifactError(record, MigrationStepBackup, record.Backup.RelativePath, "backup manifest is missing or unreadable", err)
	}
	actual := sha256Hex(raw)
	if actual != record.Backup.SHA256 {
		return migrationHashError(record, MigrationStepBackup, record.Backup.RelativePath, record.Backup.SHA256, actual, "backup manifest hash differs from durable record")
	}
	manifest, err := decodeBackupManifest(raw)
	if err != nil {
		return err
	}
	if manifest.MigrationID != record.MigrationID || manifest.CanonicalWorkspace != record.CanonicalWorkspace {
		return migrationArtifactError(record, MigrationStepBackup, record.Backup.RelativePath, "backup manifest payload does not match the migration record", nil)
	}
	expectationsHash, _ := canonicalSHA256(record.Sources)
	if manifest.SourceExpectationsSHA256 != expectationsHash {
		return migrationHashError(record, MigrationStepBackup, record.Backup.RelativePath, expectationsHash, manifest.SourceExpectationsSHA256, "backup manifest source binding differs")
	}
	for _, entry := range manifest.Entries {
		if !entry.Exists {
			continue
		}
		raw, err := readMigrationArtifact(workspace, entry.BackupPath, entry.Size+1)
		if err != nil {
			return migrationArtifactError(record, MigrationStepBackup, entry.BackupPath, "backup file is missing or unreadable", err)
		}
		actual := sha256Hex(raw)
		if int64(len(raw)) != entry.Size || actual != entry.SHA256 {
			return migrationHashError(record, MigrationStepBackup, entry.BackupPath, entry.SHA256, actual, "backup file bytes differ from manifest")
		}
	}
	return nil
}

func encodeBackupManifest(manifest BackupManifest) ([]byte, error) {
	if err := validateBackupManifest(manifest); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func decodeBackupManifest(raw []byte) (BackupManifest, error) {
	var manifest BackupManifest
	if err := decodeStrictMigrationJSON(raw, maxMigrationManifestBytes, &manifest); err != nil {
		return BackupManifest{}, &MigrationError{Code: CodeMigrationArtifactInvalid, Step: MigrationStepBackup, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "backup manifest is not strict unambiguous JSON", Err: err}
	}
	if err := validateBackupManifest(manifest); err != nil {
		return BackupManifest{}, err
	}
	return manifest, nil
}

func validateBackupManifest(manifest BackupManifest) error {
	record := MigrationRecord{MigrationID: manifest.MigrationID, State: MigrationBackedUp}
	if manifest.RecordVersion != BackupManifestVersionV1 || ValidateMigrationID(manifest.MigrationID) != nil || !filepath.IsAbs(manifest.CanonicalWorkspace) || !validSHA256(manifest.SourceExpectationsSHA256) {
		return migrationArtifactError(record, MigrationStepBackup, backupManifestRelativePath(manifest.MigrationID), "backup manifest identity or version is invalid", nil)
	}
	if !sort.SliceIsSorted(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path }) {
		return migrationArtifactError(record, MigrationStepBackup, backupManifestRelativePath(manifest.MigrationID), "backup manifest entries are not stably sorted", nil)
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if _, exists := seen[entry.Path]; exists {
			return migrationArtifactError(record, MigrationStepBackup, entry.Path, "backup manifest contains a duplicate path", nil)
		}
		seen[entry.Path] = struct{}{}
		if _, err := ValidateRelativePath(entry.Path, PathOptions{Intent: PathIntentExisting}); err != nil {
			return migrationArtifactError(record, MigrationStepBackup, entry.Path, "backup manifest contains an invalid source path", err)
		}
		if !entry.Exists {
			if entry.NodeType != BackupNodeMissing || entry.BackupPath != "" || entry.Size != 0 || entry.SHA256 != "" {
				return migrationArtifactError(record, MigrationStepBackup, entry.Path, "missing backup entry contains file evidence", nil)
			}
			continue
		}
		if entry.NodeType != BackupNodeFile || !validSHA256(entry.SHA256) || entry.Size < 0 || entry.BackupPath == "" {
			return migrationArtifactError(record, MigrationStepBackup, entry.Path, "backup file entry is incomplete", nil)
		}
		prefix := MigrationRootRelativePath + "/" + manifest.MigrationID + "/backup/files/"
		if _, err := ValidateRelativePath(entry.BackupPath, PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}); err != nil || entry.BackupPath != prefix+entry.Path {
			return migrationArtifactError(record, MigrationStepBackup, entry.BackupPath, "backup file reference escapes its migration backup", nil)
		}
		if entry.Mode > 0o777 {
			return migrationArtifactError(record, MigrationStepBackup, entry.Path, "backup file mode is invalid", nil)
		}
	}
	return nil
}

func decodeStrictMigrationJSON(raw []byte, limit int, target any) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return errors.New("JSON is empty, too large, or invalid UTF-8")
	}
	if _, err := validateMarkerJSON(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func migrationArtifactError(record MigrationRecord, step MigrationStep, path, message string, err error) *MigrationError {
	return &MigrationError{Code: CodeMigrationArtifactInvalid, MigrationID: record.MigrationID, State: record.State, Step: step, Path: path, Durability: DurabilityPending, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: message, Err: err}
}

func migrationHashError(record MigrationRecord, step MigrationStep, path, expected, actual, message string) *MigrationError {
	return &MigrationError{Code: CodeMigrationArtifactInvalid, MigrationID: record.MigrationID, State: record.State, Step: step, Path: path, ExpectedSHA256: expected, ActualSHA256: actual, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: message}
}

func readMigrationArtifact(workspace, rel string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	return readBoundedRootFile(root, rel, info, limit)
}
