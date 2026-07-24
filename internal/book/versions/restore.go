package versions

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"denova/internal/durablefs"
)

type restorePlanner struct {
	service *Service
}

func (s *Service) restorePlanner() restorePlanner {
	return restorePlanner{service: s}
}

func (s *Service) RestorePlan(id string, paths []string, settings VersionAutoSettings) (VersionRestorePlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restorePlanner().PlanLocked(id, paths, settings)
}

func (s *Service) Restore(id string, settings VersionAutoSettings) (VersionRestoreResult, error) {
	return s.RestoreWithPaths(id, nil, settings)
}

func (s *Service) RestoreWithPaths(id string, paths []string, settings VersionAutoSettings) (VersionRestoreResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restorePlanner().ApplyLocked(id, paths, settings)
}

func (p restorePlanner) PlanLocked(id string, paths []string, settings VersionAutoSettings) (VersionRestorePlan, error) {
	s := p.service
	version, err := s.findVersion(id)
	if err != nil {
		return VersionRestorePlan{}, err
	}
	scope, normalizedPaths, err := normalizeRestorePaths(s.workspace, paths)
	if err != nil {
		return VersionRestorePlan{}, err
	}
	settings = normalizeVersionAutoSettings(settings)
	status, err := s.statusLocked(settings)
	if err != nil {
		return VersionRestorePlan{}, err
	}
	changes, err := s.restoreChanges(version, scope, normalizedPaths)
	if err != nil {
		return VersionRestorePlan{}, err
	}

	planPaths := normalizedPaths
	if scope == VersionRestoreScopeWorkspace {
		planPaths = make([]string, 0, len(changes))
		for _, change := range changes {
			planPaths = append(planPaths, change.Path)
		}
	}
	warnings := []string{}
	if len(changes) == 0 {
		warnings = append(warnings, "目标版本与当前工作区一致，无需恢复")
	}
	if scope == VersionRestoreScopePaths {
		warnings = append(warnings, "文件恢复会作为未保存变更应用，不会移动当前版本")
	}

	willCreateBackup := scope == VersionRestoreScopeWorkspace && !status.Clean
	backupMessage := ""
	if willCreateBackup {
		backupMessage = defaultVersionMessage(VersionSourceRollbackBackup)
	}
	return VersionRestorePlan{
		Target:           version,
		Scope:            scope,
		Paths:            planPaths,
		Changes:          changes,
		WillCreateBackup: willCreateBackup,
		CurrentDirty:     !status.Clean,
		BackupMessage:    backupMessage,
		Warnings:         warnings,
	}, nil
}

func (p restorePlanner) ApplyLocked(id string, paths []string, settings VersionAutoSettings) (VersionRestoreResult, error) {
	s := p.service
	plan, err := p.PlanLocked(id, paths, settings)
	if err != nil {
		return VersionRestoreResult{}, err
	}

	var backupVersion *VersionEntry
	if plan.Scope == VersionRestoreScopeWorkspace && plan.CurrentDirty {
		backup, err := s.createLocked(defaultVersionMessage(VersionSourceRollbackBackup), VersionSourceRollbackBackup, settings)
		if err != nil && !errors.Is(err, ErrVersionClean) {
			return VersionRestoreResult{}, fmt.Errorf("创建回滚前自动备份失败: %w", err)
		}
		backupVersion = backup.Version
	}

	if plan.Scope == VersionRestoreScopeWorkspace {
		if err := s.restoreCommitToWorkspace(plan.Target.ID); err != nil {
			return VersionRestoreResult{}, err
		}
	} else if err := s.restorePathsFromCommit(plan.Target.ID, plan.Paths); err != nil {
		return VersionRestoreResult{}, err
	}

	nextStatus, statusErr := s.statusLocked(settings)
	target := plan.Target
	restoredPaths := make([]string, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		restoredPaths = append(restoredPaths, change.Path)
	}
	message := "已恢复到所选版本"
	if plan.Scope == VersionRestoreScopePaths {
		message = "已恢复所选文件"
	}
	result := VersionRestoreResult{
		Message:       message,
		Target:        target,
		Version:       &target,
		BackupVersion: backupVersion,
		RestoredPaths: restoredPaths,
		Scope:         plan.Scope,
	}
	if statusErr == nil {
		result.Status = &nextStatus
	}
	return result, nil
}

