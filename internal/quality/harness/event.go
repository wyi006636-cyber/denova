package harness

const (
	ContractKind      = "denova.quality-event"
	ContractVersionV1 = "v1"
)

// EventType is one member of the closed Quality event v1 vocabulary.
type EventType string

const (
	EventWorkflowRunCreated       EventType = "workflow.run.created"
	EventWorkflowStageStarted     EventType = "workflow.stage.started"
	EventWorkflowStageCompleted   EventType = "workflow.stage.completed"
	EventWorkflowStageFailed      EventType = "workflow.stage.failed"
	EventWorkflowInputInvalidated EventType = "workflow.input.invalidated"
	EventWorkflowDecisionRequired EventType = "workflow.decision.required"
	EventArtifactCreated          EventType = "artifact.created"
	EventCandidateCreated         EventType = "candidate.created"
	EventCandidateCompared        EventType = "candidate.compared"
	EventCandidateSelected        EventType = "candidate.selected"
	EventReviewIssueCreated       EventType = "review.issue.created"
	EventReviewCompleted          EventType = "review.completed"
	EventRevisionCompleted        EventType = "revision.completed"
	EventPreferenceConfirmed      EventType = "preference.confirmed"
	EventPreferenceRevoked        EventType = "preference.revoked"
	EventFinalizationStarted      EventType = "finalization.started"
	EventFinalizationCompleted    EventType = "finalization.completed"
	EventFinalizationRolledBack   EventType = "finalization.rolled_back"
)

var eventTypes = [...]EventType{
	EventWorkflowRunCreated,
	EventWorkflowStageStarted,
	EventWorkflowStageCompleted,
	EventWorkflowStageFailed,
	EventWorkflowInputInvalidated,
	EventWorkflowDecisionRequired,
	EventArtifactCreated,
	EventCandidateCreated,
	EventCandidateCompared,
	EventCandidateSelected,
	EventReviewIssueCreated,
	EventReviewCompleted,
	EventRevisionCompleted,
	EventPreferenceConfirmed,
	EventPreferenceRevoked,
	EventFinalizationStarted,
	EventFinalizationCompleted,
	EventFinalizationRolledBack,
}

func AllEventTypes() []EventType {
	result := make([]EventType, len(eventTypes))
	copy(result, eventTypes[:])
	return result
}

type Contract struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

type SummaryArgumentName string

const (
	SummaryArgumentProfileID      SummaryArgumentName = "profile_id"
	SummaryArgumentStageKind      SummaryArgumentName = "stage_kind"
	SummaryArgumentArtifactKind   SummaryArgumentName = "artifact_kind"
	SummaryArgumentDecisionKind   SummaryArgumentName = "decision_kind"
	SummaryArgumentPreferenceKind SummaryArgumentName = "preference_kind"
	SummaryArgumentReasonCode     SummaryArgumentName = "reason_code"
	SummaryArgumentResultCode     SummaryArgumentName = "result_code"
	SummaryArgumentItemCount      SummaryArgumentName = "item_count"
)

type SummaryArgument struct {
	Name  SummaryArgumentName `json:"name"`
	Value string              `json:"value"`
}

type Summary struct {
	Code      string            `json:"code"`
	Arguments []SummaryArgument `json:"arguments"`
}

// Event is the transport-neutral exact-v1 envelope. OccurredAt stays textual so
// replay can preserve the producer's original RFC3339 representation byte-for-byte.
type Event struct {
	Contract   Contract  `json:"contract"`
	EventType  EventType `json:"event_type"`
	EventID    string    `json:"event_id"`
	RunID      string    `json:"run_id"`
	OccurredAt string    `json:"occurred_at"`
	Sequence   uint64    `json:"sequence"`
	Summary    Summary   `json:"summary"`
	StageID    string    `json:"stage_id,omitempty"`
	ArtifactID string    `json:"artifact_id,omitempty"`
}
