package evaluation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteOfflineHarnessRunsFourStagesAndProducesFinalH(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	generator := &recordingHarnessGenerator{}
	run, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(generator.Requests), 4*len(run.Tasks); got != want {
		t.Fatalf("calls=%d want=%d", got, want)
	}
	for _, task := range run.Tasks {
		h := task.Arms["H"]
		if h.Status != StatusReady || len(h.Stages) != 4 || h.Usage.ModelCalls != 4 || h.OutputFile == "" {
			t.Fatalf("task %s H=%#v", task.TaskID, h)
		}
		if got := h.Stages[3].Stage; got != HarnessStageRevision {
			t.Fatalf("task %s final stage=%q", task.TaskID, got)
		}
	}
}

func TestExecuteOfflineHarnessResumesMatchingStagesWithoutPaidRepeats(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	first := &recordingHarnessGenerator{FailStage: HarnessStageReview}
	if _, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, first); err == nil {
		t.Fatal("first review failure must be returned")
	}
	second := &recordingHarnessGenerator{}
	run, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, second)
	if err != nil {
		t.Fatal(err)
	}
	if second.Count(HarnessStageCandidateA) != 0 || second.Count(HarnessStageCandidateB) != 0 {
		t.Fatal("valid persisted candidates were repeated")
	}
	if got, want := len(second.Requests), 2*len(run.Tasks); got != want {
		t.Fatalf("resume calls=%d want=%d", got, want)
	}
}

func TestExecuteOfflineHarnessInvalidatesMismatchedStageHash(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	first := &recordingHarnessGenerator{FailStage: HarnessStageReview}
	_, _ = ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, first)
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	stage := run.Tasks[0].Arms["H"].Stages[0]
	stage.TemplateSHA256 = "sha256:invalid"
	run.Tasks[0].Arms["H"].Stages[0] = stage
	if err := SaveRun(fixture.RunRoot, run); err != nil {
		t.Fatal(err)
	}
	second := &recordingHarnessGenerator{}
	if _, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, second); err != nil {
		t.Fatal(err)
	}
	if second.Count(HarnessStageCandidateA) != 1 || second.Count(HarnessStageCandidateB) != 0 {
		t.Fatalf("hash invalidation calls: A=%d B=%d", second.Count(HarnessStageCandidateA), second.Count(HarnessStageCandidateB))
	}
}

func TestExecuteOfflineHarnessRecordsStructuredReviewFailure(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	generator := &recordingHarnessGenerator{InvalidReview: true}
	_, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator)
	if err == nil {
		t.Fatal("invalid structured review must fail")
	}
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range run.Tasks {
		h := task.Arms["H"]
		if h.Status != StatusFailed || h.FailureType != "harness_review_failed" || h.OutputFile != "" || len(h.Stages) != 3 || h.Stages[2].Status != StatusFailed {
			t.Fatalf("task %s failed H=%#v", task.TaskID, h)
		}
	}
}

func TestExecuteOfflineHarnessStopsAfterOutputPersistenceFailure(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Tasks) < 2 {
		t.Fatal("fixture requires two selected tasks")
	}
	firstTask := run.Tasks[0].TaskID
	target := filepath.Join(fixture.RunRoot, fixture.RunID, "private", "outputs", firstTask, "h", string(HarnessStageCandidateA)+".txt")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	generator := &recordingHarnessGenerator{}
	_, err = ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator)
	var persistenceErr *harnessPersistenceError
	if !errors.As(err, &persistenceErr) {
		t.Fatalf("error = %v, want typed persistence error", err)
	}
	if got := len(generator.Requests); got != 1 {
		t.Fatalf("calls=%d want=1; later task must not run", got)
	}
	stored, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	h := stored.Tasks[0].Arms["H"]
	if h.Status != StatusFailed || h.FailureType != "harness_candidate_a_failed" || len(h.Stages) != 1 || h.Stages[0].Status != StatusFailed {
		t.Fatalf("durable failed stage=%#v", h)
	}
}

func TestExecuteOfflineHarnessStopsAfterRunRecordPersistenceFailure(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	runRecord := filepath.Join(fixture.RunRoot, fixture.RunID, "run.json")
	generator := &recordingHarnessGenerator{BeforeResult: func(request HarnessRequest) {
		if request.Stage != HarnessStageCandidateA {
			return
		}
		if err := os.Remove(runRecord); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(runRecord, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runRecord, "sentinel"), []byte("block rename"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator)
	var persistenceErr *harnessPersistenceError
	if !errors.As(err, &persistenceErr) {
		t.Fatalf("error = %v, want typed persistence error", err)
	}
	if got := len(generator.Requests); got != 1 {
		t.Fatalf("calls=%d want=1; later task must not run", got)
	}
	if _, err := LoadRun(fixture.RunRoot, fixture.RunID); err == nil {
		t.Fatal("run record unexpectedly remained readable after persistence failure")
	}
}

type harnessExecutionFixture struct {
	ManifestPath string
	RunRoot      string
	RunID        string
	PolicyPath   string
}

func newHarnessExecutionFixture(t *testing.T) harnessExecutionFixture {
	t.Helper()
	manifestPath, manifest := writeValidManifest(t)
	policyPath := writeHarnessPolicyFixture(t, nil)
	policy, err := LoadHarnessPolicy(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewRunSelection([]DataSplit{SplitRegression}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run, err := CreateRun(manifestPath, CreateRunOptions{RunRoot: runRoot, Selection: &selection, HarnessPolicySHA256: HarnessPolicySHA256(policy)})
	if err != nil {
		t.Fatal(err)
	}
	return harnessExecutionFixture{manifestPath, runRoot, run.RunID, policyPath}
}

type recordingHarnessGenerator struct {
	Requests      []HarnessRequest
	FailStage     HarnessStage
	InvalidReview bool
	BeforeResult  func(HarnessRequest)
}

func (g *recordingHarnessGenerator) GenerateHarness(_ context.Context, request HarnessRequest) (GenerationResult, error) {
	g.Requests = append(g.Requests, request)
	if g.BeforeResult != nil {
		g.BeforeResult(request)
	}
	if request.Stage == g.FailStage {
		return GenerationResult{}, fmt.Errorf("injected %s failure", request.Stage)
	}
	if request.Stage == HarnessStageReview {
		if g.InvalidReview {
			return GenerationResult{Output: `{"preferred_candidate":"A","issues":[],"preserve":[],"unexpected":true}`}, nil
		}
		return GenerationResult{Output: `{"preferred_candidate":"A","issues":[],"preserve":[]}`, Usage: UsageRecord{ModelCalls: 1}}, nil
	}
	return GenerationResult{Output: fmt.Sprintf("%s output for %s", request.Stage, request.TaskID), Usage: UsageRecord{ModelCalls: 1}}, nil
}

func (g *recordingHarnessGenerator) Count(stage HarnessStage) int {
	count := 0
	for _, request := range g.Requests {
		if request.Stage == stage {
			count++
		}
	}
	return count
}
