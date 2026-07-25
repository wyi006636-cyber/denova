package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (repository *recordRepository) publishRecord(root *os.Root, rel string, raw []byte, create bool, expected os.FileInfo, expectedHash string, limit int64) error {
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if err := ensureRootDirectoryTree(root, repository.workspace, parent, 0o700); err != nil {
		return err
	}
	tempRel, err := siblingTempName(root, rel)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(filepath.FromSlash(tempRel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanupRel := tempRel
	preserveCleanup := false
	defer func() {
		_ = file.Close()
		if cleanupRel != "" && !preserveCleanup {
			_ = root.Remove(filepath.FromSlash(cleanupRel))
		}
	}()
	if _, err := repository.writeFile(file, raw); err != nil {
		return err
	}
	if err := repository.fail(RepositoryFaultAfterWrite); err != nil {
		return err
	}
	if err := repository.fail(RepositoryFaultBeforeFileSync); err != nil {
		return err
	}
	if err := repository.syncFile(file); err != nil {
		return fmt.Errorf("%w: sync complete sibling: %v", ErrRecordDurability, err)
	}
	stagedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect synced record sibling: %w", err)
	}
	stagedHash := recordSHA256(raw)
	if err := verifyRecordCAS(root, tempRel, stagedInfo, stagedHash, limit); err != nil {
		return fmt.Errorf("%w: synced record sibling identity changed", ErrRecordConflict)
	}
	if err := repository.fail(RepositoryFaultBeforePublish); err != nil {
		return err
	}
	if err := verifyRecordCAS(root, tempRel, stagedInfo, stagedHash, limit); err != nil {
		return fmt.Errorf("%w: record sibling changed before publish", ErrRecordConflict)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%w: close complete sibling: %v", ErrRecordDurability, err)
	}
	if create {
		if _, err := root.Lstat(filepath.FromSlash(rel)); err == nil {
			return ErrRecordExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := repository.linkFile(root, tempRel, rel); err != nil {
			if _, statErr := root.Lstat(filepath.FromSlash(rel)); statErr == nil {
				return ErrRecordExists
			}
			return err
		}
		if err := verifyRecordCAS(root, rel, stagedInfo, stagedHash, limit); err != nil {
			if recoveryErr := repository.withdrawMismatchedCreate(root, parent, rel, raw, limit); recoveryErr != nil {
				preserveCleanup = true
				return errors.Join(ErrRecordConflict, ErrRecordRecoveryRequired, recoveryErr)
			}
			return errors.Join(ErrRecordConflict, ErrRecordRecoveryRequired)
		}
		if err := root.Remove(filepath.FromSlash(tempRel)); err != nil {
			return fmt.Errorf("%w: remove published sibling link: %v", ErrRecordDurability, err)
		}
		cleanupRel = ""
		if err := repository.fail(RepositoryFaultBeforeParentSync); err != nil {
			return err
		}
		if err := repository.syncParent(root, parent); err != nil {
			return fmt.Errorf("%w: sync record parent: %v", ErrRecordDurability, err)
		}
		return nil
	} else {
		if err := verifyRecordCAS(root, rel, expected, expectedHash, limit); err != nil {
			return err
		}
		if err := repository.fail(RepositoryFaultAfterCASBeforePublish); err != nil {
			return err
		}
		displacedRel, err := repository.replaceFile(root, rel, tempRel)
		if err != nil {
			if errors.Is(err, ErrRecordRecoveryRequired) {
				preserveCleanup = true
				if _, preserveErr := preserveCompleteRecordSibling(root, rel, raw, limit); preserveErr != nil {
					return errors.Join(err, fmt.Errorf("preserve intended update bytes: %w", preserveErr))
				}
				if syncErr := repository.syncParent(root, parent); syncErr != nil {
					return errors.Join(err, fmt.Errorf("%w: sync intended update recovery: %v", ErrRecordDurability, syncErr))
				}
			}
			return err
		}
		cleanupRel = displacedRel
		if err := verifyRecordCAS(root, rel, stagedInfo, stagedHash, limit); err != nil {
			if restoreErr := restoreRecordAtomically(root, repository.workspace, rel, displacedRel, tempRel); restoreErr != nil {
				preserveCleanup = true
				return errors.Join(ErrRecordConflict, fmt.Errorf("%w: restore after replacement substitution: %v", ErrRecordDurability, restoreErr))
			}
			preserveCleanup = true
			if syncErr := repository.syncParent(root, parent); syncErr != nil {
				return errors.Join(ErrRecordConflict, fmt.Errorf("%w: sync restored target after replacement substitution: %v", ErrRecordDurability, syncErr))
			}
			return ErrRecordConflict
		}
		if err := verifyRecordCAS(root, displacedRel, expected, expectedHash, limit); err != nil {
			cleanupRel = tempRel
			if restoreErr := repository.rollbackRecordReplacement(root, parent, rel, displacedRel, tempRel); restoreErr != nil {
				preserveCleanup = true
				return errors.Join(ErrRecordConflict, fmt.Errorf("%w: restore substituted record: %v", ErrRecordDurability, restoreErr))
			}
			cleanupRel = ""
			return ErrRecordConflict
		}
		if err := repository.fail(RepositoryFaultBeforeParentSync); err != nil {
			cleanupRel = tempRel
			if restoreErr := repository.rollbackRecordReplacement(root, parent, rel, displacedRel, tempRel); restoreErr != nil {
				preserveCleanup = true
				return errors.Join(err, fmt.Errorf("%w: restore after pre-sync fault: %v", ErrRecordDurability, restoreErr))
			}
			cleanupRel = ""
			return err
		}
		if err := repository.syncParent(root, parent); err != nil {
			cleanupRel = tempRel
			if restoreErr := repository.rollbackRecordReplacement(root, parent, rel, displacedRel, tempRel); restoreErr != nil {
				preserveCleanup = true
				return errors.Join(fmt.Errorf("%w: sync record parent: %v", ErrRecordDurability, err), fmt.Errorf("restore after sync failure: %w", restoreErr))
			}
			cleanupRel = ""
			return fmt.Errorf("%w: sync record parent before rollback: %v", ErrRecordDurability, err)
		}
		if err := root.Remove(filepath.FromSlash(displacedRel)); err != nil {
			preserveCleanup = true
			return fmt.Errorf("%w: remove displaced CAS record: %v", ErrRecordDurability, err)
		}
		cleanupRel = ""
		if err := repository.syncParent(root, parent); err != nil {
			return fmt.Errorf("%w: sync displaced-record removal: %v", ErrRecordDurability, err)
		}
		return nil
	}
}

func (repository *recordRepository) withdrawMismatchedCreate(root *os.Root, parent, targetRel string, intended []byte, limit int64) error {
	if _, err := preserveCompleteRecordSibling(root, targetRel, intended, limit); err != nil {
		return fmt.Errorf("preserve intended create bytes: %w", err)
	}
	if err := repository.syncParent(root, parent); err != nil {
		return fmt.Errorf("%w: sync intended create recovery: %v", ErrRecordDurability, err)
	}
	conflictRel, err := siblingNameWithMarker(root, targetRel, ".conflict-")
	if err != nil {
		return err
	}
	if err := root.Rename(filepath.FromSlash(targetRel), filepath.FromSlash(conflictRel)); err != nil {
		return fmt.Errorf("withdraw substituted create target: %w", err)
	}
	if err := repository.syncParent(root, parent); err != nil {
		return fmt.Errorf("%w: sync withdrawn create target: %v", ErrRecordDurability, err)
	}
	return nil
}

func preserveCompleteRecordSibling(root *os.Root, targetRel string, raw []byte, limit int64) (string, error) {
	recoveryRel, err := siblingTempName(root, targetRel)
	if err != nil {
		return "", err
	}
	file, err := root.OpenFile(filepath.FromSlash(recoveryRel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = root.Remove(filepath.FromSlash(recoveryRel))
		}
	}()
	written, err := file.Write(raw)
	if err != nil {
		return "", fmt.Errorf("write complete recovery sibling: %w", err)
	}
	if written != len(raw) {
		return "", fmt.Errorf("write complete recovery sibling: wrote %d of %d bytes", written, len(raw))
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync complete recovery sibling: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if err := verifyRecordCAS(root, recoveryRel, info, recordSHA256(raw), limit); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close complete recovery sibling: %w", err)
	}
	keep = true
	return recoveryRel, nil
}

func (repository *recordRepository) rollbackRecordReplacement(root *os.Root, parent, targetRel, displacedRel, cleanupRel string) error {
	if err := restoreRecordAtomically(root, repository.workspace, targetRel, displacedRel, cleanupRel); err != nil {
		return err
	}
	if err := repository.syncParent(root, parent); err != nil {
		return fmt.Errorf("sync record rollback: %w", err)
	}
	if err := root.Remove(filepath.FromSlash(cleanupRel)); err != nil {
		return fmt.Errorf("remove rolled-back replacement: %w", err)
	}
	if err := repository.syncParent(root, parent); err != nil {
		return fmt.Errorf("sync rolled-back replacement removal: %w", err)
	}
	return nil
}

func verifyRecordCAS(root *os.Root, rel string, expected os.FileInfo, expectedHash string, limit int64) error {
	current, err := root.Lstat(filepath.FromSlash(rel))
	if errors.Is(err, os.ErrNotExist) {
		return ErrRecordConflict
	}
	if err != nil || !current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 || expected == nil || !os.SameFile(expected, current) {
		return ErrRecordConflict
	}
	raw, err := readBoundedRootFile(root, rel, current, limit)
	if err != nil || recordSHA256(raw) != expectedHash {
		return ErrRecordConflict
	}
	return nil
}