func normalizeRestorePaths(workspace string, paths []string) (string, []string, error) {
	if len(paths) == 0 {
		return VersionRestoreScopeWorkspace, []string{}, nil
	}
	seen := map[string]bool{}
	normalized := []string{}
	for _, path := range paths {
		if _, err := safeVisiblePath(workspace, path); err != nil {
			return "", nil, err
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(path))))
		if seen[clean] {
			continue
		}
		seen[clean] = true
		normalized = append(normalized, clean)
	}
	if len(normalized) == 0 {
		return "", nil, errors.New("恢复路径不能为空")
	}
	sort.Strings(normalized)
	return VersionRestoreScopePaths, normalized, nil
}

func (s *Service) restoreChanges(version VersionEntry, scope string, paths []string) ([]VersionRestoreChange, error) {
	currentFiles, err := s.collectVisibleFiles()
	if err != nil {
		return nil, err
	}
	current := make(map[string]versionFileData, len(currentFiles))
	for _, file := range currentFiles {
		current[file.Path] = file
	}
	target, err := s.commitFiles(version.ID)
	if err != nil {
		return nil, err
	}

	candidates := map[string]bool{}
	if scope == VersionRestoreScopePaths {
		for _, path := range paths {
			candidates[path] = true
		}
	} else {
		for path := range current {
			candidates[path] = true
		}
		for path := range target {
			candidates[path] = true
		}
	}

	sorted := make([]string, 0, len(candidates))
	for path := range candidates {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)

	changes := []VersionRestoreChange{}
	for _, path := range sorted {
		currentFile, currentOK := current[path]
		targetFile, targetOK := target[path]
		if !currentOK && !targetOK {
			continue
		}
		if currentOK && targetOK && currentFile.Hash == targetFile.Hash {
			continue
		}
		status := "modified"
		switch {
		case !targetOK:
			status = "deleted"
		case !currentOK:
			status = "added"
		}
		text := true
		if currentOK && !currentFile.Text {
			text = false
		}
		if targetOK && !targetFile.Text {
			text = false
		}
		changes = append(changes, VersionRestoreChange{
			Path:               path,
			Status:             status,
			Text:               text,
			Binary:             !text,
			MissingInVersion:   !targetOK,
			MissingInWorkspace: !currentOK,
		})
	}
	return changes, nil
}

