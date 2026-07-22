package evaluation

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildHarnessRequestKeepsStageInputsIsolated(t *testing.T) {
	inputs := validHarnessRequestInputs(t)
	candidate, err := BuildHarnessRequest(HarnessStageCandidateA, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(candidate.UserInput, inputs.CandidateB) || strings.Contains(candidate.UserInput, inputs.ReviewJSON) {
		t.Fatal("candidate request received downstream artifacts")
	}
	revision, err := BuildHarnessRequest(HarnessStageRevision, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{inputs.OriginalInput, inputs.CandidateA, inputs.CandidateB, inputs.ReviewJSON} {
		if !strings.Contains(revision.UserInput, required) {
			t.Fatalf("revision request omitted required bounded input %q", required)
		}
	}
}

func TestBuildHarnessRequestUsesCanonicalStageHashAndBoundedUTF8(t *testing.T) {
	inputs := validHarnessRequestInputs(t)
	request, err := BuildHarnessRequest(HarnessStageReview, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if request.InputSHA256 != OutputSHA256(request.UserInput) {
		t.Fatalf("stage input hash = %q, want canonical user input hash", request.InputSHA256)
	}
	if request.Model != inputs.Model || request.Stage != HarnessStageReview || request.SystemTemplate != inputs.SystemTemplate {
		t.Fatalf("request did not preserve frozen stage identity: %#v", request)
	}

	inputs.OriginalInput = strings.Repeat("界", 65536)
	if _, err := BuildHarnessRequest(HarnessStageCandidateA, inputs); err == nil || !strings.Contains(err.Error(), "131072") {
		t.Fatalf("oversized UTF-8 request error = %v", err)
	}
}

func TestBuildHarnessRequestRejectsUnknownStage(t *testing.T) {
	if _, err := BuildHarnessRequest(HarnessStage("unknown"), validHarnessRequestInputs(t)); err == nil {
		t.Fatal("unknown stage must fail")
	}
}

func TestParseHarnessReviewRejectsUnknownFieldsAndOversize(t *testing.T) {
	if _, err := ParseHarnessReview([]byte(`{"preferred_candidate":"A","issues":[],"preserve":[],"reasoning":"secret"}`), 49152, []string{"goal.opening"}); err == nil {
		t.Fatal("unknown review fields must fail")
	}
	if _, err := ParseHarnessReview(bytes.Repeat([]byte("x"), 49153), 49152, []string{"goal.opening"}); err == nil {
		t.Fatal("oversized review must fail")
	}
}

func TestParseHarnessReviewRequiresOneCompleteSafeStructuredObject(t *testing.T) {
	valid := []byte(`{"preferred_candidate":"B","issues":[{"goal_id":"goal.opening","severity":"high","location":"opening","evidence":"late conflict","action":"move conflict earlier"}],"preserve":["voice"]}`)
	review, err := ParseHarnessReview(valid, 49152, []string{"goal.opening"})
	if err != nil {
		t.Fatal(err)
	}
	if review.PreferredCandidate != "B" || len(review.Issues) != 1 {
		t.Fatalf("review = %#v", review)
	}

	for name, payload := range map[string][]byte{
		"trailing":          []byte(`{"preferred_candidate":"A","issues":[],"preserve":[]} {}`),
		"candidate":         []byte(`{"preferred_candidate":"C","issues":[],"preserve":[]}`),
		"unknown goal":      []byte(`{"preferred_candidate":"A","issues":[{"goal_id":"goal.unknown","severity":"low","location":"x","evidence":"x","action":"x"}],"preserve":[]}`),
		"missing issue key": []byte(`{"preferred_candidate":"A","issues":[{"goal_id":"goal.opening","severity":"low","location":"x","evidence":"x"}],"preserve":[]}`),
		"credential":        []byte(`{"preferred_candidate":"A","issues":[],"preserve":[],"api_key":"sk-abcdefghijkl"}`),
		"reasoning":         []byte(`{"preferred_candidate":"A","issues":[{"goal_id":"goal.opening","severity":"low","location":"x","evidence":"x","action":"x","reasoning_content":"hidden"}],"preserve":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseHarnessReview(payload, 49152, []string{"goal.opening"}); err == nil {
				t.Fatal("invalid review must fail")
			}
		})
	}

	issues := strings.Repeat(`{"goal_id":"goal.opening","severity":"low","location":"x","evidence":"x","action":"x"},`, 64) + `{"goal_id":"goal.opening","severity":"low","location":"x","evidence":"x","action":"x"}`
	if _, err := ParseHarnessReview([]byte(`{"preferred_candidate":"A","issues":[`+issues+`],"preserve":[]}`), 49152, []string{"goal.opening"}); err == nil {
		t.Fatal("more than 64 issues must fail")
	}
}

func validHarnessRequestInputs(t *testing.T) HarnessRequestInputs {
	t.Helper()
	return HarnessRequestInputs{
		TaskID: "task.opening.001", OriginalInput: "A courier opens a sealed letter.", InputSHA256: "sha256:" + strings.Repeat("a", 64),
		QualityGoals: []string{"goal.opening"}, CandidateA: "Candidate A prose.", CandidateB: "Candidate B prose.",
		ReviewJSON:     `{"preferred_candidate":"A","issues":[],"preserve":["voice"]}`,
		StagePolicy:    HarnessStagePolicy{Stage: HarnessStageCandidateA, MaxOutputTokens: 4096, MaxOutputBytes: 49152},
		SystemTemplate: "Write the bounded stage.",
		Model:          ModelConfigSnapshot{Provider: "test-provider", BaseURL: "https://example.test/v1", ModelProfileID: "default", Model: "test-model", CredentialSource: "runtime_only", Parameters: ModelParameters{Temperature: 0.7, MaxOutputTokens: 4096}},
	}
}
