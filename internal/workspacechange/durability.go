package workspacechange

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type mutationStage string

const (
	mutationStageUnchanged mutationStage = "unchanged"
	mutationStageVisible   mutationStage = "visible"
	mutationStageDurable   mutationStage = "durable"

	visibleWriteStageBeforeReplace = "before_replace"
	visibleWriteStageAfterReplace  = "after_replace"
)

// mutationResult distinguishes a visible namespace mutation from one whose
// parent directory has crossed the durability barrier.
type mutationResult struct {
	Stage            mutationStage
	ParentRel        string
	WorkspaceMutated bool
	PathUncertain    bool
	writeIdentity    *visibleWriteIdentity
}

// visibleWriteIdentity binds in-process recovery to the root and parent
// directory inodes used by a descriptor-based visible write.
type visibleWriteIdentity struct {
	root   os.FileInfo
	parent os.FileInfo
}

type durabilityOps struct {
	syncRootDirFn      func(*os.Root, string) error
	visibleWriteHookFn func(stage, path string) error
}

func defaultDurabilityOps() *durabilityOps {
	return &durabilityOps{
		syncRootDirFn: syncRootDirectory,
	}
}

func (o *durabilityOps) syncRootDir(root *os.Root, rel string) error {
	if o == nil || o.syncRootDirFn == nil {
		return syncRootDirectory(root, rel)
	}
	return o.syncRootDirFn(root, rel)
}

func (o *durabilityOps) visibleWriteHook(stage, path string) error {
	if o == nil || o.visibleWriteHookFn == nil {
		return nil
	}
	return o.visibleWriteHookFn(stage, path)
}

func syncRootDirectory(root *os.Root, rel string) error {
	if rel == "" {
		rel = "."
	}
	file, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	defer file.Close()
	return syncDirectory(file)
}

// mkdirAllRootDurable creates a private directory chain beneath an opened
// workspace root. Existing symlinks are rejected even when they point back
// inside the workspace, keeping ledger/blob storage on an unambiguous path.
func mkdirAllRootDurable(root *os.Root, rel string, mode os.FileMode, ops *durabilityOps) error {
	current := "."
	for _, component := range strings.Split(filepath.ToSlash(rel), "/") {
		if component == "" || component == "." {
			continue
		}
		next := path.Join(current, component)
		info, err := root.Lstat(filepath.FromSlash(next))
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(filepath.FromSlash(next), mode); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = root.Lstat(filepath.FromSlash(next))
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return newError(ErrorCodeConflict, "workspace change storage path contains a symbolic link", map[string]any{"path": next})
		}
		if !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: next, Err: errors.New("path is not a directory")}
		}
		if err := ops.syncRootDir(root, next); err != nil {
			return err
		}
		if err := ops.syncRootDir(root, current); err != nil {
			return err
		}
		current = next
	}
	return nil
}

func visibleParentRel(rel string) string {
	parent := path.Dir(filepath.ToSlash(rel))
	if parent == "" {
		return "."
	}
	return parent
}

func (s *Service) ensureVisibleParentDurable(root *os.Root, parent string) error {
	if parent == "." {
		return nil
	}
	current := "."
	for _, component := range strings.Split(parent, "/") {
		if component == "" || component == "." {
			continue
		}
		next := path.Join(current, component)
		info, err := root.Lstat(filepath.FromSlash(next))
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(filepath.FromSlash(next), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = root.Lstat(filepath.FromSlash(next))
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return newError(ErrorCodeConflict, "workspace parent path contains a symbolic link", map[string]any{"path": next})
		}
		if !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: next, Err: errors.New("path is not a directory")}
		}
		if err := s.durability.syncRootDir(root, next); err != nil {
			return err
		}
		if err := s.durability.syncRootDir(root, current); err != nil {
			return err
		}
		current = next
	}
	return nil
}

