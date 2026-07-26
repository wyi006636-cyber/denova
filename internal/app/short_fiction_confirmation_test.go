package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/internal/book"
	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

func TestFanqieConfirmCreatesExactCheckpoint(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	before := "# 旧稿\n\n等待作者确认。"
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# 完整短篇\n\n作者确认后的正文。")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)

	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "一名外卖员发现订单来自明天。",
		},
	}, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != shortfiction.ConfirmationWritten || !result.WorkspaceMutated || result.Checkpoint == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.WriteRevision != workspacechange.Revision([]byte(candidate.PreviewMarkdown)) {
		t.Fatalf("revision = %q", result.WriteRevision)
	}
	if result.CandidateID != candidate.CandidateID || result.ChangeGroupID == "" || result.ChangeSetID == "" {
		t.Fatalf("write identity = %#v", result)
	}
	if result.CheckpointStatus != shortfiction.CheckpointCreated || result.Retryable {
		t.Fatalf("checkpoint outcome = %#v", result)
	}
	if result.Checkpoint.Source != book.VersionSourceManual || result.Checkpoint.Path != candidate.TargetPath || result.Checkpoint.Revision != result.WriteRevision {
		t.Fatalf("checkpoint = %#v", result.Checkpoint)
	}

	later := "# 后续编辑\n\n检查点必须仍然指向确认正文。"
	changeService, err := application.WorkspaceChangeService()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changeService.SaveFile(ctx, path, later, result.WriteRevision); err != nil {
		t.Fatal(err)
	}
	diff, err := application.VersionDiff(ctx, result.Checkpoint.VersionID, candidate.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Original != candidate.PreviewMarkdown || diff.Modified != later {
		t.Fatalf("diff = %#v", diff)
	}
}

