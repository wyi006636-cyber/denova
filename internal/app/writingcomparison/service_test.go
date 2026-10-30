package writingcomparison

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"denova/config"
	"denova/internal/agents/skills"
	projectdomain "denova/internal/project"

	"github.com/alfredxw/denova/agent/model/providers"
)

type testHost struct {
	cfg  config.Config
	dirs []skills.Directory
}

func (host testHost) Config(context.Context, projectdomain.Layout) (config.Config, error) {
	return host.cfg, nil
}
func (host testHost) SkillDirectories(context.Context, projectdomain.Layout) ([]skills.Directory, error) {
	return host.dirs, nil
}

type testSourceResolver struct{}

func (testSourceResolver) ResolveCandidateSource(context.Context, WritingSourceRef) (WritingSourceSnapshot, error) {
	return WritingSourceSnapshot{Kind: "skill", ID: "novel-lite", Version: "v1", Scope: "builtin", Title: "Novel Lite", ContentSHA256: "source-hash"}, nil
}

type selectiveSourceResolver struct{}

func (selectiveSourceResolver) ResolveCandidateSource(_ context.Context, ref WritingSourceRef) (WritingSourceSnapshot, error) {
	if ref.Kind == "template" {
		return WritingSourceSnapshot{}, &Error{Code: CodeTemplateCatalogUnavailable, Message: "template catalog unavailable"}
	}
	return WritingSourceSnapshot{Kind: "skill", ID: "novel-lite", Version: "v1", Scope: "builtin", Title: "Novel Lite", ContentSHA256: "source-hash"}, nil
}

func testConfig() config.Config {
	return config.Config{
		ModelEndpoints: []config.ModelEndpointSettings{{ID: "fixture", Provider: string(providers.ProviderOpenAICompatible), Protocol: string(providers.ProtocolOpenAIChatCompletions), BaseURL: "http://127.0.0.1:18080/v1", APIKey: "fixture-key"}},
		ModelProfiles: []config.ModelProfileSettings{
			{ID: "writer-a", Name: "Writer A", EndpointID: "fixture", Model: "fixture-a"},
			{ID: "writer-b", Name: "Writer B", EndpointID: "fixture", Model: "fixture-b"},
		},
	}
}