// openVerifiedVisibleParent returns an opened parent directory only after each
// visible path entry is confirmed to be the directory the handle references.
func openVerifiedVisibleParent(root *os.Root, parent string) (*os.Root, error) {
	current := root
	currentOwned := false
	closeCurrent := func() {
		if currentOwned {
			_ = current.Close()
		}
	}
	for _, component := range strings.Split(parent, "/") {
		if component == "" || component == "." {
			continue
		}
		entry, err := current.Lstat(component)
		if err != nil {
			closeCurrent()
			return nil, err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			closeCurrent()
			return nil, newError(ErrorCodeConflict, "workspace parent path contains a symbolic link", map[string]any{"path": parent})
		}
		if !entry.IsDir() {
			closeCurrent()
			return nil, newError(ErrorCodeConflict, "workspace parent path is not a directory", map[string]any{"path": parent})
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			closeCurrent()
			return nil, err
		}
		opened, err := next.Lstat(".")
		if err != nil {
			_ = next.Close()
			closeCurrent()
			return nil, err
		}
		if !os.SameFile(entry, opened) {
			_ = next.Close()
			closeCurrent()
			return nil, newError(ErrorCodeConflict, "workspace parent path changed while opening", map[string]any{"path": parent})
		}
		closeCurrent()
		current = next
		currentOwned = true
	}
	return current, nil
}

func verifyVisibleParentIdentity(root *os.Root, parent string, openedParent *os.Root) error {
	if parent == "." {
		return nil
	}
	currentParent, err := openVerifiedVisibleParent(root, parent)
	if err != nil {
		return err
	}
	defer currentParent.Close()
	currentInfo, err := currentParent.Lstat(".")
	if err != nil {
		return err
	}
	openedInfo, err := openedParent.Lstat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(currentInfo, openedInfo) {
		return newError(ErrorCodeConflict, "workspace parent path no longer matches the opened directory", map[string]any{"path": parent})
	}
	return nil
}

func openedVisibleWriteIdentity(root, parent *os.Root) (*visibleWriteIdentity, error) {
	rootInfo, err := root.Lstat(".")
	if err != nil {
		return nil, err
	}
	parentInfo, err := parent.Lstat(".")
	if err != nil {
		return nil, err
	}
	return &visibleWriteIdentity{root: rootInfo, parent: parentInfo}, nil
}

func (s *Service) verifyVisibleWriteIdentity(root *os.Root, parent string, openedParent *os.Root) error {
	visibleRoot, err := os.Lstat(s.workspace)
	if err != nil {
		return newError(ErrorCodeConflict, "workspace root path is no longer available", map[string]any{"workspace": s.workspace})
	}
	if visibleRoot.Mode()&os.ModeSymlink != 0 {
		return newError(ErrorCodeConflict, "workspace root path became a symbolic link", map[string]any{"workspace": s.workspace})
	}
	if !visibleRoot.IsDir() {
		return newError(ErrorCodeConflict, "workspace root path is not a directory", map[string]any{"workspace": s.workspace})
	}
	openedRoot, err := root.Lstat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(visibleRoot, openedRoot) {
		return newError(ErrorCodeConflict, "workspace root path no longer matches the opened directory", map[string]any{"workspace": s.workspace})
	}
	return verifyVisibleParentIdentity(root, parent, openedParent)
}

func (s *Service) syncVisibleParent(rel string) error {
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	return s.durability.syncRootDir(root, visibleParentRel(rel))
}

func (s *Service) markPendingParentSync(path string, result mutationResult) {
	parent := result.ParentRel
	if parent == "" {
		parent = visibleParentRel(path)
	}
	pending := pendingParentSyncIntent{
		Path:          path,
		PathUncertain: result.PathUncertain,
		writeIdentity: result.writeIdentity,
	}
	if existing, ok := s.pendingParentSync[parent]; ok {
		pending.PathUncertain = pending.PathUncertain || existing.PathUncertain
		if existing.writeIdentity != nil {
			pending.writeIdentity = existing.writeIdentity
		}
	}
	s.pendingParentSync[parent] = pending
}

