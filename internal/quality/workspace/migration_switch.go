package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const SwitchBoundarySameFilesystemNamespaceRename = "same_filesystem_namespace_rename"

type migrationCheckpointError struct {
	point migrationFaultPoint
	err   error
}

func (err *migrationCheckpointError) Error() string { return string(err.point) }
func (err *migrationCheckpointError) Unwrap() error { return err.err }

func buildSwitchIntent(workspace string, record MigrationRecord) (MigrationSwitchIntent, error) {
	if record.Backup == nil || record.Stage == nil {
		return MigrationSwitchIntent{}, migrationArtifactError(record, MigrationStepPrepareSwitch, "switch", "switch intent requires durable backup and stage references", nil)
	}
	manifest, err := loadStageManifest(workspace, record)
	if err != nil {
		return MigrationSwitchIntent{}, err
	}
	publishedHash := record.Stage.SHA256
	if record.WorkspaceKind == WorkspaceKindCurrent {
		if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "workspace-schema.json" {
			return MigrationSwitchIntent{}, migrationArtifactError(record, MigrationStepPrepareSwitch, "stage", "current adoption stage must contain exactly one marker", nil)
		}
		publishedHash = manifest.Entries[0].SHA256
	}
	return MigrationSwitchIntent{
		SourceRoot:      record.CanonicalSourceRoot,
		TargetRoot:      record.CanonicalTargetRoot,
		BackupManifest:  *record.Backup,
		Stage:           *record.Stage,
		PublishedEntry:  manifest.PublishedEntry,
		PublishedSHA256: publishedHash,
		Boundary:        SwitchBoundarySameFilesystemNamespaceRename,
		NextAction:      MigrationNextSwitch,
	}, nil
}

func performNamespaceSwitch(workspace string, record MigrationRecord, checkpoint func(migrationFaultPoint) error) (bool, error) {
	if record.Switch == nil || record.Switch.Boundary != SwitchBoundarySameFilesystemNamespaceRename {
		return false, migrationArtifactError(record, MigrationStepSwitch, "switch", "durable same-filesystem switch intent is missing", nil)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return false, migrationArtifactError(record, MigrationStepSwitch, "", "workspace root cannot be opened for switch", err)
	}
	defer root.Close()
	sourceRel, destinationRel, sourceParent, destinationParent := switchPaths(record)
	sourceExists, err := safeEntryExists(root, sourceRel)
	if err != nil {
		return false, migrationArtifactError(record, MigrationStepSwitch, sourceRel, "staged switch entry cannot be inspected", err)
	}
	destinationExists, err := safeEntryExists(root, destinationRel)
	if err != nil {
		return false, migrationArtifactError(record, MigrationStepSwitch, destinationRel, "live switch destination cannot be inspected", err)
	}
	if destinationExists {
		if err := verifyPublishedDestination(workspace, record, false, false); err != nil {
			return false, switchConflict(record, destinationRel, err)
		}
		return false, nil
	}
	if !sourceExists {
		return false, switchConflict(record, destinationRel, errors.New("both staged entry and live destination are absent"))
	}
	if err := root.Rename(filepath.FromSlash(sourceRel), filepath.FromSlash(destinationRel)); err != nil {
		return false, migrationArtifactError(record, MigrationStepSwitch, destinationRel, "same-filesystem namespace rename failed", err)
	}
	if checkpoint != nil {
		if err := checkpoint(faultAfterVisibleSwitch); err != nil {
			return true, &migrationCheckpointError{point: faultAfterVisibleSwitch, err: err}
		}
	}
	if err := syncRootDirectory(root, workspace, destinationParent); err != nil {
		return true, migrationArtifactError(record, MigrationStepSwitch, destinationParent, "destination parent metadata is visible but not confirmed durable", err)
	}
	if sourceParent != destinationParent {
		if err := syncRootDirectory(root, workspace, sourceParent); err != nil {
			return true, migrationArtifactError(record, MigrationStepSwitch, sourceParent, "source parent metadata is visible but not confirmed durable", err)
		}
	}
	if checkpoint != nil {
		if err := checkpoint(faultAfterSwitchParentSync); err != nil {
			return true, &migrationCheckpointError{point: faultAfterSwitchParentSync, err: err}
		}
	}
	return true, nil
}

func switchPaths(record MigrationRecord) (source, destination, sourceParent, destinationParent string) {
	stageRoot := MigrationRootRelativePath + "/" + record.MigrationID + "/stage"
	if record.WorkspaceKind == WorkspaceKindCurrent {
		source = stageRoot + "/workspace-schema.json"
		destination = MarkerRelativePath
	} else {
		source = stageRoot + "/.denova"
		destination = ".denova"
	}
	sourceParent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(source)))
	destinationParent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(destination)))
	return
}

func safeEntryExists(root *os.Root, rel string) (bool, error) {
	info, err := root.Lstat(filepath.FromSlash(rel))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s is a symbolic link or reparse point", rel)
	}
	return true, nil
}

