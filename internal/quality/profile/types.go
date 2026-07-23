package profile

import "denova/internal/quality/domain"

const (
	ProfileContractKind = "denova.quality-profile"
	SharedEngineID      = "denova.shared-quality-engine"
	ProfileDataOnly     = "profile_data_only"
)

type EngineContract struct {
	EngineID                string `json:"engine_id"`
	ContractVersion         string `json:"contract_version"`
	ImplementationBranching string `json:"implementation_branching"`
}

type IdentityPolicy struct {
	UnknownProfileID string `json:"unknown_profile_id"`
	SilentFallback   bool   `json:"silent_fallback"`
	ModelMutation    string `json:"model_mutation"`
}

type AuthorOverridePolicy struct {
	Allowed                      bool                   `json:"allowed"`
	AllowedScopes                []domain.OverrideScope `json:"allowed_scopes"`
	RequiresExplicitConfirmation bool                   `json:"requires_explicit_confirmation"`
	UnsupportedValuePolicy       string                 `json:"unsupported_value_policy"`
}

type Setting struct {
	ID                   string               `json:"id"`
	Value                any                  `json:"value"`
	Provenance           domain.Provenance    `json:"provenance"`
	AuthorOverridePolicy AuthorOverridePolicy `json:"author_override_policy"`
}

type Settings struct {
	RequiredArtifacts    []Setting `json:"required_artifacts"`
	RequiredCapabilities []Setting `json:"required_capabilities"`
	CandidatePolicy      []Setting `json:"candidate_policy"`
	ReviewRubric         []Setting `json:"review_rubric"`
	ExportConfig         []Setting `json:"export_config"`
}

type Walkthrough struct {
	OperationID     string               `json:"operation_id"`
	ArtifactRef     string               `json:"artifact_ref"`
	Description     domain.LocalizedText `json:"description"`
	EvaluationFocus []string             `json:"evaluation_focus"`
}

// Profile is one exact v1 data contract consumed by the shared quality engine.
type Profile struct {
	Contract          domain.Contract      `json:"contract"`
	ProfileID         domain.ProfileID     `json:"profile_id"`
	DisplayName       domain.LocalizedText `json:"display_name"`
	EngineContract    EngineContract       `json:"engine_contract"`
	ProfileProvenance domain.Provenance    `json:"profile_provenance"`
	IdentityPolicy    IdentityPolicy       `json:"identity_policy"`
	Settings          Settings             `json:"settings"`
	Walkthrough       Walkthrough          `json:"walkthrough"`
	QualitySpec       domain.QualitySpec   `json:"quality_spec"`
}
