package harness

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var opaqueIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{2,127}$`)
var boundedTokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
var canonicalUnsignedPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// ValidateEvent applies the semantic exact-v1 contract independently of JSON shape validation.
func ValidateEvent(event Event) error {
	if event.Contract.Kind != ContractKind {
		return fmt.Errorf("contract.kind must be %q", ContractKind)
	}
	if event.Contract.Version != ContractVersionV1 {
		return fmt.Errorf("contract.version must be %q", ContractVersionV1)
	}
	if err := validateEventType(event.EventType); err != nil {
		return err
	}
	if err := validateOpaqueID("event_id", event.EventID); err != nil {
		return err
	}
	if err := validateOpaqueID("run_id", event.RunID); err != nil {
		return err
	}
	if event.Sequence == 0 {
		return fmt.Errorf("sequence must be positive")
	}
	if !strings.HasSuffix(event.OccurredAt, "Z") {
		return fmt.Errorf("occurred_at must use the UTC Z designator")
	}
	parsed, err := time.Parse(time.RFC3339, event.OccurredAt)
	if err != nil || parsed.Location() != time.UTC {
		return fmt.Errorf("occurred_at must be UTC RFC3339 using Z")
	}
	if err := validateEventScope(event); err != nil {
		return err
	}
	if err := validateSummary(event.EventType, event.Summary); err != nil {
		return err
	}
	return nil
}

func validateSummary(eventType EventType, summary Summary) error {
	if summary.Code != "quality.event."+string(eventType) {
		return fmt.Errorf("summary.code does not match event_type")
	}
	if summary.Arguments == nil {
		return fmt.Errorf("summary.arguments must be an array")
	}
	if len(summary.Arguments) > 8 {
		return fmt.Errorf("summary.arguments exceeds eight entries")
	}
	seen := make(map[SummaryArgumentName]struct{}, len(summary.Arguments))
	for index, argument := range summary.Arguments {
		if _, exists := seen[argument.Name]; exists {
			return fmt.Errorf("summary.arguments[%d].name is duplicated", index)
		}
		seen[argument.Name] = struct{}{}
		if len(argument.Value) > 128 {
			return fmt.Errorf("summary.arguments[%d].value exceeds 128 bytes", index)
		}
		switch argument.Name {
		case SummaryArgumentProfileID:
			switch argument.Value {
			case "long_serial", "fanqie_short", "zhihu_salt_short":
			default:
				return fmt.Errorf("summary.arguments[%d].value is not a Profile v1 ID", index)
			}
		case SummaryArgumentItemCount:
			if !canonicalUnsignedPattern.MatchString(argument.Value) {
				return fmt.Errorf("summary.arguments[%d].value is not canonical unsigned decimal", index)
			}
		case SummaryArgumentStageKind,
			SummaryArgumentArtifactKind,
			SummaryArgumentDecisionKind,
			SummaryArgumentPreferenceKind,
			SummaryArgumentReasonCode,
			SummaryArgumentResultCode:
			if !boundedTokenPattern.MatchString(argument.Value) {
				return fmt.Errorf("summary.arguments[%d].value is not a bounded lowercase token", index)
			}
		default:
			return fmt.Errorf("summary.arguments[%d].name is not in the v1 vocabulary", index)
		}
	}
	canonical, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode canonical summary: %w", err)
	}
	if len(canonical) > 1024 {
		return fmt.Errorf("canonical summary exceeds 1024 bytes")
	}
	return nil
}

func validateEventScope(event Event) error {
	switch event.EventType {
	case EventWorkflowRunCreated,
		EventWorkflowInputInvalidated,
		EventPreferenceConfirmed,
		EventPreferenceRevoked:
		if event.StageID != "" || event.ArtifactID != "" {
			return fmt.Errorf("%s is run-scoped and forbids stage_id and artifact_id", event.EventType)
		}
		return nil
	case EventWorkflowStageStarted,
		EventWorkflowStageCompleted,
		EventWorkflowStageFailed,
		EventWorkflowDecisionRequired:
		if err := validateOpaqueID("stage_id", event.StageID); err != nil {
			return err
		}
		if event.ArtifactID != "" {
			return fmt.Errorf("%s forbids artifact_id", event.EventType)
		}
		return nil
	case EventArtifactCreated,
		EventCandidateCreated,
		EventCandidateCompared,
		EventCandidateSelected,
		EventReviewIssueCreated,
		EventReviewCompleted,
		EventRevisionCompleted,
		EventFinalizationStarted,
		EventFinalizationCompleted,
		EventFinalizationRolledBack:
		if err := validateOpaqueID("stage_id", event.StageID); err != nil {
			return err
		}
		if err := validateOpaqueID("artifact_id", event.ArtifactID); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("event_type %q has no v1 scope rule", event.EventType)
}

func validateEventType(eventType EventType) error {
	switch eventType {
	case EventWorkflowRunCreated,
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
		EventFinalizationRolledBack:
		return nil
	}
	return fmt.Errorf("event_type %q is not in the v1 vocabulary", eventType)
}

func validateOpaqueID(field, value string) error {
	if len(value) < 3 || len(value) > 128 || !opaqueIDPattern.MatchString(value) {
		return fmt.Errorf("%s is not a valid Quality v1 opaque ID", field)
	}
	return nil
}
