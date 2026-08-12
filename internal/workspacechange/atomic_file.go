package workspacechange

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

func (s *Service) atomicWriteVisibleFile(rel string, content []byte) (mutationResult, error) {
	result := mutationResult{Stage: mutationStageUnchanged, ParentRel: visibleParentRel(rel)}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return result, err
	}
	defer root.Close()
	parent := result.ParentRel
	if err := s.ensureVisibleParentDurable(root, parent); err != nil {
		return result, err
	}
	parentRoot := root
	if parent != "." {
		parentRoot, err = openVerifiedVisibleParent(root, parent)
		if err != nil {
			return result, err
		}
		defer parentRoot.Close()
	}
	result.writeIdentity, err = openedVisibleWriteIdentity(root, parentRoot)
	if err != nil {
		return result, err
	}
	targetName := filepath.FromSlash(path.Base(rel))
	mode := os.FileMode(0o644)
	if info, err := parentRoot.Lstat(targetName); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return result, newError(ErrorCodeConflict, "workspace path is not a regular file", map[string]any{"path": rel})
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return result, err
	}
	var random [8]byte
	if _, err := cryptorand.Read(random[:]); err != nil {
		return result, err
	}
	tempName := filepath.FromSlash(fmt.Sprintf(".%s.denova-%x.tmp", path.Base(rel), random[:]))
	file, err := parentRoot.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return result, err
	}
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = parentRoot.Remove(tempName)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return result, err
	}
	if err := file.Sync(); err != nil {
		return result, err
	}
	if err := file.Close(); err != nil {
		return result, err
	}
	if err := s.durability.visibleWriteHook(visibleWriteStageBeforeReplace, rel); err != nil {
		return result, err
	}
	if err := s.verifyVisibleWriteIdentity(root, parent, parentRoot); err != nil {
		return result, err
	}
	if err := parentRoot.Rename(tempName, targetName); err != nil {
		return result, err
	}
	removeTemp = false
	result.Stage = mutationStageVisible
	result.WorkspaceMutated = true
	if err := s.durability.visibleWriteHook(visibleWriteStageAfterReplace, rel); err != nil {
		return result, err
	}
	syncErr := s.durability.syncRootDir(parentRoot, ".")
	if identityErr := s.verifyVisibleWriteIdentity(root, parent, parentRoot); identityErr != nil {
		result.PathUncertain = true
		return result, errors.Join(syncErr, identityErr)
	}
	if syncErr != nil {
		return result, syncErr
	}
	result.Stage = mutationStageDurable
	return result, nil
}

func (s *Service) atomicRemoveVisibleFile(rel string) (mutationResult, error) {
	result := mutationResult{Stage: mutationStageUnchanged, ParentRel: visibleParentRel(rel)}
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return result, err
	}
	defer root.Close()
	if err := root.Remove(filepath.FromSlash(rel)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The desired after-state is already visible, but synchronize the
			// directory before any durable journal claims that state.
			result.Stage = mutationStageVisible
		} else {
			return result, err
		}
	} else {
		result.Stage = mutationStageVisible
		result.WorkspaceMutated = true
	}
	if err := s.durability.syncRootDir(root, result.ParentRel); err != nil {
		return result, err
	}
	result.Stage = mutationStageDurable
	return result, nil
}
