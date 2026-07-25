package domain

import "fmt"

// ErrorCode identifies one stable Quality Profile or QualitySpec contract failure.
type ErrorCode string

const (
	CodeMalformedRecord      ErrorCode = "malformed_record"
	CodeContractKind         ErrorCode = "contract_kind"
	CodeUnsupportedVersion   ErrorCode = "unsupported_version"
	CodeSchemaViolation      ErrorCode = "schema_violation"
	CodeUnknownProfile       ErrorCode = "unknown_profile"
	CodeUnknownGoal          ErrorCode = "unknown_goal"
	CodeDuplicateGoal        ErrorCode = "duplicate_goal"
	CodeMissingEvidence      ErrorCode = "missing_evidence"
	CodeMissingProvenance    ErrorCode = "missing_provenance"
	CodeInvalidValueType     ErrorCode = "invalid_value_type"
	CodeUnsupportedValue     ErrorCode = "unsupported_value"
	CodeOverrideScope        ErrorCode = "override_scope"
	CodeLayerConflict        ErrorCode = "layer_conflict"
	CodeLayerOrder           ErrorCode = "layer_order"
	CodeProvenanceChain      ErrorCode = "provenance_chain"
	CodeProfileMismatch      ErrorCode = "profile_mismatch"
	CodeOperationScope       ErrorCode = "operation_scope"
	CodeUnsupportedOperation ErrorCode = "unsupported_operation"
	CodeAuthorAuthorization  ErrorCode = "author_authorization"
	CodeConfirmationRequired ErrorCode = "confirmation_required"
	CodeConfirmationMismatch ErrorCode = "confirmation_mismatch"
	CodeCandidateApplied     ErrorCode = "candidate_applied"
	CodeResolutionMismatch   ErrorCode = "resolution_mismatch"
	CodeMissingDefault       ErrorCode = "missing_profile_default"
	CodeRegistryIncomplete   ErrorCode = "registry_incomplete"
	CodeDuplicateProfile     ErrorCode = "duplicate_profile"
	CodeEngineMismatch       ErrorCode = "engine_mismatch"
	CodeWalkthroughMismatch  ErrorCode = "walkthrough_mismatch"
	CodeStateVocabulary      ErrorCode = "state_vocabulary"
	CodeStateTransition      ErrorCode = "state_transition"
	CodeHistoryContinuity    ErrorCode = "history_continuity"
	CodeDuplicateIdentity    ErrorCode = "duplicate_identity"
	CodeBindingMismatch      ErrorCode = "binding_mismatch"
	CodeHashMismatch         ErrorCode = "hash_mismatch"
	CodeRangeInvalid         ErrorCode = "range_invalid"
	CodeLineageInvalid       ErrorCode = "lineage_invalid"
	CodeEvidenceInvalid      ErrorCode = "evidence_invalid"
	CodeAuthorityViolation   ErrorCode = "authority_violation"
	CodeReferenceInvalid     ErrorCode = "reference_invalid"
	CodeJournalInvalid       ErrorCode = "journal_invalid"
	CodeResolutionCycle      ErrorCode = "resolution_cycle"
	CodeScopeExpansion       ErrorCode = "scope_expansion"
)

// ContractError locates a contract failure without reducing it to a warning.
type ContractError struct {
	Code    ErrorCode
	Path    string
	GoalID  string
	Layer   Layer
	Value   any
	Message string
	Err     error
}

func (err *ContractError) Error() string {
	if err == nil {
		return "<nil>"
	}
	location := err.Path
	if err.GoalID != "" {
		location += fmt.Sprintf(" goal=%q", err.GoalID)
	}
	if err.Layer != "" {
		location += fmt.Sprintf(" layer=%q", err.Layer)
	}
	message := err.Message
	if message == "" {
		message = string(err.Code)
	}
	if err.Value != nil {
		return fmt.Sprintf("quality contract %s at %s value=%v: %s", err.Code, location, err.Value, message)
	}
	return fmt.Sprintf("quality contract %s at %s: %s", err.Code, location, message)
}

func (err *ContractError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}
