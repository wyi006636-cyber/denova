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
	if run.Selection == nil || run.HarnessPolicyID != policy.PolicyID || run.HarnessPolicySHA256 != policyHash || run.RunID != StableCohortRunID(manifest, *run.Selection, policyHash) {
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
			return failHarnessStage(runRoot, run, runTask, h, index, stage, GenerationResult{}, false, "harness_"+string(stage)+"_failed", "request_build_failed")
		}
		if index < len(h.Stages) && validHarnessStageOutput(filepath.Join(runRoot, run.RunID), h.Stages[index], request, stagePolicy, task.ModelConfigSnapshot.SHA256, policyHash) {
			payload, err := os.ReadFile(filepath.Join(runRoot, run.RunID, filepath.FromSlash(h.Stages[index].OutputFile)))
			if err != nil {
				return failHarnessStage(runRoot, run, runTask, h, index, stage, GenerationResult{}, false, "harness_"+string(stage)+"_failed", "prior_output_unreadable")
			}
			outputs[stage] = string(payload)
			continue
		}
		result, err := generator.GenerateHarness(ctx, request)
		if err != nil {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, result, true, "harness_"+string(stage)+"_failed", "provider_error")
		}
		if strings.TrimSpace(result.Output) == "" {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, result, true, "harness_"+string(stage)+"_failed", "empty_output")
		}
		if len([]byte(result.Output)) > stagePolicy.MaxOutputBytes {
			return failHarnessStage(runRoot, run, runTask, h, index, stage, result, true, "harness_"+string(stage)+"_failed", "oversize_output")
		}
		if stage == HarnessStageReview {
			if _, err := ParseHarnessReview([]byte(result.Output), stagePolicy.MaxOutputBytes, task.QualitySpec.Goals); err != nil {
				return failHarnessStage(runRoot, run, runTask, h, index, stage, result, true, "harness_review_failed", harnessReviewFailureDetail(err))
			}
		}
		outputRel := filepath.ToSlash(filepath.Join("private", "outputs", task.ID, "h", string(stage)+".txt"))
		if err := writePrivateHarnessOutput(filepath.Join(runRoot, run.RunID, filepath.FromSlash(outputRel)), []byte(result.Output)); err != nil {
			return failHarnessOutputPersistence(runRoot, run, runTask, h, index, stage, result, err)
		}
		record := HarnessStageRecord{
			Stage: stage, Status: StatusReady, InputSHA256: request.InputSHA256, OutputSHA256: OutputSHA256(result.Output), OutputFile: outputRel,
			ModelConfigSHA256: task.ModelConfigSnapshot.SHA256, HarnessPolicySHA256: policyHash, TemplateSHA256: stagePolicy.TemplateSHA256,
			Usage: normalizedHarnessUsage(result.Usage), Cost: normalizedHarnessCost(result.Cost),
		}
		if index < len(h.Stages) {
			record.Attempts = append(h.Stages[index].Attempts, HarnessCallAttempt{ProviderAttempted: true, Status: StatusReady, Usage: record.Usage, Cost: record.Cost})
			h.Stages[index] = record
		} else {
			record.Attempts = []HarnessCallAttempt{{ProviderAttempted: true, Status: StatusReady, Usage: record.Usage, Cost: record.Cost}}
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
	if h.Status != StatusReady {
		return fmt.Errorf("run %s task %s Harness does not have exactly four attempted calls", run.RunID, runTask.TaskID)
	}
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

func failHarnessStage(runRoot string, run *RunRecord, runTask *RunTask, h ArmRecord, index int, stage HarnessStage, result GenerationResult, providerAttempted bool, failureType, detail string) error {
	if err := persistHarnessStageFailure(runRoot, run, runTask, h, index, stage, result, providerAttempted, failureType, detail); err != nil {
		return err
	}
	return fmt.Errorf("run %s task %s Harness %s failed: %s", run.RunID, runTask.TaskID, stage, detail)
}

func failHarnessOutputPersistence(runRoot string, run *RunRecord, runTask *RunTask, h ArmRecord, index int, stage HarnessStage, result GenerationResult, cause error) error {
	if err := persistHarnessStageFailure(runRoot, run, runTask, h, index, stage, result, true, "harness_"+string(stage)+"_failed", "output_persistence_failed"); err != nil {
		return err
	}
	return harnessRecordError(run.RunID, runTask.TaskID, stage, cause)
}

func persistHarnessStageFailure(runRoot string, run *RunRecord, runTask *RunTask, h ArmRecord, index int, stage HarnessStage, result GenerationResult, providerAttempted bool, failureType, detail string) error {
	usage, cost := normalizedHarnessAttempt(result, providerAttempted)
	attempt := HarnessCallAttempt{ProviderAttempted: providerAttempted, Status: StatusFailed, Usage: usage, Cost: cost, FailureType: failureType, FailureDetail: detail}
	if result.Output != "" {
		attemptNumber := 1
		if index < len(h.Stages) {
			attemptNumber = len(h.Stages[index].Attempts) + 1
		}
		outputRel := filepath.ToSlash(filepath.Join("private", "failures", runTask.TaskID, "h", string(stage)+fmt.Sprintf("-%d.txt", attemptNumber)))
		if err := writePrivateHarnessOutput(filepath.Join(runRoot, run.RunID, filepath.FromSlash(outputRel)), []byte(result.Output)); err != nil {
			return harnessRecordError(run.RunID, runTask.TaskID, stage, err)
		}
		attempt.FailureOutputFile = outputRel
		attempt.FailureOutputSHA256 = OutputSHA256(result.Output)
	}
	record := HarnessStageRecord{Stage: stage, Status: StatusFailed, Usage: usage, Cost: cost, FailureType: failureType, FailureDetail: detail, FailureOutputFile: attempt.FailureOutputFile, FailureOutputSHA256: attempt.FailureOutputSHA256, Attempts: []HarnessCallAttempt{attempt}}
	if index < len(h.Stages) {
		record.InputSHA256 = h.Stages[index].InputSHA256
		record.ModelConfigSHA256 = h.Stages[index].ModelConfigSHA256
		record.HarnessPolicySHA256 = h.Stages[index].HarnessPolicySHA256
		record.TemplateSHA256 = h.Stages[index].TemplateSHA256
		record.Attempts = append(h.Stages[index].Attempts, attempt)
		h.Stages = append(append([]HarnessStageRecord(nil), h.Stages[:index]...), record)
	} else {
		h.Stages = append(h.Stages, record)
	}
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
	if hasFourReadyHarnessStages(h.Stages) && h.Usage.ModelCalls == 4 {
		h.Status = StatusReady
		h.OutputFile = h.Stages[3].OutputFile
		h.OutputSHA256 = h.Stages[3].OutputSHA256
	}
	return h
}

func hasFourReadyHarnessStages(stages []HarnessStageRecord) bool {
	expected := []HarnessStage{HarnessStageCandidateA, HarnessStageCandidateB, HarnessStageReview, HarnessStageRevision}
	if len(stages) != len(expected) {
		return false
	}
	for index, stage := range stages {
		if stage.Stage != expected[index] || stage.Status != StatusReady {
			return false
		}
	}
	return true
}

func aggregateHarnessStages(stages []HarnessStageRecord) (UsageRecord, CostRecord) {
	usage := UsageRecord{}
	for _, stage := range stages {
		for _, attempt := range harnessStageAttempts(stage) {
			if !attempt.ProviderAttempted {
				continue
			}
			usage.PromptTokens += attempt.Usage.PromptTokens
			usage.CompletionTokens += attempt.Usage.CompletionTokens
			usage.ReasoningTokens += attempt.Usage.ReasoningTokens
			usage.TotalTokens += attempt.Usage.TotalTokens
			usage.ModelCalls += attempt.Usage.ModelCalls
		}
	}
	if len(stages) == 0 {
		return usage, CostRecord{Status: "not_recorded", Note: "No successful Harness stage response was recorded."}
	}
	var amount float64
	currency := ""
	providerAttempts := 0
	for _, stage := range stages {
		for _, attempt := range harnessStageAttempts(stage) {
			if !attempt.ProviderAttempted {
				continue
			}
			providerAttempts++
			if attempt.Cost.Status != "recorded" || attempt.Cost.Amount == nil || strings.TrimSpace(attempt.Cost.Currency) == "" {
				return usage, CostRecord{Status: "NOT-AVAILABLE", Note: "Harness stage pricing was not fully recorded."}
			}
			if currency == "" {
				currency = attempt.Cost.Currency
			} else if currency != attempt.Cost.Currency {
				return usage, CostRecord{Status: "NOT-AVAILABLE", Note: "Harness stage prices use different currencies."}
			}
			amount += *attempt.Cost.Amount
		}
	}
	if providerAttempts == 0 {
		return usage, CostRecord{Status: "not_recorded", Note: "No Harness Provider call was attempted."}
	}
	return usage, CostRecord{Status: "recorded", Currency: currency, Amount: &amount}
}

func harnessStageAttempts(stage HarnessStageRecord) []HarnessCallAttempt {
	if len(stage.Attempts) != 0 {
		return stage.Attempts
	}
	return []HarnessCallAttempt{{ProviderAttempted: stage.Status == StatusReady, Status: stage.Status, Usage: stage.Usage, Cost: stage.Cost}}
}

func normalizedHarnessAttempt(result GenerationResult, providerAttempted bool) (UsageRecord, CostRecord) {
	if !providerAttempted {
		return UsageRecord{}, CostRecord{Status: "not_recorded", Note: "Provider was not called."}
	}
	return normalizedHarnessUsage(result.Usage), normalizedHarnessCost(result.Cost)
}

func harnessReviewFailureDetail(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "sensitive"):
		return "sensitive_field"
	case strings.Contains(message, "decode") || strings.Contains(message, "JSON"):
		return "invalid_json"
	default:
		return "contract_mismatch"
	}
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
	if err := protectPrivateEvidence(filepath.Dir(path), true); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := protectPrivateEvidence(tmpPath, false); err != nil {
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
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	if err := protectPrivateEvidence(path, false); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func harnessRecordError(runID, taskID string, stage HarnessStage, err error) error {
	return &harnessPersistenceError{Operation: "run record", RunID: runID, TaskID: taskID, Stage: stage, Err: err}
}
