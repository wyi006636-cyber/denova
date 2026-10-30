package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"

	"denova/config"
	"denova/internal/agents/skills"
	writingcomparisonapp "denova/internal/app/writingcomparison"
	projectdomain "denova/internal/project"
)

func TestWritingComparisonEndpointsSuccessAndErrors(t *testing.T) {
	application := newTestApplication(t)
	server := NewServer(application, "0")
	projectID := application.ProjectID()
	configureWritingComparisonFixture(t, application)
	prefix := "/api/projects/" + url.PathEscape(projectID) + "/writing-comparisons"

	options := performJSONRequest(t, server, http.MethodGet, prefix+"/candidate-options", nil)
	if options.Code != http.StatusOK {
		t.Fatalf("candidate options status=%d body=%s", options.Code, options.Body.String())
	}
	var optionBody struct {
		Profiles []writingcomparisonapp.CandidateOption `json:"profiles"`
	}
	decodeResponse(t, options.Body.Bytes(), &optionBody)
	if len(optionBody.Profiles) < 2 {
		t.Fatalf("candidate options = %#v", optionBody.Profiles)
	}

	createBody := map[string]any{
		"target_path":  "chapters/ch00001.md",
		"chapter_task": map[string]any{"text": "Write the opening scene", "source": "author"},
		"candidates": []any{
			map[string]any{"profile_id": "writer-a", "source": map[string]any{"kind": "skill", "id": "novel-lite"}},
			map[string]any{"profile_id": "writer-b", "source": map[string]any{"kind": "skill", "id": "novel-lite"}},
		},
		"review_model_profile_id": "writer-a",
		"final_writer_profile_id": "writer-b",
	}
	created := performJSONRequest(t, server, http.MethodPost, prefix, createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Task writingcomparisonapp.Task `json:"task"`
	}
	decodeResponse(t, created.Body.Bytes(), &createResponse)
	if createResponse.Task.ID == "" || createResponse.Task.Target.Path != "chapters/ch00001.md" {
		t.Fatalf("created task = %#v", createResponse.Task)
	}

	list := performJSONRequest(t, server, http.MethodGet, prefix, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var listResponse struct {
		Tasks []writingcomparisonapp.TaskSummary `json:"tasks"`
	}
	decodeResponse(t, list.Body.Bytes(), &listResponse)
	if len(listResponse.Tasks) != 1 || listResponse.Tasks[0].ID != createResponse.Task.ID {
		t.Fatalf("list response = %#v", listResponse.Tasks)
	}

	detail := performJSONRequest(t, server, http.MethodGet, prefix+"/"+url.PathEscape(createResponse.Task.ID), nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var detailResponse struct {
		Task writingcomparisonapp.Task `json:"task"`
	}
	decodeResponse(t, detail.Body.Bytes(), &detailResponse)
	if detailResponse.Task.ID != createResponse.Task.ID {
		t.Fatalf("detail response = %#v", detailResponse.Task)
	}

	invalidCreate := performJSONRequest(t, server, http.MethodPost, prefix, map[string]any{
		"target_path": "chapters/ch00001.md", "chapter_task": map[string]any{"text": "task"},
		"candidates":              []map[string]any{{"profile_id": "writer-a", "source": map[string]any{"kind": "skill", "id": "novel-lite"}}},
		"review_model_profile_id": "writer-a", "final_writer_profile_id": "writer-b",
	})
	assertWritingComparisonError(t, invalidCreate, http.StatusBadRequest, writingcomparisonapp.CodeCandidateCount)

	invalidPhase := performJSONRequest(t, server, http.MethodGet, prefix+"?phase=nonsense", nil)
	assertWritingComparisonError(t, invalidPhase, http.StatusBadRequest, writingcomparisonapp.CodeInvalidRequest)

	missing := performJSONRequest(t, server, http.MethodGet, prefix+"/missing-task", nil)
	assertWritingComparisonError(t, missing, http.StatusNotFound, writingcomparisonapp.CodeNotFound)
}

func configureWritingComparisonFixture(t *testing.T, application interface {
	ProjectID() string
	Workspace() string
}) {
	t.Helper()
	skillPath := filepath.Join(application.Workspace(), "skills", "novel-lite", skills.SkillFileName)
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: novel-lite\ndescription: fixture\n---\nUse concise prose."), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := projectdomain.NewRegistry(application.Workspace())
	record, err := registry.Get(application.ProjectID())
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.Layout(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteSettingsFile(layout.ConfigPath(), config.Settings{
		ModelEndpoints: []config.ModelEndpointSettings{{ID: "fixture", Provider: "openai-compatible", Protocol: "openai-chat-completions", BaseURL: "http://127.0.0.1:18080/v1", APIKey: "fixture-key"}},
		ModelProfiles:  []config.ModelProfileSettings{{ID: "writer-a", Name: "Writer A", EndpointID: "fixture", Model: "fixture-a"}, {ID: "writer-b", Name: "Writer B", EndpointID: "fixture", Model: "fixture-b"}},
	}); err != nil {
		t.Fatalf("write comparison fixture config: %v", err)
	}
	if err := config.WriteSettingsFile(filepath.Join(application.Workspace(), "config.toml"), config.Settings{
		ModelEndpoints: []config.ModelEndpointSettings{{ID: "fixture", Provider: "openai-compatible", Protocol: "openai-chat-completions", BaseURL: "http://127.0.0.1:18080/v1", APIKey: "fixture-key"}},
		ModelProfiles:  []config.ModelProfileSettings{{ID: "writer-a", Name: "Writer A", EndpointID: "fixture", Model: "fixture-a"}, {ID: "writer-b", Name: "Writer B", EndpointID: "fixture", Model: "fixture-b"}},
	}); err != nil {
		t.Fatalf("write comparison global fixture config: %v", err)
	}
}

func assertWritingComparisonError(t *testing.T, response *ut.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("error status=%d body=%s, want %d", response.Code, response.Body.String(), status)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != code {
		t.Fatalf("error code=%q body=%s, want %q", body.Code, response.Body.String(), code)
	}
}
