package evaluation

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type BlindIndex struct {
	Contract    string        `json:"contract"`
	Version     string        `json:"version"`
	RunID       string        `json:"run_id"`
	GeneratedAt string        `json:"generated_at"`
	Status      ResultStatus  `json:"status"`
	Samples     []BlindSample `json:"samples"`
}

type BlindSample struct {
	SampleID     string       `json:"sample_id"`
	TaskHash     string       `json:"task_hash"`
	ProfileID    ProfileID    `json:"profile_id"`
	Genre        string       `json:"genre"`
	TaskType     TaskType     `json:"task_type"`
	LengthBucket LengthBucket `json:"length_bucket"`
	DataSplit    DataSplit    `json:"data_split"`
	Status       ResultStatus `json:"status"`
	OptionAFile  string       `json:"option_a_file,omitempty"`
	OptionBFile  string       `json:"option_b_file,omitempty"`
	MissingArms  []string     `json:"missing_arms,omitempty"`
}

type blindMap struct {
	Contract string           `json:"contract"`
	Version  string           `json:"version"`
	RunID    string           `json:"run_id"`
	Samples  []blindMapSample `json:"samples"`
}

type blindMapSample struct {
	SampleID   string `json:"sample_id"`
	TaskID     string `json:"task_id"`
	OptionAArm string `json:"option_a_arm"`
	OptionBArm string `json:"option_b_arm"`
}

type ReviewFormTemplate struct {
	Contract            string   `json:"contract"`
	Version             string   `json:"version"`
	Instructions        []string `json:"instructions"`
	RequiredRestatement []string `json:"required_restatement"`
	AllowedDecisions    []string `json:"allowed_decisions"`
	RequiredEvidence    string   `json:"required_evidence"`
}

func BlindOrder(taskHash string) [2]string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(taskHash)))
	if sum[0]&1 == 0 {
		return [2]string{"S", "H"}
	}
	return [2]string{"H", "S"}
}

func DeidentifyOutput(output string, clues []string) string {
	cleaned := output
	unique := make(map[string]bool, len(clues))
	filtered := make([]string, 0, len(clues))
	for _, clue := range clues {
		clue = strings.TrimSpace(clue)
		if clue == "" || unique[strings.ToLower(clue)] {
			continue
		}
		unique[strings.ToLower(clue)] = true
		filtered = append(filtered, clue)
	}
	sort.Slice(filtered, func(i, j int) bool { return len(filtered[i]) > len(filtered[j]) })
	for _, clue := range filtered {
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(clue))
		cleaned = re.ReplaceAllString(cleaned, "[source removed]")
	}
	return strings.TrimSpace(cleaned)
}

