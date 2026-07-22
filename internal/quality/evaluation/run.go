package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CreateRunOptions struct {
	RunRoot             string
	BaselineStatus      ResultStatus
	HarnessStatus       ResultStatus
	BaselineFailureType string
	Selection           *RunSelection
	HarnessPolicySHA256 string
}

type RunRecord struct {
	Contract            string        `json:"contract"`
	Version             string        `json:"version"`
	RunID               string        `json:"run_id"`
	CreatedAt           string        `json:"created_at"`
	ManifestFile        string        `json:"manifest_file"`
	ManifestSHA256      string        `json:"manifest_sha256"`
	TemplateVersion     string        `json:"template_version"`
	TemplateSHA256      string        `json:"template_sha256"`
	BaselineStatus      ResultStatus  `json:"baseline_status"`
	HarnessStatus       ResultStatus  `json:"harness_status"`
	Selection           *RunSelection `json:"selection,omitempty"`
	HarnessPolicySHA256 string        `json:"harness_policy_sha256,omitempty"`
	Tasks               []RunTask     `json:"tasks"`
}

type taskIdentity struct {
	ID         string `json:"task_id"`
	TaskSHA256 string `json:"task_sha256"`
}

type RunTask struct {
	TaskID            string               `json:"task_id"`
	TaskHash          string               `json:"task_hash"`
	ProfileID         ProfileID            `json:"profile_id"`
	Genre             string               `json:"genre"`
	TaskType          TaskType             `json:"task_type"`
	LengthBucket      LengthBucket         `json:"length_bucket"`
	DataSplit         DataSplit            `json:"data_split"`
	InputSHA256       string               `json:"input_sha256"`
	ModelConfigSHA256 string               `json:"model_config_sha256"`
	Arms              map[string]ArmRecord `json:"arms"`
}

type ArmRecord struct {
	Arm            string               `json:"arm"`
	Status         ResultStatus         `json:"status"`
	Provider       string               `json:"provider,omitempty"`
	BaseURL        string               `json:"base_url,omitempty"`
	ModelProfileID string               `json:"model_profile_id,omitempty"`
	Model          string               `json:"model,omitempty"`
	Parameters     ModelParameters      `json:"parameters"`
	InputSHA256    string               `json:"input_sha256"`
	OutputSHA256   string               `json:"output_sha256,omitempty"`
	OutputFile     string               `json:"output_file,omitempty"`
	Usage          UsageRecord          `json:"usage"`
	Cost           CostRecord           `json:"cost"`
	FailureType    string               `json:"failure_type,omitempty"`
	FailureDetail  string               `json:"failure_detail,omitempty"`
	Stages         []HarnessStageRecord `json:"stages,omitempty"`
}

// HarnessStageRecord is the private, resumable evidence for one fixed Harness stage.
type HarnessStageRecord struct {
	Stage               HarnessStage `json:"stage"`
	Status              ResultStatus `json:"status"`
	InputSHA256         string       `json:"input_sha256"`
	OutputSHA256        string       `json:"output_sha256,omitempty"`
	OutputFile          string       `json:"output_file,omitempty"`
	ModelConfigSHA256   string       `json:"model_config_sha256"`
	HarnessPolicySHA256 string       `json:"harness_policy_sha256"`
	TemplateSHA256      string       `json:"template_sha256"`
	Usage               UsageRecord  `json:"usage"`
	Cost                CostRecord   `json:"cost"`
	FailureType         string       `json:"failure_type,omitempty"`
}

