package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MigrationPreflightRequest contains the bounded facts needed by a platform
// capability probe. It carries no authority beyond the enclosing lease.
type MigrationPreflightRequest struct {
	Workspace            string
	MigrationID          string
	StageParent          string
	TargetParent         string
	TargetParentRelative string
	RequiredBytes        uint64
}

// MigrationPreflightCapabilities are all mandatory; no false value is silently
// downgraded into a weaker switch claim.
type MigrationPreflightCapabilities struct {
	AvailableBytes        uint64
	RequiredBytes         uint64
	Writable              bool
	SameFilesystem        bool
	AtomicNamespaceRename bool
	LongPaths             bool
}

func runMigrationPreflight(preview MigrationPreview, record MigrationRecord, probe func(MigrationPreflightRequest) (MigrationPreflightCapabilities, error)) error {
	required := estimateMigrationBytes(preview)
	request := MigrationPreflightRequest{
		Workspace:            preview.Workspace,
		MigrationID:          record.MigrationID,
		StageParent:          filepath.Join(preview.Workspace, MigrationRootRelativePath, record.MigrationID),
		TargetParent:         filepath.Dir(record.CanonicalTargetRoot),
		TargetParentRelative: ".",
		RequiredBytes:        required,
	}
	if record.WorkspaceKind == WorkspaceKindCurrent {
		request.TargetParent = record.CanonicalTargetRoot
		request.TargetParentRelative = record.TargetRoot
	}
	if probe == nil {
		probe = defaultMigrationPreflight
	}
	capabilities, err := probe(request)
	if err != nil {
		return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: preview.Workspace, WorkspaceMutated: true, Durability: DurabilityDurable, Recovery: RecoveryAvailable, NextAction: MigrationNextResume, Message: "filesystem capability preflight failed", Err: err}
	}
	if capabilities.RequiredBytes != required {
		return preflightCapabilityError(record, "required_bytes", fmt.Sprintf("probe returned required=%d, canonical requirement=%d", capabilities.RequiredBytes, required))
	}
	switch {
	case !capabilities.Writable:
		return preflightCapabilityError(record, "writable", "migration namespace and target are not proven writable")
	case capabilities.AvailableBytes < capabilities.RequiredBytes:
		return preflightCapabilityError(record, "available_bytes", fmt.Sprintf("available=%d required=%d", capabilities.AvailableBytes, capabilities.RequiredBytes))
	case !capabilities.SameFilesystem:
		return preflightCapabilityError(record, "same_filesystem", "stage and final namespace are not on the same filesystem")
	case !capabilities.AtomicNamespaceRename:
		return preflightCapabilityError(record, "atomic_namespace_rename", "host does not provide the required one-entry rename boundary")
	case !capabilities.LongPaths:
		return preflightCapabilityError(record, "long_paths", "host cannot represent every authorized source and destination path")
	}
	return nil
}

func defaultMigrationPreflight(request MigrationPreflightRequest) (MigrationPreflightCapabilities, error) {
	available, err := platformAvailableBytes(request.Workspace)
	if err != nil {
		return MigrationPreflightCapabilities{}, err
	}
	stageFilesystem, err := platformFilesystemIdentity(request.StageParent)
	if err != nil {
		return MigrationPreflightCapabilities{}, err
	}
	targetFilesystem, err := platformFilesystemIdentity(request.TargetParent)
	if err != nil {
		return MigrationPreflightCapabilities{}, err
	}
	writable, err := probeMigrationNamespaceWritable(request.Workspace, request.MigrationID, request.TargetParentRelative)
	if err != nil {
		return MigrationPreflightCapabilities{}, err
	}
	return MigrationPreflightCapabilities{
		AvailableBytes:        available,
		RequiredBytes:         request.RequiredBytes,
		Writable:              writable,
		SameFilesystem:        stageFilesystem == targetFilesystem,
		AtomicNamespaceRename: platformAtomicNamespaceRenameSupported(),
		LongPaths:             platformLongPathsSupported(),
	}, nil
}

func estimateMigrationBytes(preview MigrationPreview) uint64 {
	const metadataAllowance = uint64(1024 * 1024)
	total := metadataAllowance
	for _, entry := range preview.Entries {
		if entry.Size <= 0 || entry.Source == entry.Destination {
			continue
		}
		size := uint64(entry.Size)
		if total > ^uint64(0)-size*2 {
			return ^uint64(0)
		}
		total += size * 2
	}
	return total
}

func probeMigrationNamespaceWritable(workspace, migrationID, targetParent string) (bool, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return false, err
	}
	defer root.Close()
	parents := []string{MigrationRootRelativePath + "/" + migrationID, targetParent}
	for _, parent := range parents {
		if err := probeWritableDirectory(root, workspace, parent); err != nil {
			return false, err
		}
	}
	return true, nil
}

func probeWritableDirectory(root *os.Root, workspace, parent string) error {
	base := ".denova-migrate-preflight"
	if parent != "." {
		base = parent + "/" + base
	}
	first, err := siblingTempName(root, base)
	if err != nil {
		return err
	}
	second := first + ".renamed"
	for _, rel := range []string{first, second} {
		if _, err := root.Lstat(filepath.FromSlash(rel)); err == nil {
			return fmt.Errorf("preflight probe path already exists: %s", rel)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	file, err := root.OpenFile(filepath.FromSlash(first), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = file.Close()
		_ = root.Remove(filepath.FromSlash(first))
		_ = root.Remove(filepath.FromSlash(second))
	}
	defer cleanup()
	if _, err := file.Write([]byte("denova migration preflight\n")); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Rename(filepath.FromSlash(first), filepath.FromSlash(second)); err != nil {
		return err
	}
	if err := root.Remove(filepath.FromSlash(second)); err != nil {
		return err
	}
	directoryBase := ".denova-migrate-directory-preflight"
	if parent != "." {
		directoryBase = parent + "/" + directoryBase
	}
	directorySource, err := siblingTempName(root, directoryBase)
	if err != nil {
		return err
	}
	directoryDestination := directorySource + ".renamed"
	for _, rel := range []string{directorySource, directoryDestination} {
		if _, err := root.Lstat(filepath.FromSlash(rel)); err == nil {
			return fmt.Errorf("directory preflight probe path already exists: %s", rel)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	defer func() {
		_ = root.Remove(filepath.FromSlash(directorySource))
		_ = root.Remove(filepath.FromSlash(directoryDestination))
	}()
	if err := root.Mkdir(filepath.FromSlash(directorySource), 0o700); err != nil {
		return err
	}
	if err := root.Rename(filepath.FromSlash(directorySource), filepath.FromSlash(directoryDestination)); err != nil {
		return err
	}
	if err := root.Remove(filepath.FromSlash(directoryDestination)); err != nil {
		return err
	}
	if err := syncRootDirectory(root, workspace, parent); err != nil {
		return err
	}
	return nil
}

func preflightCapabilityError(record MigrationRecord, path, message string) *MigrationError {
	return &MigrationError{Code: CodeMigrationPreflight, MigrationID: record.MigrationID, State: record.State, Step: MigrationStepPreflight, Path: path, WorkspaceMutated: true, Durability: DurabilityDurable, Recovery: RecoveryAvailable, NextAction: MigrationNextResume, Message: message}
}
