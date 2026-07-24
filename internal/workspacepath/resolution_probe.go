package workspacepath

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type boundDataRoot struct {
	name     string
	handle   *os.Root
	identity os.FileInfo
}

type targetObservation struct {
	exists     bool
	meaningful bool
}

func resolveRootsOnce(workspace string, targets []string) (RootResolution, error) {
	resolution := RootResolution{
		ActiveRoot: DataDirName,
		Targets:    make([]TargetResolution, 0, len(targets)),
	}
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		return resolution, resolutionError(workspace, "open_workspace", err)
	}

	current, err := openBoundDataRoot(workspaceRoot, DataDirName)
	if err != nil {
		_ = workspaceRoot.Close()
		return resolution, err
	}
	legacy, err := openBoundDataRoot(workspaceRoot, LegacyDataDirName)
	if err != nil {
		_ = closeBoundDataRoot(current)
		_ = workspaceRoot.Close()
		return resolution, err
	}

	resolution.ActiveRoot, err = selectActiveRoot(current, legacy)
	if err == nil {
		for _, target := range targets {
			var selected string
			selected, err = selectTargetRoot(current, legacy, resolution.ActiveRoot, target)
			if err != nil {
				break
			}
			resolution.Targets = append(resolution.Targets, TargetResolution{Path: target, Root: selected})
		}
	}
	if err == nil {
		err = verifyBoundDataRoot(workspaceRoot, current)
	}
	if err == nil {
		err = verifyBoundDataRoot(workspaceRoot, legacy)
	}
	if closeErr := closeBoundDataRoot(legacy); err == nil && closeErr != nil {
		err = closeErr
	}
	if closeErr := closeBoundDataRoot(current); err == nil && closeErr != nil {
		err = closeErr
	}
	if closeErr := workspaceRoot.Close(); err == nil && closeErr != nil {
		err = resolutionError(workspace, "close_workspace", closeErr)
	}
	return resolution, err
}

func openBoundDataRoot(workspaceRoot *os.Root, name string) (*boundDataRoot, error) {
	before, err := workspaceRoot.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, resolutionError(name, "inspect_data_root", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, resolutionError(name, "inspect_data_root", fmt.Errorf("data root is a reparse point or is not a directory"))
	}

	handle, err := workspaceRoot.OpenRoot(name)
	if err != nil {
		return nil, resolutionError(name, "open_data_root", err)
	}
	bound := &boundDataRoot{name: name, handle: handle, identity: before}
	if err := verifyBoundDataRoot(workspaceRoot, bound); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return bound, nil
}

