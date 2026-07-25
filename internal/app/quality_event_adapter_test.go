package app

import (
	"errors"
	"reflect"
	"testing"

	"denova/internal/quality/harness"
)

func TestAdaptQualityEventPreservesEveryV1Envelope(t *testing.T) {
	for index, eventType := range harness.AllEventTypes() {
		eventType := eventType
		t.Run(string(eventType), func(t *testing.T) {
			input := validQualityEvent(eventType, uint64(index+1))

			adapted, err := AdaptQualityEvent(input)
			if err != nil {
				t.Fatalf("AdaptQualityEvent() error = %v, cause = %v", err, errors.Unwrap(err))
			}
			if adapted.Type != string(eventType) {
				t.Fatalf("adapted.Type = %q, want %q", adapted.Type, eventType)
			}
			got, ok := adapted.Data.(harness.Event)
			if !ok {
				t.Fatalf("adapted.Data type = %T, want harness.Event", adapted.Data)
			}
			if !reflect.DeepEqual(got, input) {
				t.Fatalf("adapted envelope mismatch\nwant: %#v\n got: %#v", input, got)
			}
		})
	}
}

func TestAdaptQualityEventDefensivelyCopiesEnvelope(t *testing.T) {
	input := validQualityEvent(harness.EventWorkflowRunCreated, 1)
	adapted, err := AdaptQualityEvent(input)
	if err != nil {
		t.Fatalf("AdaptQualityEvent() error = %v, cause = %v", err, errors.Unwrap(err))
	}

	input.Summary.Arguments[0].Value = "mutated"
	got := adapted.Data.(harness.Event)
	if got.Summary.Arguments[0].Value != "profile_id" {
		t.Fatalf("adapted summary aliased caller slice: %#v", got.Summary.Arguments)
	}
}

func TestAdaptQualityEventRejectsInvalidEnvelopeWithStableCode(t *testing.T) {
	tests := map[string]func(*harness.Event){
		"unknown version": func(event *harness.Event) { event.Contract.Version = "v2" },
		"unknown type": func(event *harness.Event) {
			event.EventType = harness.EventType("workflow.future")
			event.Summary.Code = "quality.event.workflow.future"
		},
		"invalid field combination": func(event *harness.Event) { event.StageID = "stage-001" },
		"invalid id":                func(event *harness.Event) { event.EventID = "bad\nid" },
		"invalid summary": func(event *harness.Event) {
			event.Summary.Arguments[0].Name = harness.SummaryArgumentName("body")
		},
		"invalid sequence": func(event *harness.Event) { event.Sequence = 0 },
	}

	for name, mutate := range tests {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			input := validQualityEvent(harness.EventWorkflowRunCreated, 1)
			mutate(&input)

			_, err := AdaptQualityEvent(input)
			var appErr *QualityEventAppError
			if !errors.As(err, &appErr) {
				t.Fatalf("AdaptQualityEvent() error = %v, want *QualityEventAppError", err)
			}
			if appErr.Code != QualityEventCodeInvalidEnvelope {
				t.Fatalf("error code = %q, want %q", appErr.Code, QualityEventCodeInvalidEnvelope)
			}
			if appErr.Error() != "quality event application error: quality_event_invalid_envelope" {
				t.Fatalf("public error = %q", appErr.Error())
			}
		})
	}
}

func validQualityEvent(eventType harness.EventType, sequence uint64) harness.Event {
	event := harness.Event{
		Contract:   harness.Contract{Kind: harness.ContractKind, Version: harness.ContractVersionV1},
		EventType:  eventType,
		EventID:    "event-001",
		RunID:      "run-001",
		OccurredAt: "2026-07-24T12:34:56Z",
		Sequence:   sequence,
		Summary: harness.Summary{
			Code: "quality.event." + string(eventType),
			Arguments: []harness.SummaryArgument{{
				Name:  harness.SummaryArgumentReasonCode,
				Value: "profile_id",
			}},
		},
	}

	switch eventType {
	case harness.EventWorkflowRunCreated,
		harness.EventWorkflowInputInvalidated,
		harness.EventPreferenceConfirmed,
		harness.EventPreferenceRevoked:
	case harness.EventWorkflowStageStarted,
		harness.EventWorkflowStageCompleted,
		harness.EventWorkflowStageFailed,
		harness.EventWorkflowDecisionRequired:
		event.StageID = "stage-001"
	case harness.EventArtifactCreated,
		harness.EventCandidateCreated,
		harness.EventCandidateCompared,
		harness.EventCandidateSelected,
		harness.EventReviewIssueCreated,
		harness.EventReviewCompleted,
		harness.EventRevisionCompleted,
		harness.EventFinalizationStarted,
		harness.EventFinalizationCompleted,
		harness.EventFinalizationRolledBack:
		event.StageID = "stage-001"
		event.ArtifactID = "artifact-001"
	}
	return event
}
