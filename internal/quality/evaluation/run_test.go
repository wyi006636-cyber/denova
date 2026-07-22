package evaluation

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStableRunIDIsRepeatableAndFrozenInputsSensitive(t *testing.T) {
	_, manifest := writeValidManifest(t)
	first := StableRunID(manifest)
	second := StableRunID(manifest)
	if first != second || !strings.HasPrefix(first, "run-") {
		t.Fatalf("StableRunID() = %q then %q", first, second)
	}
	inputVariant := manifest
	inputVariant.Tasks = append([]EvaluationTask(nil), manifest.Tasks...)
	inputVariant.Tasks[0].InputSHA256 = "sha256:" + strings.Repeat("f", 64)
	if changed := StableRunID(inputVariant); changed == first {
		t.Fatalf("StableRunID() did not change after input hash changed")
	}
	allowedInputVariant := manifest
	allowedInputVariant.Tasks = append([]EvaluationTask(nil), manifest.Tasks...)
	allowedInputVariant.Tasks[0].AllowedInputs = append(append([]string(nil), manifest.Tasks[0].AllowedInputs...), "unexpected_input")
	if changed := StableRunID(allowedInputVariant); changed == first {
		t.Fatalf("StableRunID() did not change after allowed inputs changed")
	}
	qualityVariant := manifest
	qualityVariant.Tasks = append([]EvaluationTask(nil), manifest.Tasks...)
	qualityVariant.Tasks[0].QualitySpec.Goals = append(append([]string(nil), manifest.Tasks[0].QualitySpec.Goals...), "new_goal")
	if changed := StableRunID(qualityVariant); changed == first {
		t.Fatalf("StableRunID() did not change after QualitySpec goals changed")
	}
}

func TestNewRunSelectionNormalizesAndRejectsReleaseHoldout(t *testing.T) {
	selection, err := NewRunSelection(
		[]DataSplit{SplitRegression, SplitTuning, SplitTuning},
		[]string{"task-b", "task-a", "task-a"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]DataSplit{SplitRegression, SplitTuning}, selection.DataSplits) {
		t.Fatalf("splits=%v", selection.DataSplits)
	}
	if !reflect.DeepEqual([]string{"task-a", "task-b"}, selection.TaskIDs) {
		t.Fatalf("task IDs=%v", selection.TaskIDs)
	}
	if _, err := NewRunSelection([]DataSplit{SplitReleaseHoldout}, nil); err == nil {
		t.Fatal("release holdout selection must fail in Phase 0")
	}
}

func TestNewRunSelectionRejectsInvalidInputs(t *testing.T) {
	for _, selection := range []struct {
		name    string
		splits  []DataSplit
		taskIDs []string
	}{
		{name: "empty splits"},
		{name: "unknown split", splits: []DataSplit{"unknown"}},
		{name: "invalid task ID", splits: []DataSplit{SplitTuning}, taskIDs: []string{"invalid task id"}},
	} {
		t.Run(selection.name, func(t *testing.T) {
			if _, err := NewRunSelection(selection.splits, selection.taskIDs); err == nil {
				t.Fatal("NewRunSelection() error = nil")
			}
		})
	}
}

