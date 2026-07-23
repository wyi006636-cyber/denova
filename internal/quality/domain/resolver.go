package domain

import (
	"fmt"
	"reflect"
)

type goalState struct {
	goal          QualityGoal
	value         any
	winningLayer  Layer
	chain         []ResolutionStep
	defaultPolicy AuthorOverridePolicy
}

// ResolveQualitySpec applies the only v1 merge order and rebuilds its deterministic receipt.
func ResolveQualitySpec(spec QualitySpec) (Resolution, error) {
	if err := validateSpecIdentity(spec); err != nil {
		return Resolution{}, err
	}
	if err := validateResolutionHeader(spec.Resolution); err != nil {
		return Resolution{}, err
	}
	confirmation := spec.Layers.OperationConfirmation
	if confirmation.OperationID == "" {
		return Resolution{}, &ContractError{
			Code:    CodeOperationScope,
			Path:    "layers.operation_confirmation.operation_id",
			Layer:   LayerOperationConfirmation,
			Value:   confirmation.OperationID,
			Message: "a confirmed operation ID is required",
		}
	}
	if confirmation.Authorization.ConfirmationID == "" {
		return Resolution{}, &ContractError{
			Code:    CodeConfirmationRequired,
			Path:    "layers.operation_confirmation.authorization.confirmation_id",
			Layer:   LayerOperationConfirmation,
			Value:   confirmation.Authorization.ConfirmationID,
			Message: "an explicit operation-level author confirmation is required",
		}
	}
	if err := validateAuthorization(confirmation.Authorization, "layers.operation_confirmation.authorization", "", LayerOperationConfirmation); err != nil {
		return Resolution{}, err
	}
	if err := validateCandidates(spec.CandidateChanges); err != nil {
		return Resolution{}, err
	}

	goals, goalOrder, err := validateGoals(spec, confirmation.OperationID)
	if err != nil {
		return Resolution{}, err
	}
	states := make(map[string]*goalState, len(goals))
	if err := applyProfileDefaults(spec.Layers.ProfileDefaults, goals, states); err != nil {
		return Resolution{}, err
	}
	for _, goalID := range goalOrder {
		if states[goalID] == nil {
			return Resolution{}, &ContractError{
				Code:    CodeMissingDefault,
				Path:    "layers.profile_defaults",
				GoalID:  goalID,
				Layer:   LayerProfileDefaults,
				Message: "every applicable goal requires a Profile default",
			}
		}
	}
	if err := applyOverrides(spec.Layers.ProjectOverrides, goals, states, LayerProjectOverrides, ScopeProject, "layers.project_overrides", confirmation.Authorization.ConfirmationID); err != nil {
		return Resolution{}, err
	}
	if err := applyOverrides(spec.Layers.TaskOverrides, goals, states, LayerTaskOverrides, ScopeTask, "layers.task_overrides", confirmation.Authorization.ConfirmationID); err != nil {
		return Resolution{}, err
	}
	if err := applyOverrides(confirmation.Overrides, goals, states, LayerOperationConfirmation, ScopeOperationConfirmation, "layers.operation_confirmation.overrides", confirmation.Authorization.ConfirmationID); err != nil {
		return Resolution{}, err
	}

	resolved := make([]ResolvedGoal, 0, len(goalOrder))
	for _, goalID := range goalOrder {
		state := states[goalID]
		resolved = append(resolved, ResolvedGoal{
			GoalID:               goalID,
			Value:                state.value,
			WinningLayer:         state.winningLayer,
			ProvenanceChain:      append([]ResolutionStep(nil), state.chain...),
			AuthorConfirmationID: confirmation.Authorization.ConfirmationID,
		})
	}
	return Resolution{
		MergeOrder:                      MergeOrder(),
		UnknownOrUnsupportedValuePolicy: RejectExplicitly,
		ValidatorContract:               ResolutionValidatorV1,
		ValidatedAt:                     spec.Resolution.ValidatedAt,
		ResolvedGoals:                   resolved,
	}, nil
}

