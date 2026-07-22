package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/quality/evaluation"
)

func TestCreateRunRequiresExplicitPrivateRunRootForCohorts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"create-run", "--manifest", testManifestPath(t), "--splits", "tuning",
		"--harness-policy", testPolicyPath(t),
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--run-root") || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
}

func TestPrivateRunRootRejectsOutsideSymlinkBackIntoRepository(t *testing.T) {
	outside := t.TempDir()
	link := filepath.Join(outside, "back-into-repository")
	if err := os.Symlink(testRepositoryRoot(t), link); err != nil {
		t.Fatal(err)
	}
	if _, err := privateRunRoot(filepath.Join(link, "private-runs")); err == nil {
		t.Fatal("symlinked path resolving inside the repository was accepted")
	}
}

func TestCreateRunRejectsReleaseHoldoutBeforeProviderCall(t *testing.T) {
	calls := 0
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { calls++; return &fakeGenerator{} }
	t.Cleanup(func() { newEvaluationGenerator = previous })
	err := run(context.Background(), cohortArgs(t, "release_holdout"), io.Discard, io.Discard)
	if err == nil || calls != 0 {
		t.Fatalf("err=%v provider factory calls=%d", err, calls)
	}
}

func TestCreateRunRejectsInvalidSelectionBeforeProviderCall(t *testing.T) {
	calls := 0
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { calls++; return &fakeGenerator{} }
	t.Cleanup(func() { newEvaluationGenerator = previous })
	args := cohortArgs(t, "tuning,tuning")
	err := run(context.Background(), args, io.Discard, io.Discard)
	if err == nil || calls != 0 {
		t.Fatalf("err=%v provider factory calls=%d", err, calls)
	}
}

func TestCreateRunExecutesSingleBaselineCallForSelectedTask(t *testing.T) {
	configureTestProvider(t)
	generator := &fakeGenerator{}
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { return generator }
	t.Cleanup(func() { newEvaluationGenerator = previous })

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), cohortArgs(t, "tuning"), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); !strings.HasPrefix(got, "run-") || strings.Contains(got, "\n") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if len(generator.baseline) != 1 || len(generator.harness) != 0 {
		t.Fatalf("baseline=%d harness=%d", len(generator.baseline), len(generator.harness))
	}
	forbidden := []string{"test-api-key", "Authorization", "Authorized task input", "C:\\"}
	for _, value := range forbidden {
		if strings.Contains(stdout.String(), value) || strings.Contains(stderr.String(), value) {
			t.Fatalf("CLI output exposed %q: stdout=%q stderr=%q", value, stdout.String(), stderr.String())
		}
	}
}

func TestCreateRunReusesReadyBaselineForIdenticalCohort(t *testing.T) {
	configureTestProvider(t)
	generator := &fakeGenerator{}
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { return generator }
	t.Cleanup(func() { newEvaluationGenerator = previous })

	runRoot := testRunRoot(t)
	args := []string{"create-run", "--manifest", testManifestPath(t), "--splits", "tuning", "--tasks", "ls-mystery-opening-01", "--harness-policy", testPolicyPath(t), "--run-root", runRoot}
	var first, second bytes.Buffer
	if err := run(context.Background(), args, &first, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args, &second, io.Discard); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() || len(generator.baseline) != 1 {
		t.Fatalf("first=%q second=%q baseline calls=%d", first.String(), second.String(), len(generator.baseline))
	}
}

