package evaluation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoadHarnessPolicyAcceptsFrozenPolicy(t *testing.T) {
	path := writeHarnessPolicyFixture(t, nil)
	policy, err := LoadHarnessPolicy(path)
	if err != nil {
		t.Fatalf("LoadHarnessPolicy() error = %v", err)
	}
	if got, want := policy.Stages, []HarnessStagePolicy{
		{Stage: HarnessStageCandidateA, TemplateFile: "runs/templates/candidate.md", TemplateSHA256: policy.Stages[0].TemplateSHA256, MaxOutputTokens: 4096, MaxOutputBytes: 49152},
		{Stage: HarnessStageCandidateB, TemplateFile: "runs/templates/candidate.md", TemplateSHA256: policy.Stages[1].TemplateSHA256, MaxOutputTokens: 4096, MaxOutputBytes: 49152},
		{Stage: HarnessStageReview, TemplateFile: "runs/templates/review.md", TemplateSHA256: policy.Stages[2].TemplateSHA256, MaxOutputTokens: 4096, MaxOutputBytes: 49152},
		{Stage: HarnessStageRevision, TemplateFile: "runs/templates/revision.md", TemplateSHA256: policy.Stages[3].TemplateSHA256, MaxOutputTokens: 4096, MaxOutputBytes: 49152},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stages = %#v, want %#v", got, want)
	}
	if policy.ThinkingMode != "disabled" {
		t.Fatalf("thinking mode = %q, want disabled", policy.ThinkingMode)
	}
	if got := HarnessPolicySHA256(policy); !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
		t.Fatalf("HarnessPolicySHA256() = %q", got)
	}
}

func TestLoadHarnessPolicyAcceptsFrozenRepositoryPolicy(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "project-design", "implementation", "evaluation", "harness-policy-v1.json")
	policy, err := LoadHarnessPolicy(path)
	if err != nil {
		t.Fatalf("LoadHarnessPolicy(frozen repository policy) error = %v", err)
	}
	if policy.ThinkingMode != "disabled" || policy.PolicyID == "p0-offline-harness-v1" {
		t.Fatalf("repository policy must freeze disabled thinking with a new identity: %#v", policy)
	}
	wantTemplateHashes := []string{
		"sha256:c73b2a7ae645c7c5b3b067fb41316a6d035945685fc92eeff1542c8f6b47db51",
		"sha256:c73b2a7ae645c7c5b3b067fb41316a6d035945685fc92eeff1542c8f6b47db51",
		"sha256:be4c6142b14a149f5c625360122849403979795e345dc97289aefc1b3d4a8073",
		"sha256:28de666b55001e40fc859d327e9c5edbd79ba4c5439488a23aa0e2f843a5a2c8",
	}
	for index, want := range wantTemplateHashes {
		if got := policy.Stages[index].TemplateSHA256; got != want {
			t.Fatalf("stage %s template hash = %q, want unchanged %q", policy.Stages[index].Stage, got, want)
		}
	}
	manifestPath := filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "project-design", "implementation", "evaluation", "corpus-manifest-v1.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewRunSelection([]DataSplit{SplitTuning}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policyHash := HarnessPolicySHA256(policy)
	newRunID := StableCohortRunID(manifest, selection, policyHash)
	if policyHash != "sha256:10a70d3ae7cc9f4e3c3dbeaaaf28b62835db0e165ce887a995a4e010dd7a8ea5" {
		t.Fatalf("policy hash = %s, want frozen thinking policy hash", policyHash)
	}
	if newRunID != "run-2f9cce8a71c485df0881cdbb" {
		t.Fatalf("tuning run identity = %s, want stable thinking policy identity", newRunID)
	}
	if newRunID == "run-598b2c33eba7f255bd88eaec" {
		t.Fatalf("new policy must produce a new stable run identity, got preserved run %s", newRunID)
	}
}

func TestFrozenRepositoryReviewTemplateRequiresExactJSONShapes(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "project-design", "implementation", "evaluation", "runs", "templates", "harness-review-v1.md")
	template, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read frozen review template: %v", err)
	}
	content := string(template)
	for _, requirement := range []string{
		"exactly one JSON object",
		"preferred_candidate must be a JSON string enum: \"A\" or \"B\"",
		"issues must be a JSON array with at most 64 items",
		"Each issue object must contain exactly goal_id, severity, location, evidence, and action, and every value must be a JSON string",
		"goal_id must copy one exact string from the supplied quality_goals JSON array",
		"preserve must be a JSON array of JSON strings",
		"No extra root or issue fields",
		"This example demonstrates shape only; do not copy its content blindly.",
	} {
		if !strings.Contains(content, requirement) {
			t.Fatalf("frozen review template must state %q", requirement)
		}
	}

	const example = "{\"preferred_candidate\":\"A\",\"issues\":[],\"preserve\":[\"Keep the causal turn clear.\"]}"
	if !strings.Contains(content, example) {
		t.Fatalf("frozen review template must contain compact valid example %s", example)
	}
	var decoded struct {
		PreferredCandidate string            `json:"preferred_candidate"`
		Issues             []json.RawMessage `json:"issues"`
		Preserve           []string          `json:"preserve"`
	}
	if err := json.Unmarshal([]byte(example), &decoded); err != nil {
		t.Fatalf("decode compact review example: %v", err)
	}
	if decoded.PreferredCandidate != "A" || len(decoded.Issues) != 0 || !reflect.DeepEqual(decoded.Preserve, []string{"Keep the causal turn clear."}) {
		t.Fatalf("compact review example has wrong shape: %#v", decoded)
	}
}