func TestCreateFreezesProfilesAndDoesNotTouchBookFiles(t *testing.T) {
	layout := testLayout(t)
	chapters := filepath.Join(layout.ContentRoot, "chapters")
	setting := filepath.Join(layout.ContentRoot, "setting")
	if err := os.MkdirAll(chapters, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(setting, 0o755); err != nil {
		t.Fatal(err)
	}
	skillRoot := filepath.Join(layout.ContentRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillRoot, "novel-lite"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "novel-lite", skills.SkillFileName), []byte("---\nname: novel-lite\ndescription: fixture\n---\nUse concise prose."), 0o600); err != nil {
		t.Fatal(err)
	}
	chapterFile := filepath.Join(chapters, "ch00001.md")
	settingFile := filepath.Join(setting, "progress.md")
	if err := os.WriteFile(chapterFile, []byte("formal chapter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingFile, []byte("progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeChapter, _ := os.ReadFile(chapterFile)
	beforeSetting, _ := os.ReadFile(settingFile)
	service := NewService(testHost{cfg: testConfig(), dirs: []skills.Directory{{Scope: skills.ScopeWorkspace, Path: skillRoot}}})
	task, err := service.Create(context.Background(), layout, CreateInput{TargetPath: "chapters/ch00002.md", ChapterTask: ChapterTask{Text: "Write a new chapter", Source: "author"}, Candidates: []CandidateInput{{ProfileID: "writer-a", Source: WritingSourceRef{Kind: "skill", ID: "novel-lite"}, Requirement: "Tense dialogue"}, {ProfileID: "writer-b", Source: WritingSourceRef{Kind: "skill", ID: "novel-lite"}, Requirement: "Quiet atmosphere"}}, ReviewModelProfileID: "writer-a", FinalWriterProfileID: "writer-b"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Phase != PhaseConfiguring || task.Execution != ExecutionIdle || task.Target.BaseRevision != "missing" {
		t.Fatalf("created task = %+v", task)
	}
	if task.ReviewModel.ProfileID != "writer-a" || task.FinalWriter.ProfileID != "writer-b" || task.ExpectedCalls.Candidates != 2 {
		t.Fatalf("profile snapshots = %+v", task)
	}
	afterChapter, _ := os.ReadFile(chapterFile)
	afterSetting, _ := os.ReadFile(settingFile)
	if string(beforeChapter) != string(afterChapter) || string(beforeSetting) != string(afterSetting) {
		t.Fatal("formal writing files changed during task creation")
	}
}

func TestCandidateProfileReasons(t *testing.T) {
	cfg := testConfig()
	disabled := false
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "disabled", EndpointID: "fixture", Model: "disabled", Enabled: &disabled})
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "missing-endpoint", EndpointID: "gone", Model: "missing"})
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "missing-model", EndpointID: "fixture"})
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "bad-protocol", EndpointID: "fixture", Model: "bad", Protocol: "unknown"})
	cfg.ModelEndpoints = append(cfg.ModelEndpoints, config.ModelEndpointSettings{ID: "remote-no-key", Provider: string(providers.ProviderOpenAICompatible), Protocol: string(providers.ProtocolOpenAIChatCompletions), BaseURL: "https://models.example/v1"})
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "missing-key", EndpointID: "remote-no-key", Model: "remote-model"})
	checks := map[string]string{"does-not-exist": "profile_missing", "disabled": "profile_disabled", "missing-endpoint": "endpoint_missing", "missing-model": "model_missing", "bad-protocol": "protocol_unsupported", "missing-key": "endpoint_incomplete"}
	for profileID, want := range checks {
		got := CheckCandidateProfile(&cfg, profileID)
		if got.Option.Eligible || got.Option.Reason != want {
			t.Errorf("profile %s check = %+v, want %s", profileID, got.Option, want)
		}
	}
	if got := CheckCandidateProfile(&cfg, "writer-a"); !got.Option.Eligible || got.Snapshot.ProfileID != "writer-a" {
		t.Fatalf("valid profile check = %+v", got)
	}
}

func TestDefaultProfileUsesStoredSettingsAndLocalEndpointMayBeKeyless(t *testing.T) {
	cfg := testConfig()
	disabled := false
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "default", Model: "stored-default", EndpointID: "fixture", Enabled: &disabled})
	check := CheckCandidateProfile(&cfg, "default")
	if check.Option.Eligible || check.Option.Reason != "profile_disabled" {
		t.Fatalf("stored default profile check = %+v", check.Option)
	}
	local := "local"
	cfg.ModelEndpoints = append(cfg.ModelEndpoints, config.ModelEndpointSettings{ID: local, Provider: string(providers.ProviderOpenAICompatible), Protocol: string(providers.ProtocolOpenAIChatCompletions), BaseURL: "http://127.0.0.1:11434/v1"})
	cfg.ModelProfiles = append(cfg.ModelProfiles, config.ModelProfileSettings{ID: "local", EndpointID: local, Model: "local-model"})
	check = CheckCandidateProfile(&cfg, "local")
	if !check.Option.Eligible {
		t.Fatalf("keyless local profile check = %+v", check.Option)
	}
}

