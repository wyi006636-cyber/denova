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
