package projection

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"denova/internal/durablefs"
	"denova/internal/workspacepath"
)

var projectionStageSidecarSuffixes = []string{"", "-journal", "-wal", "-shm"}

var projectionFinalSidecars = []string{
	filepath.Base(DatabaseRelativePath),
	filepath.Base(DatabaseRelativePath) + "-journal",
	filepath.Base(DatabaseRelativePath) + "-wal",
	filepath.Base(DatabaseRelativePath) + "-shm",
}

type projectionDataRoot struct {
	workspace *os.Root
	data      *os.Root
	identity  os.FileInfo
	path      string
}

func openProjectionDataRoot(workspace string) (*projectionDataRoot, error) {
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, fmt.Errorf("open Projection workspace root: %w", err)
	}
	_, err = workspaceRoot.Lstat(workspacepath.DataDirName)
	created := false
	if errors.Is(err, os.ErrNotExist) {
		if err := workspaceRoot.Mkdir(workspacepath.DataDirName, 0o755); err != nil {
			workspaceRoot.Close()
			return nil, fmt.Errorf("create Projection data directory: %w", err)
		}
		created = true
	} else if err != nil {
		workspaceRoot.Close()
		return nil, fmt.Errorf("inspect Projection data directory: %w", err)
	}
	pathInfo, err := workspaceRoot.Lstat(workspacepath.DataDirName)
	if err != nil {
		workspaceRoot.Close()
		return nil, fmt.Errorf("inspect Projection data directory identity: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
		workspaceRoot.Close()
		return nil, fmt.Errorf("Projection data directory is a reparse point or not a directory")
	}
	dataRoot, err := workspaceRoot.OpenRoot(workspacepath.DataDirName)
	if err != nil {
		workspaceRoot.Close()
		return nil, fmt.Errorf("open Projection data root: %w", err)
	}
	handleInfo, err := dataRoot.Stat(".")
	if err != nil || !os.SameFile(pathInfo, handleInfo) {
		dataRoot.Close()
		workspaceRoot.Close()
		if err == nil {
			err = errors.New("data root identity changed while opening")
		}
		return nil, fmt.Errorf("verify Projection data root: %w", err)
	}
	if created {
		if err := syncProjectionDirectory(workspace, nil); err != nil {
			dataRoot.Close()
			workspaceRoot.Close()
			return nil, fmt.Errorf("sync workspace after Projection data directory creation: %w", err)
		}
	}
	return &projectionDataRoot{
		workspace: workspaceRoot,
		data:      dataRoot,
		identity:  pathInfo,
		path:      filepath.Join(workspace, workspacepath.DataDirName),
	}, nil
}

func (root *projectionDataRoot) Close() error {
	if root == nil {
		return nil
	}
	var first error
	if root.data != nil {
		first = root.data.Close()
	}
	if root.workspace != nil {
		if err := root.workspace.Close(); first == nil {
			first = err
		}
	}
	return first
}

func (root *projectionDataRoot) verify() error {
	pathInfo, err := root.workspace.Lstat(workspacepath.DataDirName)
	if err != nil {
		return fmt.Errorf("recheck Projection data root path: %w", err)
	}
	handleInfo, err := root.data.Stat(".")
	if err != nil {
		return fmt.Errorf("recheck Projection data root handle: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() || !os.SameFile(root.identity, pathInfo) || !os.SameFile(root.identity, handleInfo) {
		return errors.New("Projection data root identity changed")
	}
	return nil
}

func (root *projectionDataRoot) cleanupStage() error {
	entries, err := fs.ReadDir(root.data.FS(), ".")
	if err != nil {
		return fmt.Errorf("list owned Projection stages: %w", err)
	}
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if !isOwnedProjectionStageName(name) {
			continue
		}
		info, err := root.data.Lstat(name)
		if err != nil {
			return fmt.Errorf("inspect owned Projection stage %q: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("owned Projection stage %q is not a regular file", name)
		}
		if err := root.data.Remove(name); err != nil {
			return fmt.Errorf("remove owned Projection stage %q: %w", name, err)
		}
		removed = true
	}
	if removed {
		return syncProjectionDirectory(root.path, root.identity)
	}
	return nil
}

func (root *projectionDataRoot) newStageName() (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return "", fmt.Errorf("generate Projection stage identity: %w", err)
		}
		name := projectionStagePrefix + hex.EncodeToString(entropy[:]) + projectionStageSuffix
		_, err := root.data.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect Projection stage candidate %q: %w", name, err)
		}
	}
	return "", errors.New("cannot allocate a unique Projection stage name")
}