func validateSpecIdentity(spec QualitySpec) error {
	if spec.Contract.Kind != QualitySpecContractKind {
		return &ContractError{Code: CodeContractKind, Path: "contract.kind", Value: spec.Contract.Kind, Message: "expected denova.quality-spec"}
	}
	if spec.Contract.Version != ContractVersionV1 {
		return &ContractError{Code: CodeUnsupportedVersion, Path: "contract.version", Value: spec.Contract.Version, Message: "only exact QualitySpec v1 supports managed resolution"}
	}
	if _, err := ParseProfileID(string(spec.ProfileID)); err != nil {
		return err
	}
	return nil
}

func validateResolutionHeader(resolution Resolution) error {
	want := MergeOrder()
	if !reflect.DeepEqual(resolution.MergeOrder, want) {
		return &ContractError{Code: CodeLayerOrder, Path: "resolution.merge_order", Value: append([]Layer(nil), resolution.MergeOrder...), Message: "QualitySpec v1 merge order is fixed"}
	}
	if resolution.UnknownOrUnsupportedValuePolicy != RejectExplicitly {
		return &ContractError{Code: CodeResolutionMismatch, Path: "resolution.unknown_or_unsupported_value_policy", Value: resolution.UnknownOrUnsupportedValuePolicy, Message: "unknown values must be rejected explicitly"}
	}
	if resolution.ValidatorContract != ResolutionValidatorV1 {
		return &ContractError{Code: CodeResolutionMismatch, Path: "resolution.validator_contract", Value: resolution.ValidatorContract, Message: "unexpected resolver contract"}
	}
	if resolution.ValidatedAt == "" {
		return &ContractError{Code: CodeResolutionMismatch, Path: "resolution.validated_at", Message: "validated_at is required"}
	}
	return nil
}

func validateGoals(spec QualitySpec, operationID string) (map[string]QualityGoal, []string, error) {
	goals := make(map[string]QualityGoal, len(spec.GoalCatalog))
	order := make([]string, 0, len(spec.GoalCatalog))
	for index, goal := range spec.GoalCatalog {
		path := fmt.Sprintf("goal_catalog[%d]", index)
		if _, exists := goals[goal.ID]; exists {
			return nil, nil, &ContractError{Code: CodeDuplicateGoal, Path: path + ".id", GoalID: goal.ID, Value: goal.ID, Message: "goal IDs must be unique"}
		}
		if goal.Contract.Kind != QualityGoalContractKind || goal.Contract.Version != ContractVersionV1 {
			return nil, nil, &ContractError{Code: CodeContractKind, Path: path + ".contract", GoalID: goal.ID, Value: goal.Contract, Message: "goal must use denova.quality-goal v1"}
		}
		if err := validateFormalProvenance(goal.Source, path+".source", goal.ID, ""); err != nil {
			return nil, nil, err
		}
		if goal.EvidenceRequirement.MinimumCount < 1 || len(goal.EvidenceRequirement.AcceptedSources) == 0 || goal.EvidenceRequirement.Kind == "" {
			return nil, nil, &ContractError{Code: CodeMissingEvidence, Path: path + ".evidence_requirement", GoalID: goal.ID, Value: goal.EvidenceRequirement, Message: "goal evidence must be explicit and non-empty"}
		}
		if err := validateValueContract(goal, path+".value_contract"); err != nil {
			return nil, nil, err
		}
		if !containsProfile(goal.Scope.ProfileIDs, spec.ProfileID) {
			return nil, nil, &ContractError{Code: CodeProfileMismatch, Path: path + ".scope.profile_ids", GoalID: goal.ID, Value: spec.ProfileID, Message: "goal scope does not include the QualitySpec Profile"}
		}
		if !containsString(goal.Scope.OperationIDs, operationID) {
			return nil, nil, &ContractError{Code: CodeOperationScope, Path: path + ".scope.operation_ids", GoalID: goal.ID, Value: operationID, Message: "goal scope does not include the confirmed operation"}
		}
		goals[goal.ID] = goal
		order = append(order, goal.ID)
	}
	return goals, order, nil
}

