package workspace

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const StageManifestVersionV1 = 1

// StageManifestEntry records every file that will be published at the single
// namespace boundary. The manifest itself is excluded to avoid self-reference.
type StageManifestEntry struct {
	Path     string `json:"path"`
	NodeType string `json:"node_type"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// StageManifest is hashed as the stage artifact reference.
type StageManifest struct {
	RecordVersion      int                  `json:"record_version"`
	MigrationID        string               `json:"migration_id"`
	WorkspaceKind      WorkspaceKind        `json:"workspace_kind"`
	CanonicalWorkspace string               `json:"canonical_workspace"`
	PublishedEntry     string               `json:"published_entry"`
	Entries            []StageManifestEntry `json:"entries"`
}

func stageManifestRelativePath(migrationID string) string {
	return MigrationRootRelativePath + "/" + migrationID + "/stage/manifest.json"
}

func prepareMigrationStage(preview MigrationPreview, record MigrationRecord, writerVersion string) (StageManifest, bool, error) {
	if err := verifyBackupArtifact(preview.Workspace, record); err != nil {
		return StageManifest{}, false, err
	}
	root, err := os.OpenRoot(preview.Workspace)
	if err != nil {
		return StageManifest{}, false, migrationArtifactError(record, MigrationStepStage, "", "workspace root cannot be opened for staging", err)
	}
	defer root.Close()
	stageRoot := MigrationRootRelativePath + "/" + record.MigrationID + "/stage"
	if err := ensureRootDirectoryTree(root, preview.Workspace, stageRoot, 0o700); err != nil {
		return StageManifest{}, false, migrationArtifactError(record, MigrationStepStage, stageRoot, "stage directory cannot be durably created", err)
	}
	markerRaw, err := buildMigrationMarker(record, writerVersion, MigrationVerifying)
	if err != nil {
		return StageManifest{}, false, err
	}
	mutated := false
	publishedEntry := MarkerRelativePath
	expectedEntries := make([]StageManifestEntry, 0)
	switch preview.Kind {
	case WorkspaceKindCurrent:
		markerStagePath := stageRoot + "/workspace-schema.json"
		written, err := durableRootWrite(root, preview.Workspace, markerStagePath, markerRaw, 0o600)
		mutated = mutated || written
		if err != nil {
			return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, markerStagePath, "staged marker could not be durably written", err)
		}
		expectedEntries = append(expectedEntries, stageManifestEntry("workspace-schema.json", markerRaw, 0o600))
	case WorkspaceKindLegacy:
		publishedEntry = ".denova"
		stageNamespace := stageRoot + "/.denova"
		if err := ensureRootDirectoryTree(root, preview.Workspace, stageNamespace, 0o700); err != nil {
			return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, stageNamespace, "future .denova namespace cannot be created", err)
		}
		backup, err := loadBackupManifest(preview.Workspace, record)
		if err != nil {
			return StageManifest{}, mutated, err
		}
		destinations := make(map[string]string, len(preview.Entries))
		for _, entry := range preview.Entries {
			destinations[entry.Source] = entry.Destination
		}
		for _, entry := range backup.Entries {
			if !entry.Exists {
				continue
			}
			destination, exists := destinations[entry.Path]
			if !exists || !strings.HasPrefix(destination, ".denova/") {
				return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, entry.Path, "backup entry has no authorized future destination", nil)
			}
			stagePath := stageRoot + "/" + destination
			if err := ensureRootDirectoryTree(root, preview.Workspace, filepath.ToSlash(filepath.Dir(filepath.FromSlash(stagePath))), 0o700); err != nil {
				return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, stagePath, "stage parent cannot be durably created", err)
			}
			written, err := copyVerifiedRootFile(root, preview.Workspace, entry.BackupPath, stagePath, entry.Size, entry.SHA256, os.FileMode(entry.Mode))
			mutated = mutated || written
			if err != nil {
				return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, stagePath, "verified backup bytes could not be staged", err)
			}
			expectedEntries = append(expectedEntries, StageManifestEntry{Path: destination, NodeType: "file", Mode: entry.Mode, Size: entry.Size, SHA256: entry.SHA256})
		}
		markerStagePath := stageNamespace + "/workspace-schema.json"
		written, err := durableRootWrite(root, preview.Workspace, markerStagePath, markerRaw, 0o600)
		mutated = mutated || written
		if err != nil {
			return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, markerStagePath, "future marker could not be staged", err)
		}
		expectedEntries = append(expectedEntries, stageManifestEntry(".denova/workspace-schema.json", markerRaw, 0o600))
		draftRaw, err := encodeMigrationReceipt(draftMigrationReceipt(record))
		if err != nil {
			return StageManifest{}, mutated, err
		}
		draftPath := stageNamespace + "/quality/migration-receipts/" + record.MigrationID + ".json"
		if err := ensureRootDirectoryTree(root, preview.Workspace, filepath.ToSlash(filepath.Dir(filepath.FromSlash(draftPath))), 0o700); err != nil {
			return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, draftPath, "receipt draft parent cannot be staged", err)
		}
		written, err = durableRootWrite(root, preview.Workspace, draftPath, draftRaw, 0o600)
		mutated = mutated || written
		if err != nil {
			return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, draftPath, "receipt draft could not be staged", err)
		}
		expectedEntries = append(expectedEntries, stageManifestEntry(".denova/quality/migration-receipts/"+record.MigrationID+".json", draftRaw, 0o600))
	default:
		return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, "workspace_kind", "workspace kind does not require a staged migration", nil)
	}
	entries, err := scanStageFiles(root, stageRoot)
	if err != nil {
		return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, stageRoot, "stage files cannot be hashed", err)
	}
	sort.Slice(expectedEntries, func(i, j int) bool { return expectedEntries[i].Path < expectedEntries[j].Path })
	if !equalStageEntries(entries, expectedEntries) {
		path := stageRoot + "/" + firstStageDifference(expectedEntries, entries)
		return StageManifest{}, mutated, migrationArtifactError(record, MigrationStepStage, path, "stage differs from the exact authorized publication set", nil)
	}
	manifest := StageManifest{
		RecordVersion:      StageManifestVersionV1,
		MigrationID:        record.MigrationID,
		WorkspaceKind:      preview.Kind,
		CanonicalWorkspace: record.CanonicalWorkspace,
		PublishedEntry:     publishedEntry,
		Entries:            entries,
	}
	if err := validateStageManifest(manifest); err != nil {
		return StageManifest{}, mutated, err
	}
	return manifest, mutated, nil
}

func stageManifestEntry(path string, raw []byte, mode os.FileMode) StageManifestEntry {
	return StageManifestEntry{Path: path, NodeType: "file", Mode: uint32(mode.Perm()), Size: int64(len(raw)), SHA256: sha256Hex(raw)}
}

func firstStageDifference(expected, actual []StageManifestEntry) string {
	expectedByPath := make(map[string]StageManifestEntry, len(expected))
	for _, entry := range expected {
		expectedByPath[entry.Path] = entry
	}
	for _, entry := range actual {
		if wanted, exists := expectedByPath[entry.Path]; !exists || wanted != entry {
			return entry.Path
		}
		delete(expectedByPath, entry.Path)
	}
	for _, entry := range expected {
		if _, missing := expectedByPath[entry.Path]; missing {
			return entry.Path
		}
	}
	return "manifest.json"
}

func publishStageManifest(workspace string, record MigrationRecord, manifest StageManifest) (MigrationArtifactRef, bool, error) {
	raw, err := encodeStageManifest(manifest)
	if err != nil {
		return MigrationArtifactRef{}, false, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return MigrationArtifactRef{}, false, migrationArtifactError(record, MigrationStepStage, "", "workspace root cannot be opened to publish stage manifest", err)
	}
	defer root.Close()
	rel := stageManifestRelativePath(record.MigrationID)
	written, err := durableRootWrite(root, workspace, rel, raw, 0o600)
	if err != nil {
		return MigrationArtifactRef{}, written, migrationArtifactError(record, MigrationStepStage, rel, "stage manifest publication was not durable", err)
	}
	return MigrationArtifactRef{RelativePath: rel, SHA256: sha256Hex(raw)}, written, nil
}

func verifyStageArtifact(workspace string, record MigrationRecord) error {
	if record.Stage == nil || record.Stage.RelativePath != stageManifestRelativePath(record.MigrationID) || !validSHA256(record.Stage.SHA256) {
		return migrationArtifactError(record, MigrationStepStage, stageManifestRelativePath(record.MigrationID), "stage manifest reference is missing or invalid", nil)
	}
	raw, err := readMigrationArtifact(workspace, record.Stage.RelativePath, maxMigrationManifestBytes)
	if err != nil {
		return migrationArtifactError(record, MigrationStepStage, record.Stage.RelativePath, "stage manifest is missing or unreadable", err)
	}
	actual := sha256Hex(raw)
	if actual != record.Stage.SHA256 {
		return migrationHashError(record, MigrationStepStage, record.Stage.RelativePath, record.Stage.SHA256, actual, "stage manifest hash differs from durable record")
	}
	manifest, err := decodeStageManifest(raw)
	if err != nil {
		return err
	}
	if manifest.MigrationID != record.MigrationID || manifest.CanonicalWorkspace != record.CanonicalWorkspace || manifest.WorkspaceKind != record.WorkspaceKind {
		return migrationArtifactError(record, MigrationStepStage, record.Stage.RelativePath, "stage manifest payload does not match the migration record", nil)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return migrationArtifactError(record, MigrationStepStage, "", "workspace root cannot be opened to verify stage", err)
	}
	defer root.Close()
	stageRoot := MigrationRootRelativePath + "/" + record.MigrationID + "/stage"
	actualEntries, err := scanStageFiles(root, stageRoot)
	if err != nil {
		return migrationArtifactError(record, MigrationStepStage, stageRoot, "stage cannot be rehashed", err)
	}
	if !equalStageEntries(actualEntries, manifest.Entries) {
		return migrationArtifactError(record, MigrationStepStage, stageRoot, "stage entries were added, removed, or changed", nil)
	}
	return nil
}

func scanStageFiles(root *os.Root, stageRoot string) ([]StageManifestEntry, error) {
	entries := make([]StageManifestEntry, 0)
	err := fs.WalkDir(root.FS(), stageRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(path)
		if rel == stageRoot+"/manifest.json" {
			return nil
		}
		info, err := root.Lstat(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("stage contains a non-regular file")
		}
		size, hash, changed, err := hashBoundPreviewFile(root, rel, info)
		if err != nil || changed {
			return errors.New("stage file changed while hashing")
		}
		stageRel := strings.TrimPrefix(rel, stageRoot+"/")
		entries = append(entries, StageManifestEntry{Path: stageRel, NodeType: "file", Mode: uint32(info.Mode().Perm()), Size: size, SHA256: hash})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func encodeStageManifest(manifest StageManifest) ([]byte, error) {
	if err := validateStageManifest(manifest); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func decodeStageManifest(raw []byte) (StageManifest, error) {
	var manifest StageManifest
	if err := decodeStrictMigrationJSON(raw, maxMigrationManifestBytes, &manifest); err != nil {
		return StageManifest{}, &MigrationError{Code: CodeMigrationArtifactInvalid, Step: MigrationStepStage, Durability: DurabilityDurable, Recovery: RecoveryRequired, NextAction: MigrationNextManualRecovery, Message: "stage manifest is not strict unambiguous JSON", Err: err}
	}
	if err := validateStageManifest(manifest); err != nil {
		return StageManifest{}, err
	}
	return manifest, nil
}

func validateStageManifest(manifest StageManifest) error {
	record := MigrationRecord{MigrationID: manifest.MigrationID, State: MigrationStaged}
	if manifest.RecordVersion != StageManifestVersionV1 || ValidateMigrationID(manifest.MigrationID) != nil || !filepath.IsAbs(manifest.CanonicalWorkspace) {
		return migrationArtifactError(record, MigrationStepStage, stageManifestRelativePath(manifest.MigrationID), "stage manifest identity or version is invalid", nil)
	}
	if manifest.WorkspaceKind != WorkspaceKindCurrent && manifest.WorkspaceKind != WorkspaceKindLegacy {
		return migrationArtifactError(record, MigrationStepStage, "workspace_kind", "stage manifest workspace kind is invalid", nil)
	}
	wantEntry := MarkerRelativePath
	if manifest.WorkspaceKind == WorkspaceKindLegacy {
		wantEntry = ".denova"
	}
	if manifest.PublishedEntry != wantEntry || len(manifest.Entries) == 0 || !sort.SliceIsSorted(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path }) {
		return migrationArtifactError(record, MigrationStepStage, "published_entry", "stage manifest switch entry or stable ordering is invalid", nil)
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if _, exists := seen[entry.Path]; exists {
			return migrationArtifactError(record, MigrationStepStage, entry.Path, "stage manifest contains a duplicate path", nil)
		}
		seen[entry.Path] = struct{}{}
		if _, err := ValidateRelativePath(entry.Path, PathOptions{Intent: PathIntentNew, Platform: PathPlatformWindows}); err != nil || entry.NodeType != "file" || entry.Size < 0 || !validSHA256(entry.SHA256) {
			return migrationArtifactError(record, MigrationStepStage, entry.Path, "stage manifest entry is invalid", err)
		}
	}
	return nil
}

func loadBackupManifest(workspace string, record MigrationRecord) (BackupManifest, error) {
	if err := verifyBackupArtifact(workspace, record); err != nil {
		return BackupManifest{}, err
	}
	raw, err := readMigrationArtifact(workspace, record.Backup.RelativePath, maxMigrationManifestBytes)
	if err != nil {
		return BackupManifest{}, err
	}
	return decodeBackupManifest(raw)
}

func equalStageEntries(first, second []StageManifestEntry) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

type generatedMarker struct {
	SchemaVersion int                        `json:"schema_version"`
	Reader        generatedMarkerReader      `json:"reader"`
	Writer        generatedMarkerWriter      `json:"writer"`
	Features      map[string]FeatureContract `json:"features"`
	Migration     generatedMarkerMigration   `json:"migration"`
}

type generatedMarkerReader struct {
	MinSchemaVersion int    `json:"min_schema_version"`
	MaxSchemaVersion int    `json:"max_schema_version"`
	MinDenovaVersion string `json:"min_denova_version"`
}

type generatedMarkerWriter struct {
	SchemaVersion      int    `json:"schema_version"`
	MinDenovaVersion   string `json:"min_denova_version"`
	CompatibilityRange string `json:"compatibility_range"`
	Version            string `json:"version"`
}

type generatedMarkerMigration struct {
	ID                  string              `json:"id"`
	State               MigrationState      `json:"state"`
	SourceSchemaVersion int                 `json:"source_schema_version"`
	TargetSchemaVersion int                 `json:"target_schema_version"`
	SourceDataRoot      string              `json:"source_data_root"`
	TargetDataRoot      string              `json:"target_data_root"`
	PreviewSHA256       string              `json:"preview_sha256"`
	AuthorizationSHA256 string              `json:"authorization_sha256"`
	Confirmation        ConfirmationBinding `json:"confirmation"`
	BackupManifestRef   string              `json:"backup_manifest_ref,omitempty"`
	StagingRef          string              `json:"staging_ref,omitempty"`
	ReceiptRef          string              `json:"receipt_ref,omitempty"`
	SwitchBoundary      string              `json:"switch_boundary"`
	ResumePolicy        string              `json:"resume_policy"`
	RollbackAvailable   bool                `json:"rollback_available"`
	AtomicityClaim      string              `json:"atomicity_claim"`
}

func buildMigrationMarker(record MigrationRecord, writerVersion string, state MigrationState) ([]byte, error) {
	features := make(map[string]FeatureContract, len(record.TargetFeatures))
	for _, feature := range record.TargetFeatures {
		features[feature.ID] = FeatureContract{Version: feature.Version, Required: feature.Required}
	}
	backupRef := ""
	if record.Backup != nil {
		backupRef = record.Backup.RelativePath
	}
	stageRef := ""
	if record.Stage != nil {
		stageRef = record.Stage.RelativePath
	}
	migration := generatedMarkerMigration{
		ID: record.MigrationID, State: state, SourceSchemaVersion: 0, TargetSchemaVersion: 1,
		SourceDataRoot: record.SourceRoot, TargetDataRoot: record.TargetRoot,
		PreviewSHA256: record.PreviewSHA256, AuthorizationSHA256: record.AuthorizationSHA256,
		Confirmation: record.Confirmation, BackupManifestRef: backupRef, StagingRef: stageRef,
		ReceiptRef:     ".denova/quality/migration-receipts/" + record.MigrationID + ".json",
		SwitchBoundary: SwitchBoundarySameFilesystemNamespaceRename, ResumePolicy: "idempotent_by_migration_id_step_and_content_hash",
		RollbackAvailable: record.RollbackAvailable,
		AtomicityClaim:    "namespace_switch_only_not_cross_filesystem_or_filesystem_plus_git_acid",
	}
	if state == MigrationNotRequired {
		migration.BackupManifestRef = ""
		migration.StagingRef = ""
		migration.ReceiptRef = ""
		migration.SwitchBoundary = "none"
		migration.ResumePolicy = "idempotent_marker_publication_by_authorization_hash"
		migration.RollbackAvailable = false
		migration.AtomicityClaim = "single_marker_publication_only_no_migration_switch_or_git_transaction"
	}
	marker := generatedMarker{
		SchemaVersion: 1,
		Reader:        generatedMarkerReader{MinSchemaVersion: 1, MaxSchemaVersion: 1, MinDenovaVersion: "1.0.0"},
		Writer:        generatedMarkerWriter{SchemaVersion: 1, MinDenovaVersion: "1.0.0", CompatibilityRange: WriterCompatibilityRangeV1, Version: writerVersion},
		Features:      features,
		Migration:     migration,
	}
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if _, issues := parseMarker(raw); len(issues) != 0 {
		return nil, migrationArtifactError(record, MigrationStepStage, MarkerRelativePath, "generated marker violates Workspace Schema v1", nil)
	}
	return raw, nil
}

func decodeGeneratedMarker(raw []byte) (generatedMarker, error) {
	var marker generatedMarker
	if err := decodeStrictMigrationJSON(raw, int(maxMarkerBytes), &marker); err != nil {
		return generatedMarker{}, err
	}
	if _, issues := parseMarker(raw); len(issues) != 0 {
		return generatedMarker{}, errors.New("generated marker does not satisfy Workspace Schema v1")
	}
	return marker, nil
}
