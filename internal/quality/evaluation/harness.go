package evaluation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type harnessPersistenceError struct {
	Operation string
	RunID     string
	TaskID    string
	Stage     HarnessStage
	Err       error
}

func (err *harnessPersistenceError) Error() string {
	return fmt.Sprintf("run %s task %s Harness %s %s persistence failed: %v", err.RunID, err.TaskID, err.Stage, err.Operation, err.Err)
}

func (err *harnessPersistenceError) Unwrap() error { return err.Err }

// HarnessTextGenerator executes one already-bounded, offline Harness request.
type HarnessTextGenerator interface {
	GenerateHarness(context.Context, HarnessRequest) (GenerationResult, error)
}

// ExecuteOfflineHarness executes the fixed four-stage H sequence for a selected, non-holdout cohort.
func ExecuteOfflineHarness(ctx context.Context, manifestPath, runRoot, runID, policyPath string, generator HarnessTextGenerator) (RunRecord, error) {
	if generator == nil {
		return RunRecord{}, fmt.Errorf("offline Harness generator is required")
	}
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return RunRecord{}, err
	}
	policy, err := LoadHarnessPolicy(policyPath)
	if err != nil {
		return RunRecord{}, err
	}
	policyHash := HarnessPolicySHA256(policy)
	run, err := LoadRun(runRoot, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if run.Selection == nil || run.HarnessPolicySHA256 != policyHash || run.RunID != StableCohortRunID(manifest, *run.Selection, policyHash) {
		return RunRecord{}, fmt.Errorf("run %s does not match selected cohort and frozen Harness policy", runID)
	}
	selected, err := SelectTasks(manifest, *run.Selection)
	if err != nil {
		return RunRecord{}, err
	}
	if len(selected) != len(run.Tasks) {
		return RunRecord{}, fmt.Errorf("run %s task set does not match selected cohort", runID)
	}
	for index, task := range selected {
		if task.DataSplit == SplitReleaseHoldout {
			return RunRecord{}, fmt.Errorf("run %s includes release_holdout task %s", runID, task.ID)
		}
		if run.Tasks[index].TaskID != task.ID {
			return RunRecord{}, fmt.Errorf("run %s task set does not match selected cohort", runID)
		}
	}
	templates, err := loadHarnessTemplates(policyPath, policy)
	if err != nil {
		return RunRecord{}, err
	}
	failed := false
	for taskIndex := range run.Tasks {
		if err := ctx.Err(); err != nil {
			return RunRecord{}, fmt.Errorf("run %s interrupted before task %s: %w", runID, run.Tasks[taskIndex].TaskID, err)
		}
		runTask := &run.Tasks[taskIndex]
		task, ok := FindManifestTask(manifest, runTask.TaskID)
		if !ok || task.DataSplit == SplitReleaseHoldout || task.InputSHA256 != runTask.InputSHA256 || task.ModelConfigSnapshot.SHA256 != runTask.ModelConfigSHA256 {
			return RunRecord{}, fmt.Errorf("run %s task %s no longer matches frozen cohort inputs", runID, runTask.TaskID)
		}
		input, err := os.ReadFile(ResolveInputPath(manifestPath, manifest, task))
		if err != nil {
			return RunRecord{}, fmt.Errorf("run %s task %s read input: %w", runID, task.ID, err)
		}
		if OutputSHA256(string(input)) != task.InputSHA256 {
			return RunRecord{}, fmt.Errorf("run %s task %s input hash drift", runID, task.ID)
		}
		if err := executeHarnessTask(ctx, runRoot, &run, runTask, task, string(input), policy, policyHash, templates, generator); err != nil {
			var persistenceErr *harnessPersistenceError
			if errors.As(err, &persistenceErr) {
				return run, persistenceErr
			}
			failed = true
		}
	}
	if failed {
		run.HarnessStatus = StatusFailed
		if err := SaveRun(runRoot, run); err != nil {
			return run, harnessRecordError(run.RunID, "", "", err)
		}
		return run, fmt.Errorf("run %s has failed Harness stages", runID)
	}
	run.HarnessStatus = StatusReady
	if err := SaveRun(runRoot, run); err != nil {
		return run, harnessRecordError(run.RunID, "", "", err)
	}
	return run, nil
}