func verifyBoundDataRoot(workspaceRoot *os.Root, dataRoot *boundDataRoot) error {
	if dataRoot == nil {
		return nil
	}
	pathInfo, err := workspaceRoot.Lstat(dataRoot.name)
	if err != nil {
		return resolutionError(dataRoot.name, "verify_data_root", err)
	}
	handleInfo, err := dataRoot.handle.Stat(".")
	if err != nil {
		return resolutionError(dataRoot.name, "verify_data_root", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() || !os.SameFile(dataRoot.identity, pathInfo) || !os.SameFile(dataRoot.identity, handleInfo) {
		return resolutionError(dataRoot.name, "verify_data_root", fmt.Errorf("data root identity changed during inspection"))
	}
	return nil
}

func closeBoundDataRoot(dataRoot *boundDataRoot) error {
	if dataRoot == nil {
		return nil
	}
	if err := dataRoot.handle.Close(); err != nil {
		return resolutionError(dataRoot.name, "close_data_root", err)
	}
	return nil
}

func selectActiveRoot(current, legacy *boundDataRoot) (string, error) {
	switch {
	case current == nil && legacy == nil:
		return DataDirName, nil
	case current != nil && legacy == nil:
		return DataDirName, nil
	case current == nil && legacy != nil:
		return LegacyDataDirName, nil
	}
	legacyMeaningful, err := hasBoundWorkspaceData(legacy, ".", true)
	if err != nil {
		return DataDirName, err
	}
	currentMeaningful, err := hasBoundWorkspaceData(current, ".", true)
	if err != nil {
		return DataDirName, err
	}
	if legacyMeaningful && !currentMeaningful {
		return LegacyDataDirName, nil
	}
	return DataDirName, nil
}

func selectTargetRoot(current, legacy *boundDataRoot, activeRoot, target string) (string, error) {
	if !fs.ValidPath(target) || target == "." || strings.Contains(target, `\`) {
		return activeRoot, resolutionError(target, "validate_target", fmt.Errorf("target must be a clean forward-slash relative path"))
	}
	currentTarget, err := observeTarget(current, target)
	if err != nil {
		return activeRoot, err
	}
	legacyTarget, err := observeTarget(legacy, target)
	if err != nil {
		return activeRoot, err
	}

	if current == nil && legacy == nil {
		return DataDirName, nil
	}
	if current != nil && legacy == nil {
		return DataDirName, nil
	}
	if current == nil && legacy != nil {
		return LegacyDataDirName, nil
	}
	if legacyTarget.exists && !currentTarget.exists {
		return LegacyDataDirName, nil
	}
	if legacyTarget.exists && currentTarget.exists && legacyTarget.meaningful && !currentTarget.meaningful {
		return LegacyDataDirName, nil
	}
	return activeRoot, nil
}

func observeTarget(dataRoot *boundDataRoot, target string) (targetObservation, error) {
	if dataRoot == nil {
		return targetObservation{}, nil
	}
	info, exists, err := lookupBoundPath(dataRoot, target)
	if err != nil || !exists {
		return targetObservation{exists: exists}, err
	}
	if info.IsDir() {
		meaningful, err := hasBoundWorkspaceData(dataRoot, target, false)
		return targetObservation{exists: true, meaningful: meaningful}, err
	}
	meaningful, err := boundFileHasWorkspaceData(dataRoot, target, info)
	return targetObservation{exists: true, meaningful: meaningful}, err
}

func lookupBoundPath(dataRoot *boundDataRoot, target string) (os.FileInfo, bool, error) {
	segments := strings.Split(target, "/")
	identities := make([]struct {
		path string
		info os.FileInfo
	}, 0, len(segments))
	for index := range segments {
		child := strings.Join(segments[:index+1], "/")
		info, err := dataRoot.handle.Lstat(filepath.FromSlash(child))
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, resolutionError(dataRoot.name+"/"+child, "inspect_target", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, true, resolutionError(dataRoot.name+"/"+child, "inspect_target", fmt.Errorf("target traverses a reparse point"))
		}
		if index < len(segments)-1 && !info.IsDir() {
			return nil, true, resolutionError(dataRoot.name+"/"+child, "inspect_target", fmt.Errorf("non-directory target component"))
		}
		if index == len(segments)-1 && !info.IsDir() && !info.Mode().IsRegular() {
			return nil, true, resolutionError(dataRoot.name+"/"+child, "inspect_target", fmt.Errorf("target is not a regular file or directory"))
		}
		identities = append(identities, struct {
			path string
			info os.FileInfo
		}{path: child, info: info})
	}
	for _, identity := range identities {
		after, err := dataRoot.handle.Lstat(filepath.FromSlash(identity.path))
		if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(identity.info, after) {
			if err == nil {
				err = fmt.Errorf("target component identity changed during inspection")
			}
			return nil, true, resolutionError(dataRoot.name+"/"+identity.path, "verify_target", err)
		}
	}
	return identities[len(identities)-1].info, true, nil
}

func hasBoundWorkspaceData(dataRoot *boundDataRoot, base string, ignoreEphemeral bool) (bool, error) {
	found := false
	err := fs.WalkDir(dataRoot.handle.FS(), base, func(child string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if child == base {
			return nil
		}
		info, err := dataRoot.handle.Lstat(filepath.FromSlash(child))
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("reparse point is not safe for root selection")
		}
		if info.IsDir() {
			if ignoreEphemeral && isBoundEphemeralRoot(base, child) {
				return fs.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported filesystem node")
		}
		meaningful, err := boundFileHasWorkspaceData(dataRoot, child, info)
		if err != nil {
			return err
		}
		if meaningful {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, resolutionError(dataRoot.name+"/"+strings.TrimPrefix(base, "./"), "scan_data_root", err)
	}
	return found, nil
}

func isBoundEphemeralRoot(base, child string) bool {
	rel := child
	if base == "." {
		rel = strings.TrimPrefix(child, "./")
	} else {
		prefix := strings.TrimSuffix(base, "/") + "/"
		if !strings.HasPrefix(child, prefix) {
			return false
		}
		rel = strings.TrimPrefix(child, prefix)
	}
	return rel == "runs" || rel == "checkpoints"
}

func boundFileHasWorkspaceData(dataRoot *boundDataRoot, relative string, info os.FileInfo) (bool, error) {
	if !info.Mode().IsRegular() {
		return false, resolutionError(dataRoot.name+"/"+relative, "inspect_file", fmt.Errorf("file is not regular"))
	}
	if info.Size() == 0 || path.Base(relative) == ".DS_Store" {
		return false, nil
	}
	if path.Base(relative) != "items.json" || path.Base(path.Dir(relative)) != "lore" || info.Size() > 256*1024 {
		return true, nil
	}
	data, err := readBoundFile(dataRoot, relative, info, 256*1024)
	if err != nil {
		return false, err
	}
	return !isEmptyLoreItems(data), nil
}

func readBoundFile(dataRoot *boundDataRoot, relative string, expected os.FileInfo, limit int64) ([]byte, error) {
	file, err := dataRoot.handle.Open(filepath.FromSlash(relative))
	if err != nil {
		return nil, resolutionError(dataRoot.name+"/"+relative, "open_file", err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		_ = file.Close()
		if err == nil {
			err = fmt.Errorf("file identity changed before read")
		}
		return nil, resolutionError(dataRoot.name+"/"+relative, "verify_file", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	afterRead, statErr := file.Stat()
	closeErr := file.Close()
	pathInfo, pathErr := dataRoot.handle.Lstat(filepath.FromSlash(relative))
	if readErr != nil {
		return nil, resolutionError(dataRoot.name+"/"+relative, "read_file", readErr)
	}
	if statErr != nil || pathErr != nil || !os.SameFile(expected, afterRead) || !os.SameFile(expected, pathInfo) {
		if statErr != nil {
			err = statErr
		} else if pathErr != nil {
			err = pathErr
		} else {
			err = fmt.Errorf("file identity changed during read")
		}
		return nil, resolutionError(dataRoot.name+"/"+relative, "verify_file", err)
	}
	if closeErr != nil {
		return nil, resolutionError(dataRoot.name+"/"+relative, "close_file", closeErr)
	}
	if int64(len(data)) > limit {
		return data, nil
	}
	return data, nil
}

func isEmptyLoreItems(data []byte) bool {
	if strings.TrimSpace(string(data)) == "" {
		return true
	}
	var collection struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &collection); err == nil && collection.Items != nil {
		rawItems := strings.TrimSpace(string(collection.Items))
		if rawItems == "" || rawItems == "null" {
			return true
		}
		var items []json.RawMessage
		if err := json.Unmarshal(collection.Items, &items); err == nil {
			return len(items) == 0
		}
		return false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err == nil {
		return len(items) == 0
	}
	return false
}

func resolutionError(relative, operation string, err error) *ResolutionError {
	return &ResolutionError{Path: filepath.ToSlash(relative), Operation: operation, Err: err}
}