func TestLoadHarnessPolicyRequiresFourFrozenStages(t *testing.T) {
	path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["stages"] = []any{}
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "candidate_a, candidate_b, review, revision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHarnessPolicyRejectsReleaseHoldoutAndHashDrift(t *testing.T) {
	path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["allowed_splits"] = []any{"release_holdout"}
	})
	if _, err := LoadHarnessPolicy(path); err == nil {
		t.Fatal("release holdout policy must fail")
	}

	path = writeHarnessPolicyFixture(t, func(policy map[string]any) {
		stages := policy["stages"].([]any)
		stages[0].(map[string]any)["template_sha256"] = "sha256:" + strings.Repeat("0", 64)
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("hash drift error = %v", err)
	}
}

func TestLoadHarnessPolicyRejectsEscapingPathsAndFrozenBounds(t *testing.T) {
	path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["stages"].([]any)[0].(map[string]any)["template_file"] = "../outside.md"
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping path error = %v", err)
	}

	path = writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["stages"].([]any)[0].(map[string]any)["max_output_tokens"] = 4097
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("token bound error = %v", err)
	}

	path = writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["stages"].([]any)[0].(map[string]any)["max_output_bytes"] = 0
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "output bytes") {
		t.Fatalf("byte bound error = %v", err)
	}
}

func TestLoadHarnessPolicyRejectsTemplateSymlinkEscape(t *testing.T) {
	path := writeHarnessPolicyFixture(t, nil)
	outside := filepath.Join(t.TempDir(), "outside-template.md")
	if err := os.WriteFile(outside, []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(filepath.Dir(path), "runs", "templates", "candidate.md")
	if err := os.Remove(inside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "escapes policy directory") {
		t.Fatalf("template symlink escape error = %v", err)
	}
}

func TestLoadHarnessPolicyRejectsThinkingAndStageOrderDrift(t *testing.T) {
	path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["thinking_persisted"] = true
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "thinking") {
		t.Fatalf("thinking error = %v", err)
	}

	path = writeHarnessPolicyFixture(t, func(policy map[string]any) {
		stages := policy["stages"].([]any)
		stages[0], stages[1] = stages[1], stages[0]
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "candidate_a, candidate_b, review, revision") {
		t.Fatalf("stage order error = %v", err)
	}
}

func TestLoadHarnessPolicyRequiresDisabledThinkingMode(t *testing.T) {
	for _, mode := range []any{nil, "enabled", "unknown"} {
		path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
			if mode == nil {
				delete(policy, "thinking_mode")
				return
			}
			policy["thinking_mode"] = mode
		})
		if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "thinking_mode") {
			t.Fatalf("thinking_mode=%v error=%v, want rejection", mode, err)
		}
	}
}

func writeHarnessPolicyFixture(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	root := t.TempDir()
	templates := filepath.Join(root, "runs", "templates")
	if err := os.MkdirAll(templates, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"candidate.md": "candidate\n", "review.md": "review\n", "revision.md": "revision\n"}
	hashes := make(map[string]string, len(files))
	for name, content := range files {
		path := filepath.Join(templates, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(content))
		hashes[name] = fmt.Sprintf("sha256:%x", sum)
	}
	policy := map[string]any{
		"contract": "denova.quality-harness-policy", "version": "v1", "policy_id": "test-harness-v1",
		"allowed_splits": []any{"tuning", "regression"}, "candidate_count": 2, "thinking_persisted": false, "thinking_mode": "disabled",
		"stages": []any{
			map[string]any{"stage": "candidate_a", "template_file": "runs/templates/candidate.md", "template_sha256": hashes["candidate.md"], "max_output_tokens": 4096, "max_output_bytes": 49152},
			map[string]any{"stage": "candidate_b", "template_file": "runs/templates/candidate.md", "template_sha256": hashes["candidate.md"], "max_output_tokens": 4096, "max_output_bytes": 49152},
			map[string]any{"stage": "review", "template_file": "runs/templates/review.md", "template_sha256": hashes["review.md"], "max_output_tokens": 4096, "max_output_bytes": 49152},
			map[string]any{"stage": "revision", "template_file": "runs/templates/revision.md", "template_sha256": hashes["revision.md"], "max_output_tokens": 4096, "max_output_bytes": 49152},
		},
	}
	if mutate != nil {
		mutate(policy)
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "harness-policy-v1.json")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
