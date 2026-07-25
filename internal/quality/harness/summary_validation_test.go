package harness

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateEventAcceptsBoundedOrderedSummaryArguments(t *testing.T) {
	event := validRunEvent()
	event.Summary.Arguments = []SummaryArgument{
		{Name: "profile_id", Value: "long_serial"},
		{Name: "stage_kind", Value: "draft"},
		{Name: "artifact_kind", Value: "chapter:revision"},
		{Name: "decision_kind", Value: "candidate_selection"},
		{Name: "preference_kind", Value: "style.signal"},
		{Name: "reason_code", Value: "input_changed"},
		{Name: "result_code", Value: "accepted"},
		{Name: "item_count", Value: "0"},
	}
	want := append([]SummaryArgument(nil), event.Summary.Arguments...)
	if err := ValidateEvent(event); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}
	if !reflect.DeepEqual(event.Summary.Arguments, want) {
		t.Fatal("validation changed ordered summary arguments")
	}
}

func TestValidateEventRejectsUnsafeOrUnboundedSummary(t *testing.T) {
	eightLongValues := []SummaryArgument{
		{Name: "profile_id", Value: "long_serial"},
		{Name: "stage_kind", Value: strings.Repeat("a", 128)},
		{Name: "artifact_kind", Value: strings.Repeat("b", 128)},
		{Name: "decision_kind", Value: strings.Repeat("c", 128)},
		{Name: "preference_kind", Value: strings.Repeat("d", 128)},
		{Name: "reason_code", Value: strings.Repeat("e", 128)},
		{Name: "result_code", Value: strings.Repeat("f", 128)},
		{Name: "item_count", Value: strings.Repeat("9", 128)},
	}
	tests := []struct {
		name      string
		code      string
		arguments []SummaryArgument
	}{
		{"nil arguments", "quality.event.workflow.run.created", nil},
		{"mismatched localization code", "quality.event.workflow.stage.started", []SummaryArgument{}},
		{"too many arguments", "quality.event.workflow.run.created", append(eightLongValues, SummaryArgument{Name: "reason_code", Value: "again"})},
		{"duplicate name", "quality.event.workflow.run.created", []SummaryArgument{{Name: "reason_code", Value: "first"}, {Name: "reason_code", Value: "second"}}},
		{"free text name", "quality.event.workflow.run.created", []SummaryArgument{{Name: "message", Value: "private-text"}}},
		{"payload name", "quality.event.workflow.run.created", []SummaryArgument{{Name: "prompt", Value: "private-text"}}},
		{"unknown profile", "quality.event.workflow.run.created", []SummaryArgument{{Name: "profile_id", Value: "future_profile"}}},
		{"noncanonical count", "quality.event.workflow.run.created", []SummaryArgument{{Name: "item_count", Value: "01"}}},
		{"url value", "quality.event.workflow.run.created", []SummaryArgument{{Name: "reason_code", Value: "https://secret.example"}}},
		{"absolute path value", "quality.event.workflow.run.created", []SummaryArgument{{Name: "reason_code", Value: "/Users/author/book"}}},
		{"uppercase token", "quality.event.workflow.run.created", []SummaryArgument{{Name: "result_code", Value: "Accepted"}}},
		{"value over 128 bytes", "quality.event.workflow.run.created", []SummaryArgument{{Name: "reason_code", Value: strings.Repeat("a", 129)}}},
		{"canonical summary over 1024 bytes", "quality.event.workflow.run.created", eightLongValues},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := validRunEvent()
			event.Summary = Summary{Code: test.code, Arguments: test.arguments}
			if err := ValidateEvent(event); err == nil {
				t.Fatal("unsafe summary accepted")
			}
		})
	}
}