func isOwnedProjectionStageName(name string) bool {
	base := name
	for _, suffix := range projectionStageSidecarSuffixes[1:] {
		if len(base) > len(suffix) && base[len(base)-len(suffix):] == suffix {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	if len(base) != len(projectionStagePrefix)+32+len(projectionStageSuffix) ||
		base[:len(projectionStagePrefix)] != projectionStagePrefix ||
		base[len(base)-len(projectionStageSuffix):] != projectionStageSuffix {
		return false
	}
	encoded := base[len(projectionStagePrefix) : len(base)-len(projectionStageSuffix)]
	decoded, err := hex.DecodeString(encoded)
	return err == nil && len(decoded) == 16
}

func (root *projectionDataRoot) syncStage(stageName string) (os.FileInfo, error) {
	if !isOwnedProjectionStageName(stageName) {
		return nil, fmt.Errorf("Projection stage name is not owned: %q", stageName)
	}
	file, err := root.data.OpenFile(stageName, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open Projection stage for sync: %w", err)
	}
	info, statErr := file.Stat()
	if statErr == nil && !info.Mode().IsRegular() {
		statErr = errors.New("Projection stage is not a regular file")
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if statErr != nil {
		return nil, statErr
	}
	if syncErr != nil {
		return nil, fmt.Errorf("sync Projection stage: %w", syncErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close Projection stage sync handle: %w", closeErr)
	}
	return info, nil
}

func (root *projectionDataRoot) activate(stageName string, stageInfo os.FileInfo, hooks Hooks) (bool, []string, error) {
	if !isOwnedProjectionStageName(stageName) {
		return false, nil, fmt.Errorf("Projection stage name is not owned: %q", stageName)
	}
	if err := root.verify(); err != nil {
		return false, nil, err
	}
	sidecars, err := root.existingFinalSidecars()
	if err != nil || len(sidecars) != 0 {
		return false, nil, errors.Join(fmt.Errorf("Projection final sidecars appeared before activation: %v", sidecars), err)
	}
	stagePath := filepath.Join(root.path, stageName)
	finalPath := filepath.Join(root.path, filepath.Base(DatabaseRelativePath))
	if err := replaceProjectionFile(stagePath, finalPath); err != nil {
		return false, nil, fmt.Errorf("atomically activate Projection: %w", err)
	}
	if err := hooks.afterNamespaceReplace(); err != nil {
		return root.quarantineActivatedProjection(fmt.Errorf("after Projection namespace replacement: %w", err), hooks)
	}
	finalInfo, err := root.data.Stat(filepath.Base(DatabaseRelativePath))
	if err != nil {
		return root.quarantineActivatedProjection(fmt.Errorf("inspect activated Projection: %w", err), hooks)
	}
	if !finalInfo.Mode().IsRegular() || !os.SameFile(stageInfo, finalInfo) {
		return root.quarantineActivatedProjection(errors.New("activated Projection identity does not match the validated sibling"), hooks)
	}
	sidecars, err = root.existingFinalSidecars()
	if err != nil || len(sidecars) != 0 {
		return root.quarantineActivatedProjection(errors.Join(fmt.Errorf("Projection final sidecars appeared during activation: %v", sidecars), err), hooks)
	}
	return true, nil, nil
}

func (root *projectionDataRoot) quarantineActivatedProjection(cause error, hooks Hooks) (bool, []string, error) {
	paths, err := root.quarantineProjection(ReasonIntegrityFailed, hooks)
	if err != nil {
		return true, paths, errors.Join(cause, fmt.Errorf("quarantine activated Projection: %w", err))
	}
	return true, paths, fmt.Errorf("%w; activated Projection was quarantined", cause)
}

func (root *projectionDataRoot) quarantineVisibleSidecars(hooks Hooks) ([]string, error) {
	sidecars, err := root.existingFinalSidecars()
	if err == nil && len(sidecars) == 0 {
		return nil, nil
	}
	cause := errors.Join(fmt.Errorf("Projection final sidecars appeared after activation: %v", sidecars), err)
	paths, quarantineErr := root.quarantineProjection(ReasonIntegrityFailed, hooks)
	if quarantineErr != nil {
		return paths, errors.Join(cause, fmt.Errorf("quarantine activated Projection: %w", quarantineErr))
	}
	return paths, cause
}

func (root *projectionDataRoot) existingFinalSidecars() ([]string, error) {
	result := make([]string, 0, len(projectionFinalSidecars)-1)
	var issues error
	for _, name := range projectionFinalSidecars[1:] {
		info, err := root.data.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			result = append(result, name)
			issues = errors.Join(issues, fmt.Errorf("inspect Projection sidecar %q: %w", name, err))
			continue
		}
		result = append(result, name)
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			issues = errors.Join(issues, fmt.Errorf("Projection sidecar %q is not a regular file", name))
		}
	}
	return result, issues
}

// quarantineProjection renames a database and any SQLite sidecars inside the
// bound data root. It never copies a live SQLite file and never treats retained
// diagnostics as a recovery source.
func (root *projectionDataRoot) quarantineProjection(reason Reason, hooks Hooks) ([]string, error) {
	if err := root.verify(); err != nil {
		return nil, err
	}
	base, err := root.availableQuarantineBase(reason)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(projectionFinalSidecars))
	for index, source := range projectionFinalSidecars {
		info, err := root.data.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return paths, fmt.Errorf("quarantine Projection after %d durable renames: inspect %q: %w", len(paths), source, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return paths, fmt.Errorf("quarantine Projection after %d durable renames: diagnostic %q is not a regular file", len(paths), source)
		}
		destination := base
		if index > 0 {
			destination += source[len(filepath.Base(DatabaseRelativePath)):]
		}
		if err := hooks.beforeQuarantineRename(source, destination); err != nil {
			return paths, fmt.Errorf("quarantine Projection after %d durable renames: before rename %q to %q: %w", len(paths), source, destination, err)
		}
		moved, err := renameProjectionDiagnostic(root.data, root.path, source, destination, info)
		if moved {
			paths = append(paths, filepath.Join(root.path, destination))
		}
		if err != nil {
			return paths, fmt.Errorf("quarantine Projection after %d durable renames: rename %q to %q: %w", len(paths), source, destination, err)
		}
		if !moved {
			return paths, fmt.Errorf("quarantine Projection rename %q to %q reported no movement", source, destination)
		}
		destinationInfo, err := root.data.Lstat(destination)
		if err != nil || !destinationInfo.Mode().IsRegular() || !os.SameFile(info, destinationInfo) {
			if err == nil {
				err = errors.New("renamed diagnostic identity does not match its source")
			}
			return paths, fmt.Errorf("verify Projection diagnostic rename %q to %q: %w", source, destination, err)
		}
		if err := root.verify(); err != nil {
			return paths, fmt.Errorf("verify Projection data root after diagnostic rename: %w", err)
		}
		if err := syncProjectionDirectory(root.path, root.identity); err != nil {
			return paths, fmt.Errorf("sync Projection diagnostic rename %q to %q: %w", source, destination, err)
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("quarantine Projection database and sidecars are missing")
	}
	return paths, nil
}

func (root *projectionDataRoot) availableQuarantineBase(reason Reason) (string, error) {
	prefix := filepath.Base(DatabaseRelativePath) + "-quarantine-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + string(reason)
	for attempt := 0; attempt < 100; attempt++ {
		candidate := prefix
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", prefix, attempt)
		}
		available := true
		for index, source := range projectionFinalSidecars {
			destination := candidate
			if index > 0 {
				destination += source[len(filepath.Base(DatabaseRelativePath)):]
			}
			_, err := root.data.Lstat(destination)
			if err == nil {
				available = false
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("inspect Projection quarantine destination %q: %w", destination, err)
			}
		}
		if available {
			return candidate, nil
		}
	}
	return "", errors.New("cannot allocate a unique Projection quarantine name")
}

func syncProjectionDirectory(path string, expected os.FileInfo) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	info, statErr := directory.Stat()
	if statErr == nil && expected != nil && !os.SameFile(expected, info) {
		statErr = errors.New("directory identity changed before sync")
	}
	syncErr := durablefs.SyncDirectory(directory)
	closeErr := directory.Close()
	if statErr != nil {
		return statErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
