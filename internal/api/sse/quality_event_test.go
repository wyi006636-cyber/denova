package sse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"denova/internal/agent"
	novaApp "denova/internal/app"
	"denova/internal/quality/harness"
)

func TestWriteQualityEventFramePreservesEveryV1Event(t *testing.T) {
	for index, eventType := range harness.AllEventTypes() {
		eventType := eventType
		t.Run(string(eventType), func(t *testing.T) {
			envelope := validSSEQualityEvent(eventType, uint64(index+1))
			adapted, err := novaApp.AdaptQualityEvent(envelope)
			if err != nil {
				t.Fatalf("adapt fixture: %v", err)
			}
			var output bytes.Buffer

			if err := WriteQualityEventFrame(&output, adapted); err != nil {
				t.Fatalf("WriteQualityEventFrame() error = %v", err)
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatalf("marshal expected envelope: %v", err)
			}
			want := fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", envelope.EventID, eventType, raw)
			if output.String() != want {
				t.Fatalf("frame mismatch\nwant: %q\n got: %q", want, output.String())
			}

			decoded, err := DecodeQualityEvent(adapted)
			if err != nil {
				t.Fatalf("DecodeQualityEvent() error = %v", err)
			}
			if !reflect.DeepEqual(decoded, envelope) {
				t.Fatalf("decoded envelope mismatch\nwant: %#v\n got: %#v", envelope, decoded)
			}
		})
	}
}

func TestWriteQualityEventFrameRejectsUnsafeOrNonExactDataWithStableCodes(t *testing.T) {
	base := validSSEQualityEvent(harness.EventWorkflowRunCreated, 1)
	tests := []struct {
		name     string
		event    func() agent.Event
		wantCode QualityFrameErrorCode
	}{
		{
			name: "malformed data",
			event: func() agent.Event {
				return agent.Event{Type: string(base.EventType), Data: make(chan int)}
			},
			wantCode: QualityFrameCodeInvalidEvent,
		},
		{
			name: "unknown version",
			event: func() agent.Event {
				data := qualityEventMap(t, base)
				data["contract"].(map[string]any)["version"] = "v2"
				return agent.Event{Type: string(base.EventType), Data: data}
			},
			wantCode: QualityFrameCodeInvalidEvent,
		},
		{
			name: "unknown event type",
			event: func() agent.Event {
				data := qualityEventMap(t, base)
				data["event_type"] = "workflow.future"
				data["summary"].(map[string]any)["code"] = "quality.event.workflow.future"
				return agent.Event{Type: "workflow.future", Data: data}
			},
			wantCode: QualityFrameCodeInvalidEvent,
		},
		{
			name: "extra prompt field",
			event: func() agent.Event {
				data := qualityEventMap(t, base)
				data["prompt"] = "write my manuscript"
				return agent.Event{Type: string(base.EventType), Data: data}
			},
			wantCode: QualityFrameCodeInvalidEvent,
		},
		{
			name: "nested secret summary",
			event: func() agent.Event {
				data := qualityEventMap(t, base)
				data["summary"].(map[string]any)["arguments"] = []any{map[string]any{
					"name": "reason_code", "value": map[string]any{"secret": "token"},
				}}
				return agent.Event{Type: string(base.EventType), Data: data}
			},
			wantCode: QualityFrameCodeInvalidEvent,
		},
		{
			name: "newline event id",
			event: func() agent.Event {
				data := qualityEventMap(t, base)
				data["event_id"] = "event-001\nevent: injected"
				return agent.Event{Type: string(base.EventType), Data: data}
			},
			wantCode: QualityFrameCodeInvalidEvent,
		},
		{
			name: "agent type mismatch",
			event: func() agent.Event {
				return agent.Event{Type: string(harness.EventPreferenceRevoked), Data: base}
			},
			wantCode: QualityFrameCodeTypeMismatch,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := WriteQualityEventFrame(&output, test.event())
			var frameErr *QualityFrameError
			if !errors.As(err, &frameErr) {
				t.Fatalf("error = %v, want *QualityFrameError", err)
			}
			if frameErr.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", frameErr.Code, test.wantCode)
			}
			if output.Len() != 0 {
				t.Fatalf("rejected event wrote partial frame %q", output.String())
			}
			if strings.Contains(frameErr.Error(), "secret") || len(frameErr.Error()) > 96 {
				t.Fatalf("public error is unsafe or unbounded: %q", frameErr.Error())
			}
		})
	}
}

func TestWriteQualityEventFrameEmitsNoBodyOrSecret(t *testing.T) {
	envelope := validSSEQualityEvent(harness.EventFinalizationCompleted, 18)
	adapted, err := novaApp.AdaptQualityEvent(envelope)
	if err != nil {
		t.Fatalf("adapt fixture: %v", err)
	}
	var output bytes.Buffer
	if err := WriteQualityEventFrame(&output, adapted); err != nil {
		t.Fatalf("WriteQualityEventFrame() error = %v", err)
	}
	for _, forbidden := range []string{"prompt", "body", "secret", "candidate", "quote", "/Users/"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("frame contains forbidden %q: %q", forbidden, output.String())
		}
	}
}

func qualityEventMap(t *testing.T, event harness.Event) map[string]any {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event map fixture: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode event map fixture: %v", err)
	}
	return data
}

func validSSEQualityEvent(eventType harness.EventType, sequence uint64) harness.Event {
	event := harness.Event{
		Contract:   harness.Contract{Kind: harness.ContractKind, Version: harness.ContractVersionV1},
		EventType:  eventType,
		EventID:    fmt.Sprintf("event-%03d", sequence),
		RunID:      "run-001",
		OccurredAt: "2026-07-24T12:34:56Z",
		Sequence:   sequence,
		Summary: harness.Summary{
			Code: string("quality.event." + eventType),
			Arguments: []harness.SummaryArgument{{
				Name:  harness.SummaryArgumentReasonCode,
				Value: "accepted",
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
