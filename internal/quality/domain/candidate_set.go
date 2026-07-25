package domain

const CandidateSetContractKind = "denova.candidate-set"

type CandidateState string

const (
	CandidateOpen           CandidateState = "open"
	CandidateCompared       CandidateState = "compared"
	CandidateAuthorSelected CandidateState = "author_selected"
	CandidateMixed          CandidateState = "mixed"
	CandidateRejected       CandidateState = "rejected"
	CandidateFinalized      CandidateState = "finalized"
	CandidateArchived       CandidateState = "archived"
)

type CandidatePolicy struct {
	PolicyID       string `json:"policy_id"`
	Version        string `json:"version"`
	RequestedCount int    `json:"requested_count"`
	KeyNode        bool   `json:"key_node"`
	Rationale      string `json:"rationale"`
}

type CandidateModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
	Version  string `json:"version"`
}

type CandidateSkill struct {
	SkillID      string `json:"skill_id"`
	Version      string `json:"version"`
	Hash         string `json:"hash"`
	CapabilityID string `json:"capability_id"`
}

type Candidate struct {
	CandidateID    string               `json:"candidate_id"`
	Artifact       ArtifactBinding      `json:"artifact"`
	ContentHash    string               `json:"content_hash"`
	SourceManifest VersionedHashBinding `json:"source_manifest"`
	Model          CandidateModel       `json:"model"`
	Skill          CandidateSkill       `json:"skill"`
	Profile        ProfileBinding       `json:"profile"`
	QualitySpec    QualitySpecBinding   `json:"quality_spec"`
	CreatedAt      string               `json:"created_at"`
}

type CandidateTransition struct {
	TransitionID string         `json:"transition_id"`
	From         CandidateState `json:"from"`
	To           CandidateState `json:"to"`
	Actor        RecordActor    `json:"actor"`
	Reason       string         `json:"reason"`
	At           string         `json:"at"`
}

type CriterionEvidence struct {
	CriterionID              string   `json:"criterion_id"`
	ReaderObservableEvidence string   `json:"reader_observable_evidence"`
	SourceRef                string   `json:"source_ref"`
	Score                    *float64 `json:"score,omitempty"`
}

type CandidateEvaluation struct {
	CandidateID string              `json:"candidate_id"`
	Criteria    []CriterionEvidence `json:"criteria"`
}

type CandidateSetEvaluation struct {
	EvaluationID         string                `json:"evaluation_id"`
	Actor                RecordActor           `json:"actor"`
	At                   string                `json:"at"`
	CandidateEvaluations []CandidateEvaluation `json:"candidate_evaluations"`
	Summary              string                `json:"summary"`
}

type CandidateDecision struct {
	DecisionID           string      `json:"decision_id"`
	Kind                 string      `json:"kind"`
	Actor                RecordActor `json:"actor"`
	At                   string      `json:"at"`
	Reason               string      `json:"reason"`
	SelectedCandidateIDs []string    `json:"selected_candidate_ids"`
}

type MixedSegment struct {
	SegmentID          string    `json:"segment_id"`
	ParentCandidateID  string    `json:"parent_candidate_id"`
	ParentContentHash  string    `json:"parent_content_hash"`
	ParentRange        ByteRange `json:"parent_range"`
	OutputRange        ByteRange `json:"output_range"`
	SegmentContentHash string    `json:"segment_content_hash"`
}

type MixedOutput struct {
	ArtifactID  string         `json:"artifact_id"`
	ContentHash string         `json:"content_hash"`
	Segments    []MixedSegment `json:"segments"`
}

type BindingCheck struct {
	BindingKind  string `json:"binding_kind"`
	ExpectedHash string `json:"expected_hash"`
	ObservedHash string `json:"observed_hash"`
	Status       string `json:"status"`
	CheckedAt    string `json:"checked_at"`
	Reason       string `json:"reason,omitempty"`
}

type FinalizationHandoff struct {
	Status      string  `json:"status"`
	ContentHash *string `json:"content_hash"`
	RequestID   string  `json:"request_id,omitempty"`
	ReceiptID   string  `json:"receipt_id,omitempty"`
	ReceiptHash string  `json:"receipt_hash,omitempty"`
}

// CandidateSet is the exact CandidateSet v1 JSON contract. It remains a
// pending-review Artifact and owns no formal workspace write capability.
type CandidateSet struct {
	Contract            ArtifactRecordContract  `json:"contract"`
	CandidateSetID      string                  `json:"candidate_set_id"`
	Workspace           WorkspaceBinding        `json:"workspace"`
	Run                 RunBinding              `json:"run"`
	Stage               StageBinding            `json:"stage"`
	Artifact            ArtifactBinding         `json:"artifact"`
	SourceManifest      VersionedHashBinding    `json:"source_manifest"`
	Profile             ProfileBinding          `json:"profile"`
	QualitySpec         QualitySpecBinding      `json:"quality_spec"`
	CandidatePolicy     CandidatePolicy         `json:"candidate_policy"`
	Candidates          []Candidate             `json:"candidates"`
	CurrentState        CandidateState          `json:"current_state"`
	TransitionHistory   []CandidateTransition   `json:"transition_history"`
	Evaluation          *CandidateSetEvaluation `json:"evaluation"`
	AuthorDecision      *CandidateDecision      `json:"author_decision"`
	MixedOutput         *MixedOutput            `json:"mixed_output"`
	BindingValidation   []BindingCheck          `json:"binding_validation"`
	FinalizationHandoff FinalizationHandoff     `json:"finalization_handoff"`
}