func PackageBlind(runRoot, runID string) (BlindIndex, error) {
	run, err := LoadRun(runRoot, runID)
	if err != nil {
		return BlindIndex{}, err
	}
	runDir := filepath.Join(runRoot, runID)
	index := BlindIndex{
		Contract: "denova.quality-evaluation-blind-index", Version: "v1", RunID: runID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: StatusReady,
	}
	mapping := blindMap{Contract: "denova.quality-evaluation-blind-map", Version: "v1", RunID: runID}
	for _, task := range run.Tasks {
		sampleID := sampleIDFromTaskHash(task.TaskHash)
		sample := BlindSample{
			SampleID: sampleID, TaskHash: task.TaskHash, ProfileID: task.ProfileID, Genre: task.Genre,
			TaskType: task.TaskType, LengthBucket: task.LengthBucket, DataSplit: task.DataSplit, Status: StatusReady,
		}
		for _, arm := range []string{"S", "H"} {
			if task.Arms[arm].Status != StatusReady || strings.TrimSpace(task.Arms[arm].OutputFile) == "" {
				sample.MissingArms = append(sample.MissingArms, arm)
			}
		}
		if len(sample.MissingArms) > 0 {
			sample.Status = StatusNotReady
			index.Status = StatusNotReady
			index.Samples = append(index.Samples, sample)
			continue
		}
		order := BlindOrder(task.TaskHash)
		mapping.Samples = append(mapping.Samples, blindMapSample{
			SampleID: sampleID, TaskID: task.TaskID, OptionAArm: order[0], OptionBArm: order[1],
		})
		sampleDir := filepath.Join(runDir, "blind", "samples", sampleID)
		for optionIndex, arm := range order {
			record := task.Arms[arm]
			outputPath := filepath.Join(runDir, filepath.FromSlash(record.OutputFile))
			if !pathWithin(runDir, outputPath) {
				return BlindIndex{}, fmt.Errorf("run %s task %s arm %s output escapes run directory", runID, task.TaskID, arm)
			}
			payload, err := os.ReadFile(outputPath)
			if err != nil {
				return BlindIndex{}, fmt.Errorf("run %s task %s arm %s read output: %w", runID, task.TaskID, arm, err)
			}
			if got := bytesSHA256(payload); got != record.OutputSHA256 {
				return BlindIndex{}, fmt.Errorf("run %s task %s arm %s output hash mismatch", runID, task.TaskID, arm)
			}
			clues := []string{
				arm + " arm", record.Provider, record.BaseURL, record.ModelProfileID, record.Model, runID, runDir, filepath.Dir(outputPath),
				filepath.Base(outputPath), record.OutputFile, "Harness", "Quality Harness",
			}
			cleaned := DeidentifyOutput(string(payload), clues)
			option := "A"
			if optionIndex == 1 {
				option = "B"
			}
			rel := filepath.ToSlash(filepath.Join("samples", sampleID, option+".txt"))
			if err := os.MkdirAll(sampleDir, 0o755); err != nil {
				return BlindIndex{}, err
			}
			if err := os.WriteFile(filepath.Join(sampleDir, option+".txt"), []byte(cleaned+"\n"), 0o644); err != nil {
				return BlindIndex{}, err
			}
			if option == "A" {
				sample.OptionAFile = rel
			} else {
				sample.OptionBFile = rel
			}
		}
		index.Samples = append(index.Samples, sample)
	}
	if err := writeJSONFile(filepath.Join(runDir, "private", "blind-map.json"), mapping); err != nil {
		return BlindIndex{}, err
	}
	if err := writeJSONFile(filepath.Join(runDir, "blind", "package.json"), index); err != nil {
		return BlindIndex{}, err
	}
	template := ReviewFormTemplate{
		Contract: "denova.quality-evaluation-review-form", Version: "v1",
		Instructions: []string{
			"Read both options completely and independently; do not infer or seek their source.",
			"Restate the character goal, obstacle, choice, cost, and resulting text change before deciding.",
			"Choose A, B, or tie and cite concrete prose evidence.",
		},
		RequiredRestatement: []string{"character_goal", "obstacle", "choice", "cost", "text_change"},
		AllowedDecisions:    []string{"A", "B", "tie"},
		RequiredEvidence:    "Quote the relevant prose and explain how it supports the paired decision.",
	}
	if err := writeJSONFile(filepath.Join(runDir, "blind", "review-form-v1.json"), template); err != nil {
		return BlindIndex{}, err
	}
	return index, nil
}

func LoadBlindIndex(runRoot, runID string) (BlindIndex, error) {
	var index BlindIndex
	if err := readStrictJSON(filepath.Join(runRoot, runID, "blind", "package.json"), &index); err != nil {
		return BlindIndex{}, err
	}
	if index.Contract != "denova.quality-evaluation-blind-index" || index.Version != "v1" || index.RunID != runID {
		return BlindIndex{}, fmt.Errorf("invalid blind package for run %s", runID)
	}
	return index, nil
}

func loadBlindMap(runRoot, runID string) (blindMap, error) {
	var mapping blindMap
	if err := readStrictJSON(filepath.Join(runRoot, runID, "private", "blind-map.json"), &mapping); err != nil {
		return blindMap{}, err
	}
	if mapping.Contract != "denova.quality-evaluation-blind-map" || mapping.Version != "v1" || mapping.RunID != runID {
		return blindMap{}, fmt.Errorf("invalid blind map for run %s", runID)
	}
	return mapping, nil
}

func sampleIDFromTaskHash(taskHash string) string {
	value := strings.TrimPrefix(taskHash, "sha256:")
	if len(value) > 20 {
		value = value[:20]
	}
	return "sample-" + value
}
