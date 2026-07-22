package evaluation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxHarnessRequestInputBytes = 131072

// BuildHarnessRequest assembles one source-bounded H request without invoking a model.
func BuildHarnessRequest(stage HarnessStage, inputs HarnessRequestInputs) (HarnessRequest, error) {
	payload, err := marshalHarnessStageInput(stage, inputs)
	if err != nil {
		return HarnessRequest{}, err
	}
	if len(payload) > maxHarnessRequestInputBytes {
		return HarnessRequest{}, fmt.Errorf("harness request input exceeds %d UTF-8 bytes", maxHarnessRequestInputBytes)
	}
	userInput := string(payload)
	return HarnessRequest{
		TaskID:         inputs.TaskID,
		Stage:          stage,
		SystemTemplate: inputs.SystemTemplate,
		UserInput:      userInput,
		InputSHA256:    OutputSHA256(userInput),
		Model:          inputs.Model,
	}, nil
}

func marshalHarnessStageInput(stage HarnessStage, inputs HarnessRequestInputs) ([]byte, error) {
	base := harnessStageInput{
		TaskID:        inputs.TaskID,
		Stage:         stage,
		OriginalInput: inputs.OriginalInput,
		InputSHA256:   inputs.InputSHA256,
		QualityGoals:  append([]string(nil), inputs.QualityGoals...),
	}
	switch stage {
	case HarnessStageCandidateA, HarnessStageCandidateB:
		return json.Marshal(base)
	case HarnessStageReview:
		base.CandidateA = inputs.CandidateA
		base.CandidateB = inputs.CandidateB
		return json.Marshal(base)
	case HarnessStageRevision:
		review, err := ParseHarnessReview([]byte(inputs.ReviewJSON), inputs.StagePolicy.MaxOutputBytes, inputs.QualityGoals)
		if err != nil {
			return nil, fmt.Errorf("validate revision review: %w", err)
		}
		base.CandidateA = inputs.CandidateA
		base.CandidateB = inputs.CandidateB
		base.Review = &review
		return json.Marshal(base)
	default:
		return nil, fmt.Errorf("unknown harness stage %q", stage)
	}
}

type harnessStageInput struct {
	TaskID        string         `json:"task_id"`
	Stage         HarnessStage   `json:"stage"`
	OriginalInput string         `json:"original_input"`
	InputSHA256   string         `json:"input_sha256"`
	QualityGoals  []string       `json:"quality_goals"`
	CandidateA    string         `json:"candidate_a,omitempty"`
	CandidateB    string         `json:"candidate_b,omitempty"`
	Review        *HarnessReview `json:"review,omitempty"`
}

// ParseHarnessReview accepts exactly one safe, schema-complete reviewer JSON object.
func ParseHarnessReview(payload []byte, maxBytes int, qualityGoals []string) (HarnessReview, error) {
	if maxBytes <= 0 {
		return HarnessReview{}, fmt.Errorf("review max bytes must be positive")
	}
	if len(payload) > maxBytes {
		return HarnessReview{}, fmt.Errorf("review exceeds %d policy bytes", maxBytes)
	}
	if err := rejectSensitiveJSON(payload); err != nil {
		return HarnessReview{}, fmt.Errorf("reject sensitive review JSON: %w", err)
	}
	if err := validateHarnessReviewShape(payload); err != nil {
		return HarnessReview{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var review HarnessReview
	if err := decoder.Decode(&review); err != nil {
		return HarnessReview{}, fmt.Errorf("decode harness review: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return HarnessReview{}, fmt.Errorf("decode harness review: trailing JSON value")
	}
	if review.PreferredCandidate != "A" && review.PreferredCandidate != "B" {
		return HarnessReview{}, fmt.Errorf("review preferred_candidate must be A or B")
	}
	if len(review.Issues) > 64 {
		return HarnessReview{}, fmt.Errorf("review issues cannot exceed 64")
	}
	allowedGoals := make(map[string]struct{}, len(qualityGoals))
	for _, goal := range qualityGoals {
		allowedGoals[goal] = struct{}{}
	}
	for index, issue := range review.Issues {
		if _, ok := allowedGoals[issue.GoalID]; !ok {
			return HarnessReview{}, fmt.Errorf("review issue[%d] has unknown goal_id %q", index, issue.GoalID)
		}
	}
	return review, nil
}

func validateHarnessReviewShape(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return fmt.Errorf("decode harness review object: %w", err)
	}
	if fields == nil {
		return fmt.Errorf("harness review must be a JSON object")
	}
	required := map[string]struct{}{"preferred_candidate": {}, "issues": {}, "preserve": {}}
	if len(fields) != len(required) {
		return fmt.Errorf("harness review must contain only preferred_candidate, issues, and preserve")
	}
	for field := range required {
		if _, ok := fields[field]; !ok {
			return fmt.Errorf("harness review missing %s", field)
		}
	}
	var rawIssues []map[string]json.RawMessage
	if err := json.Unmarshal(fields["issues"], &rawIssues); err != nil {
		return fmt.Errorf("decode harness review issues: %w", err)
	}
	for index, issue := range rawIssues {
		if len(issue) != 5 {
			return fmt.Errorf("review issue[%d] must contain exactly goal_id, severity, location, evidence, and action", index)
		}
		for _, field := range []string{"goal_id", "severity", "location", "evidence", "action"} {
			if _, ok := issue[field]; !ok {
				return fmt.Errorf("review issue[%d] missing %s", index, field)
			}
		}
	}
	return nil
}
