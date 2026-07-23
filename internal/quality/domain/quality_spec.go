package domain

const (
	QualityGoalContractKind = "denova.quality-goal"
	ResolutionValidatorV1   = "quality-spec-resolution-v1"
	RejectExplicitly        = "reject_explicitly"
	CandidateOnly           = "candidate_only"
)

const (
	LayerProfileDefaults       Layer = "profile_defaults"
	LayerProjectOverrides      Layer = "project_overrides"
	LayerTaskOverrides         Layer = "task_overrides"
	LayerOperationConfirmation Layer = "operation_confirmation"
)

// MergeOrder returns the only QualitySpec v1 resolution order.
func MergeOrder() []Layer {
	return []Layer{
		LayerProfileDefaults,
		LayerProjectOverrides,
		LayerTaskOverrides,
		LayerOperationConfirmation,
	}
}

type OverrideScope string

const (
	ScopeProject               OverrideScope = "project"
	ScopeTask                  OverrideScope = "task"
	ScopeOperationConfirmation OverrideScope = "operation_confirmation"
)

type ValueType string

const (
	ValueTypeBoolean ValueType = "boolean"
	ValueTypeInteger ValueType = "integer"
	ValueTypeNumber  ValueType = "number"
	ValueTypeString  ValueType = "string"
	ValueTypeEnum    ValueType = "enum"
)

type LocalizedText struct {
	ZhCN string `json:"zh-CN"`
	En   string `json:"en"`
}

type Provenance struct {
	SourceID      string `json:"source_id"`
	SourceKind    string `json:"source_kind"`
	SourceRef     string `json:"source_ref"`
	ObservedAt    string `json:"observed_at"`
	EffectiveFrom string `json:"effective_from"`
	RecordedAt    string `json:"recorded_at"`
}

type Authorization struct {
	Actor          string `json:"actor"`
	Decision       string `json:"decision"`
	ConfirmationID string `json:"confirmation_id"`
	ConfirmedAt    string `json:"confirmed_at"`
}

type GoalScope struct {
	ProfileIDs    []ProfileID `json:"profile_ids"`
	OperationIDs  []string    `json:"operation_ids"`
	ArtifactTypes []string    `json:"artifact_types"`
}

type EvidenceRequirement struct {
	Kind            string        `json:"kind"`
	Description     LocalizedText `json:"description"`
	MinimumCount    int           `json:"minimum_count"`
	AcceptedSources []string      `json:"accepted_sources"`
}

type ValueContract struct {
	Type               ValueType `json:"type"`
	AllowedValues      []any     `json:"allowed_values"`
	UnknownValuePolicy string    `json:"unknown_value_policy"`
}

// GoalContract is intentionally narrower than a top-level Contract: the v1
// goal schema permits only kind and version and rejects issued_at.
type GoalContract struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

type QualityGoal struct {
	ID                    string              `json:"id"`
	Contract              GoalContract        `json:"contract"`
	Description           LocalizedText       `json:"description"`
	Source                Provenance          `json:"source"`
	Purpose               LocalizedText       `json:"purpose"`
	Scope                 GoalScope           `json:"scope"`
	Priority              string              `json:"priority"`
	EvidenceRequirement   EvidenceRequirement `json:"evidence_requirement"`
	ValueContract         ValueContract       `json:"value_contract"`
	AllowedOverrideScopes []OverrideScope     `json:"allowed_override_scopes"`
}

type AuthorOverridePolicy struct {
	Allowed                      bool            `json:"allowed"`
	AllowedScopes                []OverrideScope `json:"allowed_scopes"`
	RequiresExplicitConfirmation bool            `json:"requires_explicit_confirmation"`
	ForbiddenOperations          []string        `json:"forbidden_operations"`
	UnsupportedValuePolicy       string          `json:"unsupported_value_policy"`
}

type Binding struct {
	GoalID               string               `json:"goal_id"`
	Value                any                  `json:"value"`
	Provenance           Provenance           `json:"provenance"`
	AuthorOverridePolicy AuthorOverridePolicy `json:"author_override_policy"`
}

type Override struct {
	GoalID        string        `json:"goal_id"`
	Operation     string        `json:"operation"`
	Value         any           `json:"value"`
	Scope         OverrideScope `json:"scope"`
	Provenance    Provenance    `json:"provenance"`
	Authorization Authorization `json:"authorization"`
}

type OperationConfirmation struct {
	OperationID   string        `json:"operation_id"`
	Authorization Authorization `json:"authorization"`
	Overrides     []Override    `json:"overrides"`
}

type Layers struct {
	ProfileDefaults       []Binding             `json:"profile_defaults"`
	ProjectOverrides      []Override            `json:"project_overrides"`
	TaskOverrides         []Override            `json:"task_overrides"`
	OperationConfirmation OperationConfirmation `json:"operation_confirmation"`
}

type CandidateChange struct {
	CandidateID string     `json:"candidate_id"`
	ProposedBy  string     `json:"proposed_by"`
	Status      string     `json:"status"`
	Applied     bool       `json:"applied"`
	Proposal    string     `json:"proposal"`
	Provenance  Provenance `json:"provenance"`
}

type ResolutionStep struct {
	Layer      Layer      `json:"layer"`
	Value      any        `json:"value"`
	Provenance Provenance `json:"provenance"`
}

type ResolvedGoal struct {
	GoalID               string           `json:"goal_id"`
	Value                any              `json:"value"`
	WinningLayer         Layer            `json:"winning_layer"`
	ProvenanceChain      []ResolutionStep `json:"provenance_chain"`
	AuthorConfirmationID string           `json:"author_confirmation_id"`
}

type Resolution struct {
	MergeOrder                      []Layer        `json:"merge_order"`
	UnknownOrUnsupportedValuePolicy string         `json:"unknown_or_unsupported_value_policy"`
	ValidatorContract               string         `json:"validator_contract"`
	ValidatedAt                     string         `json:"validated_at"`
	ResolvedGoals                   []ResolvedGoal `json:"resolved_goals"`
}

// QualitySpec is the exact v1 author-controlled quality contract.
type QualitySpec struct {
	Contract         Contract          `json:"contract"`
	SpecID           string            `json:"spec_id"`
	Revision         int               `json:"revision"`
	ProfileID        ProfileID         `json:"profile_id"`
	GoalCatalog      []QualityGoal     `json:"goal_catalog"`
	Layers           Layers            `json:"layers"`
	CandidateChanges []CandidateChange `json:"candidate_changes"`
	Resolution       Resolution        `json:"resolution"`
}
