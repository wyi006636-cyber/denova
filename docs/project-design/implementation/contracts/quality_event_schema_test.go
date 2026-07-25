package contracts

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestQualityEventV1SchemaCompilesAndAcceptsEveryEventScope(t *testing.T) {
	schema := compileQualityEventSchema(t)
	tests := []struct {
		eventType string
		scope     string
	}{
		{"workflow.run.created", ""},
		{"workflow.stage.started", `,"stage_id":"stage-001"`},
		{"workflow.stage.completed", `,"stage_id":"stage-001"`},
		{"workflow.stage.failed", `,"stage_id":"stage-001"`},
		{"workflow.input.invalidated", ""},
		{"workflow.decision.required", `,"stage_id":"stage-001"`},
		{"artifact.created", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"candidate.created", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"candidate.compared", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"candidate.selected", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"review.issue.created", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"review.completed", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"revision.completed", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"preference.confirmed", ""},
		{"preference.revoked", ""},
		{"finalization.started", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"finalization.completed", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
		{"finalization.rolled_back", `,"stage_id":"stage-001","artifact_id":"artifact-001"`},
	}

	for _, test := range tests {
		t.Run(test.eventType, func(t *testing.T) {
			raw := fmt.Sprintf(`{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":%q,"event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":%q,"arguments":[]}%s}`,
				test.eventType, "quality.event."+test.eventType, test.scope)
			assertSchemaResult(t, schema, raw, true)
		})
	}
}

func TestQualityEventV1SchemaAcceptsEverySummaryArgumentBranch(t *testing.T) {
	schema := compileQualityEventSchema(t)
	tests := []struct {
		name      string
		arguments string
	}{
		{"profile id", `[{"name":"profile_id","value":"long_serial"}]`},
		{"bounded token", `[{"name":"reason_code","value":"input_changed"}]`},
		{"canonical item count", `[{"name":"item_count","value":"12"}]`},
		{"complete ordered summary", `[{"name":"profile_id","value":"fanqie_short"},{"name":"stage_kind","value":"draft"},{"name":"artifact_kind","value":"chapter:revision"},{"name":"decision_kind","value":"candidate_selection"},{"name":"preference_kind","value":"style.signal"},{"name":"reason_code","value":"input_changed"},{"name":"result_code","value":"accepted"},{"name":"item_count","value":"12"}]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":%s}}`, test.arguments)
			assertSchemaResult(t, schema, raw, true)
		})
	}
}

func TestQualityEventV1SchemaRejectsNonExactShapes(t *testing.T) {
	schema := compileQualityEventSchema(t)
	tests := []struct {
		name string
		raw  string
	}{
		{"unknown field", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[]},"prompt":"secret"}`},
		{"newer version", `{"contract":{"kind":"denova.quality-event","version":"v2"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[]}}`},
		{"wrong summary code", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.stage.started","arguments":[]}}`},
		{"missing stage", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.stage.started","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.stage.started","arguments":[]}}`},
		{"forbidden artifact", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.stage.started","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.stage.started","arguments":[]},"stage_id":"stage-001","artifact_id":"artifact-001"}`},
		{"duplicate argument name", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[{"name":"reason_code","value":"changed"},{"name":"reason_code","value":"again"}]}}`},
		{"noncanonical item count", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[{"name":"item_count","value":"01"}]}}`},
		{"argument unknown field", `{"contract":{"kind":"denova.quality-event","version":"v1"},"event_type":"workflow.run.created","event_id":"event-001","run_id":"run-001","occurred_at":"2026-07-24T12:00:00Z","sequence":1,"summary":{"code":"quality.event.workflow.run.created","arguments":[{"name":"reason_code","value":"changed","body":"secret"}]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertSchemaResult(t, schema, test.raw, false)
		})
	}
}

func compileQualityEventSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(QualityEventV1Schema()))
	if err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource("https://denova.example/schemas/quality-event-v1.schema.json", document); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	schema, err := compiler.Compile("https://denova.example/schemas/quality-event-v1.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema
}

func assertSchemaResult(t *testing.T, schema *jsonschema.Schema, raw string, wantValid bool) {
	t.Helper()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	err = schema.Validate(document)
	if wantValid && err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
	if !wantValid && err == nil {
		t.Fatal("invalid fixture accepted")
	}
}
