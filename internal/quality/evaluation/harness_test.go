package evaluation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestValidateHarnessPolicyModelAgreementRejectsEnabledThinkingBeforeProviderCall(t *testing.T) {
	policy := HarnessPolicy{ThinkingMode: "disabled"}
	tasks := []EvaluationTask{{ID: "task.opening.001", ModelConfigSnapshot: ModelConfigSnapshot{
		Parameters: ModelParameters{ThinkingEnabled: true},
	}}}
	if err := ValidateHarnessPolicyModelAgreement(policy, tasks); err == nil || !strings.Contains(err.Error(), "thinking") {
		t.Fatalf("error=%v, want thinking policy mismatch", err)
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
	if err == nil {
		t.Fatal("a resumed paid failed call must prevent a four-call READY sample")
	}
	if second.Count(HarnessStageCandidateA) != 0 || second.Count(HarnessStageCandidateB) != 0 {
		t.Fatal("valid persisted candidates were repeated")
	}
	if got, want := len(second.Requests), 2*len(run.Tasks); got != want {
		t.Fatalf("resume calls=%d want=%d", got, want)
	}
	for _, task := range run.Tasks {
		if task.Arms["H"].Status == StatusReady {
			t.Fatalf("task %s unexpectedly became READY", task.TaskID)
		}
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
	if _, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, second); err == nil {
		t.Fatal("replacement attempt must prevent a five-call H sample from becoming ready")
	}
	if second.Count(HarnessStageCandidateA) != 1 || second.Count(HarnessStageCandidateB) != 0 {
		t.Fatalf("hash invalidation calls: A=%d B=%d", second.Count(HarnessStageCandidateA), second.Count(HarnessStageCandidateB))
	}
}

func TestExecuteOfflineHarnessRecordsStructuredReviewFailure(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	amount := 0.42
	generator := &recordingHarnessGenerator{InvalidReview: true, ReviewResult: GenerationResult{
		Output: `{"preferred_candidate":"A","issues":[],"preserve":[],"unexpected":true}`,
		Usage:  UsageRecord{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		Cost:   CostRecord{Status: "recorded", Currency: "USD", Amount: &amount},
	}}
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
		if h.Status != StatusFailed || h.FailureType != "harness_review_failed" || h.OutputFile != "" || len(h.Stages) != 3 || h.Stages[2].Status != StatusFailed || h.Usage.ModelCalls != 3 {
			t.Fatalf("task %s failed H=%#v", task.TaskID, h)
		}
		failed := h.Stages[2]
		if failed.FailureDetail != "contract_mismatch" || failed.FailureOutputSHA256 == "" || failed.FailureOutputFile == "" || len(failed.Attempts) != 1 || failed.Attempts[0].Usage.TotalTokens != 18 {
			t.Fatalf("task %s failed review evidence=%#v", task.TaskID, failed)
		}
		assertFailedStageRequestEvidence(t, failed, harnessRequestFor(t, generator.Requests, task.TaskID, HarnessStageReview), fixture)
		failurePath := filepath.Join(fixture.RunRoot, fixture.RunID, filepath.FromSlash(failed.FailureOutputFile))
		info, err := os.Stat(failurePath)
		if err != nil {
			t.Fatalf("task %s private failure evidence mode: info=%v err=%v", task.TaskID, info, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("task %s private failure evidence mode=%#o want=0600", task.TaskID, info.Mode().Perm())
		}
	}
	blind, err := PackageBlind(fixture.RunRoot, fixture.RunID)
	if err != nil || blind.Status != StatusNotReady || len(blind.Samples) != 0 {
		t.Fatalf("blind package=%#v err=%v", blind, err)
	}
}

func TestExecuteOfflineHarnessGeneratorErrorRecordsAttemptWithoutOutput(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	generator := &recordingHarnessGenerator{FailStage: HarnessStageCandidateA}
	_, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator)
	if err == nil {
		t.Fatal("generator error must fail")
	}
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	failed := run.Tasks[0].Arms["H"].Stages[0]
	if failed.Usage.ModelCalls != 1 || len(failed.Attempts) != 1 || !failed.Attempts[0].ProviderAttempted || failed.FailureOutputFile != "" || failed.FailureOutputSHA256 != "" || failed.Cost.Status != "NOT-AVAILABLE" {
		t.Fatalf("failed generator evidence=%#v", failed)
	}
	assertFailedStageRequestEvidence(t, failed, generator.Requests[0], fixture)
}

func TestExecuteOfflineHarnessEmptyOutputRetainsRequestEvidenceWithoutOutputHash(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	generator := &recordingHarnessGenerator{EmptyStage: HarnessStageCandidateA}
	if _, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator); err == nil {
		t.Fatal("empty output must fail")
	}
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	failed := run.Tasks[0].Arms["H"].Stages[0]
	if failed.FailureDetail != "empty_output" || failed.FailureOutputSHA256 != "" || failed.FailureOutputFile != "" || failed.Usage.ModelCalls != 1 {
		t.Fatalf("empty-output evidence=%#v", failed)
	}
	assertFailedStageRequestEvidence(t, failed, generator.Requests[0], fixture)
}