func TestFanqieConfirmRejectsTamperedCandidateBeforeMutation(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	before := "untouched source"
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# Generated candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "reject client-side mutation",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	beforeTree := snapshotShortFictionTestTree(t, workspace)
	beforeVersions := listShortFictionTestVersions(t, application)
	beforeHistory := listShortFictionTestSessionHistory(t, application)
	candidate.PreviewMarkdown += "\n\ntampered"

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	if !shortfiction.IsCode(err, shortfiction.ErrorCodeCandidateMismatch) {
		t.Fatalf("error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("result = %#v, want zero result", result)
	}
	assertShortFictionTestObservationsUnchanged(t, application, workspace, beforeTree, beforeVersions, beforeHistory)
	assertShortFictionTestNoChangeMetadata(t, workspace)
}

func TestFanqieConfirmRejectsStaleRevisionWithoutCheckpoint(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	before := "candidate source"
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# Generated candidate")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "reject stale confirmation",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	changeService, err := application.WorkspaceChangeService()
	if err != nil {
		t.Fatal(err)
	}
	later := "newer author edit"
	if _, err := changeService.SaveFile(ctx, path, later, candidate.BaseRevision); err != nil {
		t.Fatal(err)
	}
	beforeVersions := listShortFictionTestVersions(t, application)

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr.Code != workspacechange.ErrorCodeRevisionConflict {
		t.Fatalf("error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("result = %#v, want zero result", result)
	}
	if got := readShortFictionTestFile(t, workspace, path); got != later {
		t.Fatalf("file = %q, want %q", got, later)
	}
	if groups := listShortFictionTestGroups(t, application); len(groups) != 0 {
		t.Fatalf("groups = %#v, want none", groups)
	}
	if versions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(versions, beforeVersions) {
		t.Fatalf("versions changed: before=%#v after=%#v", beforeVersions, versions)
	}
}

func TestFanqieConfirmRejectsSameRevisionSymlinkAuthorityBeforeWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{
			name: "target replaced by symlink",
			mutate: func(t *testing.T, workspace string) {
				target := filepath.Join(workspace, "chapters", "short.md")
				if err := os.Rename(target, target+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../real/short.md", target); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "parent replaced by symlink",
			mutate: func(t *testing.T, workspace string) {
				parent := filepath.Join(workspace, "chapters")
				if err := os.Rename(parent, parent+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("real", parent); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
			const (
				path   = "chapters/short.md"
				before = "same revision target authority"
			)
			writeShortFictionTestFile(t, workspace, path, before)
			writeShortFictionTestFile(t, workspace, "real/short.md", before)
			provider := newShortFictionTestProvider(t, "# candidate must not be written")
			application := newShortFictionTestApp(t, workspace, provider.server.URL)
			candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
				ProfileID: shortfiction.ProfileFanqieShort,
				Source: shortfiction.SourcePacket{
					Workspace: workspace, TargetPath: path,
					BaseRevision: workspacechange.Revision([]byte(before)),
					Brief:        "bind confirmation to the visible target",
				},
			}, "en-US")
			if err != nil {
				t.Fatal(err)
			}
			beforeGroups := listShortFictionTestGroups(t, application)
			beforeVersions := listShortFictionTestVersions(t, application)
			test.mutate(t, workspace)

			result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
			if !shortfiction.IsCode(err, shortfiction.ErrorCodeInvalidSource) {
				t.Fatalf("confirmation error = %v, result = %#v", err, result)
			}
			if result != (shortfiction.ConfirmationResult{}) {
				t.Fatalf("result = %#v, want zero result", result)
			}
			if got := readShortFictionTestFile(t, workspace, path); got != before {
				t.Fatalf("authority rejection changed visible target: got=%q want=%q", got, before)
			}
			if got := readShortFictionTestFile(t, workspace, "real/short.md"); got != before {
				t.Fatalf("authority rejection changed alternate target: got=%q want=%q", got, before)
			}
			if groups := listShortFictionTestGroups(t, application); !reflect.DeepEqual(groups, beforeGroups) {
				t.Fatalf("authority rejection changed ChangeSets: before=%#v after=%#v", beforeGroups, groups)
			}
			if versions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(versions, beforeVersions) {
				t.Fatalf("authority rejection changed versions: before=%#v after=%#v", beforeVersions, versions)
			}
		})
	}
}

func TestFanqieConfirmRejectsSameRevisionTargetIdentityRaceBeforeWrite(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	const (
		path   = "chapters/short.md"
		before = "same bytes in a different inode"
	)
	writeShortFictionTestFile(t, workspace, path, before)
	writeShortFictionTestFile(t, workspace, ".hidden/alternate.md", before)
	provider := newShortFictionTestProvider(t, "# candidate must not be written")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace: workspace, TargetPath: path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "reject a different target inode",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	beforeGroups := listShortFictionTestGroups(t, application)
	beforeVersions := listShortFictionTestVersions(t, application)
	application.shortFiction().openRoot = swappingShortFictionRootOpener(t, workspace, func(delegate shortFictionRoot, openName, relative string) (*os.File, error) {
		if application.mu.TryLock() {
			application.mu.Unlock()
			t.Fatal("confirmation target validation ran outside the App lease")
		}
		target := filepath.Join(workspace, filepath.FromSlash(relative))
		original := target + ".original"
		alternate := filepath.Join(workspace, ".hidden", "alternate.md")
		if err := os.Rename(target, original); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(alternate, target); err != nil {
			t.Fatal(err)
		}
		file, openErr := delegate.Open(openName)
		if err := os.Rename(target, alternate); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(original, target); err != nil {
			t.Fatal(err)
		}
		return file, openErr
	})

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	if !shortfiction.IsCode(err, shortfiction.ErrorCodeInvalidSource) {
		t.Fatalf("confirmation error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("result = %#v, want zero result", result)
	}
	if got := readShortFictionTestFile(t, workspace, path); got != before {
		t.Fatalf("identity rejection changed target: got=%q want=%q", got, before)
	}
	if groups := listShortFictionTestGroups(t, application); !reflect.DeepEqual(groups, beforeGroups) {
		t.Fatalf("identity rejection changed ChangeSets: before=%#v after=%#v", beforeGroups, groups)
	}
	if versions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(versions, beforeVersions) {
		t.Fatalf("identity rejection changed versions: before=%#v after=%#v", beforeVersions, versions)
	}
}

func TestFanqieConfirmReturnsWrittenCheckpointFailedWithoutRetryClaim(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	path := "chapters/short.md"
	before := "checkpoint failure source"
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# Durable candidate\n\nThe checkpoint will fail after this write.")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	baseline, err := application.CreateVersion(ctx, "baseline")
	if err != nil || baseline.Version == nil {
		t.Fatalf("initialize version history: result=%#v err=%v", baseline, err)
	}
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace:    workspace,
			TargetPath:   path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "report a committed write truthfully",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git", "HEAD"), []byte("invalid head\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	if err != nil {
		t.Fatalf("partial outcome returned error: %v", err)
	}
	if result.Status != shortfiction.ConfirmationWrittenCheckpointFailed || !result.WorkspaceMutated {
		t.Fatalf("result = %#v", result)
	}
	if result.WriteRevision != workspacechange.Revision([]byte(candidate.PreviewMarkdown)) || result.ChangeGroupID == "" || result.ChangeSetID == "" {
		t.Fatalf("committed identity = %#v", result)
	}
	if result.CheckpointStatus != shortfiction.CheckpointFailed || result.Checkpoint != nil || result.Retryable {
		t.Fatalf("checkpoint outcome = %#v", result)
	}
	if got := readShortFictionTestFile(t, workspace, path); got != candidate.PreviewMarkdown {
		t.Fatalf("file = %q, want committed candidate", got)
	}
	changeService, err := application.WorkspaceChangeService()
	if err != nil {
		t.Fatal(err)
	}
	group, err := changeService.GetGroup(ctx, result.ChangeGroupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(group.ChangeSets) != 1 || group.ChangeSets[0].ID != result.ChangeSetID || group.ChangeSets[0].Revision != result.WriteRevision {
		t.Fatalf("group = %#v", group)
	}
	if group.Origin != workspacechange.OriginAgent || group.ReviewStatus != workspacechange.ReviewStatusAccepted || group.ApplyState != workspacechange.ApplyStateApplied {
		t.Fatalf("group state = %#v", group)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"retry_token", "receipt", "idempot", "rollback"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("partial result contains forbidden claim %q: %s", forbidden, data)
		}
	}
}

func TestFanqieConfirmPreservesVisibleWriteDurabilityPendingTruth(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	const (
		path   = "chapters/short.md"
		before = "durability failure source"
	)
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# Visible candidate\n\nRecovery is still pending.")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace: workspace, TargetPath: path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "preserve a visible durability failure truthfully",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	beforeGroups := listShortFictionTestGroups(t, application)
	beforeVersions := listShortFictionTestVersions(t, application)
	injected := &shortFictionDurabilityPendingService{t: t, workspace: workspace}
	application.shortFiction().workspaceChangeFor = func(got string) (shortFictionChangeService, error) {
		if got != workspace {
			t.Fatalf("change workspace = %q, want %q", got, workspace)
		}
		return injected, nil
	}

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr.Code != workspacechange.ErrorCodeDurabilityPending {
		t.Fatalf("confirmation error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("durability error was mislabeled as success: %#v", result)
	}
	if got := readShortFictionTestFile(t, workspace, path); got != candidate.PreviewMarkdown {
		t.Fatalf("visible target = %q, want candidate", got)
	}
	wantRevision := workspacechange.Revision([]byte(candidate.PreviewMarkdown))
	for key, want := range map[string]any{
		"workspace_mutated": true,
		"recovery_pending":  true,
		"retryable":         false,
		"target_path":       path,
		"write_revision":    wantRevision,
		"change_group_id":   "group-durability",
		"change_set_id":     "change-durability",
	} {
		if got := changeErr.Details[key]; got != want {
			t.Fatalf("details[%q] = %#v, want %#v; details=%#v", key, got, want, changeErr.Details)
		}
	}
	if groups := listShortFictionTestGroups(t, application); !reflect.DeepEqual(groups, beforeGroups) {
		t.Fatalf("fake pending write projected a ChangeSet: before=%#v after=%#v", beforeGroups, groups)
	}
	if versions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(versions, beforeVersions) {
		t.Fatalf("durability-pending write created a checkpoint: before=%#v after=%#v", beforeVersions, versions)
	}
}

func TestFanqieConfirmDoesNotMislabelPreparedCommitFailureAsCheckpointFailure(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	const (
		path   = "chapters/short.md"
		before = "prepared commit source"
	)
	writeShortFictionTestFile(t, workspace, path, before)
	provider := newShortFictionTestProvider(t, "# candidate must remain unwritten")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace: workspace, TargetPath: path,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "separate commit failures from checkpoint failures",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	commitErr := errors.New("prepared commit failed before visible mutation")
	application.shortFiction().workspaceChangeFor = func(string) (shortFictionChangeService, error) {
		return shortFictionChangeServiceFunc(func(
			context.Context,
			workspacechange.ReplaceFileRequest,
			workspacechange.ConsistentSnapshotFunc,
		) (workspacechange.ChangeSet, error) {
			return workspacechange.ChangeSet{
				ID:         "change-prepared",
				GroupID:    "group-prepared",
				ApplyState: workspacechange.ApplyStatePrepared,
			}, commitErr
		}), nil
	}

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	if !errors.Is(err, commitErr) {
		t.Fatalf("confirmation error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("prepared commit failure was mislabeled as checkpoint failure: %#v", result)
	}
	if got := readShortFictionTestFile(t, workspace, path); got != before {
		t.Fatalf("prepared commit failure changed target: got=%q want=%q", got, before)
	}
}

type shortFictionChangeServiceFunc func(
	context.Context,
	workspacechange.ReplaceFileRequest,
	workspacechange.ConsistentSnapshotFunc,
) (workspacechange.ChangeSet, error)

func (fn shortFictionChangeServiceFunc) ReplaceFileWithConsistentSnapshot(
	ctx context.Context,
	req workspacechange.ReplaceFileRequest,
	snapshot workspacechange.ConsistentSnapshotFunc,
) (workspacechange.ChangeSet, error) {
	return fn(ctx, req, snapshot)
}

type shortFictionDurabilityPendingService struct {
	t         *testing.T
	workspace string
}

func (s *shortFictionDurabilityPendingService) ReplaceFileWithConsistentSnapshot(
	_ context.Context,
	req workspacechange.ReplaceFileRequest,
	snapshot workspacechange.ConsistentSnapshotFunc,
) (workspacechange.ChangeSet, error) {
	s.t.Helper()
	if req.Path != "chapters/short.md" || req.BaseRevision != workspacechange.Revision([]byte("durability failure source")) {
		s.t.Fatalf("replacement request = %#v", req)
	}
	parent := filepath.Join(s.workspace, "chapters")
	temp := filepath.Join(parent, ".short.md.pending-test")
	if err := os.WriteFile(temp, []byte(req.Content), 0o644); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Rename(temp, filepath.Join(parent, "short.md")); err != nil {
		s.t.Fatal(err)
	}
	change := workspacechange.ChangeSet{
		ID:           "change-durability",
		GroupID:      "group-durability",
		Path:         req.Path,
		BaseRevision: req.BaseRevision,
		Revision:     workspacechange.Revision([]byte(req.Content)),
	}
	_ = snapshot
	return change, &workspacechange.Error{
		Code:    workspacechange.ErrorCodeDurabilityPending,
		Message: "workspace mutation durability or journal finalization is pending",
		Details: map[string]any{
			"path":              req.Path,
			"mutation_stage":    "visible",
			"recovery_pending":  true,
			"workspace_mutated": true,
			"change_set_id":     change.ID,
		},
	}
}
