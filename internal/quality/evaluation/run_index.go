package evaluation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RunIndex struct {
	Contract    string          `json:"contract"`
	Version     string          `json:"version"`
	GeneratedAt string          `json:"generated_at"`
	Runs        []RunIndexEntry `json:"runs"`
}

type RunIndexEntry struct {
	RunID                  string                          `json:"run_id"`
	Selection              RunSelection                    `json:"selection"`
	TaskCount              int                             `json:"task_count"`
	HarnessPolicySHA256    string                          `json:"harness_policy_sha256"`
	BaselineTemplateSHA256 string                          `json:"baseline_template_sha256"`
	ModelConfigSHA256      []string                        `json:"model_config_sha256"`
	ArmStatusCounts        map[string]map[ResultStatus]int `json:"arm_status_counts"`
	Usage                  map[string]UsageRecord          `json:"usage"`
	Cost                   map[string]CostRecord           `json:"cost"`
	LocalEvidenceAvailable bool                            `json:"local_evidence_available"`
	BlindPackageSHA256     string                          `json:"blind_package_sha256,omitempty"`
}

func ExportRunIndex(runRoot string, runIDs []string, output string) (RunIndex, error) {
	if !filepath.IsAbs(runRoot) {
		return RunIndex{}, fmt.Errorf("run root must be an absolute private path")
	}
	if strings.TrimSpace(output) == "" {
		return RunIndex{}, fmt.Errorf("output path is required")
	}
	ids, err := normalizedRunIDs(runIDs)
	if err != nil {
		return RunIndex{}, err
	}
	index := RunIndex{Contract: "denova.quality-evaluation-run-index", Version: "v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, runID := range ids {
		run, err := LoadRun(runRoot, runID)
		if err != nil {
			return RunIndex{}, err
		}
		entry, err := boundedRunIndexEntry(runRoot, run)
		if err != nil {
			return RunIndex{}, err
		}
		index.Runs = append(index.Runs, entry)
	}
	if err := writeJSONFile(output, index); err != nil {
		return RunIndex{}, err
	}
	return index, nil
}

func normalizedRunIDs(runIDs []string) ([]string, error) {
	if len(runIDs) == 0 {
		return nil, fmt.Errorf("at least one run ID is required")
	}
	seen := make(map[string]struct{}, len(runIDs))
	ids := make([]string, 0, len(runIDs))
	for _, runID := range runIDs {
		runID = strings.TrimSpace(runID)
		if !idPattern.MatchString(runID) {
			return nil, fmt.Errorf("invalid run id %q", runID)
		}
		if _, exists := seen[runID]; exists {
			return nil, fmt.Errorf("duplicate run id %q", runID)
		}
		seen[runID] = struct{}{}
		ids = append(ids, runID)
	}
	sort.Strings(ids)
	return ids, nil
}

func boundedRunIndexEntry(runRoot string, run RunRecord) (RunIndexEntry, error) {
	selection := RunSelection{}
	if run.Selection != nil {
		selection = RunSelection{DataSplits: append([]DataSplit(nil), run.Selection.DataSplits...), TaskIDs: append([]string(nil), run.Selection.TaskIDs...)}
	}
	entry := RunIndexEntry{
		RunID: run.RunID, Selection: selection, TaskCount: len(run.Tasks), HarnessPolicySHA256: run.HarnessPolicySHA256,
		BaselineTemplateSHA256: run.TemplateSHA256, ModelConfigSHA256: runModelConfigHashes(run),
		ArmStatusCounts: map[string]map[ResultStatus]int{"S": {}, "H": {}},
		Usage:           map[string]UsageRecord{"S": {}, "H": {}}, Cost: map[string]CostRecord{},
	}
	for _, task := range run.Tasks {
		for _, armName := range []string{"S", "H"} {
			arm := task.Arms[armName]
			entry.ArmStatusCounts[armName][arm.Status]++
			entry.Usage[armName] = addUsage(entry.Usage[armName], arm.Usage)
			entry.Cost[armName] = addCost(entry.Cost[armName], arm.Cost)
		}
	}
	privateDir := filepath.Join(runRoot, run.RunID, "private")
	if info, err := os.Stat(privateDir); err == nil && info.IsDir() {
		entry.LocalEvidenceAvailable = true
	} else if err != nil && !os.IsNotExist(err) {
		return RunIndexEntry{}, err
	}
	packagePath := filepath.Join(runRoot, run.RunID, "blind", "package.json")
	if _, err := os.Stat(packagePath); err == nil {
		hash, err := FileSHA256(packagePath)
		if err != nil {
			return RunIndexEntry{}, err
		}
		entry.BlindPackageSHA256 = hash
	} else if !os.IsNotExist(err) {
		return RunIndexEntry{}, err
	}
	return entry, nil
}

func runModelConfigHashes(run RunRecord) []string {
	seen := make(map[string]struct{}, len(run.Tasks))
	for _, task := range run.Tasks {
		if task.ModelConfigSHA256 != "" {
			seen[task.ModelConfigSHA256] = struct{}{}
		}
	}
	hashes := make([]string, 0, len(seen))
	for hash := range seen {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	return hashes
}

func addUsage(total, value UsageRecord) UsageRecord {
	total.PromptTokens += value.PromptTokens
	total.CompletionTokens += value.CompletionTokens
	total.ReasoningTokens += value.ReasoningTokens
	total.TotalTokens += value.TotalTokens
	total.ModelCalls += value.ModelCalls
	return total
}

func addCost(total, value CostRecord) CostRecord {
	if total.Status == "" {
		total.Status = "recorded"
	}
	if value.Amount == nil || value.Status != "recorded" || (total.Currency != "" && value.Currency != "" && total.Currency != value.Currency) {
		total.Status, total.Currency, total.Amount = "NOT-AVAILABLE", "", nil
		return total
	}
	if total.Status == "NOT-AVAILABLE" {
		return total
	}
	if total.Currency == "" {
		total.Currency = value.Currency
	}
	amount := *value.Amount
	if total.Amount != nil {
		amount += *total.Amount
	}
	total.Amount = &amount
	return total
}
