package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"denova/internal/agent"
	"denova/internal/workspacechange"
)

func TestQualityHarnessTaskSnapshotLiveBoundaryExecutesOnceAndReplaysCompletion(t *testing.T) {
	firstEventEmitted := make(chan struct{})
	releaseLiveEvents := make(chan struct{})
	var executions atomic.Int32
	task := NewTask(func(_ context.Context, _ *Task, emit func(agent.Event)) {
		executions.Add(1)
		emit(agent.Event{Type: "snapshot", Data: map[string]string{"sequence": "one"}})
		close(firstEventEmitted)
		<-releaseLiveEvents
		emit(agent.Event{Type: "live", Data: map[string]string{"sequence": "two"}})
		emit(agent.Event{Type: "done", Data: map[string]string{"sequence": "three"}})
	})

	<-firstEventEmitted
	snapshot, live := task.Subscribe()
	close(releaseLiveEvents)
	firstDelivery := append([]agent.Event(nil), snapshot...)
	for event := range live {
		firstDelivery = append(firstDelivery, event)
	}

	if executions.Load() != 1 {
		t.Fatalf("task executions = %d, want 1", executions.Load())
	}
	if got := eventTypes(firstDelivery); !reflect.DeepEqual(got, []string{"snapshot", "live", "done"}) {
		t.Fatalf("snapshot/live delivery = %v, want each event exactly once in order", got)
	}

	replayed, replayLive := task.Subscribe()
	if _, open := <-replayLive; open {
		t.Fatal("completed task returned an open live subscription")
	}
	if got := eventTypes(replayed); !reflect.DeepEqual(got, []string{"snapshot", "live", "done"}) {
		t.Fatalf("completed task replay = %v, want completion included", got)
	}
	if executions.Load() != 1 {
		t.Fatalf("reconnect reran task: executions=%d", executions.Load())
	}
}

func TestQualityHarnessFormalWorkspaceMutationSequencePreservesRevisionsAndVersionSnapshot(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	application := newWorkspaceMutationTestApp(workspace)
	const path = "chapters/ch01.md"
	if err := application.BookService().Create(path, "file", "draft"); err != nil {
		t.Fatalf("create formal chapter: %v", err)
	}
	sharedService, err := workspacechange.ForWorkspace(workspace)
	if err != nil {
		t.Fatalf("create shared workspace change service: %v", err)
	}
	_, draftRevision, err := sharedService.ReadFile(path)
	if err != nil {
		t.Fatalf("read formal chapter revision: %v", err)
	}

	var editorSave workspacechange.SaveResult
	if _, err := application.WithWorkspaceChangeMutation(ctx, workspace, func(service *workspacechange.Service) (WorkspaceChangeMutationHooks, error) {
		if service != sharedService {
			return WorkspaceChangeMutationHooks{}, errors.New("editor save did not receive the shared workspace service")
		}
		var saveErr error
		editorSave, saveErr = service.SaveFile(ctx, path, "editor draft", draftRevision)
		return WorkspaceChangeMutationHooks{}, saveErr
	}); err != nil {
		t.Fatalf("editor save: %v", err)
	}

	var agentChange workspacechange.ChangeSet
	if _, err := application.WithWorkspaceChangeMutation(ctx, workspace, func(service *workspacechange.Service) (WorkspaceChangeMutationHooks, error) {
		if service != sharedService {
			return WorkspaceChangeMutationHooks{}, errors.New("Agent change did not receive the shared workspace service")
		}
		var applyErr error
		agentChange, applyErr = service.ApplyEdits(ctx, workspacechange.ApplyEditsRequest{
			Path:         path,
			BaseRevision: editorSave.Revision,
			Edits:        []workspacechange.TextEdit{{ID: "agent-edit", OldString: "editor", NewString: "agent"}},
			Metadata:     workspacechange.ChangeMetadata{Origin: workspacechange.OriginAgent, ChangeGroupID: "quality-harness-group"},
		})
		return WorkspaceChangeMutationHooks{}, applyErr
	}); err != nil {
		t.Fatalf("Agent change: %v", err)
	}
	if agentChange.BaseRevision != editorSave.Revision {
		t.Fatalf("Agent base revision = %q, want editor revision %q", agentChange.BaseRevision, editorSave.Revision)
	}

	var reviewResult workspacechange.ReviewResult
	if _, err := application.WithWorkspaceChangeMutation(ctx, workspace, func(service *workspacechange.Service) (WorkspaceChangeMutationHooks, error) {
		if service != sharedService {
			return WorkspaceChangeMutationHooks{}, errors.New("review did not receive the shared workspace service")
		}
		var reviewErr error
		reviewResult, reviewErr = service.ReviewWithResult(ctx, workspacechange.ReviewRequest{
			GroupID:      agentChange.GroupID,
			ChangeSetID:  agentChange.ID,
			Decision:     workspacechange.ReviewDecisionReject,
			EditIDs:      []string{"agent-edit"},
			BaseRevision: agentChange.Revision,
		})
		return WorkspaceChangeMutationHooks{}, reviewErr
	}); err != nil {
		t.Fatalf("review rejection: %v", err)
	}
	if !reflect.DeepEqual(reviewResult.AffectedPaths, []string{path}) {
		t.Fatalf("review affected paths = %v, want [%s]", reviewResult.AffectedPaths, path)
	}
	if got, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path))); err != nil || string(got) != "editor draft" {
		t.Fatalf("reviewed formal content = %q, err=%v", got, err)
	}

	version, err := application.CreateVersion(ctx, "quality harness boundary snapshot")
	if err != nil || version.Version == nil {
		t.Fatalf("create version snapshot: result=%#v err=%v", version, err)
	}
	if version.Version.FileCount != 1 {
		t.Fatalf("version file count = %d, want 1", version.Version.FileCount)
	}
	if !reflect.DeepEqual(version.Version.ChangedPaths, []string{path}) {
		t.Fatalf("version changed paths = %v, want [%s]", version.Version.ChangedPaths, path)
	}
	status, err := application.VersionStatus(ctx)
	if err != nil || !status.Clean || status.Latest == nil || status.Latest.ID != version.Version.ID {
		t.Fatalf("version snapshot did not capture the reviewed workspace: status=%#v err=%v", status, err)
	}
}

func TestQualityHarnessWrongBaseRevisionCannotOverwriteFormalFile(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	application := newWorkspaceMutationTestApp(workspace)
	const path = "chapters/ch01.md"
	if err := application.BookService().Create(path, "file", "formal content"); err != nil {
		t.Fatalf("create formal chapter: %v", err)
	}

	_, err := application.WithWorkspaceChangeMutation(ctx, workspace, func(service *workspacechange.Service) (WorkspaceChangeMutationHooks, error) {
		_, saveErr := service.SaveFile(ctx, path, "silent overwrite", workspacechange.Revision([]byte("wrong base")))
		return WorkspaceChangeMutationHooks{}, saveErr
	})
	var changeErr *workspacechange.Error
	if !errors.As(err, &changeErr) || changeErr.Code != workspacechange.ErrorCodeRevisionConflict {
		t.Fatalf("wrong base error = %#v, want %s", err, workspacechange.ErrorCodeRevisionConflict)
	}
	content, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
	if readErr != nil || string(content) != "formal content" {
		t.Fatalf("wrong base overwrote formal file: content=%q err=%v", content, readErr)
	}
}

func eventTypes(events []agent.Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}
