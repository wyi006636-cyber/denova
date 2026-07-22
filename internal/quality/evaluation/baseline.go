package evaluation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type BaselineRequest struct {
	TaskID            string
	ProfileID         ProfileID
	TaskType          TaskType
	LengthBucket      LengthBucket
	SystemTemplate    string
	TemplateVersion   string
	TemplateSHA256    string
	AllowedInputs     []string
	Input             string
	InputSHA256       string
	QualityGoals      []string
	Model             ModelConfigSnapshot
	ModelCallLimit    int
	ThinkingPersisted bool
}

type GenerationResult struct {
	Output string
	Usage  UsageRecord
	Cost   CostRecord
}

type TextGenerator interface {
	Generate(context.Context, BaselineRequest) (GenerationResult, error)
}

func ExecuteSingleTurnBaseline(ctx context.Context, manifestPath, runRoot, runID string, generator TextGenerator) (RunRecord, error) {
	if generator == nil {
		return RunRecord{}, fmt.Errorf("single-turn baseline generator is required")
	}
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return RunRecord{}, err
	}
	run, err := LoadRun(runRoot, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if err := validateBaselineRun(manifest, run); err != nil {
		return RunRecord{}, err
	}
	template, err := os.ReadFile(ResolveTemplatePath(manifestPath, manifest))
	if err != nil {
		return RunRecord{}, fmt.Errorf("run %s read frozen single-turn template: %w", runID, err)
	}
	failed := false
	for taskIndex := range run.Tasks {
		if err := ctx.Err(); err != nil {
			return RunRecord{}, fmt.Errorf("run %s interrupted before task %s: %w", runID, run.Tasks[taskIndex].TaskID, err)
		}
		runTask := &run.Tasks[taskIndex]
		task, ok := FindManifestTask(manifest, runTask.TaskID)
		if !ok {
			return RunRecord{}, fmt.Errorf("run %s task %s missing from manifest", runID, runTask.TaskID)
		}
		input, err := os.ReadFile(ResolveInputPath(manifestPath, manifest, task))
		if err != nil {
			return RunRecord{}, fmt.Errorf("run %s task %s profile %s read input: %w", runID, task.ID, task.ProfileID, err)
		}
		sArm := runTask.Arms["S"]
		if validReadyBaselineOutput(runRoot, run, *runTask, task, manifest) {
			continue
		}
		request := BaselineRequest{
			TaskID: task.ID, ProfileID: task.ProfileID, TaskType: task.TaskType, LengthBucket: task.LengthBucket,
			SystemTemplate: string(template), TemplateVersion: manifest.Baseline.TemplateVersion,
			TemplateSHA256: manifest.Baseline.TemplateSHA256, AllowedInputs: append([]string(nil), task.AllowedInputs...),
			Input: string(input), InputSHA256: task.InputSHA256, QualityGoals: append([]string(nil), task.QualitySpec.Goals...),
			Model: task.ModelConfigSnapshot, ModelCallLimit: manifest.Baseline.ModelCallLimit,
			ThinkingPersisted: manifest.Baseline.ThinkingPersisted,
		}
		result, generationErr := generator.Generate(ctx, request)
		if generationErr != nil {
			failed = true
			sArm.Status = StatusFailed
			sArm.FailureType = "model_call_failed"
			sArm.FailureDetail = "The provider rejected or failed the single model call; no response content was persisted."
			sArm.OutputFile = ""
			sArm.OutputSHA256 = ""
			sArm.Usage = UsageRecord{}
			sArm.Cost = CostRecord{Status: "not_recorded", Note: "The single model call failed before a usable response was recorded."}
			runTask.Arms["S"] = sArm
			continue
		}
		if strings.TrimSpace(result.Output) == "" {
			failed = true
			sArm.Status = StatusFailed
			sArm.FailureType = "empty_model_output"
			sArm.FailureDetail = "The provider returned no usable text."
			runTask.Arms["S"] = sArm
			continue
		}
		if result.Usage.ModelCalls > 1 {
			failed = true
			sArm.Status = StatusFailed
			sArm.FailureType = "model_call_limit_exceeded"
			sArm.FailureDetail = fmt.Sprintf("Recorded %d calls; the S arm permits exactly one.", result.Usage.ModelCalls)
			runTask.Arms["S"] = sArm
			continue
		}
		result.Usage.ModelCalls = 1
		outputRel := filepath.ToSlash(filepath.Join("private", "outputs", task.ID, "s.txt"))
		outputPath := filepath.Join(runRoot, runID, filepath.FromSlash(outputRel))
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return RunRecord{}, fmt.Errorf("run %s task %s profile %s create output directory: %w", runID, task.ID, task.ProfileID, err)
		}
		if err := os.WriteFile(outputPath, []byte(result.Output), 0o600); err != nil {
			return RunRecord{}, fmt.Errorf("run %s task %s profile %s save S output: %w", runID, task.ID, task.ProfileID, err)
		}
		sArm.Status = StatusReady
		sArm.FailureType = ""
		sArm.FailureDetail = ""
		sArm.OutputFile = outputRel
		sArm.OutputSHA256 = OutputSHA256(result.Output)
		sArm.Usage = result.Usage
		sArm.Cost = result.Cost
		if strings.TrimSpace(sArm.Cost.Status) == "" {
			sArm.Cost = CostRecord{Status: "NOT-AVAILABLE", Note: "Provider usage was recorded but pricing data was unavailable."}
		}
		runTask.Arms["S"] = sArm
	}
	if failed {
		run.BaselineStatus = StatusFailed
	} else {
		run.BaselineStatus = StatusReady
	}
	if err := SaveRun(runRoot, run); err != nil {
		return RunRecord{}, err
	}
	return run, nil
}