func TestExecuteOfflineHarnessOversizeOutputRetainsRequestEvidenceAsPrivateFailure(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	generator := &recordingHarnessGenerator{OversizeStage: HarnessStageCandidateA}
	if _, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator); err == nil {
		t.Fatal("oversize output must fail")
	}
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	failed := run.Tasks[0].Arms["H"].Stages[0]
	if failed.FailureDetail != "oversize_output" || failed.OutputSHA256 != "" || failed.OutputFile != "" || failed.FailureOutputSHA256 == "" || failed.FailureOutputFile == "" || failed.Usage.ModelCalls != 1 || len(failed.Attempts) != 1 || !failed.Attempts[0].ProviderAttempted {
		t.Fatalf("oversize-output evidence=%#v", failed)
	}
	if failed.Attempts[0].FailureOutputSHA256 != failed.FailureOutputSHA256 || failed.Attempts[0].FailureOutputFile != failed.FailureOutputFile {
		t.Fatalf("oversize-output attempt evidence=%#v", failed.Attempts[0])
	}
	assertFailedStageRequestEvidence(t, failed, generator.Requests[0], fixture)
}

func TestPersistHarnessPreProviderFailuresDoNotFabricateModelCalls(t *testing.T) {
	for _, failureType := range []string{"request_build_failed", "prior_output_unreadable"} {
		t.Run(failureType, func(t *testing.T) {
			fixture := newHarnessExecutionFixture(t)
			run, err := LoadRun(fixture.RunRoot, fixture.RunID)
			if err != nil {
				t.Fatal(err)
			}
			runTask := &run.Tasks[0]
			policy, err := LoadHarnessPolicy(fixture.PolicyPath)
			if err != nil {
				t.Fatal(err)
			}
			failureContext := harnessFailureContextForStage(HarnessStageCandidateA, runTask.ModelConfigSHA256, run.HarnessPolicySHA256, policy.Stages[0].TemplateSHA256)
			if failureType == "prior_output_unreadable" {
				manifest, err := LoadManifest(fixture.ManifestPath)
				if err != nil {
					t.Fatal(err)
				}
				task, ok := FindManifestTask(manifest, runTask.TaskID)
				if !ok {
					t.Fatalf("missing fixture task %q", runTask.TaskID)
				}
				input, err := os.ReadFile(ResolveInputPath(fixture.ManifestPath, manifest, task))
				if err != nil {
					t.Fatal(err)
				}
				request, err := BuildHarnessRequest(HarnessStageCandidateA, HarnessRequestInputs{
					TaskID: task.ID, OriginalInput: string(input), InputSHA256: task.InputSHA256, QualityGoals: task.QualitySpec.Goals,
					StagePolicy: policy.Stages[0], SystemTemplate: "candidate\n", Model: task.ModelConfigSnapshot,
				})
				if err != nil {
					t.Fatal(err)
				}
				failureContext = harnessFailureContextForRequest(request, runTask.ModelConfigSHA256, run.HarnessPolicySHA256, policy.Stages[0].TemplateSHA256)
			}
			if err := persistHarnessStageFailure(fixture.RunRoot, &run, runTask, runTask.Arms["H"], 0, failureContext, GenerationResult{}, false, "harness_candidate_a_failed", failureType); err != nil {
				t.Fatal(err)
			}
			stored, err := LoadRun(fixture.RunRoot, fixture.RunID)
			if err != nil {
				t.Fatal(err)
			}
			failed := stored.Tasks[0].Arms["H"].Stages[0]
			if failed.Usage.ModelCalls != 0 || len(failed.Attempts) != 1 || failed.Attempts[0].ProviderAttempted || failed.Attempts[0].FailureDetail != failureType {
				t.Fatalf("pre-provider failure=%#v", failed)
			}
			if failureType == "request_build_failed" && failed.InputSHA256 != "" {
				t.Fatalf("request build failure fabricated input hash=%q", failed.InputSHA256)
			}
			if failureType == "prior_output_unreadable" {
				if failed.OutputSHA256 != "" || failed.OutputFile != "" || failed.FailureOutputSHA256 != "" || failed.FailureOutputFile != "" {
					t.Fatalf("prior-output failure fabricated output evidence=%#v", failed)
				}
				assertFailedStageRequestEvidence(t, failed, HarnessRequest{TaskID: runTask.TaskID, InputSHA256: failureContext.InputSHA256}, fixture)
			}
		})
	}
}

func TestExecuteOfflineHarnessResumePreservesFailedAttemptAndBlocksFourCallReady(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	first := &recordingHarnessGenerator{InvalidReview: true}
	if _, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, first); err == nil {
		t.Fatal("first invalid review must fail")
	}
	firstRun, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	firstFailedReview := firstRun.Tasks[0].Arms["H"].Stages[2]
	assertFailedStageRequestEvidence(t, firstFailedReview, harnessRequestFor(t, first.Requests, firstRun.Tasks[0].TaskID, HarnessStageReview), fixture)
	second := &recordingHarnessGenerator{}
	_, err = ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, second)
	if err == nil {
		t.Fatal("five-call resumed sample must not become ready")
	}
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range run.Tasks {
		h := task.Arms["H"]
		if h.Status == StatusReady || h.Usage.ModelCalls != 5 || len(h.Stages[2].Attempts) != 2 || h.Stages[2].InputSHA256 == "" {
			t.Fatalf("task %s resumed H=%#v", task.TaskID, h)
		}
	}
}