func (s *Service) restorePathsFromCommit(id string, paths []string) error {
	target, err := s.commitFiles(id)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return err
	}
	defer root.Close()

	plan := make([]selectiveRestoreEntry, 0, len(paths))
	for _, restorePath := range paths {
		if _, err := safeVisiblePath(s.workspace, restorePath); err != nil {
			return err
		}
		rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(restorePath)))
		if err := validateSelectiveRestoreParent(root, rel); err != nil {
			return fmt.Errorf("恢复路径 %s 的父目录无效: %w", rel, err)
		}
		entry := selectiveRestoreEntry{path: rel}
		if _, ok := target[rel]; ok {
			entry.targetExists = true
			entry.targetData, err = s.readCommitFile(id, rel)
			if err != nil {
				return err
			}
		}
		info, statErr := root.Lstat(filepath.FromSlash(rel))
		switch {
		case statErr == nil:
			if !info.Mode().IsRegular() {
				return fmt.Errorf("恢复路径不是普通文件: %s", rel)
			}
			entry.beforeExists = true
			entry.beforeMode = info.Mode().Perm()
			entry.beforeData, err = root.ReadFile(filepath.FromSlash(rel))
			if err != nil {
				return fmt.Errorf("读取恢复前文件 %s 失败: %w", rel, err)
			}
		case errors.Is(statErr, os.ErrNotExist):
		default:
			return statErr
		}
		plan = append(plan, entry)
	}

	applied := make([]selectiveRestoreEntry, 0, len(plan))
	rollback := func(cause error) error {
		var rollbackErr error
		for i := len(applied) - 1; i >= 0; i-- {
			entry := applied[i]
			if entry.beforeExists {
				if err := atomicWriteRestoreFile(root, entry.path, entry.beforeData, entry.beforeMode); err != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("回滚 %s 失败: %w", entry.path, err))
				}
			} else if err := root.Remove(filepath.FromSlash(entry.path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("回滚删除 %s 失败: %w", entry.path, err))
			}
		}
		if rollbackErr != nil {
			return fmt.Errorf("%w; 选择性恢复回滚失败: %v", cause, rollbackErr)
		}
		return cause
	}

	for _, entry := range plan {
		relPath := filepath.FromSlash(entry.path)
		// Add the entry before mutation so even a post-rename fsync failure
		// restores the file that may already have changed.
		applied = append(applied, entry)
		if !entry.targetExists {
			if err := root.Remove(relPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return rollback(err)
			}
			continue
		}
		if err := atomicWriteRestoreFile(root, entry.path, entry.targetData, 0o644); err != nil {
			return rollback(err)
		}
	}
	if err := s.removeEmptyVisibleDirs(); err != nil {
		return rollback(err)
	}
	return nil
}

type selectiveRestoreEntry struct {
	path         string
	targetData   []byte
	targetExists bool
	beforeData   []byte
	beforeMode   os.FileMode
	beforeExists bool
}

func validateSelectiveRestoreParent(root *os.Root, rel string) error {
	parent := path.Dir(filepath.ToSlash(rel))
	if parent == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(parent, "/") {
		current = path.Join(current, component)
		info, err := root.Lstat(filepath.FromSlash(current))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s 不是目录", current)
		}
		child, err := root.OpenRoot(filepath.FromSlash(current))
		if err != nil {
			return err
		}
		if err := child.Close(); err != nil {
			return err
		}
	}
	return nil
}

