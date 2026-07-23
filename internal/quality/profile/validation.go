package profile

import (
	"fmt"

	"denova/internal/quality/domain"
)

// Validate enforces Profile semantics that span the Profile and bound QualitySpec records.
func Validate(item Profile) error {
	if item.Contract.Kind != ProfileContractKind {
		return &domain.ContractError{Code: domain.CodeContractKind, Path: "contract.kind", Value: item.Contract.Kind, Message: "expected denova.quality-profile"}
	}
	if item.Contract.Version != domain.ContractVersionV1 {
		return &domain.ContractError{Code: domain.CodeUnsupportedVersion, Path: "contract.version", Value: item.Contract.Version, Message: "only exact Profile v1 supports managed operations"}
	}
	if _, err := domain.ParseProfileID(string(item.ProfileID)); err != nil {
		return err
	}
	if item.EngineContract.EngineID != SharedEngineID || item.EngineContract.ContractVersion != domain.ContractVersionV1 || item.EngineContract.ImplementationBranching != ProfileDataOnly {
		return &domain.ContractError{Code: domain.CodeEngineMismatch, Path: "engine_contract", Value: item.EngineContract, Message: "all Profile v1 records must use the shared engine with data-only differences"}
	}
	if item.IdentityPolicy.UnknownProfileID != domain.RejectExplicitly || item.IdentityPolicy.SilentFallback || item.IdentityPolicy.ModelMutation != domain.CandidateOnly {
		return &domain.ContractError{Code: domain.CodeProfileMismatch, Path: "identity_policy", Value: item.IdentityPolicy, Message: "Profile identity must reject unknown IDs, forbid fallback, and keep model changes candidate-only"}
	}
	if err := requireProvenance(item.ProfileProvenance, "profile_provenance"); err != nil {
		return err
	}
	if item.QualitySpec.ProfileID != item.ProfileID {
		return &domain.ContractError{Code: domain.CodeProfileMismatch, Path: "quality_spec.profile_id", Value: item.QualitySpec.ProfileID, Message: "bound QualitySpec Profile must match the outer Profile"}
	}
	if err := domain.ValidateQualitySpec(item.QualitySpec); err != nil {
		return err
	}
	if err := validateSettings(item.Settings); err != nil {
		return err
	}
	return validateWalkthrough(item)
}

func validateSettings(settings Settings) error {
	groups := []struct {
		name  string
		items []Setting
	}{
		{"required_artifacts", settings.RequiredArtifacts},
		{"required_capabilities", settings.RequiredCapabilities},
		{"candidate_policy", settings.CandidatePolicy},
		{"review_rubric", settings.ReviewRubric},
		{"export_config", settings.ExportConfig},
	}
	for _, group := range groups {
		if len(group.items) == 0 {
			return &domain.ContractError{Code: domain.CodeSchemaViolation, Path: "settings." + group.name, Message: "Profile setting group must not be empty"}
		}
		for index, setting := range group.items {
			path := fmt.Sprintf("settings.%s[%d]", group.name, index)
			if err := requireProvenance(setting.Provenance, path+".provenance"); err != nil {
				return err
			}
			if !setting.AuthorOverridePolicy.RequiresExplicitConfirmation || setting.AuthorOverridePolicy.UnsupportedValuePolicy != domain.RejectExplicitly {
				return &domain.ContractError{Code: domain.CodeOverrideScope, Path: path + ".author_override_policy", Value: setting.AuthorOverridePolicy, Message: "mutable Profile settings require explicit confirmation and explicit value rejection"}
			}
		}
	}
	return nil
}

func validateWalkthrough(item Profile) error {
	if item.Walkthrough.OperationID == "" || item.Walkthrough.ArtifactRef == "" || len(item.Walkthrough.EvaluationFocus) == 0 {
		return &domain.ContractError{Code: domain.CodeWalkthroughMismatch, Path: "walkthrough", Value: item.Walkthrough, Message: "Profile walkthrough must bind an operation, Artifact, and focus goals"}
	}
	if item.Walkthrough.OperationID != item.QualitySpec.Layers.OperationConfirmation.OperationID {
		return &domain.ContractError{Code: domain.CodeWalkthroughMismatch, Path: "walkthrough.operation_id", Value: item.Walkthrough.OperationID, Message: "walkthrough operation must match QualitySpec confirmation"}
	}
	known := make(map[string]struct{}, len(item.QualitySpec.GoalCatalog))
	for _, goal := range item.QualitySpec.GoalCatalog {
		known[goal.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(item.Walkthrough.EvaluationFocus))
	for index, goalID := range item.Walkthrough.EvaluationFocus {
		if _, ok := known[goalID]; !ok {
			return &domain.ContractError{Code: domain.CodeUnknownGoal, Path: fmt.Sprintf("walkthrough.evaluation_focus[%d]", index), GoalID: goalID, Value: goalID, Message: "walkthrough references an unknown goal"}
		}
		if _, duplicate := seen[goalID]; duplicate {
			return &domain.ContractError{Code: domain.CodeDuplicateGoal, Path: fmt.Sprintf("walkthrough.evaluation_focus[%d]", index), GoalID: goalID, Value: goalID, Message: "walkthrough focus goals must be unique"}
		}
		seen[goalID] = struct{}{}
	}
	return nil
}

func requireProvenance(source domain.Provenance, path string) error {
	if source.SourceID == "" || source.SourceKind == "" || source.SourceRef == "" || source.ObservedAt == "" || source.EffectiveFrom == "" || source.RecordedAt == "" {
		return &domain.ContractError{Code: domain.CodeMissingProvenance, Path: path, Value: source, Message: "complete provenance is required"}
	}
	return nil
}
