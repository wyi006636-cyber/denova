package agentui

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"denova/internal/agent"
	"denova/internal/quality/harness"
)

func TestStreamEncoderProjectsValidQualityEventToSafeActivity(t *testing.T) {
	event := harness.Event{
		Contract:   harness.Contract{Kind: harness.ContractKind, Version: harness.ContractVersionV1},
		EventType:  harness.EventFinalizationCompleted,
		EventID:    "event-018",
		RunID:      "run-001",
		OccurredAt: "2026-07-24T12:34:56Z",
		Sequence:   18,
		StageID:    "stage-001",
		ArtifactID: "artifact-001",
		Summary: harness.Summary{
			Code: "quality.event.finalization.completed",
			Arguments: []harness.SummaryArgument{{
				Name: harness.SummaryArgumentResultCode, Value: "accepted",
			}},
		},
	}
	var output bytes.Buffer
	encoder := NewStreamEncoder(&output)
	if err := encoder.WriteEvent(agent.Event{Type: string(event.EventType), Data: event}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	activity := qualityActivityChunk(t, output.String())
	if activity["id"] != "event-018" {
		t.Fatalf("activity id = %#v, want original event_id", activity["id"])
	}
	wantData := map[string]any{
		"contract":    map[string]any{"kind": "denova.quality-event", "version": "v1"},
		"event":       "finalization.completed",
		"event_type":  "finalization.completed",
		"event_id":    "event-018",
		"run_id":      "run-001",
		"occurred_at": "2026-07-24T12:34:56Z",
		"sequence":    float64(18),
		"stage_id":    "stage-001",
		"artifact_id": "artifact-001",
		"summary": map[string]any{
			"code": "quality.event.finalization.completed",
			"arguments": []any{map[string]any{
				"name": "result_code", "value": "accepted",
			}},
		},
	}
	if !reflect.DeepEqual(activity["data"], wantData) {
		t.Fatalf("activity data mismatch\nwant: %#v\n got: %#v", wantData, activity["data"])
	}
}

func TestStreamEncoderProjectsUnknownQualityEventWithoutArbitraryPayload(t *testing.T) {
	malicious := map[string]any{
		"contract": map[string]any{
			"kind": "denova.quality-event", "version": "v9", "secret": "contract-secret",
		},
		"event_type":  "workflow.future",
		"event_id":    "event-999",
		"run_id":      "run-001",
		"occurred_at": "2026-07-24T12:34:56Z",
		"sequence":    99,
		"stage_id":    "stage-001",
		"artifact_id": "artifact-001",
		"summary": map[string]any{
			"code": "quality.event.workflow.future",
			"arguments": []any{
				map[string]any{"name": "reason_code", "value": "future_reason"},
				map[string]any{"name": "body", "value": "summary-body-secret"},
				map[string]any{"name": "result_code", "value": map[string]any{"secret": "nested-secret"}},
			},
			"body": "summary-secret",
		},
		"id":          "secret-start-id",
		"created_at":  "secret-created-at",
		"prompt":      "secret-prompt",
		"thinking":    "secret-thinking",
		"body":        "secret-body",
		"candidate":   "secret-candidate",
		"quote":       "secret-quote",
		"preference":  "secret-preference",
		"tool_result": map[string]any{"secret": "secret-tool-result"},
		"path":        "/Users/author/private.md",
		"nested":      map[string]any{"secret": "nested-payload-secret"},
	}
	var output bytes.Buffer
	encoder := NewStreamEncoder(&output)
	if err := encoder.WriteEvent(agent.Event{Type: "workflow.future", Data: malicious}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	activity := qualityActivityChunk(t, output.String())
	if activity["id"] != "event-999" {
		t.Fatalf("activity id = %#v, want original bounded event_id", activity["id"])
	}
	data, ok := activity["data"].(map[string]any)
	if !ok {
		t.Fatalf("activity data type = %T", activity["data"])
	}
	wantKeys := []string{"artifact_id", "contract", "event", "event_id", "event_type", "occurred_at", "run_id", "sequence", "stage_id", "summary"}
	gotKeys := sortedMapKeys(data)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("activity keys = %v, want %v", gotKeys, wantKeys)
	}
	summary := data["summary"].(map[string]any)
	wantSummary := map[string]any{
		"code": "quality.event.workflow.future",
		"arguments": []any{map[string]any{
			"name": "reason_code", "value": "future_reason",
		}},
	}
	if !reflect.DeepEqual(summary, wantSummary) {
		t.Fatalf("safe summary = %#v, want %#v", summary, wantSummary)
	}

	raw, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal activity: %v", err)
	}
	for _, forbidden := range []string{
		"secret-start-id", "secret-created-at", "secret-prompt", "secret-thinking",
		"secret-body", "secret-candidate", "secret-quote", "secret-preference",
		"secret-tool-result", "/Users/author", "nested-payload-secret", "summary-body-secret", "nested-secret",
	} {
		if strings.Contains(output.String(), forbidden) || strings.Contains(string(raw), forbidden) {
			t.Fatalf("Quality UI output leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestStreamEncoderBoundsUnknownQualityActivityFallbackID(t *testing.T) {
	var output bytes.Buffer
	encoder := NewStreamEncoder(&output)
	unsafeType := "future\n" + strings.Repeat("secret", 40)
	if err := encoder.WriteEvent(agent.Event{Type: unsafeType, Data: map[string]any{
		"contract":   map[string]any{"kind": harness.ContractKind, "version": "v99"},
		"event_type": unsafeType,
		"run_id":     "invalid run id",
	}}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}
	activity := qualityActivityChunk(t, output.String())
	if activity["id"] != "quality-event" {
		t.Fatalf("unsafe activity fallback id = %#v, want quality-event", activity["id"])
	}
	if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "\nsecret") {
		t.Fatalf("unsafe Quality routing leaked into stream: %q", output.String())
	}
}

func TestStreamEncoderQualityShapePreemptsLegacyEventTypeBranches(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		malicious map[string]any
	}{
		{
			name:      "workspace change",
			eventType: "workspace_change",
			malicious: map[string]any{
				"path": "/Users/author/private.md", "body": "workspace-body-secret",
			},
		},
		{
			name:      "tool result",
			eventType: "tool_result",
			malicious: map[string]any{
				"id": "secret-tool-id", "name": "read_file", "content": "tool-content-secret",
				"tool_result": map[string]any{"secret": "nested-tool-secret"},
			},
		},
		{
			name:      "chunk",
			eventType: "chunk",
			malicious: map[string]any{
				"content": "chunk-content-secret", "body": "chunk-body-secret",
			},
		},
		{
			name:      "error",
			eventType: "error",
			malicious: map[string]any{
				"message": "error-message-secret", "error": "error-detail-secret",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{
				"contract":    map[string]any{"kind": harness.ContractKind, "version": "v9"},
				"event_type":  test.eventType,
				"event_id":    "event-999",
				"run_id":      "run-001",
				"occurred_at": "2026-07-24T12:34:56Z",
				"sequence":    99,
				"summary": map[string]any{
					"code":      "quality.event.future",
					"arguments": []any{map[string]any{"name": "reason_code", "value": "future_reason"}},
				},
				"secret": map[string]any{"nested": "top-level-nested-secret"},
			}
			for key, value := range test.malicious {
				data[key] = value
			}
			var output bytes.Buffer
			encoder := NewStreamEncoder(&output)
			if err := encoder.WriteEvent(agent.Event{Type: test.eventType, Data: data}); err != nil {
				t.Fatalf("WriteEvent() error = %v", err)
			}

			activity := qualityActivityChunk(t, output.String())
			if activity["id"] != "event-999" {
				t.Fatalf("activity id = %#v, want original event_id", activity["id"])
			}
			projected, ok := activity["data"].(map[string]any)
			if !ok {
				t.Fatalf("activity data type = %T", activity["data"])
			}
			for _, forbiddenKey := range []string{"path", "body", "content", "message", "error", "secret", "nested", "tool_result", "id", "name"} {
				if _, exists := projected[forbiddenKey]; exists {
					t.Fatalf("Quality activity copied forbidden key %q: %#v", forbiddenKey, projected)
				}
			}
			for _, forbiddenValue := range []string{
				"/Users/author", "workspace-body-secret", "secret-tool-id", "tool-content-secret",
				"nested-tool-secret", "chunk-content-secret", "chunk-body-secret",
				"error-message-secret", "error-detail-secret", "top-level-nested-secret",
			} {
				if strings.Contains(output.String(), forbiddenValue) {
					t.Fatalf("Quality event type collision leaked %q: %q", forbiddenValue, output.String())
				}
			}
		})
	}
}

func qualityActivityChunk(t *testing.T, raw string) map[string]any {
	t.Helper()
	chunks, _ := parseUIStreamChunks(t, raw)
	for _, chunk := range chunks {
		if chunk["type"] == DataTypeActivity {
			return chunk
		}
	}
	t.Fatalf("missing Quality activity chunk in %q", raw)
	return nil
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
