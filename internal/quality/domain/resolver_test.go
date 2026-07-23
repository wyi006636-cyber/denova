package domain_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"denova/internal/quality/domain"
	"denova/internal/quality/profile"
)

func TestResolveQualitySpecMatchesEveryCommittedReceipt(t *testing.T) {
	for _, example := range []string{"long_serial.json", "fanqie_short.json", "zhihu_salt_short.json"} {
		t.Run(example, func(t *testing.T) {
			spec := loadExampleQualitySpec(t, example)
			got, err := domain.ResolveQualitySpec(spec)
			if err != nil {
				t.Fatalf("ResolveQualitySpec: %v", err)
			}
			if !reflect.DeepEqual(got, spec.Resolution) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(spec.Resolution, "", "  ")
				t.Fatalf("complete resolution mismatch:\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestResolveQualitySpecRejectsCrossRecordAndLayerViolations(t *testing.T) {
	tests := []struct {
		name    string
		example string
		mutate  func(*domain.QualitySpec)
		code    domain.ErrorCode
		path    string
		goalID  string
		layer   domain.Layer
	}{
		{
			name:    "unknown goal reference",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.TaskOverrides[0].GoalID = "qg.unknown.goal"
			},
			code:   domain.CodeUnknownGoal,
			path:   "layers.task_overrides[0].goal_id",
			goalID: "qg.unknown.goal",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "duplicate goal id",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.GoalCatalog = append(spec.GoalCatalog, spec.GoalCatalog[0])
			},
			code:   domain.CodeDuplicateGoal,
			path:   "goal_catalog[2].id",
			goalID: "qg.serial.continuity",
		},
		{
			name:    "missing evidence",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.GoalCatalog[0].EvidenceRequirement.MinimumCount = 0
			},
			code:   domain.CodeMissingEvidence,
			path:   "goal_catalog[0].evidence_requirement",
			goalID: "qg.serial.continuity",
		},
		{
			name:    "missing provenance",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.ProfileDefaults[0].Provenance.SourceID = ""
			},
			code:   domain.CodeMissingProvenance,
			path:   "layers.profile_defaults[0].provenance",
			goalID: "qg.serial.continuity",
			layer:  domain.LayerProfileDefaults,
		},
		{
			name:    "invalid value type",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.GoalCatalog[1].ValueContract.Type = domain.ValueTypeString
				spec.Layers.TaskOverrides[0].Value = true
			},
			code:   domain.CodeInvalidValueType,
			path:   "layers.task_overrides[0].value",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "unsupported value",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.ProjectOverrides[0].Value = "relaxed"
			},
			code:   domain.CodeUnsupportedValue,
			path:   "layers.project_overrides[0].value",
			goalID: "qg.serial.continuity",
			layer:  domain.LayerProjectOverrides,
		},
		{
			name:    "override exceeds goal scope",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.GoalCatalog[1].AllowedOverrideScopes = []domain.OverrideScope{domain.ScopeProject}
			},
			code:   domain.CodeOverrideScope,
			path:   "layers.task_overrides[0].scope",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "conflicting writes in one layer",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				conflict := spec.Layers.TaskOverrides[0]
				conflict.Value = "medium"
				spec.Layers.TaskOverrides = append(spec.Layers.TaskOverrides, conflict)
			},
			code:   domain.CodeLayerConflict,
			path:   "layers.task_overrides[1].value",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "wrong merge order",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Resolution.MergeOrder[1], spec.Resolution.MergeOrder[2] = spec.Resolution.MergeOrder[2], spec.Resolution.MergeOrder[1]
			},
			code: domain.CodeLayerOrder,
			path: "resolution.merge_order",
		},
		{
			name:    "forbidden delete operation is located precisely",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.TaskOverrides[0].Operation = "delete"
			},
			code:   domain.CodeUnsupportedOperation,
			path:   "layers.task_overrides[0].operation",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "missing confirmed operation id is located precisely",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.OperationConfirmation.OperationID = ""
			},
			code:  domain.CodeOperationScope,
			path:  "layers.operation_confirmation.operation_id",
			layer: domain.LayerOperationConfirmation,
		},
		{
			name:    "missing operation confirmation",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.OperationConfirmation.Authorization.ConfirmationID = ""
			},
			code:  domain.CodeConfirmationRequired,
			path:  "layers.operation_confirmation.authorization.confirmation_id",
			layer: domain.LayerOperationConfirmation,
		},
		{
			name:    "override is not explicitly author authorized",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.TaskOverrides[0].Authorization.Actor = "model"
			},
			code:   domain.CodeAuthorAuthorization,
			path:   "layers.task_overrides[0].authorization.actor",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "operation override confirmation does not match",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				override := spec.Layers.TaskOverrides[0]
				override.Scope = domain.ScopeOperationConfirmation
				override.Authorization.ConfirmationID = "confirm-op-other"
				spec.Layers.OperationConfirmation.Overrides = []domain.Override{override}
			},
			code:   domain.CodeConfirmationMismatch,
			path:   "layers.operation_confirmation.overrides[0].authorization.confirmation_id",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerOperationConfirmation,
		},
		{
			name:    "model proposal provenance enters formal goal catalog",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.GoalCatalog[0].Source.SourceKind = "model_proposal"
			},
			code:   domain.CodeCandidateApplied,
			path:   "goal_catalog[0].source.source_kind",
			goalID: "qg.short.opening_clarity",
		},
		{
			name:    "model proposal provenance enters Profile default",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.ProfileDefaults[0].Provenance.SourceKind = "model_proposal"
			},
			code:   domain.CodeCandidateApplied,
			path:   "layers.profile_defaults[0].provenance.source_kind",
			goalID: "qg.short.opening_clarity",
			layer:  domain.LayerProfileDefaults,
		},
		{
			name:    "model proposal provenance enters formal override",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.Layers.TaskOverrides[0].Provenance.SourceKind = "model_proposal"
			},
			code:   domain.CodeCandidateApplied,
			path:   "layers.task_overrides[0].provenance.source_kind",
			goalID: "qg.short.hook_intensity",
			layer:  domain.LayerTaskOverrides,
		},
		{
			name:    "Profile scope mismatch",
			example: "long_serial.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.ProfileID = domain.ProfileFanqieShort
			},
			code:   domain.CodeProfileMismatch,
			path:   "goal_catalog[0].scope.profile_ids",
			goalID: "qg.serial.continuity",
		},
		{
			name:    "model candidate marked applied",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.CandidateChanges[0].Applied = true
			},
			code: domain.CodeCandidateApplied,
			path: "candidate_changes[0].applied",
		},
		{
			name:    "candidate status becomes authoritative",
			example: "fanqie_short.json",
			mutate: func(spec *domain.QualitySpec) {
				spec.CandidateChanges[0].Status = "applied"
			},
			code: domain.CodeCandidateApplied,
			path: "candidate_changes[0].status",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := loadExampleQualitySpec(t, test.example)
			test.mutate(&spec)
			_, err := domain.ResolveQualitySpec(spec)
			contractErr := assertContractErrorLocation(t, err, test.code, test.path, test.goalID, test.layer)
			if contractErr.Value == nil && (test.code == domain.CodeInvalidValueType || test.code == domain.CodeUnsupportedValue || test.code == domain.CodeLayerConflict) {
				t.Fatalf("error %q did not retain the offending value: %#v", test.code, contractErr)
			}
			if test.name == "forbidden delete operation is located precisely" && contractErr.Value != "delete" {
				t.Fatalf("delete error lost offending operation: %#v", contractErr)
			}
			if test.name == "missing confirmed operation id is located precisely" && contractErr.Value != "" {
				t.Fatalf("missing operation error lost offending value: %#v", contractErr)
			}
			if test.name == "override is not explicitly author authorized" && contractErr.Value != "model" {
				t.Fatalf("author error lost offending actor: %#v", contractErr)
			}
		})
	}
}

