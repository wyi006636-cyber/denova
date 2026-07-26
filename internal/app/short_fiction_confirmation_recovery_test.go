package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

func TestFanqieConfirmRejectsParentSymlinkSwapAfterAppPreflightBeforeVisibleWrite(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	const (
		targetPath     = "chapters/current.md"
		redirectedPath = "redirect/current.md"
		before         = "same revision before the parent swap"
	)
	writeShortFictionTestFile(t, workspace, targetPath, before)
	writeShortFictionTestFile(t, workspace, redirectedPath, before)
	provider := newShortFictionTestProvider(t, "# candidate must not reach the redirected target")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace: workspace, TargetPath: targetPath,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "bind the visible write after application preflight",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	beforeGroups := listShortFictionTestGroups(t, application)
	beforeVersions := listShortFictionTestVersions(t, application)
	delegate, err := workspacechange.ForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	swapped := false
	application.shortFiction().workspaceChangeFor = func(string) (shortFictionChangeService, error) {
		return shortFictionChangeServiceFunc(func(
			ctx context.Context,
			req workspacechange.ReplaceFileRequest,
			snapshot workspacechange.ConsistentSnapshotFunc,
		) (workspacechange.ChangeSet, error) {
			if !swapped {
				swapped = true
				parent := filepath.Join(workspace, "chapters")
				if err := os.Rename(parent, parent+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("redirect", parent); err != nil {
					t.Fatal(err)
				}
			}
			return delegate.ReplaceFileWithConsistentSnapshot(ctx, req, snapshot)
		}), nil
	}

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr.Code != workspacechange.ErrorCodeConflict {
		t.Fatalf("confirmation error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("symlink swap returned a confirmation result: %#v", result)
	}
	if got := readShortFictionTestFile(t, workspace, "chapters.original/current.md"); got != before {
		t.Fatalf("original target changed: got=%q want=%q", got, before)
	}
	if got := readShortFictionTestFile(t, workspace, redirectedPath); got != before {
		t.Fatalf("redirected target changed: got=%q want=%q", got, before)
	}
	if groups := listShortFictionTestGroups(t, application); !reflect.DeepEqual(groups, beforeGroups) {
		t.Fatalf("symlink rejection created a ChangeSet: before=%#v after=%#v", beforeGroups, groups)
	}
	if versions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(versions, beforeVersions) {
		t.Fatalf("symlink rejection created a version: before=%#v after=%#v", beforeVersions, versions)
	}
}

func TestFanqieConfirmKeepsPriorDurabilityRecoverySeparateFromCurrentCandidate(t *testing.T) {
	ctx := context.Background()
	workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
	const (
		priorPath   = "recovery/prior.md"
		currentPath = "chapters/current.md"
		before      = "current candidate source"
	)
	writeShortFictionTestFile(t, workspace, currentPath, before)
	provider := newShortFictionTestProvider(t, "# current candidate must remain unwritten")
	application := newShortFictionTestApp(t, workspace, provider.server.URL)
	candidate, err := application.GenerateShortFictionCandidate(ctx, shortfiction.GenerateRequest{
		ProfileID: shortfiction.ProfileFanqieShort,
		Source: shortfiction.SourcePacket{
			Workspace: workspace, TargetPath: currentPath,
			BaseRevision: workspacechange.Revision([]byte(before)),
			Brief:        "do not attribute prior recovery to this candidate",
		},
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	beforeGroups := listShortFictionTestGroups(t, application)
	beforeVersions := listShortFictionTestVersions(t, application)
	application.shortFiction().workspaceChangeFor = func(string) (shortFictionChangeService, error) {
		return shortFictionChangeServiceFunc(func(
			context.Context,
			workspacechange.ReplaceFileRequest,
			workspacechange.ConsistentSnapshotFunc,
		) (workspacechange.ChangeSet, error) {
			return workspacechange.ChangeSet{}, &workspacechange.Error{
				Code:    workspacechange.ErrorCodeDurabilityPending,
				Message: "a prior workspace change still needs recovery",
				Details: map[string]any{
					"path":              priorPath,
					"change_set_id":     "change-prior",
					"workspace_mutated": true,
					"recovery_pending":  true,
				},
			}
		}), nil
	}

	result, err := application.ConfirmShortFictionCandidate(ctx, shortfiction.ConfirmRequest{Candidate: candidate})
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr.Code != workspacechange.ErrorCodeDurabilityPending {
		t.Fatalf("confirmation error = %v, result = %#v", err, result)
	}
	if result != (shortfiction.ConfirmationResult{}) {
		t.Fatalf("prior recovery returned a current confirmation result: %#v", result)
	}
	for key, want := range map[string]any{
		"workspace_mutated":    false,
		"recovery_pending":     true,
		"retryable":            false,
		"recovery_target_path": priorPath,
	} {
		if got := changeErr.Details[key]; got != want {
			t.Fatalf("details[%q] = %#v, want %#v; details=%#v", key, got, want, changeErr.Details)
		}
	}
	for _, forbidden := range []string{"target_path", "write_revision", "change_group_id", "change_set_id"} {
		if _, exists := changeErr.Details[forbidden]; exists {
			t.Fatalf("prior recovery claimed current identity %q: %#v", forbidden, changeErr.Details)
		}
	}
	if got := readShortFictionTestFile(t, workspace, currentPath); got != before {
		t.Fatalf("prior recovery changed current target: got=%q want=%q", got, before)
	}
	if groups := listShortFictionTestGroups(t, application); !reflect.DeepEqual(groups, beforeGroups) {
		t.Fatalf("prior recovery created a current ChangeSet: before=%#v after=%#v", beforeGroups, groups)
	}
	if versions := listShortFictionTestVersions(t, application); !reflect.DeepEqual(versions, beforeVersions) {
		t.Fatalf("prior recovery created a current version: before=%#v after=%#v", beforeVersions, versions)
	}
}
