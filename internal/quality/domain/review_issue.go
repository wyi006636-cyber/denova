package domain

const ReviewIssueContractKind = "denova.review-issue"

type ReviewStatus string

const (
	ReviewOpen             ReviewStatus = "open"
	ReviewRevisionProposed ReviewStatus = "revision_proposed"
	ReviewResolved         ReviewStatus = "resolved"
	ReviewVerifiedClosed   ReviewStatus = "verified_closed"
	ReviewReopened         ReviewStatus = "reopened"
	ReviewDismissed        ReviewStatus = "dismissed"
)

type CapabilityRouting struct {
	CapabilityID        string `json:"capability_id"`
	ContractVersion     string `json:"contract_version"`
	UnknownCapabilityID string `json:"unknown_capability_id"`
}

type ReviewBinding struct {
	Workspace            WorkspaceBinding     `json:"workspace"`
	Run                  RunBinding           `json:"run"`
	Stage                StageBinding         `json:"stage"`
	Artifact             ArtifactBinding      `json:"artifact"`
	CandidateSetID       string               `json:"candidate_set_id"`
	CandidateSetHash     string               `json:"candidate_set_hash"`
	CandidateID          string               `json:"candidate_id"`
	CandidateContentHash string               `json:"candidate_content_hash"`
	SourceManifest       VersionedHashBinding `json:"source_manifest"`
	Profile              ProfileBinding       `json:"profile"`
	QualitySpec          QualitySpecBinding   `json:"quality_spec"`
	ReviewedContentHash  string               `json:"reviewed_content_hash"`
}

type ReviewAttachment struct {
	Kind                    string `json:"kind"`
	TargetID                string `json:"target_id"`
	TargetHash              string `json:"target_hash"`
	FinalizationReceiptID   string `json:"finalization_receipt_id,omitempty"`
	FinalizationReceiptHash string `json:"finalization_receipt_hash,omitempty"`
}

type ReviewLocation struct {
	ArtifactPath   string    `json:"artifact_path"`
	ByteRange      ByteRange `json:"byte_range"`
	AnchorHash     string    `json:"anchor_hash"`
	QuotedTextHash string    `json:"quoted_text_hash"`
}

type EvidenceExcerpt struct {
	Quote    string    `json:"quote"`
	Location ByteRange `json:"location"`
	Hash     string    `json:"hash"`
}

type ReaderEvidence struct {
	ObservableEffect string            `json:"observable_effect"`
	Summary          string            `json:"summary"`
	Excerpts         []EvidenceExcerpt `json:"excerpts"`
}

type ReviewCause struct {
	Category    string `json:"category"`
	Explanation string `json:"explanation"`
}

type ReviewRecommendation struct {
	MinimumImpactChange string    `json:"minimum_impact_change"`
	AffectedRange       ByteRange `json:"affected_range"`
	DimensionsToRecheck []string  `json:"dimensions_to_recheck"`
}

type ReviewerProvenance struct {
	SourceID      string `json:"source_id"`
	SourceKind    string `json:"source_kind"`
	SourceVersion string `json:"source_version"`
	SourceHash    string `json:"source_hash"`
	CreatedAt     string `json:"created_at"`
}

type ReviewerOutputPolicy struct {
	Output                  string `json:"output"`
	WriterChainOfThought    string `json:"writer_chain_of_thought"`
	FormalMutationAuthority bool   `json:"formal_mutation_authority"`
}

type ReviewStatusTransition struct {
	TransitionID        string        `json:"transition_id"`
	From                *ReviewStatus `json:"from"`
	To                  ReviewStatus  `json:"to"`
	Actor               RecordActor   `json:"actor"`
	Reason              string        `json:"reason"`
	At                  string        `json:"at"`
	ReviewedContentHash string        `json:"reviewed_content_hash"`
}

type Reverification struct {
	AttemptID          string             `json:"attempt_id"`
	Result             string             `json:"result"`
	RevisedContentHash string             `json:"revised_content_hash"`
	Evidence           string             `json:"evidence"`
	VerifierProvenance ReviewerProvenance `json:"verifier_provenance"`
	At                 string             `json:"at"`
}

// ReviewIssue is the exact v1 evidence record. It has no formal mutation or
// Author Finalization capability.
type ReviewIssue struct {
	Contract              ArtifactRecordContract   `json:"contract"`
	IssueID               string                   `json:"issue_id"`
	CapabilityRouting     CapabilityRouting        `json:"capability_routing"`
	Binding               ReviewBinding            `json:"binding"`
	Attachment            ReviewAttachment         `json:"attachment"`
	Location              ReviewLocation           `json:"location"`
	ReaderEvidence        ReaderEvidence           `json:"reader_evidence"`
	Cause                 ReviewCause              `json:"cause"`
	Severity              string                   `json:"severity"`
	RevisionLayer         string                   `json:"revision_layer"`
	Recommendation        ReviewRecommendation     `json:"recommendation"`
	ReviewerProvenance    ReviewerProvenance       `json:"reviewer_provenance"`
	ReviewerOutputPolicy  ReviewerOutputPolicy     `json:"reviewer_output_policy"`
	Score                 *float64                 `json:"score,omitempty"`
	Status                ReviewStatus             `json:"status"`
	StatusHistory         []ReviewStatusTransition `json:"status_history"`
	ReverificationHistory []Reverification         `json:"reverification_history"`
}