func TestValidateQualitySpecRejectsReceiptAndConfirmationMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.QualitySpec)
		code   domain.ErrorCode
		path   string
	}{
		{
			name: "mismatched resolved confirmation",
			mutate: func(spec *domain.QualitySpec) {
				spec.Resolution.ResolvedGoals[0].AuthorConfirmationID = "confirm-op-other"
			},
			code: domain.CodeConfirmationMismatch,
			path: "resolution.resolved_goals[0].author_confirmation_id",
		},
		{
			name: "missing provenance chain",
			mutate: func(spec *domain.QualitySpec) {
				spec.Resolution.ResolvedGoals[0].ProvenanceChain = nil
			},
			code: domain.CodeProvenanceChain,
			path: "resolution.resolved_goals[0].provenance_chain",
		},
		{
			name: "mismatched resolved value",
			mutate: func(spec *domain.QualitySpec) {
				spec.Resolution.ResolvedGoals[0].Value = "normal"
			},
			code: domain.CodeResolutionMismatch,
			path: "resolution.resolved_goals[0].value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := loadExampleQualitySpec(t, "long_serial.json")
			test.mutate(&spec)
			err := domain.ValidateQualitySpec(spec)
			assertContractErrorLocation(t, err, test.code, test.path, "qg.serial.continuity", "")
		})
	}
}

func loadExampleQualitySpec(t *testing.T, name string) domain.QualitySpec {
	t.Helper()
	var envelope struct {
		QualitySpec json.RawMessage `json:"quality_spec"`
	}
	if err := json.Unmarshal(exampleBytes(t, name), &envelope); err != nil {
		t.Fatalf("decode example envelope %s: %v", name, err)
	}
	decoder, err := profile.NewDecoder(
		contractBytes(t, "profile-v1.schema.json"),
		contractBytes(t, "quality-spec-v1.schema.json"),
	)
	if err != nil {
		t.Fatalf("New Profile decoder: %v", err)
	}
	record, err := decoder.ParseQualitySpec(envelope.QualitySpec)
	if err != nil {
		t.Fatalf("Parse QualitySpec from %s: %v", name, err)
	}
	spec, err := record.Managed()
	if err != nil {
		t.Fatalf("Managed QualitySpec from %s: %v", name, err)
	}
	return *spec
}

func exampleBytes(t *testing.T, name string) []byte {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "project-design", "implementation", "contracts", "examples", name)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example %s: %v", path, err)
	}
	return payload
}

func assertContractErrorLocation(t *testing.T, err error, code domain.ErrorCode, path, goalID string, layer domain.Layer) *domain.ContractError {
	t.Helper()
	contractErr := assertContractError(t, err, code, path, contractErrorValue(t, err))
	if contractErr.GoalID != goalID || contractErr.Layer != layer {
		t.Fatalf("contract error = %#v, want goal=%q layer=%q", contractErr, goalID, layer)
	}
	return contractErr
}

func contractErrorValue(t *testing.T, err error) any {
	t.Helper()
	if err == nil {
		return nil
	}
	var contractErr *domain.ContractError
	if !errors.As(err, &contractErr) {
		return nil
	}
	return contractErr.Value
}