func (s *Service) syncIdentityBoundParent(root *os.Root, parent string, identity *visibleWriteIdentity) (identityMatched bool, err error) {
	parentRoot := root
	if parent != "." {
		parentRoot, err = openVerifiedVisibleParent(root, parent)
		if err != nil {
			return false, err
		}
		defer parentRoot.Close()
	}
	rootInfo, err := root.Lstat(".")
	if err != nil {
		return false, err
	}
	parentInfo, err := parentRoot.Lstat(".")
	if err != nil {
		return false, err
	}
	if identity == nil || identity.root == nil || identity.parent == nil ||
		!os.SameFile(rootInfo, identity.root) || !os.SameFile(parentInfo, identity.parent) {
		return false, newError(ErrorCodeConflict, "pending workspace parent no longer matches the mutated directory identity", nil)
	}
	if err := s.verifyVisibleWriteIdentity(root, parent, parentRoot); err != nil {
		return false, err
	}
	syncErr := s.durability.syncRootDir(parentRoot, ".")
	if identityErr := s.verifyVisibleWriteIdentity(root, parent, parentRoot); identityErr != nil {
		return false, errors.Join(syncErr, identityErr)
	}
	return true, syncErr
}

func (s *Service) syncPendingParentsLocked() error {
	parents := make([]string, 0, len(s.pendingParentSync))
	for parent := range s.pendingParentSync {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	if len(parents) == 0 {
		return nil
	}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		parent := parents[0]
		pending := s.pendingParentSync[parent]
		if pending.writeIdentity != nil {
			pending.PathUncertain = true
			s.pendingParentSync[parent] = pending
		}
		return durabilityPendingError(pending.Path, "", "", mutationResult{
			Stage:            mutationStageVisible,
			ParentRel:        parent,
			WorkspaceMutated: true,
			PathUncertain:    pending.PathUncertain,
		}, err)
	}
	defer root.Close()
	for _, parent := range parents {
		pending := s.pendingParentSync[parent]
		identityMatched := true
		var syncErr error
		if pending.writeIdentity != nil {
			identityMatched, syncErr = s.syncIdentityBoundParent(root, parent, pending.writeIdentity)
		} else {
			syncErr = s.durability.syncRootDir(root, parent)
		}
		if syncErr != nil {
			if !identityMatched {
				pending.PathUncertain = true
				s.pendingParentSync[parent] = pending
			}
			return durabilityPendingError(pending.Path, "", "", mutationResult{
				Stage:            mutationStageVisible,
				ParentRel:        parent,
				WorkspaceMutated: true,
				PathUncertain:    pending.PathUncertain,
			}, syncErr)
		}
		delete(s.pendingParentSync, parent)
		for rel, pendingSave := range s.pendingSaves {
			if pendingSave.ParentRel == parent {
				pendingSave.Durable = true
				pendingSave.PathUncertain = pendingSave.PathUncertain || pending.PathUncertain
				s.pendingSaves[rel] = pendingSave
			}
		}
	}
	return nil
}

func (s *Service) reconcilePendingDurabilityLocked() error {
	if err := s.syncPendingParentsLocked(); err != nil {
		return err
	}
	for rel, pending := range s.pendingSaves {
		if !pending.Durable || pending.RedoInvalidated {
			continue
		}
		if err := s.invalidateRedoExcept(OriginUser); err != nil {
			return durabilityPendingError(rel, "", "", mutationResult{
				Stage:            mutationStageDurable,
				ParentRel:        pending.ParentRel,
				WorkspaceMutated: true,
				PathUncertain:    pending.PathUncertain,
			}, err)
		}
		pending.RedoInvalidated = true
		s.pendingSaves[rel] = pending
	}
	if err := s.recoverOperations(); err != nil {
		return err
	}
	return s.recoverPrepared()
}

func durabilityPendingError(path, changeSetID, operationID string, result mutationResult, cause error) error {
	details := map[string]any{
		"mutation_stage":    result.Stage,
		"recovery_pending":  true,
		"workspace_mutated": result.WorkspaceMutated,
	}
	if path != "" && !result.PathUncertain {
		details["path"] = path
	}
	if changeSetID != "" {
		details["change_set_id"] = changeSetID
	}
	if operationID != "" {
		details["operation_id"] = operationID
	}
	pending := newError(ErrorCodeDurabilityPending, "workspace mutation durability or journal finalization is pending", details)
	if cause == nil {
		return pending
	}
	return errors.Join(pending, cause)
}