func TestExecuteHarnessExecutesFourCallsForSelectedTask(t *testing.T) {
	configureTestProvider(t)
	generator := &fakeGenerator{}
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { return generator }
	t.Cleanup(func() { newEvaluationGenerator = previous })

	runRoot := testRunRoot(t)
	createArgs := []string{"create-run", "--manifest", testManifestPath(t), "--splits", "tuning", "--tasks", "ls-mystery-opening-01", "--harness-policy", testPolicyPath(t), "--run-root", runRoot}
	var createOut bytes.Buffer
	if err := run(context.Background(), createArgs, &createOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	runID := strings.TrimSpace(createOut.String())
	args := []string{"execute-harness", "--run", runID, "--manifest", testManifestPath(t), "--harness-policy", testPolicyPath(t), "--run-root", runRoot}
	var stdout bytes.Buffer
	if err := run(context.Background(), args, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(generator.harness) != 4 {
		t.Fatalf("harness calls=%d", len(generator.harness))
	}
	if !strings.Contains(stdout.String(), "status=READY") || !strings.Contains(stdout.String(), "model_calls=4") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestEvaluationThinkingFieldsUseDeepSeekV4NestedContract(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "frozen disabled", enabled: false, want: "disabled"},
		{name: "frozen enabled", enabled: true, want: "enabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := evaluationThinkingFields(evaluation.ModelConfigSnapshot{
				Provider: "deepseek", Model: "deepseek-v4-pro",
				Parameters: evaluation.ModelParameters{ThinkingEnabled: test.enabled},
			})
			thinking, ok := fields["thinking"].(map[string]any)
			if !ok || thinking["type"] != test.want {
				t.Fatalf("thinking = %#v, want nested type %q", fields["thinking"], test.want)
			}
			if _, legacy := fields["enable_thinking"]; legacy {
				t.Fatalf("DeepSeek V4 request includes legacy enable_thinking: %#v", fields)
			}
		})
	}
}

func TestPackageBlindAndSummarizeAcceptExplicitRunRoot(t *testing.T) {
	root := testRunRoot(t)
	for _, command := range []string{"package-blind", "summarize"} {
		err := run(context.Background(), []string{command, "--run", "run-invalid", "--run-root", root}, io.Discard, io.Discard)
		if err == nil || strings.Contains(err.Error(), "--run-root") {
			t.Fatalf("%s err=%v", command, err)
		}
	}
}

func TestRecordReviewRequiresAllPrivateArguments(t *testing.T) {
	for _, args := range [][]string{
		{"record-review", "--run-root", testRunRoot(t), "--input", "review.json"},
		{"record-review", "--run", "run-id", "--input", "review.json"},
		{"record-review", "--run", "run-id", "--run-root", testRunRoot(t)},
	} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("args=%q accepted", args)
		}
	}
}

func TestRecordReviewRejectsPrivateInputOutsideInboxAndRepository(t *testing.T) {
	runRoot, runID, _ := readyReviewRun(t)
	inRepository := filepath.Join(testRepositoryRoot(t), "cmd", "quality-eval", "inrepo-review.json")
	t.Cleanup(func() { _ = os.Remove(inRepository) })
	for _, input := range []string{
		inRepository,
		filepath.Join(runRoot, runID, "private", "review.json"),
	} {
		if err := os.WriteFile(input, []byte(`{}`), 0o600); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		err := run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", input}, io.Discard, io.Discard)
		if err == nil {
			t.Fatalf("input outside private inbox accepted: %s", input)
		}
	}
}

func TestRecordReviewRejectsUnknownAndTrailingJSON(t *testing.T) {
	runRoot, runID, sample := readyReviewRun(t)
	for _, payload := range []string{
		`{"unknown":true}`,
		string(reviewJSON(sample, "reviewer-strict", "review-strict", "A")) + " {}",
	} {
		input := writeReviewInboxFile(t, runRoot, runID, "strict.json", []byte(payload))
		if err := run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", input}, io.Discard, io.Discard); err == nil {
			t.Fatalf("invalid review JSON accepted: %s", payload)
		}
	}
}

