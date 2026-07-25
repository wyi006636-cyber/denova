package harness

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestValidatorAcceptsCompleteOrderedNonEmptySummary(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	raw := []byte(`{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[{"name":"profile_id","value":"long_serial"},{"name":"stage_kind","value":"draft"},{"name":"artifact_kind","value":"chapter:revision"},{"name":"decision_kind","value":"candidate_selection"},{"name":"preference_kind","value":"style.signal"},{"name":"reason_code","value":"input_changed"},{"name":"result_code","value":"accepted"},{"name":"item_count","value":"12"}]}}`)
	event, err := validator.ValidateJSON(raw)
	if err != nil {
		t.Fatalf("valid non-empty summary rejected: %v", err)
	}
	want := []SummaryArgument{
		{Name: SummaryArgumentProfileID, Value: "long_serial"},
		{Name: SummaryArgumentStageKind, Value: "draft"},
		{Name: SummaryArgumentArtifactKind, Value: "chapter:revision"},
		{Name: SummaryArgumentDecisionKind, Value: "candidate_selection"},
		{Name: SummaryArgumentPreferenceKind, Value: "style.signal"},
		{Name: SummaryArgumentReasonCode, Value: "input_changed"},
		{Name: SummaryArgumentResultCode, Value: "accepted"},
		{Name: SummaryArgumentItemCount, Value: "12"},
	}
	if !reflect.DeepEqual(event.Summary.Arguments, want) {
		t.Fatalf("ordered summary changed: got %#v want %#v", event.Summary.Arguments, want)
	}
}

func TestValidatorRequiresSchemaAndSemanticExactV1Admission(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	valid := `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[]}}`
	event, err := validator.ValidateJSON([]byte(valid))
	if err != nil {
		t.Fatalf("valid exact-v1 JSON rejected: %v", err)
	}
	if event.EventID != "event-001" || event.OccurredAt != "2026-07-24T12:00:00Z" || event.Sequence != 1 {
		t.Fatalf("validated identity changed: %#v", event)
	}

	longArguments := `[{"name":"profile_id","value":"long_serial"},{"name":"stage_kind","value":"` + strings.Repeat("a", 128) + `"},{"name":"artifact_kind","value":"` + strings.Repeat("b", 128) + `"},{"name":"decision_kind","value":"` + strings.Repeat("c", 128) + `"},{"name":"preference_kind","value":"` + strings.Repeat("d", 128) + `"},{"name":"reason_code","value":"` + strings.Repeat("e", 128) + `"},{"name":"result_code","value":"` + strings.Repeat("f", 128) + `"},{"name":"item_count","value":"` + strings.Repeat("9", 128) + `"}]`
	tests := []struct {
		name string
		raw  string
	}{
		{"schema unknown field", strings.TrimSuffix(valid, "}") + `,"prompt":"secret"}`},
		{"schema nested payload", strings.Replace(valid, `"arguments":[]`, `"arguments":[{"name":"reason_code","value":{"secret":"text"}}]`, 1)},
		{"semantic encoded summary bound", strings.Replace(valid, `"arguments":[]`, `"arguments":`+longArguments, 1)},
		{"duplicate key", strings.Replace(valid, `"sequence":1`, `"sequence":1,"sequence":2`, 1)},
		{"multiple JSON values", valid + `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validator.ValidateJSON([]byte(test.raw)); err == nil {
				t.Fatal("non-exact event JSON accepted")
			}
		})
	}
}

func TestInspectEventPreservesUnknownVersionButExactValidationRejectsIt(t *testing.T) {
	raw := []byte(`{"contract":{"kind":"denova.quality-event","version":"v2","extension":"kept"},"event_type":"future.event","opaque":{"nested":true}}`)
	inspection, err := InspectEvent(raw)
	if err != nil {
		t.Fatalf("inspect newer event: %v", err)
	}
	if inspection.Contract() != (Contract{Kind: ContractKind, Version: "v2"}) || inspection.EventType() != EventType("future.event") {
		t.Fatalf("unexpected inspection header: %#v %q", inspection.Contract(), inspection.EventType())
	}
	first := inspection.RawBytes()
	if !bytes.Equal(first, raw) {
		t.Fatal("inspection did not preserve exact bytes")
	}
	first[0] ^= 0xff
	if !bytes.Equal(inspection.RawBytes(), raw) {
		t.Fatal("caller mutated preserved inspection bytes")
	}
	validator, err := NewValidator()
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	if _, err := validator.ValidateJSON(raw); err == nil {
		t.Fatal("newer event admitted as exact v1")
	}
}