func applyProfileDefaults(bindings []Binding, goals map[string]QualityGoal, states map[string]*goalState) error {
	for index, binding := range bindings {
		path := fmt.Sprintf("layers.profile_defaults[%d]", index)
		goal, ok := goals[binding.GoalID]
		if !ok {
			return unknownGoalError(path+".goal_id", binding.GoalID, LayerProfileDefaults)
		}
		if err := validateFormalProvenance(binding.Provenance, path+".provenance", goal.ID, LayerProfileDefaults); err != nil {
			return err
		}
		if err := validatePolicy(binding.AuthorOverridePolicy, path+".author_override_policy", goal.ID, LayerProfileDefaults); err != nil {
			return err
		}
		if err := validateGoalValue(goal, binding.Value, path+".value", LayerProfileDefaults); err != nil {
			return err
		}
		if existing := states[goal.ID]; existing != nil {
			if !contractValuesEqual(existing.value, binding.Value) || !reflect.DeepEqual(existing.defaultPolicy, binding.AuthorOverridePolicy) {
				return &ContractError{Code: CodeLayerConflict, Path: path + ".value", GoalID: goal.ID, Layer: LayerProfileDefaults, Value: binding.Value, Message: "conflicting Profile defaults for one goal"}
			}
			existing.chain = append(existing.chain, ResolutionStep{Layer: LayerProfileDefaults, Value: binding.Value, Provenance: binding.Provenance})
			continue
		}
		states[goal.ID] = &goalState{
			goal:          goal,
			value:         binding.Value,
			winningLayer:  LayerProfileDefaults,
			chain:         []ResolutionStep{{Layer: LayerProfileDefaults, Value: binding.Value, Provenance: binding.Provenance}},
			defaultPolicy: binding.AuthorOverridePolicy,
		}
	}
	return nil
}

func applyOverrides(overrides []Override, goals map[string]QualityGoal, states map[string]*goalState, layer Layer, scope OverrideScope, prefix, operationConfirmationID string) error {
	layerValues := make(map[string]any, len(overrides))
	for index, override := range overrides {
		path := fmt.Sprintf("%s[%d]", prefix, index)
		goal, ok := goals[override.GoalID]
		if !ok {
			return unknownGoalError(path+".goal_id", override.GoalID, layer)
		}
		if override.Operation != "set" {
			return &ContractError{Code: CodeUnsupportedOperation, Path: path + ".operation", GoalID: goal.ID, Layer: layer, Value: override.Operation, Message: "QualitySpec v1 overrides support only set"}
		}
		if override.Scope != scope {
			return &ContractError{Code: CodeOverrideScope, Path: path + ".scope", GoalID: goal.ID, Layer: layer, Value: override.Scope, Message: "override scope does not match its layer"}
		}
		state := states[goal.ID]
		if state == nil {
			return &ContractError{Code: CodeMissingDefault, Path: path + ".goal_id", GoalID: goal.ID, Layer: layer, Message: "override has no Profile default"}
		}
		if !scopeAllowed(goal.AllowedOverrideScopes, scope) || !state.defaultPolicy.Allowed || !scopeAllowed(state.defaultPolicy.AllowedScopes, scope) {
			return &ContractError{Code: CodeOverrideScope, Path: path + ".scope", GoalID: goal.ID, Layer: layer, Value: override.Scope, Message: "override exceeds goal or Profile-default authority"}
		}
		if err := validateFormalProvenance(override.Provenance, path+".provenance", goal.ID, layer); err != nil {
			return err
		}
		if err := validateAuthorization(override.Authorization, path+".authorization", goal.ID, layer); err != nil {
			return err
		}
		if scope == ScopeOperationConfirmation && override.Authorization.ConfirmationID != operationConfirmationID {
			return &ContractError{Code: CodeConfirmationMismatch, Path: path + ".authorization.confirmation_id", GoalID: goal.ID, Layer: layer, Value: override.Authorization.ConfirmationID, Message: "operation override confirmation must match the enclosing operation confirmation"}
		}
		if err := validateGoalValue(goal, override.Value, path+".value", layer); err != nil {
			return err
		}
		if previous, exists := layerValues[goal.ID]; exists && !contractValuesEqual(previous, override.Value) {
			return &ContractError{Code: CodeLayerConflict, Path: path + ".value", GoalID: goal.ID, Layer: layer, Value: override.Value, Message: "one layer contains conflicting writes for the same goal"}
		}
		layerValues[goal.ID] = override.Value
		state.value = override.Value
		state.winningLayer = layer
		state.chain = append(state.chain, ResolutionStep{Layer: layer, Value: override.Value, Provenance: override.Provenance})
	}
	return nil
}