func TestRecordReviewPersistsPrivateReviewAndOmitsPrivateData(t *testing.T) {
	runRoot, runID, sample := readyReviewRun(t)
	payload := reviewJSON(sample, "reviewer-private", "review-private", "A")
	input := writeReviewInboxFile(t, runRoot, runID, "private.json", payload)
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", input}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, runID, "private", "reviews", "review-private.json")); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(input)
	if err != nil || string(before) != string(after) {
		t.Fatalf("input mutated: err=%v", err)
	}
	output := stdout.String()
	for _, secret := range []string{"reviewer-private", "fiction output", "decision", input, runRoot} {
		if strings.Contains(output, secret) {
			t.Fatalf("stdout exposed %q: %q", secret, output)
		}
	}
	if output != "REVIEW run="+runID+" sample="+sample+" kind=independent status=RECORDED\n" {
		t.Fatalf("stdout=%q", output)
	}
}

func TestRecordReviewEnforcesIndependentAndAdjudicationRules(t *testing.T) {
	runRoot, runID, sample := readyReviewRun(t)
	invoke := func(name, reviewer, reviewID, decision string, conflicts ...string) error {
		payload := reviewJSON(sample, reviewer, reviewID, decision)
		if len(conflicts) > 0 {
			payload = adjudicationJSON(sample, reviewer, reviewID, decision, conflicts)
		}
		input := writeReviewInboxFile(t, runRoot, runID, name+".json", payload)
		return run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", input}, io.Discard, io.Discard)
	}
	if err := invoke("one", "reviewer-one", "review-one", "A"); err != nil {
		t.Fatal(err)
	}
	if err := invoke("duplicate", "reviewer-one", "review-duplicate", "B"); err == nil {
		t.Fatal("duplicate reviewer accepted")
	}
	if err := invoke("two", "reviewer-two", "review-two", "B"); err != nil {
		t.Fatal(err)
	}
	if err := invoke("third", "reviewer-three", "review-three", "A"); err == nil {
		t.Fatal("third independent review accepted")
	}
	if err := invoke("adjudication", "adjudicator", "review-adjudication", "tie", "review-one", "review-two"); err != nil {
		t.Fatal(err)
	}

	agreeRoot, agreeRunID, agreeSample := readyReviewRun(t)
	for _, review := range []struct{ reviewer, reviewID string }{{"reviewer-agree-one", "review-agree-one"}, {"reviewer-agree-two", "review-agree-two"}} {
		input := writeReviewInboxFile(t, agreeRoot, agreeRunID, review.reviewID+".json", reviewJSON(agreeSample, review.reviewer, review.reviewID, "A"))
		if err := run(context.Background(), []string{"record-review", "--run", agreeRunID, "--run-root", agreeRoot, "--input", input}, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	input := writeReviewInboxFile(t, agreeRoot, agreeRunID, "unnecessary-adjudication.json", adjudicationJSON(agreeSample, "adjudicator-agree", "review-agree-adjudication", "A", []string{"review-agree-one", "review-agree-two"}))
	if err := run(context.Background(), []string{"record-review", "--run", agreeRunID, "--run-root", agreeRoot, "--input", input}, io.Discard, io.Discard); err == nil {
		t.Fatal("unnecessary adjudication accepted")
	}
}

func TestRecordReviewReturnsOnlySafeErrors(t *testing.T) {
	runRoot, runID, sample := readyReviewRun(t)
	markerPath := filepath.Join(runRoot, runID, "private", "review-inbox", "marker-private-path.json")
	for _, payload := range [][]byte{
		[]byte(`{"reviewer_id":"marker-reviewer","decision":"marker-decision","evidence":"marker-evidence"}`),
		[]byte(`{"contract":"denova.quality-evaluation-review","version":"v1","review_id":"marker-review","sample_id":"` + sample + `","reviewer_id":"marker-reviewer","kind":"independent","restatement":{"character_goal":"marker-path-` + markerPath + `"},"decision":"marker-decision","evidence":[],"fact_errors":{"A":0,"B":0},"author_edit_ratio":{"A":0,"B":0}}`),
	} {
		input := writeReviewInboxFile(t, runRoot, runID, "marker-private-path.json", payload)
		err := run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", input}, io.Discard, io.Discard)
		if err == nil {
			t.Fatal("private marker payload accepted")
		}
		for _, marker := range []string{markerPath, "marker-reviewer", "marker-decision", "marker-evidence", "marker-review"} {
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("error exposed %q: %v", marker, err)
			}
		}
		if err.Error() != "invalid_json" && err.Error() != "invalid_review" {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestRecordReviewRejectsUnsafeRunIDBeforePathUse(t *testing.T) {
	runRoot, _, sample := readyReviewRun(t)
	input := writeReviewInboxFile(t, runRoot, "run-placeholder", "review.json", reviewJSON(sample, "reviewer-safe", "review-safe", "A"))
	for _, runID := range []string{"../run-escape", `run\\escape`, "run\nescape"} {
		err := run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", input}, io.Discard, io.Discard)
		if err == nil || err.Error() != "invalid_run" {
			t.Fatalf("run=%q err=%v", runID, err)
		}
	}
}

func TestRecordReviewRejectsFinalSymlinkInput(t *testing.T) {
	runRoot, runID, sample := readyReviewRun(t)
	inbox := filepath.Join(runRoot, runID, "private", "review-inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, reviewJSON(sample, "reviewer-link", "review-link", "A"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(inbox, "review-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"record-review", "--run", runID, "--run-root", runRoot, "--input", link}, io.Discard, io.Discard)
	if err == nil || err.Error() != "invalid_input_location" {
		t.Fatalf("err=%v", err)
	}
}

func TestSameReviewInputFileBindsCanonicalPathToOpenedHandle(t *testing.T) {
	directory := t.TempDir()
	openedPath := filepath.Join(directory, "opened.json")
	canonicalPath := filepath.Join(directory, "canonical.json")
	if err := os.WriteFile(openedPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := os.Stat(openedPath)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.Stat(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if sameReviewInputFile(opened, canonical) {
		t.Fatal("different opened and canonical files were treated as identical")
	}
	if !sameReviewInputFile(opened, opened) {
		t.Fatal("the same opened and canonical file was rejected")
	}
}

func TestExportRunIndexRequiresExplicitPrivateArguments(t *testing.T) {
	for _, args := range [][]string{
		{"export-run-index", "--runs", "run-one", "--output", "index.json"},
		{"export-run-index", "--run-root", testRunRoot(t), "--output", "index.json"},
		{"export-run-index", "--run-root", testRunRoot(t), "--runs", "run-one"},
		{"export-run-index", "--run-root", testRunRoot(t), "--runs", "run-one,run-one", "--output", "index.json"},
	} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("args=%q accepted", args)
		}
	}
}

func TestExportRunIndexWritesBoundedIndexFromExplicitPrivateRoot(t *testing.T) {
	runRoot := testRunRoot(t)
	policy, err := evaluation.LoadHarnessPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := evaluation.NewRunSelection([]evaluation.DataSplit{evaluation.SplitTuning}, []string{"ls-mystery-opening-01"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := evaluation.CreateRun(testManifestPath(t), evaluation.CreateRunOptions{
		RunRoot: runRoot, Selection: &selection, HarnessPolicyID: policy.PolicyID, HarnessPolicySHA256: evaluation.HarnessPolicySHA256(policy),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bounded-index.json")
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"export-run-index", "--run-root", runRoot, "--runs", created.RunID, "--output", output}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), runRoot) || !strings.Contains(stdout.String(), "runs=1") || strings.Contains(stdout.String(), runRoot) {
		t.Fatalf("payload=%q stdout=%q", payload, stdout.String())
	}
}

type fakeGenerator struct {
	baseline []evaluation.BaselineRequest
	harness  []evaluation.HarnessRequest
}

func (generator *fakeGenerator) Generate(_ context.Context, request evaluation.BaselineRequest) (evaluation.GenerationResult, error) {
	generator.baseline = append(generator.baseline, request)
	return evaluation.GenerationResult{Output: "baseline", Usage: evaluation.UsageRecord{ModelCalls: 1, TotalTokens: 3}}, nil
}

func (generator *fakeGenerator) GenerateHarness(_ context.Context, request evaluation.HarnessRequest) (evaluation.GenerationResult, error) {
	generator.harness = append(generator.harness, request)
	output := "candidate"
	if request.Stage == evaluation.HarnessStageReview {
		output = `{"preferred_candidate":"A","issues":[],"preserve":[]}`
	}
	if request.Stage == evaluation.HarnessStageRevision {
		output = "revision"
	}
	return evaluation.GenerationResult{Output: output, Usage: evaluation.UsageRecord{ModelCalls: 1, TotalTokens: 3}}, nil
}

func cohortArgs(t *testing.T, split string) []string {
	t.Helper()
	return []string{"create-run", "--manifest", testManifestPath(t), "--splits", split, "--tasks", "ls-mystery-opening-01", "--harness-policy", testPolicyPath(t), "--run-root", testRunRoot(t)}
}

func configureTestProvider(t *testing.T) {
	t.Helper()
	t.Setenv("NOVA_DIR", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-api-key")
	t.Setenv("OPENAI_BASE_URL", "https://api.deepseek.com")
	t.Setenv("OPENAI_MODEL", "deepseek-v4-pro")
}

func testManifestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testRepositoryRoot(t), "docs", "project-design", "implementation", "evaluation", "corpus-manifest-v1.json")
}

func testPolicyPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testRepositoryRoot(t), "docs", "project-design", "implementation", "evaluation", "harness-policy-v1.json")
}

func testRunRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "private-runs")
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readyReviewRun(t *testing.T) (string, string, string) {
	t.Helper()
	configureTestProvider(t)
	generator := &fakeGenerator{}
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { return generator }
	t.Cleanup(func() { newEvaluationGenerator = previous })
	runRoot := testRunRoot(t)
	createArgs := []string{"create-run", "--manifest", testManifestPath(t), "--splits", "tuning", "--tasks", "ls-mystery-opening-01", "--harness-policy", testPolicyPath(t), "--run-root", runRoot}
	var created bytes.Buffer
	if err := run(context.Background(), createArgs, &created, io.Discard); err != nil {
		t.Fatal(err)
	}
	runID := strings.TrimSpace(created.String())
	if err := run(context.Background(), []string{"execute-harness", "--run", runID, "--manifest", testManifestPath(t), "--harness-policy", testPolicyPath(t), "--run-root", runRoot}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"package-blind", "--run", runID, "--run-root", runRoot}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	index, err := evaluation.LoadBlindIndex(runRoot, runID)
	if err != nil {
		t.Fatal(err)
	}
	return runRoot, runID, index.Samples[0].SampleID
}