func TestExecuteOfflineHarnessStopsAfterOutputPersistenceFailure(t *testing.T) {
	fixture := newHarnessExecutionFixtureWithTaskIDs(t, []string{"long_serial-02", "long_serial-05"})
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
	if h.Stages[0].FailureOutputSHA256 == "" {
		t.Fatalf("accepted provider output must retain failure hash: %#v", h.Stages[0])
	}
	assertFailedStageRequestEvidence(t, h.Stages[0], generator.Requests[0], fixture)
}

func TestExecuteOfflineHarnessStopsAfterRunRecordPersistenceFailure(t *testing.T) {
	fixture := newHarnessExecutionFixtureWithTaskIDs(t, []string{"long_serial-02", "long_serial-05"})
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
	return newHarnessExecutionFixtureWithTaskIDs(t, []string{"long_serial-02"})
}

func newHarnessExecutionFixtureWithTaskIDs(t *testing.T, taskIDs []string) harnessExecutionFixture {
	t.Helper()
	manifestPath, manifest := writeValidManifest(t)
	policyPath := writeHarnessPolicyFixture(t, nil)
	policy, err := LoadHarnessPolicy(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewRunSelection([]DataSplit{SplitRegression}, taskIDs)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run, err := CreateRun(manifestPath, CreateRunOptions{RunRoot: runRoot, Selection: &selection, HarnessPolicyID: policy.PolicyID, HarnessPolicySHA256: HarnessPolicySHA256(policy)})
	if err != nil {
		t.Fatal(err)
	}
	return harnessExecutionFixture{manifestPath, runRoot, run.RunID, policyPath}
}

type recordingHarnessGenerator struct {
	Requests      []HarnessRequest
	FailStage     HarnessStage
	EmptyStage    HarnessStage
	OversizeStage HarnessStage
	InvalidReview bool
	ErrorResult   GenerationResult
	ReviewResult  GenerationResult
	BeforeResult  func(HarnessRequest)
}

func (g *recordingHarnessGenerator) GenerateHarness(_ context.Context, request HarnessRequest) (GenerationResult, error) {
	g.Requests = append(g.Requests, request)
	if g.BeforeResult != nil {
		g.BeforeResult(request)
	}
	if request.Stage == g.FailStage {
		return g.ErrorResult, fmt.Errorf("injected %s failure", request.Stage)
	}
	if request.Stage == g.EmptyStage {
		return GenerationResult{}, nil
	}
	if request.Stage == g.OversizeStage {
		return GenerationResult{Output: strings.Repeat("x", frozenHarnessMaxOutputBytes+1)}, nil
	}
	if request.Stage == HarnessStageReview {
		if g.ReviewResult.Output != "" {
			return g.ReviewResult, nil
		}
		if g.InvalidReview {
			return GenerationResult{Output: `{"preferred_candidate":"A","issues":[],"preserve":[],"unexpected":true}`}, nil
		}
		return GenerationResult{Output: `{"preferred_candidate":"A","issues":[],"preserve":[]}`, Usage: UsageRecord{ModelCalls: 1}}, nil
	}
	return GenerationResult{Output: fmt.Sprintf("%s output for %s", request.Stage, request.TaskID), Usage: UsageRecord{ModelCalls: 1}}, nil
}

func assertFailedStageRequestEvidence(t *testing.T, failed HarnessStageRecord, request HarnessRequest, fixture harnessExecutionFixture) {
	t.Helper()
	policy, err := LoadHarnessPolicy(fixture.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	var stagePolicy HarnessStagePolicy
	for _, candidate := range policy.Stages {
		if candidate.Stage == failed.Stage {
			stagePolicy = candidate
			break
		}
	}
	if stagePolicy.Stage == "" {
		t.Fatalf("policy missing stage %q", failed.Stage)
	}
	run, err := LoadRun(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var task RunTask
	for _, candidate := range run.Tasks {
		if candidate.TaskID == request.TaskID || request.TaskID == "" {
			task = candidate
			break
		}
	}
	if failed.InputSHA256 != request.InputSHA256 || failed.ModelConfigSHA256 != task.ModelConfigSHA256 || failed.HarnessPolicySHA256 != HarnessPolicySHA256(policy) || failed.TemplateSHA256 != stagePolicy.TemplateSHA256 {
		t.Fatalf("failed request evidence=%#v request=%#v", failed, request)
	}
}

func harnessRequestFor(t *testing.T, requests []HarnessRequest, taskID string, stage HarnessStage) HarnessRequest {
	t.Helper()
	for _, request := range requests {
		if request.TaskID == taskID && request.Stage == stage {
			return request
		}
	}
	t.Fatalf("missing %s request for %s", stage, taskID)
	return HarnessRequest{}
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
