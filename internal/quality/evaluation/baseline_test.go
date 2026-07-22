package evaluation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteSingleTurnBaselineCallsModelOncePerTask(t *testing.T) {
	manifestPath, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run, err := CreateRun(manifestPath, CreateRunOptions{
		RunRoot: runRoot, BaselineStatus: StatusNotReady, HarnessStatus: StatusNotReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	generator := &recordingGenerator{}
	updated, err := ExecuteSingleTurnBaseline(context.Background(), manifestPath, runRoot, run.RunID, generator)
	if err != nil {
		t.Fatal(err)
	}
	if len(generator.requests) != len(manifest.Tasks) {
		t.Fatalf("model calls = %d, want %d", len(generator.requests), len(manifest.Tasks))
	}
	for i, task := range updated.Tasks {
		sArm := task.Arms["S"]
		if sArm.Status != StatusReady || sArm.Usage.ModelCalls != 1 || sArm.OutputSHA256 == "" || sArm.OutputFile == "" {
			t.Fatalf("task %s S arm = %#v", task.TaskID, sArm)
		}
		if task.Arms["H"].Status != StatusNotReady || task.Arms["H"].OutputFile != "" {
			t.Fatalf("task %s H arm was changed: %#v", task.TaskID, task.Arms["H"])
		}
		if generator.requests[i].ModelCallLimit != 1 || generator.requests[i].ThinkingPersisted {
			t.Fatalf("request %s violates baseline boundary: %#v", task.TaskID, generator.requests[i])
		}
	}
}

func TestExecuteSingleTurnBaselineSupportsSelectedCohort(t *testing.T) {
	manifestPath, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	selection, err := NewRunSelection([]DataSplit{SplitTuning}, []string{"long_serial-01"})
	if err != nil {
		t.Fatal(err)
	}
	policyHash := "sha256:" + strings.Repeat("1", 64)
	run, err := CreateRun(manifestPath, CreateRunOptions{
		RunRoot: runRoot, BaselineStatus: StatusNotReady, HarnessStatus: StatusNotReady,
		Selection: &selection, HarnessPolicySHA256: policyHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID != StableCohortRunID(manifest, selection, policyHash) || len(run.Tasks) != 1 {
		t.Fatalf("cohort run=%#v", run)
	}
	generator := &recordingGenerator{}
	updated, err := ExecuteSingleTurnBaseline(context.Background(), manifestPath, runRoot, run.RunID, generator)
	if err != nil {
		t.Fatal(err)
	}
	if len(generator.requests) != 1 || generator.requests[0].TaskID != "long_serial-01" {
		t.Fatalf("requests=%#v", generator.requests)
	}
	if updated.BaselineStatus != StatusReady || updated.Tasks[0].Arms["S"].Usage.ModelCalls != 1 {
		t.Fatalf("updated=%#v", updated)
	}
}

func TestExecuteSingleTurnBaselineRejectsMismatchedCohortSelection(t *testing.T) {
	manifestPath, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	selection, err := NewRunSelection([]DataSplit{SplitTuning}, []string{"long_serial-01"})
	if err != nil {
		t.Fatal(err)
	}
	policyHash := "sha256:" + strings.Repeat("1", 64)
	run, err := CreateRun(manifestPath, CreateRunOptions{RunRoot: runRoot, Selection: &selection, HarnessPolicySHA256: policyHash})
	if err != nil {
		t.Fatal(err)
	}
	run.Selection = &RunSelection{DataSplits: []DataSplit{SplitRegression}, TaskIDs: []string{"long_serial-02"}}
	if err := SaveRun(runRoot, run); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteSingleTurnBaseline(context.Background(), manifestPath, runRoot, run.RunID, &recordingGenerator{}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err=%v", err)
	}
}

func TestExecuteSingleTurnBaselineRecordsFailureWithoutFabricatingOutput(t *testing.T) {
	manifestPath, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run, err := CreateRun(manifestPath, CreateRunOptions{
		RunRoot: runRoot, BaselineStatus: StatusNotReady, HarnessStatus: StatusNotReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	generator := &recordingGenerator{failTask: manifest.Tasks[0].ID}
	updated, err := ExecuteSingleTurnBaseline(context.Background(), manifestPath, runRoot, run.RunID, generator)
	if err != nil {
		t.Fatal(err)
	}
	failed := updated.Tasks[0].Arms["S"]
	if failed.Status != StatusFailed || failed.FailureType != "model_call_failed" || failed.OutputFile != "" || failed.OutputSHA256 != "" {
		t.Fatalf("failed S arm = %#v", failed)
	}
	if updated.BaselineStatus != StatusFailed {
		t.Fatalf("baseline status = %s, want %s", updated.BaselineStatus, StatusFailed)
	}
}

type recordingGenerator struct {
	requests []BaselineRequest
	failTask string
}

func (generator *recordingGenerator) Generate(_ context.Context, request BaselineRequest) (GenerationResult, error) {
	generator.requests = append(generator.requests, request)
	if request.TaskID == generator.failTask {
		return GenerationResult{}, fmt.Errorf("provider rejected request")
	}
	return GenerationResult{
		Output: "single-turn fiction for " + request.TaskID,
		Usage:  UsageRecord{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, ModelCalls: 1},
		Cost:   CostRecord{Status: "recorded", Currency: "USD", Amount: floatPtr64(0.01)},
	}, nil
}

func TestBaselineRequestContainsOnlyFrozenAllowedInputs(t *testing.T) {
	manifestPath, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(manifestPath), manifest.RunRoot)
	run, err := CreateRun(manifestPath, CreateRunOptions{RunRoot: runRoot, BaselineStatus: StatusNotReady, HarnessStatus: StatusNotReady})
	if err != nil {
		t.Fatal(err)
	}
	generator := &recordingGenerator{}
	if _, err := ExecuteSingleTurnBaseline(context.Background(), manifestPath, runRoot, run.RunID, generator); err != nil {
		t.Fatal(err)
	}
	for _, request := range generator.requests {
		joined := strings.ToLower(request.Input)
		for _, forbidden := range []string{"reviewer output", "revision output", "thinking", "future answer"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("request %s contains forbidden input %q", request.TaskID, forbidden)
			}
		}
	}
}
