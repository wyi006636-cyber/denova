package evaluation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestAcceptsThreeProfileCorpus(t *testing.T) {
	path, want := writeValidManifest(t)
	got, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if len(got.Tasks) != len(want.Tasks) {
		t.Fatalf("tasks = %d, want %d", len(got.Tasks), len(want.Tasks))
	}
}

func TestLoadManifestRejectsDuplicateTaskID(t *testing.T) {
	path, manifest := writeValidManifest(t)
	manifest.Tasks[1].ID = manifest.Tasks[0].ID
	writeManifest(t, path, manifest)
	assertManifestError(t, path, "duplicate task id")
}

func TestLoadManifestRejectsUnknownProfile(t *testing.T) {
	path, manifest := writeValidManifest(t)
	manifest.Tasks[0].ProfileID = "generic_short"
	writeManifest(t, path, manifest)
	assertManifestError(t, path, "unknown profile")
}

func TestLoadManifestRejectsInsufficientTaskCount(t *testing.T) {
	path, manifest := writeValidManifest(t)
	manifest.Tasks = manifest.Tasks[:35]
	extra := manifest.Tasks[0]
	extra.ID = "long_serial-extra"
	extra.ActualCostRecord = "runs/{run_id}/tasks/long_serial-extra/cost.json"
	manifest.Tasks = append(manifest.Tasks, extra)
	writeManifest(t, path, manifest)
	assertManifestError(t, path, "at least 12 tasks")
}

func TestLoadManifestRejectsInsufficientGenreStrata(t *testing.T) {
	path, manifest := writeValidManifest(t)
	for i := range manifest.Tasks {
		if manifest.Tasks[i].ProfileID == ProfileLongSerial {
			manifest.Tasks[i].Genre = "mystery"
		}
	}
	writeManifest(t, path, manifest)
	assertManifestError(t, path, "at least 2 genre strata")
}

func TestLoadManifestRejectsIncorrectInputHash(t *testing.T) {
	path, manifest := writeValidManifest(t)
	manifest.Tasks[0].InputSHA256 = "sha256:" + strings.Repeat("0", 64)
	writeManifest(t, path, manifest)
	assertManifestError(t, path, "input hash mismatch")
}

func TestLoadManifestRejectsSensitiveFields(t *testing.T) {
	path, _ := writeValidManifest(t)
	var raw map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	tasks := raw["tasks"].([]any)
	model := tasks[0].(map[string]any)["model_config_snapshot"].(map[string]any)
	model["api_key"] = "must-not-be-accepted"
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	assertManifestError(t, path, "sensitive field")
}

func assertManifestError(t *testing.T, path, want string) {
	t.Helper()
	_, err := LoadManifest(path)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
		t.Fatalf("LoadManifest() error = %v, want containing %q", err, want)
	}
}

func writeValidManifest(t *testing.T) (string, CorpusManifest) {
	t.Helper()
	root := t.TempDir()
	inputRoot := filepath.Join(root, "inputs")
	contractRoot := filepath.Join(root, "contracts")
	if err := os.MkdirAll(inputRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(contractRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join(root, "single-turn.md")
	contractPath := filepath.Join(contractRoot, "quality-spec.json")
	if err := os.WriteFile(templatePath, []byte("single turn template v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, []byte("{\"contract\":\"quality-spec-v1\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	templateHash := testFileHash(t, templatePath)
	contractHash := testFileHash(t, contractPath)
	manifest := CorpusManifest{
		Contract:     "denova.quality-evaluation-corpus",
		Version:      "v1",
		InputRoot:    "inputs",
		ContractRoot: "contracts",
		RunRoot:      "runs",
		Baseline: BaselineProtocol{
			Arm: "S", TemplateVersion: "single-turn-v1", TemplateFile: "single-turn.md",
			TemplateSHA256: templateHash, ModelCallLimit: 1,
			HarnessArtifactsAllowed: false, ThinkingPersisted: false,
		},
	}
	profiles := []ProfileID{ProfileLongSerial, ProfileFanqieShort, ProfileZhihuSaltShort}
	taskTypes := []TaskType{TaskOpening, TaskDialogue, TaskStructureTurn, TaskEnding, TaskCharacterChoice, TaskContinuity}
	for _, profile := range profiles {
		for i := 0; i < 12; i++ {
			id := fmt.Sprintf("%s-%02d", profile, i+1)
			rel := filepath.Join(string(profile), id+".md")
			path := filepath.Join(inputRoot, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("synthetic licensed task "+id+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			model := ModelConfigSnapshot{
				Provider: "test-provider", BaseURL: "https://example.test/v1", ModelProfileID: "default", Model: "test-model", CredentialSource: "runtime_only",
				Parameters: ModelParameters{Temperature: 0.7, MaxOutputTokens: 4096, ThinkingEnabled: false},
			}
			model.SHA256 = StableSHA256(model.hashPayload())
			manifest.Tasks = append(manifest.Tasks, EvaluationTask{
				ID: id, ProfileID: profile, Genre: []string{"mystery", "family"}[i%2],
				TaskType: taskTypes[i%len(taskTypes)], Purpose: "exercise a profile-specific fiction decision",
				LengthBucket:  []LengthBucket{LengthScene, LengthChapter}[i%2],
				DataSplit:     []DataSplit{SplitTuning, SplitRegression, SplitReleaseHoldout}[i%3],
				AllowedInputs: []string{"task_brief", "quality_spec"}, InputFile: filepath.ToSlash(rel),
				InputSHA256:         testFileHash(t, path),
				Source:              SourceRecord{Kind: "synthetic", Reference: "Denova P0-T07 synthetic fixture", LicenseStatus: "project_owned", AnonymizationStatus: "not_applicable"},
				QualitySpec:         QualitySpecSnapshot{ContractFile: "quality-spec.json", ContractSHA256: contractHash, Goals: []string{"profile_specific_quality"}},
				ModelConfigSnapshot: model,
				ActualCostRecord:    filepath.ToSlash(filepath.Join("runs", "{run_id}", "tasks", id, "cost.json")),
			})
		}
	}
	path := filepath.Join(root, "manifest.json")
	writeManifest(t, path, manifest)
	return path, manifest
}

func writeManifest(t *testing.T, path string, manifest CorpusManifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testFileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}