func StableRunID(manifest CorpusManifest) string {
	tasks := append([]EvaluationTask(nil), manifest.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	identities := make([]taskIdentity, 0, len(tasks))
	for _, task := range tasks {
		identities = append(identities, taskIdentity{ID: task.ID, TaskSHA256: StableTaskHash(task)})
	}
	payload := struct {
		Contract string           `json:"contract"`
		Version  string           `json:"version"`
		Baseline BaselineProtocol `json:"baseline"`
		Tasks    []taskIdentity   `json:"tasks"`
	}{manifest.Contract, manifest.Version, manifest.Baseline, identities}
	hash := StableSHA256(payload)
	if len(hash) < len("sha256:")+24 {
		return "run-invalid"
	}
	return "run-" + hash[len("sha256:"):len("sha256:")+24]
}

func NewRunSelection(dataSplits []DataSplit, taskIDs []string) (RunSelection, error) {
	if len(dataSplits) == 0 {
		return RunSelection{}, fmt.Errorf("run selection requires at least one data split")
	}
	selection := RunSelection{
		DataSplits: append([]DataSplit(nil), dataSplits...),
		TaskIDs:    append([]string(nil), taskIDs...),
	}
	sort.Slice(selection.DataSplits, func(i, j int) bool { return selection.DataSplits[i] < selection.DataSplits[j] })
	selection.DataSplits = deduplicateDataSplits(selection.DataSplits)
	for _, split := range selection.DataSplits {
		if !knownDataSplit(split) || split == SplitReleaseHoldout {
			return RunSelection{}, fmt.Errorf("data split %q is not selectable in Phase 0", split)
		}
	}
	sort.Strings(selection.TaskIDs)
	selection.TaskIDs = deduplicateStrings(selection.TaskIDs)
	for _, taskID := range selection.TaskIDs {
		if !idPattern.MatchString(taskID) {
			return RunSelection{}, fmt.Errorf("invalid selected task id %q", taskID)
		}
	}
	return selection, nil
}

func SelectTasks(manifest CorpusManifest, selection RunSelection) ([]EvaluationTask, error) {
	normalized, err := NewRunSelection(selection.DataSplits, selection.TaskIDs)
	if err != nil {
		return nil, err
	}
	selectedSplits := make(map[DataSplit]bool, len(normalized.DataSplits))
	for _, split := range normalized.DataSplits {
		selectedSplits[split] = true
	}
	requestedTasks := make(map[string]bool, len(normalized.TaskIDs))
	for _, taskID := range normalized.TaskIDs {
		requestedTasks[taskID] = true
	}
	foundTasks := make(map[string]bool, len(normalized.TaskIDs))
	tasks := make([]EvaluationTask, 0, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		if !selectedSplits[task.DataSplit] {
			continue
		}
		if len(requestedTasks) != 0 && !requestedTasks[task.ID] {
			continue
		}
		tasks = append(tasks, task)
		foundTasks[task.ID] = true
	}
	for _, taskID := range normalized.TaskIDs {
		if !foundTasks[taskID] {
			return nil, fmt.Errorf("selected task %q is absent or outside selected data splits", taskID)
		}
	}
	return tasks, nil
}

func StableCohortRunID(manifest CorpusManifest, selection RunSelection, policySHA256 string) string {
	normalized, err := NewRunSelection(selection.DataSplits, selection.TaskIDs)
	if err != nil {
		return "run-invalid"
	}
	tasks, err := SelectTasks(manifest, normalized)
	if err != nil {
		return "run-invalid"
	}
	identities := make([]taskIdentity, 0, len(tasks))
	for _, task := range tasks {
		identities = append(identities, taskIdentity{ID: task.ID, TaskSHA256: StableTaskHash(task)})
	}
	payload := struct {
		Contract            string           `json:"contract"`
		Version             string           `json:"version"`
		Baseline            BaselineProtocol `json:"baseline"`
		Selection           RunSelection     `json:"selection"`
		HarnessPolicySHA256 string           `json:"harness_policy_sha256"`
		Tasks               []taskIdentity   `json:"tasks"`
	}{manifest.Contract, manifest.Version, manifest.Baseline, normalized, policySHA256, identities}
	hash := StableSHA256(payload)
	if len(hash) < len("sha256:")+24 {
		return "run-invalid"
	}
	return "run-" + hash[len("sha256:"):len("sha256:")+24]
}

func StableTaskHash(task EvaluationTask) string {
	return StableSHA256(task)
}

func CreateRun(manifestPath string, options CreateRunOptions) (RunRecord, error) {
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return RunRecord{}, err
	}
	runRoot := options.RunRoot
	if strings.TrimSpace(runRoot) == "" {
		runRoot = filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.RunRoot))
	}
	runRoot, err = filepath.Abs(runRoot)
	if err != nil {
		return RunRecord{}, fmt.Errorf("resolve run root: %w", err)
	}
	selectedTasks := manifest.Tasks
	var selection *RunSelection
	policySHA256 := ""
	runID := StableRunID(manifest)
	if options.Selection != nil {
		normalized, err := NewRunSelection(options.Selection.DataSplits, options.Selection.TaskIDs)
		if err != nil {
			return RunRecord{}, err
		}
		if !sha256Pattern.MatchString(options.HarnessPolicySHA256) {
			return RunRecord{}, fmt.Errorf("invalid harness policy sha256 %q", options.HarnessPolicySHA256)
		}
		selectedTasks, err = SelectTasks(manifest, normalized)
		if err != nil {
			return RunRecord{}, err
		}
		selection = &normalized
		policySHA256 = options.HarnessPolicySHA256
		runID = StableCohortRunID(manifest, normalized, options.HarnessPolicySHA256)
	}
	if existing, err := LoadRun(runRoot, runID); err == nil {
		return existing, nil
	} else if !os.IsNotExist(rootCause(err)) {
		return RunRecord{}, err
	}
	baselineStatus := options.BaselineStatus
	if baselineStatus == "" {
		baselineStatus = StatusNotReady
	}
	harnessStatus := options.HarnessStatus
	if harnessStatus == "" {
		harnessStatus = StatusNotReady
	}
	manifestHash, err := FileSHA256(manifestPath)
	if err != nil {
		return RunRecord{}, fmt.Errorf("hash manifest: %w", err)
	}
	run := RunRecord{
		Contract: "denova.quality-evaluation-run", Version: "v1", RunID: runID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), ManifestFile: filepath.Base(manifestPath),
		ManifestSHA256: manifestHash, TemplateVersion: manifest.Baseline.TemplateVersion,
		TemplateSHA256: manifest.Baseline.TemplateSHA256, BaselineStatus: baselineStatus, HarnessStatus: harnessStatus,
		Selection: selection, HarnessPolicySHA256: policySHA256,
	}
	for _, task := range selectedTasks {
		model := task.ModelConfigSnapshot
		baselineFailure := options.BaselineFailureType
		if baselineFailure == "" && baselineStatus == StatusEnvironmentBlocked {
			baselineFailure = "provider_unavailable"
		}
		run.Tasks = append(run.Tasks, RunTask{
			TaskID: task.ID, TaskHash: StableTaskHash(task), ProfileID: task.ProfileID,
			Genre: task.Genre, TaskType: task.TaskType, LengthBucket: task.LengthBucket,
			DataSplit: task.DataSplit, InputSHA256: task.InputSHA256, ModelConfigSHA256: model.SHA256,
			Arms: map[string]ArmRecord{
				"S": {
					Arm: "S", Status: baselineStatus, Provider: model.Provider, BaseURL: model.BaseURL, ModelProfileID: model.ModelProfileID, Model: model.Model,
					Parameters: model.Parameters, InputSHA256: task.InputSHA256,
					Cost:        CostRecord{Status: "not_recorded", Note: "No successful model response was recorded."},
					FailureType: baselineFailure,
				},
				"H": {
					Arm: "H", Status: harnessStatus, Provider: model.Provider, BaseURL: model.BaseURL, ModelProfileID: model.ModelProfileID, Model: model.Model,
					Parameters: model.Parameters, InputSHA256: task.InputSHA256,
					Cost:        CostRecord{Status: "not_recorded", Note: "P0-T07 does not implement or fabricate the Harness arm."},
					FailureType: "harness_arm_not_available",
				},
			},
		})
	}
	if err := SaveRun(runRoot, run); err != nil {
		return RunRecord{}, err
	}
	return run, nil
}

