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