func loadHarnessTemplates(policyPath string, policy HarnessPolicy) (map[HarnessStage]string, error) {
	base := filepath.Dir(policyPath)
	templates := make(map[HarnessStage]string, len(policy.Stages))
	for _, stage := range policy.Stages {
		path := filepath.Join(base, filepath.FromSlash(stage.TemplateFile))
		if err := verifyFileHash(path, stage.TemplateSHA256, "frozen Harness "+string(stage.Stage)+" template"); err != nil {
			return nil, err
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		templates[stage.Stage] = string(payload)
	}
	return templates, nil
}

func executeHarnessTask(ctx context.Context, runRoot string, run *RunRecord, runTask *RunTask, task EvaluationTask, originalInput string, policy HarnessPolicy, policyHash string, templates map[HarnessStage]string, generator HarnessTextGenerator) error {
	h := runTask.Arms["H"]
	outputs := make(map[HarnessStage]string, len(h.Stages))
	stages := []HarnessStage{HarnessStageCandidateA, HarnessStageCandidateB, HarnessStageReview, HarnessStageRevision}
	for index, stage := range stages {
		stagePolicy := policy.Stages[index]
		request, err := BuildHarnessRequest(stage, HarnessRequestInputs{
			TaskID: task.ID, OriginalInput: originalInput, InputSHA256: task.InputSHA256, QualityGoals: task.QualitySpec.Goals,
			CandidateA: outputs[HarnessStageCandidateA], CandidateB: outputs[HarnessStageCandidateB], ReviewJSON: outputs[HarnessStageReview],
			StagePolicy: stagePolicy, SystemTemplate: templates[stage], Model: task.ModelConfigSnapshot,
		})
		if err != nil {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, "harness_"+string(stage)+"_failed", err.Error())
		}
		if index < len(h.Stages) && validHarnessStageOutput(filepath.Join(runRoot, run.RunID), h.Stages[index], request, stagePolicy, task.ModelConfigSnapshot.SHA256, policyHash) {
			payload, err := os.ReadFile(filepath.Join(runRoot, run.RunID, filepath.FromSlash(h.Stages[index].OutputFile)))
			if err != nil {
				return failHarnessStage(runRoot, run, runTask, h, index, stage, "harness_"+string(stage)+"_failed", err.Error())
			}
			outputs[stage] = string(payload)
			continue
		}
		result, err := generator.GenerateHarness(ctx, request)
		if err != nil {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, "harness_"+string(stage)+"_failed", "generator failed")
		}
		if strings.TrimSpace(result.Output) == "" {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, "harness_"+string(stage)+"_failed", "generator returned empty output")
		}
		if len([]byte(result.Output)) > stagePolicy.MaxOutputBytes {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, "harness_"+string(stage)+"_failed", "generator output exceeds frozen stage byte limit")
		}
		if stage == HarnessStageReview {
			if _, err := ParseHarnessReview([]byte(result.Output), stagePolicy.MaxOutputBytes, task.QualitySpec.Goals); err != nil {
				return failHarnessStage(runRoot, run, runTask, h, index, stage, "harness_review_failed", "structured review validation failed")
			}
		}
		outputRel := filepath.ToSlash(filepath.Join("private", "outputs", task.ID, "h", string(stage)+".txt"))
		if err := writePrivateHarnessOutput(filepath.Join(runRoot, run.RunID, filepath.FromSlash(outputRel)), []byte(result.Output)); err != nil {
			return failHarnessOutputPersistence(runRoot, run, runTask, h, index, stage, err)
		}
		record := HarnessStageRecord{
			Stage: stage, Status: StatusReady, InputSHA256: request.InputSHA256, OutputSHA256: OutputSHA256(result.Output), OutputFile: outputRel,
			ModelConfigSHA256: task.ModelConfigSnapshot.SHA256, HarnessPolicySHA256: policyHash, TemplateSHA256: stagePolicy.TemplateSHA256,
			Usage: normalizedHarnessUsage(result.Usage), Cost: normalizedHarnessCost(result.Cost),
		}
		if index < len(h.Stages) {
			h.Stages[index] = record
		} else {
			h.Stages = append(h.Stages, record)
		}
		outputs[stage] = result.Output
		h = readyHarnessArm(h)
		runTask.Arms["H"] = h
		if err := SaveRun(runRoot, *run); err != nil {
			return harnessRecordError(run.RunID, task.ID, stage, err)
		}
	}
	h = readyHarnessArm(h)
	runTask.Arms["H"] = h
	return nil
}

func validHarnessStageOutput(runDir string, record HarnessStageRecord, request HarnessRequest, policy HarnessStagePolicy, modelHash, policyHash string) bool {
	if record.Stage != request.Stage || record.Status != StatusReady || record.InputSHA256 != request.InputSHA256 || record.ModelConfigSHA256 != modelHash || record.HarnessPolicySHA256 != policyHash || record.TemplateSHA256 != policy.TemplateSHA256 || record.OutputFile == "" || record.OutputSHA256 == "" {
		return false
	}
	path := filepath.Join(runDir, filepath.FromSlash(record.OutputFile))
	if !pathWithin(runDir, path) {
		return false
	}
	hash, err := FileSHA256(path)
	return err == nil && hash == record.OutputSHA256
}