func deduplicateDataSplits(values []DataSplit) []DataSplit {
	if len(values) == 0 {
		return nil
	}
	unique := values[:1]
	for _, value := range values[1:] {
		if value != unique[len(unique)-1] {
			unique = append(unique, value)
		}
	}
	return unique
}

func deduplicateStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	unique := values[:1]
	for _, value := range values[1:] {
		if value != unique[len(unique)-1] {
			unique = append(unique, value)
		}
	}
	return unique
}

func SaveRun(runRoot string, run RunRecord) error {
	if !idPattern.MatchString(run.RunID) {
		return fmt.Errorf("invalid run id %q", run.RunID)
	}
	runDir := filepath.Join(runRoot, run.RunID)
	if err := writeJSONFile(filepath.Join(runDir, "run.json"), run); err != nil {
		return fmt.Errorf("save run %s: %w", run.RunID, err)
	}
	for _, task := range run.Tasks {
		costs := struct {
			Contract string                `json:"contract"`
			Version  string                `json:"version"`
			RunID    string                `json:"run_id"`
			TaskID   string                `json:"task_id"`
			Arms     map[string]CostRecord `json:"arms"`
		}{"denova.quality-evaluation-cost", "v1", run.RunID, task.TaskID, map[string]CostRecord{
			"S": task.Arms["S"].Cost,
			"H": task.Arms["H"].Cost,
		}}
		if err := writeJSONFile(filepath.Join(runDir, "tasks", task.TaskID, "cost.json"), costs); err != nil {
			return fmt.Errorf("save run %s task %s cost: %w", run.RunID, task.TaskID, err)
		}
	}
	return nil
}

func LoadRun(runRoot, runID string) (RunRecord, error) {
	if !idPattern.MatchString(runID) {
		return RunRecord{}, fmt.Errorf("invalid run id %q", runID)
	}
	var run RunRecord
	if err := readStrictJSON(filepath.Join(runRoot, runID, "run.json"), &run); err != nil {
		return RunRecord{}, err
	}
	if run.Contract != "denova.quality-evaluation-run" || run.Version != "v1" || run.RunID != runID {
		return RunRecord{}, fmt.Errorf("invalid run record for %s", runID)
	}
	return run, nil
}

func RunDirectory(runRoot, runID string) (string, error) {
	if !idPattern.MatchString(runID) {
		return "", fmt.Errorf("invalid run id %q", runID)
	}
	return filepath.Join(runRoot, runID), nil
}

func ResolveRunRoot(manifestPath string, manifest CorpusManifest) string {
	return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.RunRoot))
}

func ResolveInputPath(manifestPath string, manifest CorpusManifest, task EvaluationTask) string {
	return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.InputRoot), filepath.FromSlash(task.InputFile))
}

func ResolveTemplatePath(manifestPath string, manifest CorpusManifest) string {
	return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Baseline.TemplateFile))
}

func FindManifestTask(manifest CorpusManifest, taskID string) (EvaluationTask, bool) {
	for _, task := range manifest.Tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return EvaluationTask{}, false
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
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
	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, path)
}

func readStrictJSON(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func rootCause(err error) error {
	for {
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok || unwrapped.Unwrap() == nil {
			return err
		}
		err = unwrapped.Unwrap()
	}
}
