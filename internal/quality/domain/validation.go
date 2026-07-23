package domain

import (
	"fmt"
	"reflect"
)

// ValidateQualitySpec verifies that the committed receipt is the exact result of v1 resolution.
func ValidateQualitySpec(spec QualitySpec) error {
	computed, err := ResolveQualitySpec(spec)
	if err != nil {
		return err
	}
	if len(spec.Resolution.ResolvedGoals) != len(computed.ResolvedGoals) {
		return &ContractError{Code: CodeResolutionMismatch, Path: "resolution.resolved_goals", Value: len(spec.Resolution.ResolvedGoals), Message: "receipt must contain exactly one result per applicable goal"}
	}

	actualByGoal := make(map[string]struct {
		index int
		goal  ResolvedGoal
	}, len(spec.Resolution.ResolvedGoals))
	for index, actual := range spec.Resolution.ResolvedGoals {
		if _, exists := actualByGoal[actual.GoalID]; exists {
			return &ContractError{Code: CodeDuplicateGoal, Path: fmt.Sprintf("resolution.resolved_goals[%d].goal_id", index), GoalID: actual.GoalID, Value: actual.GoalID, Message: "receipt goal IDs must be unique"}
		}
		actualByGoal[actual.GoalID] = struct {
			index int
			goal  ResolvedGoal
		}{index: index, goal: actual}
	}

	for _, expected := range computed.ResolvedGoals {
		entry, ok := actualByGoal[expected.GoalID]
		if !ok {
			return &ContractError{Code: CodeResolutionMismatch, Path: "resolution.resolved_goals", GoalID: expected.GoalID, Message: "receipt is missing an applicable goal"}
		}
		path := fmt.Sprintf("resolution.resolved_goals[%d]", entry.index)
		actual := entry.goal
		if actual.AuthorConfirmationID != expected.AuthorConfirmationID {
			return &ContractError{Code: CodeConfirmationMismatch, Path: path + ".author_confirmation_id", GoalID: expected.GoalID, Value: actual.AuthorConfirmationID, Message: "resolved goal must reference the operation confirmation"}
		}
		if len(actual.ProvenanceChain) != len(expected.ProvenanceChain) {
			return &ContractError{Code: CodeProvenanceChain, Path: path + ".provenance_chain", GoalID: expected.GoalID, Value: actual.ProvenanceChain, Message: "receipt must preserve the complete ordered provenance chain"}
		}
		for stepIndex := range expected.ProvenanceChain {
			if !resolutionStepsEqual(actual.ProvenanceChain[stepIndex], expected.ProvenanceChain[stepIndex]) {
				return &ContractError{Code: CodeProvenanceChain, Path: fmt.Sprintf("%s.provenance_chain[%d]", path, stepIndex), GoalID: expected.GoalID, Value: actual.ProvenanceChain[stepIndex], Message: "receipt provenance step does not match the considered layer value"}
			}
		}
		if !contractValuesEqual(actual.Value, expected.Value) {
			return &ContractError{Code: CodeResolutionMismatch, Path: path + ".value", GoalID: expected.GoalID, Value: actual.Value, Message: "resolved value does not match deterministic layering"}
		}
		if actual.WinningLayer != expected.WinningLayer {
			return &ContractError{Code: CodeResolutionMismatch, Path: path + ".winning_layer", GoalID: expected.GoalID, Layer: actual.WinningLayer, Value: actual.WinningLayer, Message: "winning layer does not match deterministic layering"}
		}
	}
	return nil
}

func resolutionStepsEqual(left, right ResolutionStep) bool {
	return left.Layer == right.Layer && contractValuesEqual(left.Value, right.Value) && reflect.DeepEqual(left.Provenance, right.Provenance)
}