func validateCandidates(candidates []CandidateChange) error {
	for index, candidate := range candidates {
		path := fmt.Sprintf("candidate_changes[%d]", index)
		if candidate.Status != CandidateOnly {
			return &ContractError{Code: CodeCandidateApplied, Path: path + ".status", Value: candidate.Status, Message: "model, Skill, and Automation changes must remain candidate_only"}
		}
		if candidate.Applied {
			return &ContractError{Code: CodeCandidateApplied, Path: path + ".applied", Value: candidate.Applied, Message: "model, Skill, and Automation changes must remain candidate_only with applied=false"}
		}
		if err := validateProvenance(candidate.Provenance, path+".provenance", "", ""); err != nil {
			return err
		}
	}
	return nil
}

func validateProvenance(provenance Provenance, path, goalID string, layer Layer) error {
	if provenance.SourceID == "" || provenance.SourceKind == "" || provenance.SourceRef == "" || provenance.ObservedAt == "" || provenance.EffectiveFrom == "" || provenance.RecordedAt == "" {
		return &ContractError{Code: CodeMissingProvenance, Path: path, GoalID: goalID, Layer: layer, Value: provenance, Message: "complete provenance is required"}
	}
	return nil
}

func validateFormalProvenance(provenance Provenance, path, goalID string, layer Layer) error {
	if err := validateProvenance(provenance, path, goalID, layer); err != nil {
		return err
	}
	if provenance.SourceKind == "model_proposal" {
		return &ContractError{
			Code:    CodeCandidateApplied,
			Path:    path + ".source_kind",
			GoalID:  goalID,
			Layer:   layer,
			Value:   provenance.SourceKind,
			Message: "model proposals must remain candidate-only",
		}
	}
	return nil
}

func validateAuthorization(authorization Authorization, path, goalID string, layer Layer) error {
	if authorization.Actor != "author" {
		return &ContractError{Code: CodeAuthorAuthorization, Path: path + ".actor", GoalID: goalID, Layer: layer, Value: authorization.Actor, Message: "authorization actor must be the author"}
	}
	if authorization.Decision != "confirmed" {
		return &ContractError{Code: CodeAuthorAuthorization, Path: path + ".decision", GoalID: goalID, Layer: layer, Value: authorization.Decision, Message: "author decision must be confirmed"}
	}
	if authorization.ConfirmationID == "" {
		return &ContractError{Code: CodeAuthorAuthorization, Path: path + ".confirmation_id", GoalID: goalID, Layer: layer, Value: authorization.ConfirmationID, Message: "author confirmation ID is required"}
	}
	if authorization.ConfirmedAt == "" {
		return &ContractError{Code: CodeAuthorAuthorization, Path: path + ".confirmed_at", GoalID: goalID, Layer: layer, Value: authorization.ConfirmedAt, Message: "author confirmation timestamp is required"}
	}
	return nil
}

func validatePolicy(policy AuthorOverridePolicy, path, goalID string, layer Layer) error {
	if !policy.RequiresExplicitConfirmation || policy.UnsupportedValuePolicy != RejectExplicitly || !reflect.DeepEqual(policy.ForbiddenOperations, []string{"delete"}) {
		return &ContractError{Code: CodeOverrideScope, Path: path, GoalID: goalID, Layer: layer, Value: policy, Message: "author override policy must require confirmation, reject unsupported values, and forbid delete"}
	}
	return nil
}

func unknownGoalError(path, goalID string, layer Layer) error {
	return &ContractError{Code: CodeUnknownGoal, Path: path, GoalID: goalID, Layer: layer, Value: goalID, Message: "layer references an unknown goal"}
}

func containsProfile(values []ProfileID, want ProfileID) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func scopeAllowed(values []OverrideScope, want OverrideScope) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
