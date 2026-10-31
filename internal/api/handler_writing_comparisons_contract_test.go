package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	writingcomparisonapp "denova/internal/app/writingcomparison"
	"denova/internal/i18n"
)

// blockingCandidateModel is a local OpenAI-compatible fixture. Every
// candidate call waits for release so the test can observe an active run
// deterministically.
func blockingCandidateModel(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	releaseModel := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "model": request.Model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "candidate prose from " + request.Model}, "finish_reason": "stop"}}})
	}))
	// Cleanups run last-in first-out: release blocked handlers before Close.
	t.Cleanup(server.Close)
	t.Cleanup(releaseModel)
	return server, releaseModel
}

func waitForComparisonTask(t *testing.T, server *Server, detailPath string, ready func(writingcomparisonapp.Task) bool) writingcomparisonapp.Task {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		response := performJSONRequest(t, server, http.MethodGet, detailPath, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
		}
		var body struct {
			Task writingcomparisonapp.Task `json:"task"`
		}
		decodeResponse(t, response.Body.Bytes(), &body)
		if ready(body.Task) {
			return body.Task
		}
		if time.Now().After(deadline) {
			t.Fatalf("comparison task did not reach the expected state: %+v", body.Task)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func sortedJSONKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var object map[string]json.RawMessage
	decodeResponse(t, data, &object)
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestWritingComparisonRunStreamAndContentContract(t *testing.T) {
	model, releaseModel := blockingCandidateModel(t)
	application := newTestApplication(t)
	server := NewServer(application, "0")
	layout := configureWritingComparisonFixture(t, application, model.URL+"/v1")
	prefix := "/api/projects/" + url.PathEscape(application.ProjectID()) + "/writing-comparisons"

	created := performJSONRequest(t, server, http.MethodPost, prefix, map[string]any{
		"target_path":  "chapters/ch00001.md",
		"chapter_task": map[string]any{"text": "Write the opening scene", "source": "author"},
		"candidates": []any{
			map[string]any{"profile_id": "writer-a", "source": map[string]any{"kind": "skill", "id": "novel-lite"}},
			map[string]any{"profile_id": "writer-b", "source": map[string]any{"kind": "skill", "id": "novel-lite"}},
		},
		"review_model_profile_id": "writer-a",
		"final_writer_profile_id": "writer-b",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var createBody struct {
		Task writingcomparisonapp.Task `json:"task"`
	}
	decodeResponse(t, created.Body.Bytes(), &createBody)
	taskPath := prefix + "/" + url.PathEscape(createBody.Task.ID)

	run := performJSONRequest(t, server, http.MethodPost, taskPath+"/run", map[string]any{"expected_revision": createBody.Task.Revision})
	if run.Code != http.StatusAccepted {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}
	if keys := sortedJSONKeys(t, run.Body.Bytes()); !slices.Equal(keys, []string{"project_id", "stream_cursor", "task", "task_id"}) {
		t.Fatalf("run payload keys = %v", keys)
	}
	if bytes.Contains(run.Body.Bytes(), []byte(`"body"`)) {
		t.Fatalf("run payload leaked a source body: %s", run.Body.String())
	}
	var runBody struct {
		ProjectID string                    `json:"project_id"`
		Task      writingcomparisonapp.Task `json:"task"`
		TaskID    string                    `json:"task_id"`
	}
	decodeResponse(t, run.Body.Bytes(), &runBody)
	if runBody.ProjectID != application.ProjectID() || runBody.TaskID == "" || runBody.Task.Phase != writingcomparisonapp.PhaseGenerating || runBody.Task.Execution != writingcomparisonapp.ExecutionQueued || runBody.Task.Snapshot == nil {
		t.Fatalf("run payload = %+v", runBody)
	}

	// Once candidate-1 is blocked in the model call the revision is stable,
	// so a second run with the latest revision reaches the active-run guard.
	active := waitForComparisonTask(t, server, taskPath, func(task writingcomparisonapp.Task) bool {
		return task.Candidates[0].Status == writingcomparisonapp.CandidateRunning
	})
	again := performJSONRequest(t, server, http.MethodPost, taskPath+"/run", map[string]any{"expected_revision": active.Revision})
	assertWritingComparisonError(t, again, http.StatusConflict, writingcomparisonapp.CodeRunActive)
	var againBody struct {
		Error string `json:"error"`
	}
	decodeResponse(t, again.Body.Bytes(), &againBody)
	if want := i18n.FromHeader("").T("api.writingComparison.runActive"); againBody.Error != want || want == "api.writingComparison.runActive" {
		t.Fatalf("run_active message = %q, want %q", againBody.Error, want)
	}

	releaseModel()
	done := waitForComparisonTask(t, server, taskPath, func(task writingcomparisonapp.Task) bool {
		return task.Phase == writingcomparisonapp.PhaseGenerated
	})
	if done.Execution != writingcomparisonapp.ExecutionDone {
		t.Fatalf("finished task = %+v", done)
	}

	stream := performJSONRequest(t, server, http.MethodGet, taskPath+"/stream", nil)
	if stream.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", stream.Code, stream.Body.String())
	}
	for _, event := range []string{"execution.changed", "candidate.started", "candidate.completed", "comparison.completed"} {
		if !bytes.Contains(stream.Body.Bytes(), []byte(event)) {
			t.Fatalf("stream replay is missing %q:\n%s", event, stream.Body.String())
		}
	}

	content := performJSONRequest(t, server, http.MethodGet, taskPath+"/candidates/candidate-1/content?revision=1", nil)
	if content.Code != http.StatusOK {
		t.Fatalf("content status=%d body=%s", content.Code, content.Body.String())
	}
	if keys := sortedJSONKeys(t, content.Body.Bytes()); !slices.Equal(keys, []string{"candidate_id", "content", "revision", "sha256"}) {
		t.Fatalf("content payload keys = %v", keys)
	}
	var contentBody writingcomparisonapp.CandidateContent
	decodeResponse(t, content.Body.Bytes(), &contentBody)
	digest := sha256.Sum256([]byte(contentBody.Content))
	if contentBody.CandidateID != "candidate-1" || contentBody.Revision != 1 || contentBody.Content != "candidate prose from fixture-a" || contentBody.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("content payload = %+v", contentBody)
	}

	assetPath, err := writingcomparisonapp.NewStore(layout).AssetPath(createBody.Task.ID, "candidates/candidate-1/1.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(assetPath); err != nil {
		t.Fatal(err)
	}
	missingAsset := performJSONRequest(t, server, http.MethodGet, taskPath+"/candidates/candidate-1/content?revision=1", nil)
	assertWritingComparisonError(t, missingAsset, http.StatusConflict, writingcomparisonapp.CodeAssetCorrupt)
}

func TestWritingComparisonRecoveryFailureStaysOnComparisonRoutes(t *testing.T) {
	application := newTestApplication(t)
	server := NewServer(application, "0")
	layout := configureWritingComparisonFixture(t, application, "http://127.0.0.1:1/v1")
	project := "/api/projects/" + url.PathEscape(application.ProjectID())

	// A regular file where the comparisons directory belongs makes the
	// first-access recovery scan fail.
	comparisons := layout.ComparisonsDir()
	if err := os.RemoveAll(comparisons); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(comparisons), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(comparisons, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	unrelated := performJSONRequest(t, server, http.MethodGet, project+"/skills", nil)
	if unrelated.Code != http.StatusOK {
		t.Fatalf("unrelated Project request status=%d body=%s", unrelated.Code, unrelated.Body.String())
	}
	failed := performJSONRequest(t, server, http.MethodGet, project+"/writing-comparisons", nil)
	assertWritingComparisonError(t, failed, http.StatusInternalServerError, writingcomparisonapp.CodeRecoveryFailed)
	if again := performJSONRequest(t, server, http.MethodGet, project+"/skills", nil); again.Code != http.StatusOK {
		t.Fatalf("unrelated Project request after recovery failure status=%d body=%s", again.Code, again.Body.String())
	}

	// A failed scan is not remembered, so the next comparison request retries.
	if err := os.Remove(comparisons); err != nil {
		t.Fatal(err)
	}
	recovered := performJSONRequest(t, server, http.MethodGet, project+"/writing-comparisons", nil)
	if recovered.Code != http.StatusOK {
		t.Fatalf("comparison list after recovery retry status=%d body=%s", recovered.Code, recovered.Body.String())
	}
}
