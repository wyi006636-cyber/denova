package writingcomparison

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"denova/config"
	"denova/internal/agents/prompts"
	"denova/internal/agents/skills"
	"denova/internal/writingtemplates"
	"github.com/alfredxw/denova/agent/model/providers"
)

type fixtureTemplateResolver struct{}

func (fixtureTemplateResolver) ResolveCandidateSource(context.Context, WritingSourceRef) (WritingSourceSnapshot, error) {
	return WritingSourceSnapshot{Kind: "template", ID: "fixture", Version: "1", Scope: "builtin", Title: writingtemplates.Localized{ZH: "Fixture", EN: "Fixture"}, ContentSHA256: AssetSHA256([]byte("Write prose.")), Body: "Write prose."}, nil
}

func TestRunCandidatesAreReadOnlyAndPartialFailureKeepsCompletedCandidate(t *testing.T) {
	var mu sync.Mutex
	var requests []struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		if request.Model == "fixture-b" {
			http.Error(w, "fixture failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "model": request.Model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "completed candidate"}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	layout := testLayout(t)
	chapters := filepath.Join(layout.ContentRoot, "chapters")
	setting := filepath.Join(layout.ContentRoot, "setting")
	if err := os.MkdirAll(chapters, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(setting, 0o755); err != nil {
		t.Fatal(err)
	}
	formal := []byte("formal chapter")
	progress := []byte("formal progress")
	if err := os.WriteFile(filepath.Join(chapters, "ch00001.md"), formal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setting, "progress.md"), progress, 0o600); err != nil {
		t.Fatal(err)
	}
	skillRoot := filepath.Join(layout.ContentRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillRoot, "fixture-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "fixture-skill", skills.SkillFileName), []byte("---\nname: fixture-skill\ndescription: fixture\n---\nWrite a complete chapter."), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{ModelEndpoints: []config.ModelEndpointSettings{{ID: "fixture", Provider: string(providers.ProviderOpenAICompatible), Protocol: string(providers.ProtocolOpenAIChatCompletions), BaseURL: server.URL + "/v1", APIKey: "test-key"}}, ModelProfiles: []config.ModelProfileSettings{{ID: "writer-a", EndpointID: "fixture", Model: "fixture-a"}, {ID: "writer-b", EndpointID: "fixture", Model: "fixture-b"}}}
	host := testHost{cfg: cfg, dirs: []skills.Directory{{Scope: skills.ScopeWorkspace, Path: skillRoot}}}
	service := NewService(host, WithTemplateResolver(fixtureTemplateResolver{}))
	input := CreateInput{TargetPath: "chapters/ch00002.md", ChapterTask: ChapterTask{Text: "Write the chapter", Source: "author"}, Candidates: []CandidateInput{{ProfileID: "writer-a", Source: WritingSourceRef{Kind: "skill", ID: "fixture-skill"}}, {ProfileID: "writer-b", Source: WritingSourceRef{Kind: "template", ID: "fixture", Version: "1"}}}, ReviewModelProfileID: "writer-a", FinalWriterProfileID: "writer-b"}
	task, err := service.Create(context.Background(), layout, input)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.Run(context.Background(), layout, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(context.Background(), layout, queued.ID, nil); err != nil {
		t.Fatal(err)
	}
	completed, err := service.Get(context.Background(), layout, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Phase != PhasePartial || completed.Execution != ExecutionDone || completed.Candidates[0].Status != CandidateCompleted || completed.Candidates[1].Status != CandidateFailed {
		t.Fatalf("run result = %+v", completed)
	}
	content, err := service.CandidateContent(context.Background(), layout, completed.ID, "candidate-1", 1)
	if err != nil || content.SHA256 != AssetSHA256([]byte(content.Content)) {
		t.Fatalf("candidate content = %+v, err=%v", content, err)
	}
	if got, _ := os.ReadFile(filepath.Join(chapters, "ch00001.md")); string(got) != string(formal) {
		t.Fatal("formal chapter changed during candidate execution")
	}
	if got, _ := os.ReadFile(filepath.Join(setting, "progress.md")); string(got) != string(progress) {
		t.Fatal("formal progress changed during candidate execution")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0].Messages[len(requests[0].Messages)-1].Content != requests[1].Messages[len(requests[1].Messages)-1].Content {
		t.Fatalf("model requests do not share the frozen user message: %+v", requests)
	}
	if completed.Snapshot == nil || completed.Snapshot.SHA256 == "" {
		t.Fatal("completed task is missing the frozen snapshot hash")
	}
	if _, err := service.Run(context.Background(), layout, completed.ID, completed.Revision); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("partial task rerun error=%v, want state conflict", err)
	}
	for index, request := range requests {
		if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
			t.Fatalf("candidate request %d is not the two-message read-only contract: %+v", index, request)
		}
	}
	if requests[0].Messages[0].Content != prompts.ComposeCandidateDraftInstruction("---\nname: fixture-skill\ndescription: fixture\n---\nWrite a complete chapter.", "") {
		t.Fatalf("candidate system instruction diverges from preview contract: %q", requests[0].Messages[0].Content)
	}
}

func TestRecoverMarksStartedCandidateIncompleteWithoutReplay(t *testing.T) {
	layout := testLayout(t)
	service := NewService(testHost{cfg: testConfig()})
	task, err := NewStore(layout).Create(context.Background(), testTask(layout))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := NewStore(layout).Open(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err = handle.Append(context.Background(), task.Revision, Event{Kind: EventPhaseChanged, FromPhase: PhaseConfiguring, ToPhase: PhaseGenerating})
	if err != nil {
		t.Fatal(err)
	}
	task, err = handle.Append(context.Background(), task.Revision, Event{Kind: EventExecutionChanged, FromExecution: ExecutionIdle, ToExecution: ExecutionQueued})
	if err != nil {
		t.Fatal(err)
	}
	task, err = handle.Append(context.Background(), task.Revision, Event{Kind: EventExecutionChanged, FromExecution: ExecutionQueued, ToExecution: ExecutionRunning})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Append(context.Background(), task.Revision, Event{Kind: EventCandidateStarted, CandidateID: "candidate-1"}); err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	if err := service.Recover(context.Background(), layout); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.Get(context.Background(), layout, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Execution != ExecutionIncomplete || recovered.Candidates[0].Status != CandidateIncomplete {
		t.Fatalf("recovered task = %+v", recovered)
	}
}

func TestRecoveryDoesNotReplayCandidateModelCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "model": "fixture-a", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "candidate"}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	layout := testLayout(t)
	cfg := config.Config{ModelEndpoints: []config.ModelEndpointSettings{{ID: "fixture", Provider: string(providers.ProviderOpenAICompatible), Protocol: string(providers.ProtocolOpenAIChatCompletions), BaseURL: server.URL + "/v1", APIKey: "test-key"}}, ModelProfiles: []config.ModelProfileSettings{{ID: "writer-a", EndpointID: "fixture", Model: "fixture-a"}, {ID: "writer-b", EndpointID: "fixture", Model: "fixture-b"}}}
	service := NewService(testHost{cfg: cfg}, WithTemplateResolver(fixtureTemplateResolver{}))
	task, err := service.Create(context.Background(), layout, CreateInput{TargetPath: "chapters/ch00001.md", ChapterTask: ChapterTask{Text: "task"}, Candidates: []CandidateInput{{ProfileID: "writer-a", Source: WritingSourceRef{Kind: "template", ID: "fixture", Version: "1"}}, {ProfileID: "writer-b", Source: WritingSourceRef{Kind: "template", ID: "fixture", Version: "1"}}}, ReviewModelProfileID: "writer-a", FinalWriterProfileID: "writer-b"})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.Run(context.Background(), layout, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := NewStore(layout).Open(context.Background(), queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err = handle.Append(context.Background(), queued.Revision, Event{Kind: EventExecutionChanged, FromExecution: ExecutionQueued, ToExecution: ExecutionRunning})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Append(context.Background(), queued.Revision, Event{Kind: EventCandidateStarted, CandidateID: "candidate-1"}); err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	restartedService := NewService(testHost{cfg: cfg}, WithTemplateResolver(fixtureTemplateResolver{}))
	if err := restartedService.Recover(context.Background(), layout); err != nil {
		t.Fatal(err)
	}
	recovered, err := restartedService.Get(context.Background(), layout, queued.ID)
	if err != nil || recovered.Candidates[0].Status != CandidateIncomplete {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("recovery replayed %d model calls", got)
	}
}

func TestRunWhileActiveReturnsRunActiveCode(t *testing.T) {
	layout := testLayout(t)
	service := NewService(testHost{cfg: testConfig()})
	task, err := NewStore(layout).Create(context.Background(), testTask(layout))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.Run(context.Background(), layout, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// The latest revision passes the CAS check, so only the active-run guard
	// can reject this request.
	_, err = service.Run(context.Background(), layout, queued.ID, queued.Revision)
	if code := ErrorCode(err); code != CodeRunActive {
		t.Fatalf("second run code=%q err=%v, want %q", code, err, CodeRunActive)
	}
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("run_active must remain a state conflict subtype: %v", err)
	}
	current, err := service.Get(context.Background(), layout, queued.ID)
	if err != nil || current.Revision != queued.Revision || current.Execution != ExecutionQueued {
		t.Fatalf("rejected rerun changed the task: %+v err=%v", current, err)
	}
}

func TestRegistrationFailureReleasesActiveRunAndAllowsRunAgain(t *testing.T) {
	layout := testLayout(t)
	service := NewService(testHost{cfg: testConfig()})
	task, err := NewStore(layout).Create(context.Background(), testTask(layout))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.Run(context.Background(), layout, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RegistrationFailed(context.Background(), layout, queued.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.Get(context.Background(), layout, queued.ID)
	if err != nil || recovered.Execution != ExecutionIncomplete {
		t.Fatalf("registration rollback task=%+v err=%v", recovered, err)
	}
	if _, err := service.Run(context.Background(), layout, queued.ID, recovered.Revision); err != nil {
		t.Fatalf("task could not be queued again after registration failure: %v", err)
	}
}