func TestSelectTasksFiltersManifestOrderAndRejectsSplitMismatch(t *testing.T) {
	_, manifest := writeValidManifest(t)
	selection, err := NewRunSelection(
		[]DataSplit{SplitRegression, SplitTuning},
		[]string{"long_serial-02", "long_serial-01"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := SelectTasks(manifest, selection)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []string{tasks[0].ID, tasks[1].ID}, []string{"long_serial-01", "long_serial-02"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected tasks=%v, want %v", got, want)
	}
	mismatch, err := NewRunSelection([]DataSplit{SplitTuning}, []string{"long_serial-02"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SelectTasks(manifest, mismatch); err == nil {
		t.Fatal("selection task outside selected split must fail")
	}
	missing, err := NewRunSelection([]DataSplit{SplitTuning}, []string{"missing-task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SelectTasks(manifest, missing); err == nil {
		t.Fatal("missing selected task must fail")
	}
}

func TestStableCohortRunIDIncludesSelectionAndHarnessPolicy(t *testing.T) {
	_, manifest := writeValidManifest(t)
	tuning, _ := NewRunSelection([]DataSplit{SplitTuning}, nil)
	regression, _ := NewRunSelection([]DataSplit{SplitRegression}, nil)
	left := StableCohortRunID(manifest, tuning, "sha256:"+strings.Repeat("1", 64))
	right := StableCohortRunID(manifest, regression, "sha256:"+strings.Repeat("1", 64))
	changedPolicy := StableCohortRunID(manifest, tuning, "sha256:"+strings.Repeat("2", 64))
	if left == right || left == changedPolicy || !strings.HasPrefix(left, "run-") {
		t.Fatalf("cohort identities are not isolated: %q %q %q", left, right, changedPolicy)
	}
}

func TestStableCohortRunIDNormalizesSelectionBeforeHashing(t *testing.T) {
	_, manifest := writeValidManifest(t)
	normalized, err := NewRunSelection(
		[]DataSplit{SplitRegression, SplitTuning},
		[]string{"long_serial-01", "long_serial-02"},
	)
	if err != nil {
		t.Fatal(err)
	}
	literal := RunSelection{
		DataSplits: []DataSplit{SplitTuning, SplitRegression, SplitTuning},
		TaskIDs:    []string{"long_serial-02", "long_serial-01", "long_serial-02"},
	}
	policy := "sha256:" + strings.Repeat("1", 64)
	if got, want := StableCohortRunID(manifest, literal, policy), StableCohortRunID(manifest, normalized, policy); got != want {
		t.Fatalf("equivalent selections produced IDs %q and %q", got, want)
	}
}

func TestCreateRunWithSelectionUsesCohortIdentityAndTasks(t *testing.T) {
	path, manifest := writeValidManifest(t)
	selection, err := NewRunSelection([]DataSplit{SplitRegression}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := "sha256:" + strings.Repeat("1", 64)
	run, err := CreateRun(path, CreateRunOptions{
		RunRoot: filepath.Join(filepath.Dir(path), manifest.RunRoot), Selection: &selection,
		HarnessPolicySHA256: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID != StableCohortRunID(manifest, selection, policy) || !reflect.DeepEqual(run.Selection, &selection) || run.HarnessPolicySHA256 != policy {
		t.Fatalf("cohort run=%#v", run)
	}
	for _, task := range run.Tasks {
		if task.DataSplit != SplitRegression {
			t.Fatalf("unexpected task outside cohort: %#v", task)
		}
	}
}

func TestBlindOrderIsDeterministic(t *testing.T) {
	taskHash := "sha256:" + strings.Repeat("a", 64)
	first := BlindOrder(taskHash)
	for i := 0; i < 10; i++ {
		if got := BlindOrder(taskHash); !reflect.DeepEqual(got, first) {
			t.Fatalf("BlindOrder() = %#v, want %#v", got, first)
		}
	}
}

func TestDeidentifyOutputRemovesSourceLabels(t *testing.T) {
	input := "S arm generated by deepseek-v4-pro in C:\\private\\run-1\\s-output.txt using skill novelist"
	got := DeidentifyOutput(input, []string{"S arm", "deepseek-v4-pro", "C:\\private\\run-1", "s-output.txt", "novelist"})
	for _, forbidden := range []string{"S arm", "deepseek-v4-pro", "C:\\private\\run-1", "s-output.txt", "novelist"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Fatalf("DeidentifyOutput() retained %q in %q", forbidden, got)
		}
	}
}

func TestPackageBlindMarksMissingArmNotReady(t *testing.T) {
	path, manifest := writeValidManifest(t)
	runRoot := filepath.Join(filepath.Dir(path), manifest.RunRoot)
	run := newTestRun(t, path, manifest, runRoot, false)
	index, err := PackageBlind(runRoot, run.RunID)
	if err != nil {
		t.Fatalf("PackageBlind() error = %v", err)
	}
	if index.Status != StatusNotReady || len(index.Samples) != len(manifest.Tasks) {
		t.Fatalf("blind index = %#v", index)
	}
	for _, sample := range index.Samples {
		if sample.Status != StatusNotReady || sample.OptionAFile != "" || sample.OptionBFile != "" {
			t.Fatalf("missing-arm sample = %#v", sample)
		}
	}
}

func newTestRun(t *testing.T, manifestPath string, manifest CorpusManifest, runRoot string, ready bool) RunRecord {
	t.Helper()
	run, err := CreateRun(manifestPath, CreateRunOptions{RunRoot: runRoot, BaselineStatus: StatusEnvironmentBlocked, HarnessStatus: StatusNotReady})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if !ready {
		return run
	}
	for i := range run.Tasks {
		taskDir := filepath.Join(runRoot, run.RunID, "private", "outputs", run.Tasks[i].TaskID)
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, arm := range []string{"S", "H"} {
			path := filepath.Join(taskDir, strings.ToLower(arm)+".txt")
			if err := os.WriteFile(path, []byte("fiction output "+arm+" for "+run.Tasks[i].TaskID), 0o644); err != nil {
				t.Fatal(err)
			}
			record := run.Tasks[i].Arms[arm]
			record.Status = StatusReady
			record.OutputFile = filepath.ToSlash(filepath.Join("private", "outputs", run.Tasks[i].TaskID, strings.ToLower(arm)+".txt"))
			record.OutputSHA256 = testFileHash(t, path)
			record.Usage = UsageRecord{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, ModelCalls: 1}
			record.Cost = CostRecord{Status: "recorded", Currency: "USD", Amount: floatPtr64(0.01)}
			run.Tasks[i].Arms[arm] = record
		}
	}
	if err := SaveRun(runRoot, run); err != nil {
		t.Fatal(err)
	}
	return run
}

func floatPtr64(value float64) *float64 { return &value }
