package evaluation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	frozenHarnessMaxOutputTokens = 4096
	frozenHarnessMaxOutputBytes  = 49152
)

type HarnessStage string

const (
	HarnessStageCandidateA HarnessStage = "candidate_a"
	HarnessStageCandidateB HarnessStage = "candidate_b"
	HarnessStageReview     HarnessStage = "review"
	HarnessStageRevision   HarnessStage = "revision"
)

// HarnessStagePolicy freezes the bounded output and template identity for one offline H stage.
type HarnessStagePolicy struct {
	Stage           HarnessStage `json:"stage"`
	TemplateFile    string       `json:"template_file"`
	TemplateSHA256  string       `json:"template_sha256"`
	MaxOutputTokens int          `json:"max_output_tokens"`
	MaxOutputBytes  int          `json:"max_output_bytes"`
}

// HarnessPolicy is the versioned, evaluation-only policy recorded with cohort runs.
type HarnessPolicy struct {
	Contract       string               `json:"contract"`
	Version        string               `json:"version"`
	PolicyID       string               `json:"policy_id"`
	AllowedSplits  []DataSplit          `json:"allowed_splits"`
	CandidateCount int                  `json:"candidate_count"`
	ThinkingStored bool                 `json:"thinking_persisted"`
	Stages         []HarnessStagePolicy `json:"stages"`
}

// LoadHarnessPolicy decodes and verifies the frozen H policy and all referenced templates.
func LoadHarnessPolicy(path string) (HarnessPolicy, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return HarnessPolicy{}, fmt.Errorf("read harness policy %s: %w", path, err)
	}
	var policy HarnessPolicy
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return HarnessPolicy{}, fmt.Errorf("decode harness policy: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return HarnessPolicy{}, fmt.Errorf("decode harness policy: trailing JSON value")
	}
	if err := validateHarnessPolicy(path, policy); err != nil {
		return HarnessPolicy{}, err
	}
	return policy, nil
}

// HarnessPolicySHA256 returns the stable identity of a policy already accepted by LoadHarnessPolicy.
func HarnessPolicySHA256(policy HarnessPolicy) string {
	return StableSHA256(policy)
}

func validateHarnessPolicy(path string, policy HarnessPolicy) error {
	if policy.Contract != "denova.quality-harness-policy" || policy.Version != "v1" {
		return fmt.Errorf("unsupported harness policy contract %q version %q", policy.Contract, policy.Version)
	}
	if !idPattern.MatchString(policy.PolicyID) {
		return fmt.Errorf("invalid harness policy id %q", policy.PolicyID)
	}
	if len(policy.AllowedSplits) != 2 || policy.AllowedSplits[0] != SplitTuning || policy.AllowedSplits[1] != SplitRegression {
		return fmt.Errorf("harness allowed_splits must be tuning, regression and never release_holdout")
	}
	if policy.CandidateCount != 2 {
		return fmt.Errorf("harness candidate_count must be 2")
	}
	if policy.ThinkingStored {
		return fmt.Errorf("harness policy cannot persist thinking")
	}
	expectedStages := []HarnessStage{HarnessStageCandidateA, HarnessStageCandidateB, HarnessStageReview, HarnessStageRevision}
	if len(policy.Stages) != len(expectedStages) {
		return fmt.Errorf("harness stages must be exactly candidate_a, candidate_b, review, revision")
	}
	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve harness policy directory: %w", err)
	}
	baseDir, err = filepath.EvalSymlinks(baseDir)
	if err != nil {
		return fmt.Errorf("resolve harness policy directory: %w", err)
	}
	for index, stage := range policy.Stages {
		if stage.Stage != expectedStages[index] {
			return fmt.Errorf("harness stages must be exactly candidate_a, candidate_b, review, revision")
		}
		if err := validateHarnessStage(baseDir, index, stage); err != nil {
			return err
		}
	}
	return nil
}

func validateHarnessStage(baseDir string, index int, stage HarnessStagePolicy) error {
	prefix := fmt.Sprintf("harness stage[%d]", index)
	if err := validateRelativePath(stage.TemplateFile, prefix+" template_file", true); err != nil {
		return err
	}
	templatePath := filepath.Join(baseDir, filepath.FromSlash(stage.TemplateFile))
	canonicalTemplatePath, err := filepath.EvalSymlinks(templatePath)
	if err != nil {
		return fmt.Errorf("resolve %s template path: %w", prefix, err)
	}
	if !pathWithin(baseDir, canonicalTemplatePath) {
		return fmt.Errorf("%s template_file escapes policy directory", prefix)
	}
	if err := verifyFileHash(canonicalTemplatePath, stage.TemplateSHA256, prefix+" template"); err != nil {
		return err
	}
	if stage.MaxOutputTokens <= 0 || stage.MaxOutputTokens > frozenHarnessMaxOutputTokens {
		return fmt.Errorf("%s max_output_tokens must be 1..%d", prefix, frozenHarnessMaxOutputTokens)
	}
	if stage.MaxOutputBytes <= 0 || stage.MaxOutputBytes > frozenHarnessMaxOutputBytes {
		return fmt.Errorf("%s max output bytes must be 1..%d", prefix, frozenHarnessMaxOutputBytes)
	}
	return nil
}
