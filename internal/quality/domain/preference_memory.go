package domain

const PreferenceSignalContractKind = "denova.preference-signal"

type PreferenceContract struct {
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	SchemaID string `json:"schema_id"`
}

type PreferenceScope struct {
	Kind        string `json:"kind"`
	AuthorID    string `json:"author_id"`
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
}

type PreferenceAuthor struct {
	ActorID   string `json:"actor_id"`
	ActorType string `json:"actor_type"`
}

type PreferenceWorkspace struct {
	WorkspaceID   string `json:"workspace_id"`
	ProjectID     string `json:"project_id"`
	CanonicalPath string `json:"canonical_path"`
	Revision      string `json:"revision"`
	ContentHash   string `json:"content_hash"`
}

type PreferenceProfileBinding struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

type PreferenceQualitySpecBinding struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Version  string `json:"version"`
	Hash     string `json:"hash"`
}

type PreferenceProvenance struct {
	SourceKind  string                       `json:"source_kind"`
	OperationID string                       `json:"operation_id"`
	Profile     PreferenceProfileBinding     `json:"profile"`
	QualitySpec PreferenceQualitySpecBinding `json:"quality_spec"`
	ContentHash string                       `json:"content_hash"`
}

// PreferenceEventReference is a closed tagged union. The normative schema
// decides which fields are legal for each Event.
type PreferenceEventReference struct {
	Kind                    string   `json:"kind"`
	CandidateSetID          string   `json:"candidate_set_id,omitempty"`
	CandidateID             string   `json:"candidate_id,omitempty"`
	CandidateHash           string   `json:"candidate_hash,omitempty"`
	ComposedArtifactID      string   `json:"composed_artifact_id,omitempty"`
	ComposedHash            string   `json:"composed_hash,omitempty"`
	ParentCandidateIDs      []string `json:"parent_candidate_ids,omitempty"`
	SegmentMapHash          string   `json:"segment_map_hash,omitempty"`
	IssueID                 string   `json:"issue_id,omitempty"`
	IssueHash               string   `json:"issue_hash,omitempty"`
	OriginalArtifactID      string   `json:"original_artifact_id,omitempty"`
	OriginalHash            string   `json:"original_hash,omitempty"`
	RewrittenArtifactID     string   `json:"rewritten_artifact_id,omitempty"`
	RewrittenHash           string   `json:"rewritten_hash,omitempty"`
	RuleID                  string   `json:"rule_id,omitempty"`
	RuleHash                string   `json:"rule_hash,omitempty"`
	CorrectedSignalID       string   `json:"corrected_signal_id,omitempty"`
	ReplacementEvidenceHash string   `json:"replacement_evidence_hash,omitempty"`
	RevocationReasonHash    string   `json:"revocation_reason_hash,omitempty"`
}

type PreferenceValue struct {
	Dimension  string  `json:"dimension"`
	Value      string  `json:"value"`
	Reason     string  `json:"reason"`
	Strength   string  `json:"strength"`
	Confidence float64 `json:"confidence"`
}

type PreferenceConfirmation struct {
	Explicit     bool   `json:"explicit"`
	Method       string `json:"method"`
	ConfirmedAt  string `json:"confirmed_at"`
	EvidenceHash string `json:"evidence_hash"`
}

// PreferenceSignal is one immutable journal event. Updates and removals are
// represented only by later correction or revocation records.
type PreferenceSignal struct {
	Contract            PreferenceContract       `json:"contract"`
	SignalID            string                   `json:"signal_id"`
	Event               string                   `json:"event"`
	Scope               PreferenceScope          `json:"scope"`
	Author              PreferenceAuthor         `json:"author"`
	Workspace           PreferenceWorkspace      `json:"workspace"`
	Provenance          PreferenceProvenance     `json:"provenance"`
	EventReference      PreferenceEventReference `json:"event_reference"`
	Preference          PreferenceValue          `json:"preference"`
	Confirmation        PreferenceConfirmation   `json:"confirmation"`
	SupersedesSignalIDs []string                 `json:"supersedes_signal_ids,omitempty"`
	RevokesSignalIDs    []string                 `json:"revokes_signal_ids,omitempty"`
	RecordedAt          string                   `json:"recorded_at"`
}