func verifyPublishedDestination(workspace string, record MigrationRecord, allowFinalReceipt, allowCompletedMarker bool) error {
	manifest, err := loadStageManifest(workspace, record)
	if err != nil {
		return err
	}
	for _, entry := range manifest.Entries {
		livePath := entry.Path
		if record.WorkspaceKind == WorkspaceKindCurrent {
			livePath = MarkerRelativePath
		}
		if livePath == MarkerRelativePath && allowCompletedMarker {
			if !validSHA256(record.PublishedMarkerSHA256) {
				return migrationArtifactError(record, MigrationStepVerify, livePath, "completed marker hash is missing", nil)
			}
			if err := verifyLiveFileHash(workspace, livePath, -1, record.PublishedMarkerSHA256); err != nil {
				return migrationHashError(record, MigrationStepVerify, livePath, record.PublishedMarkerSHA256, liveFileHash(workspace, livePath), "completed marker differs from durable record")
			}
			continue
		}
		receiptPath := receiptRelativePath(record.MigrationID)
		if livePath == receiptPath && allowFinalReceipt {
			if err := verifyReceiptArtifact(workspace, record); err != nil {
				return err
			}
			continue
		}
		if err := verifyLiveFileHash(workspace, livePath, entry.Size, entry.SHA256); err != nil {
			return migrationHashError(record, MigrationStepVerify, livePath, entry.SHA256, liveFileHash(workspace, livePath), "published namespace differs from the staged manifest")
		}
	}
	if record.WorkspaceKind == WorkspaceKindLegacy {
		paths, err := scanPublishedNamespaceFiles(workspace, ".denova")
		if err != nil {
			return migrationArtifactError(record, MigrationStepVerify, ".denova", "published namespace cannot be enumerated exactly", err)
		}
		expected := make([]string, 0, len(manifest.Entries))
		for _, entry := range manifest.Entries {
			expected = append(expected, entry.Path)
		}
		if len(paths) != len(expected) {
			return migrationArtifactError(record, MigrationStepVerify, ".denova", "published namespace contains added or missing files", nil)
		}
		for index := range paths {
			if paths[index] != expected[index] {
				return migrationArtifactError(record, MigrationStepVerify, paths[index], "published namespace path set differs from the complete stage", nil)
			}
		}
	}
	if record.WorkspaceKind == WorkspaceKindCurrent && allowFinalReceipt && record.Receipt != nil {
		return verifyReceiptArtifact(workspace, record)
	}
	return nil
}

func scanPublishedNamespaceFiles(workspace, namespace string) ([]string, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	paths := make([]string, 0)
	err = fs.WalkDir(root.FS(), namespace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == namespace || entry.IsDir() {
			return nil
		}
		info, err := root.Lstat(filepath.FromSlash(path))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("published namespace contains unsupported node %s", path)
		}
		paths = append(paths, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func verifyLiveFileHash(workspace, rel string, expectedSize int64, expectedHash string) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("live path is not a regular file")
	}
	size, hash, changed, err := hashBoundPreviewFile(root, rel, info)
	if err != nil || changed || (expectedSize >= 0 && size != expectedSize) || hash != expectedHash {
		return fmt.Errorf("live file mismatch size=%d hash=%s changed=%t: %w", size, hash, changed, err)
	}
	return nil
}

func liveFileHash(workspace, rel string) string {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "missing_or_unreadable"
	}
	defer root.Close()
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "missing_or_unreadable"
	}
	_, hash, changed, err := hashBoundPreviewFile(root, rel, info)
	if err != nil || changed {
		return "missing_or_unreadable"
	}
	return hash
}

func loadStageManifest(workspace string, record MigrationRecord) (StageManifest, error) {
	if record.Stage == nil || !validSHA256(record.Stage.SHA256) {
		return StageManifest{}, migrationArtifactError(record, MigrationStepStage, stageManifestRelativePath(record.MigrationID), "stage manifest reference is missing", nil)
	}
	raw, err := readMigrationArtifact(workspace, record.Stage.RelativePath, maxMigrationManifestBytes)
	if err != nil {
		return StageManifest{}, migrationArtifactError(record, MigrationStepStage, record.Stage.RelativePath, "stage manifest cannot be read", err)
	}
	actual := sha256Hex(raw)
	if actual != record.Stage.SHA256 {
		return StageManifest{}, migrationHashError(record, MigrationStepStage, record.Stage.RelativePath, record.Stage.SHA256, actual, "stage manifest hash differs")
	}
	return decodeStageManifest(raw)
}

func switchConflict(record MigrationRecord, path string, err error) *MigrationError {
	expected := ""
	if record.Switch != nil {
		expected = record.Switch.PublishedSHA256
	}
	return &MigrationError{Code: CodeMigrationSwitchConflict, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepSwitch, Path: path, ExpectedSHA256: expected, ActualSHA256: liveFileHash(record.CanonicalWorkspace, path), WorkspaceMutated: true, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "live destination conflicts with the durable switch intent", Err: err}
}

func publishedOwnedPaths(record MigrationRecord) []string {
	if record.WorkspaceKind == WorkspaceKindCurrent {
		return []string{MarkerRelativePath, receiptRelativePath(record.MigrationID)}
	}
	return []string{".denova"}
}

func isOwnedPublishedPath(path string, owned []string) bool {
	for _, prefix := range owned {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