func atomicWriteRestoreFile(root *os.Root, rel string, data []byte, mode os.FileMode) error {
	parent := path.Dir(filepath.ToSlash(rel))
	if parent != "." {
		if err := root.MkdirAll(filepath.FromSlash(parent), 0o755); err != nil {
			return err
		}
	}
	var random [8]byte
	if _, err := cryptorand.Read(random[:]); err != nil {
		return err
	}
	tempRel := path.Join(parent, fmt.Sprintf(".%s.denova-%x.tmp", path.Base(rel), random[:]))
	tempPath := filepath.FromSlash(tempRel)
	targetPath := filepath.FromSlash(rel)
	file, err := root.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = root.Remove(tempPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Rename(tempPath, targetPath); err != nil {
		return err
	}
	removeTemp = false
	if parentFile, err := root.Open(filepath.FromSlash(parent)); err == nil {
		defer parentFile.Close()
		if err := durablefs.SyncDirectory(parentFile); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) removeVisibleFilesAbsentFromCommit(id string) error {
	target, err := s.commitFiles(id)
	if err != nil {
		return err
	}
	files, err := s.collectVisibleFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, ok := target[file.Path]; ok {
			continue
		}
		if err := os.Remove(file.Abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return s.removeEmptyVisibleDirs()
}

type protectedEntryMove struct {
	rel       string
	backupRel string
}

func (s *Service) withProtectedExcludedWorkspaceDirs(fn func() error) error {
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return err
	}
	defer root.Close()

	entries, err := protectedExcludedWorkspaceEntries(s.workspace)
	if err != nil {
		return err
	}
	tempRel := ""
	if len(entries) > 0 {
		tempRel, err = createProtectedRestoreTemp(root)
		if err != nil {
			return err
		}
	}

	moves := make([]protectedEntryMove, 0, len(entries))
	for index, rel := range entries {
		backupRel := path.Join(tempRel, fmt.Sprintf("%04d", index))
		if err := root.Rename(filepath.FromSlash(rel), filepath.FromSlash(backupRel)); err != nil {
			restoreErr := restoreProtectedEntryMoves(root, moves)
			if restoreErr != nil {
				return fmt.Errorf("移动版本排除项 %s 失败: %w; 已移动项恢复失败: %v; 恢复数据保留在 %s", rel, err, restoreErr, tempRel)
			}
			_ = root.RemoveAll(filepath.FromSlash(tempRel))
			return fmt.Errorf("移动版本排除项 %s 失败: %w", rel, err)
		}
		moves = append(moves, protectedEntryMove{rel: rel, backupRel: backupRel})
	}

	runErr := fn()
	clearErr := removeExcludedWorkspaceEntries(root, s.workspace)
	restoreErr := restoreProtectedEntryMoves(root, moves)
	if restoreErr == nil && tempRel != "" {
		if cleanupErr := root.RemoveAll(filepath.FromSlash(tempRel)); cleanupErr != nil {
			restoreErr = cleanupErr
		}
	}
	if restoreErr != nil {
		restoreErr = fmt.Errorf("恢复版本排除项失败，恢复数据保留在 %s: %w", tempRel, restoreErr)
	}
	return errors.Join(runErr, clearErr, restoreErr)
}

func createProtectedRestoreTemp(root *os.Root) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		var random [8]byte
		if _, err := cryptorand.Read(random[:]); err != nil {
			return "", err
		}
		rel := path.Join(".git", fmt.Sprintf(".denova-version-restore-%x", random[:]))
		if err := root.Mkdir(filepath.FromSlash(rel), 0o700); err == nil {
			return rel, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("无法创建唯一的版本恢复临时目录")
}

func protectedExcludedWorkspaceEntries(workspace string) ([]string, error) {
	entries := make([]string, 0)
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == workspace {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		policy := versionPolicyForRelPath(rel)
		if !policy.excluded {
			return nil
		}
		if entry.IsDir() && !policy.excludesDescendants {
			return nil
		}
		entries = append(entries, rel)
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	return entries, nil
}

func removeExcludedWorkspaceEntries(root *os.Root, workspace string) error {
	entries, err := protectedExcludedWorkspaceEntries(workspace)
	if err != nil {
		return err
	}
	var removeErr error
	for _, rel := range entries {
		if err := root.RemoveAll(filepath.FromSlash(rel)); err != nil {
			removeErr = errors.Join(removeErr, fmt.Errorf("清理版本排除项 %s 失败: %w", rel, err))
		}
	}
	return removeErr
}

func restoreProtectedEntryMoves(root *os.Root, moves []protectedEntryMove) error {
	var restoreErr error
	for i := len(moves) - 1; i >= 0; i-- {
		move := moves[i]
		if err := root.RemoveAll(filepath.FromSlash(move.rel)); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("清理 %s 失败: %w", move.rel, err))
			continue
		}
		parent := path.Dir(move.rel)
		if parent != "." {
			if err := root.MkdirAll(filepath.FromSlash(parent), 0o755); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("创建 %s 的父目录失败: %w", move.rel, err))
				continue
			}
		}
		if err := root.Rename(filepath.FromSlash(move.backupRel), filepath.FromSlash(move.rel)); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("恢复 %s 失败: %w", move.rel, err))
		}
	}
	return restoreErr
}

func (s *Service) removeEmptyVisibleDirs() error {
	dirs := []string{}
	err := filepath.WalkDir(s.workspace, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == s.workspace {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.workspace, path)
		if err != nil {
			return nil
		}
		policy := versionPolicyForRelPath(filepath.ToSlash(rel))
		if policy.excluded && policy.excludesDescendants {
			return filepath.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.SliceStable(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			continue
		}
		if len(entries) > 0 {
			continue
		}
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
				continue
			}
			return err
		}
	}
	return nil
}