func validReadyBaselineOutput(runRoot string, run RunRecord, runTask RunTask, task EvaluationTask, manifest CorpusManifest) bool {
	arm := runTask.Arms["S"]
	model := task.ModelConfigSnapshot
	if arm.Status != StatusReady || run.TemplateVersion != manifest.Baseline.TemplateVersion || run.TemplateSHA256 != manifest.Baseline.TemplateSHA256 || arm.InputSHA256 != task.InputSHA256 || arm.Provider != model.Provider || arm.BaseURL != model.BaseURL || arm.ModelProfileID != model.ModelProfileID || arm.Model != model.Model || arm.Parameters != model.Parameters || arm.OutputFile == "" || arm.OutputSHA256 == "" {
		return false
	}
	runDir, err := RunDirectory(runRoot, run.RunID)
	if err != nil {
		return false
	}
	outputPath := filepath.Join(runDir, filepath.FromSlash(arm.OutputFile))
	if !pathWithin(runDir, outputPath) {
		return false
	}
	resolvedRunDir, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		return false
	}
	resolvedOutputPath, err := filepath.EvalSymlinks(outputPath)
	if err != nil || !pathWithin(resolvedRunDir, resolvedOutputPath) {
		return false
	}
	hash, err := FileSHA256(outputPath)
	return err == nil && hash == arm.OutputSHA256
}

func validateBaselineRun(manifest CorpusManifest, run RunRecord) error {
	if run.Selection == nil {
		if run.RunID != StableRunID(manifest) {
			return fmt.Errorf("run %s does not match manifest inputs and frozen template", run.RunID)
		}
		return nil
	}
	selection, err := NewRunSelection(run.Selection.DataSplits, run.Selection.TaskIDs)
	if err != nil || !sha256Pattern.MatchString(run.HarnessPolicySHA256) || run.RunID != StableCohortRunID(manifest, selection, run.HarnessPolicySHA256) {
		return fmt.Errorf("run %s does not match selected cohort and frozen Harness policy", run.RunID)
	}
	selected, err := SelectTasks(manifest, selection)
	if err != nil || len(selected) != len(run.Tasks) {
		return fmt.Errorf("run %s task set does not match selected cohort", run.RunID)
	}
	for index, task := range selected {
		runTask := run.Tasks[index]
		if runTask.TaskID != task.ID || runTask.TaskHash != StableTaskHash(task) || runTask.InputSHA256 != task.InputSHA256 || runTask.ModelConfigSHA256 != task.ModelConfigSnapshot.SHA256 {
			return fmt.Errorf("run %s task set does not match selected cohort", run.RunID)
		}
	}
	return nil
}