func TestCreateValidationAndTemplateUnavailable(t *testing.T) {
	layout := testLayout(t)
	skillRoot := filepath.Join(layout.ContentRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillRoot, "novel-lite"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "novel-lite", skills.SkillFileName), []byte("---\nname: novel-lite\ndescription: fixture\n---\nUse concise prose."), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(testHost{cfg: testConfig(), dirs: []skills.Directory{{Scope: skills.ScopeWorkspace, Path: skillRoot}}}, WithTemplateResolver(selectiveSourceResolver{}))
	base := CreateInput{TargetPath: "chapters/ch00001.md", ChapterTask: ChapterTask{Text: "task"}, Candidates: []CandidateInput{{ProfileID: "writer-a", Source: WritingSourceRef{Kind: "skill", ID: "novel-lite"}}, {ProfileID: "writer-b", Source: WritingSourceRef{Kind: "template", ID: "missing", Version: "1"}}}, ReviewModelProfileID: "writer-a", FinalWriterProfileID: "writer-b"}
	if _, err := service.Create(context.Background(), layout, CreateInput{TargetPath: base.TargetPath, ChapterTask: base.ChapterTask, Candidates: base.Candidates[:1], ReviewModelProfileID: base.ReviewModelProfileID, FinalWriterProfileID: base.FinalWriterProfileID}); !hasCode(err, CodeCandidateCount) {
		t.Fatalf("candidate count error = %v", err)
	}
	tooMany := append([]CandidateInput(nil), base.Candidates...)
	tooMany = append(tooMany, base.Candidates...)
	tooMany = append(tooMany, CandidateInput{ProfileID: "writer-a", Source: WritingSourceRef{Kind: "skill", ID: "novel-lite"}})
	if _, err := service.Create(context.Background(), layout, CreateInput{TargetPath: base.TargetPath, ChapterTask: base.ChapterTask, Candidates: tooMany, ReviewModelProfileID: base.ReviewModelProfileID, FinalWriterProfileID: base.FinalWriterProfileID}); !hasCode(err, CodeCandidateCount) {
		t.Fatalf("too many candidates error = %v", err)
	}
	if _, err := service.Create(context.Background(), layout, base); !hasCode(err, CodeTemplateCatalogUnavailable) {
		t.Fatalf("template error = %v", err)
	}
	if _, err := service.Create(context.Background(), layout, CreateInput{TargetPath: "../outside.md", ChapterTask: base.ChapterTask, Candidates: base.Candidates, ReviewModelProfileID: base.ReviewModelProfileID, FinalWriterProfileID: base.FinalWriterProfileID}); !hasCode(err, CodeTargetPathInvalid) {
		t.Fatalf("target path error = %v", err)
	}
	for _, path := range []string{"/absolute.md", `chapters\win.md`} {
		if _, err := service.Create(context.Background(), layout, CreateInput{TargetPath: path, ChapterTask: base.ChapterTask, Candidates: base.Candidates, ReviewModelProfileID: base.ReviewModelProfileID, FinalWriterProfileID: base.FinalWriterProfileID}); !hasCode(err, CodeTargetPathInvalid) {
			t.Fatalf("target path %q error = %v", path, err)
		}
	}
}

func TestSourceResolutionErrorCodes(t *testing.T) {
	layout := testLayout(t)
	skillRoot := filepath.Join(layout.ContentRoot, "skills")
	if err := os.MkdirAll(filepath.Join(skillRoot, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillRoot, "versioned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "empty", skills.SkillFileName), []byte("---\nname: empty\ndescription: fixture\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "versioned", skills.SkillFileName), []byte("---\nname: versioned\ndescription: fixture\n---\nUse this skill."), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(testHost{cfg: testConfig(), dirs: []skills.Directory{{Scope: skills.ScopeWorkspace, Path: skillRoot}}})
	for _, test := range []struct {
		name string
		ref  WritingSourceRef
		code string
	}{
		{name: "missing", ref: WritingSourceRef{Kind: "skill", ID: "missing"}, code: CodeSkillMissing},
		{name: "empty", ref: WritingSourceRef{Kind: "skill", ID: "empty"}, code: CodeSkillNotUsable},
		{name: "version changed", ref: WritingSourceRef{Kind: "skill", ID: "versioned", Version: "other"}, code: CodeSkillVersionChanged},
		{name: "template catalog absent", ref: WritingSourceRef{Kind: "template", ID: "draft", Version: "1"}, code: CodeTemplateCatalogUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.resolveSource(context.Background(), layout, test.ref); !hasCode(err, test.code) {
				t.Fatalf("source error=%v, want %s", err, test.code)
			}
		})
	}
}

func TestWritingSourceRefAcceptsNumericContentVersion(t *testing.T) {
	var ref WritingSourceRef
	if err := json.Unmarshal([]byte(`{"kind":"template","id":"full-chapter.draft","content_version":3}`), &ref); err != nil {
		t.Fatal(err)
	}
	if ref.Kind != "template" || ref.ID != "full-chapter.draft" || ref.Version != "3" {
		t.Fatalf("decoded source ref = %+v", ref)
	}
}

func hasCode(err error, want string) bool {
	if typed, ok := err.(*Error); ok {
		return typed.Code == want
	}
	return false
}