func failHarnessStage(runRoot string, run *RunRecord, runTask *RunTask, h ArmRecord, index int, stage HarnessStage, failureType, detail string) error {
	if err := persistHarnessStageFailure(runRoot, run, runTask, h, index, stage, failureType, detail); err != nil {
		return err
	}
	return fmt.Errorf("run %s task %s Harness %s failed: %s", run.RunID, runTask.TaskID, stage, detail)
}

func failHarnessOutputPersistence(runRoot string, run *RunRecord, runTask *RunTask, h ArmRecord, index int, stage HarnessStage, cause error) error {
	if err := persistHarnessStageFailure(runRoot, run, runTask, h, index, stage, "harness_"+string(stage)+"_failed", "output persistence failed"); err != nil {
		return err
	}
	return harnessRecordError(run.RunID, runTask.TaskID, stage, cause)
}

func persistHarnessStageFailure(runRoot string, run *RunRecord, runTask *RunTask, h ArmRecord, index int, stage HarnessStage, failureType, detail string) error {
	if index < len(h.Stages) {
		h.Stages = append([]HarnessStageRecord(nil), h.Stages[:index]...)
	}
	h.Stages = append(h.Stages, HarnessStageRecord{Stage: stage, Status: StatusFailed, FailureType: failureType})
	h.Status = StatusFailed
	h.FailureType = failureType
	h.FailureDetail = detail
	h.OutputFile = ""
	h.OutputSHA256 = ""
	h.Usage, h.Cost = aggregateHarnessStages(h.Stages)
	runTask.Arms["H"] = h
	run.HarnessStatus = StatusFailed
	if err := SaveRun(runRoot, *run); err != nil {
		return harnessRecordError(run.RunID, runTask.TaskID, stage, err)
	}
	return nil
}

func readyHarnessArm(h ArmRecord) ArmRecord {
	h.Status = StatusNotReady
	h.FailureType, h.FailureDetail = "", ""
	h.OutputFile, h.OutputSHA256 = "", ""
	h.Usage, h.Cost = aggregateHarnessStages(h.Stages)
	if len(h.Stages) == 4 && h.Stages[3].Stage == HarnessStageRevision && h.Stages[3].Status == StatusReady {
		h.Status = StatusReady
		h.OutputFile = h.Stages[3].OutputFile
		h.OutputSHA256 = h.Stages[3].OutputSHA256
	}
	return h
}

func aggregateHarnessStages(stages []HarnessStageRecord) (UsageRecord, CostRecord) {
	usage := UsageRecord{}
	for _, stage := range stages {
		if stage.Status != StatusReady {
			continue
		}
		usage.PromptTokens += stage.Usage.PromptTokens
		usage.CompletionTokens += stage.Usage.CompletionTokens
		usage.ReasoningTokens += stage.Usage.ReasoningTokens
		usage.TotalTokens += stage.Usage.TotalTokens
		usage.ModelCalls += stage.Usage.ModelCalls
	}
	if len(stages) == 0 {
		return usage, CostRecord{Status: "not_recorded", Note: "No successful Harness stage response was recorded."}
	}
	var amount float64
	currency := ""
	for _, stage := range stages {
		if stage.Status != StatusReady || stage.Cost.Status != "recorded" || stage.Cost.Amount == nil || strings.TrimSpace(stage.Cost.Currency) == "" {
			return usage, CostRecord{Status: "NOT-AVAILABLE", Note: "Harness stage pricing was not fully recorded."}
		}
		if currency == "" {
			currency = stage.Cost.Currency
		} else if currency != stage.Cost.Currency {
			return usage, CostRecord{Status: "NOT-AVAILABLE", Note: "Harness stage prices use different currencies."}
		}
		amount += *stage.Cost.Amount
	}
	return usage, CostRecord{Status: "recorded", Currency: currency, Amount: &amount}
}

func normalizedHarnessUsage(usage UsageRecord) UsageRecord {
	usage.ModelCalls = 1
	return usage
}

func normalizedHarnessCost(cost CostRecord) CostRecord {
	if strings.TrimSpace(cost.Status) == "" {
		return CostRecord{Status: "NOT-AVAILABLE", Note: "Provider usage was recorded but pricing data was unavailable."}
	}
	return cost
}

func writePrivateHarnessOutput(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func harnessRecordError(runID, taskID string, stage HarnessStage, err error) error {
	return &harnessPersistenceError{Operation: "run record", RunID: runID, TaskID: taskID, Stage: stage, Err: err}
}