func writeReviewInboxFile(t *testing.T, runRoot, runID, name string, payload []byte) string {
	t.Helper()
	path := filepath.Join(runRoot, runID, "private", "review-inbox", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func reviewJSON(sampleID, reviewer, reviewID, decision string) []byte {
	return []byte(`{"contract":"denova.quality-evaluation-review","version":"v1","review_id":"` + reviewID + `","sample_id":"` + sampleID + `","reviewer_id":"` + reviewer + `","kind":"independent","restatement":{"character_goal":"protect the relationship","obstacle":"conflicting evidence","choice":"tell the truth","cost":"loss of trust","text_change":"the relationship becomes irreversible"},"decision":"` + decision + `","evidence":[{"option":"A","quote":"fiction output","reason":"the choice changes the relationship"}],"fact_errors":{"A":0,"B":1},"author_edit_ratio":{"A":0.2,"B":0.4}}`)
}

func adjudicationJSON(sampleID, reviewer, reviewID, decision string, conflicts []string) []byte {
	payload := string(reviewJSON(sampleID, reviewer, reviewID, decision))
	payload = strings.Replace(payload, `"kind":"independent"`, `"kind":"adjudication","conflict_review_ids":["`+conflicts[0]+`","`+conflicts[1]+`"]`, 1)
	return []byte(payload)
}
