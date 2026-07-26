package workspacechange

import (
	"context"
	"errors"
)

// ConsistentSnapshotFunc receives the applied replacement while the workspace
// mutation lease is held.
type ConsistentSnapshotFunc func(ChangeSet) error

// ReplaceFileWithConsistentSnapshot replaces one visible file, then invokes a
// caller snapshot before the mutation lease is released. The callback must not
// call another method on this non-reentrant workspacechange Service.
func (s *Service) ReplaceFileWithConsistentSnapshot(
	ctx context.Context,
	req ReplaceFileRequest,
	snapshot ConsistentSnapshotFunc,
) (ChangeSet, error) {
	if s == nil {
		return ChangeSet{}, newError(ErrorCodeConflict, "change service is nil", nil)
	}
	if snapshot == nil {
		return ChangeSet{}, newError(ErrorCodeConflict, "consistent snapshot callback is nil", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	change, err := s.replaceFileLocked(ctx, req)
	if err != nil {
		return change, err
	}
	if err := snapshot(cloneChangeSet(change)); err != nil {
		return change, err
	}
	return change, nil
}

// ReplaceFile records a full-file replacement through the same journal used by
// batch edits. It also supports creating a previously missing visible file.
func (s *Service) ReplaceFile(ctx context.Context, req ReplaceFileRequest) (ChangeSet, error) {
	if s == nil {
		return ChangeSet{}, newError(ErrorCodeConflict, "change service is nil", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	change, err := s.replaceFileLocked(ctx, req)
	if err != nil {
		return ChangeSet{}, err
	}
	return change, nil
}

func (s *Service) replaceFileLocked(ctx context.Context, req ReplaceFileRequest) (ChangeSet, error) {
	if err := s.contextError(ctx); err != nil {
		return ChangeSet{}, err
	}
	if err := s.reconcilePendingDurabilityLocked(); err != nil {
		return ChangeSet{}, err
	}
	rel, err := s.visibleRelPath(req.Path)
	if err != nil {
		return ChangeSet{}, err
	}
	expectedRevision, err := requireBaseRevision(rel, req.BaseRevision)
	if err != nil {
		return ChangeSet{}, err
	}
	before, readErr := s.readVisibleFile(rel)
	beforeExists := readErr == nil
	if readErr != nil {
		var typed *Error
		if !errors.As(readErr, &typed) || typed.Code != ErrorCodeNotFound {
			return ChangeSet{}, readErr
		}
		before = nil
	}
	actualRevision := "missing"
	if beforeExists {
		actualRevision = Revision(before)
	}
	if err := requireRevision(rel, expectedRevision, actualRevision); err != nil {
		return ChangeSet{}, err
	}
	after := []byte(req.Content)
	if beforeExists && string(before) == req.Content {
		return ChangeSet{}, newError(ErrorCodeInvalidEdit, "replacement does not change the file", map[string]any{"path": rel})
	}
	metadata := normalizeMetadata(req.Metadata)
	reviewStatus := ReviewStatusPending
	if metadata.AutoAccept {
		reviewStatus = ReviewStatusAccepted
	}
	editID := newID("edit")
	edits := []AppliedEdit{{
		ID:           editID,
		OldString:    string(before),
		NewString:    req.Content,
		ReviewStatus: reviewStatus,
		Hunks: []Hunk{{
			ID:          newID("hunk"),
			BeforeStart: 0,
			BeforeEnd:   len(before),
			AfterStart:  0,
			AfterEnd:    len(after),
		}},
	}}
	change := newChangeSet(rel, before, after, beforeExists, true, edits, metadata)
	if err := s.commitChangeLocked(ctx, &change, before, after, metadata); err != nil {
		return cloneChangeSet(change), err
	}
	return cloneChangeSet(change), nil
}
