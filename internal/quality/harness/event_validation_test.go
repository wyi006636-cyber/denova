package harness

import "testing"

func TestValidateEventAcceptsExactCoreEnvelope(t *testing.T) {
	event := validRunEvent()
	if err := ValidateEvent(event); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	got := AllEventTypes()
	if len(got) != 18 || got[0] != EventWorkflowRunCreated || got[17] != EventFinalizationRolledBack {
		t.Fatalf("unexpected closed event vocabulary: %#v", got)
	}
	got[0] = EventType("future.event")
	if AllEventTypes()[0] != EventWorkflowRunCreated {
		t.Fatal("caller mutated the closed event vocabulary")
	}
}

func TestValidateEventRejectsInvalidCoreEnvelope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"contract kind", func(event *Event) { event.Contract.Kind = "denova.other" }},
		{"contract version", func(event *Event) { event.Contract.Version = "v2" }},
		{"unknown event", func(event *Event) { event.EventType = EventType("future.event") }},
		{"short event id", func(event *Event) { event.EventID = "e1" }},
		{"uppercase run id", func(event *Event) { event.RunID = "Run-001" }},
		{"non ASCII run id", func(event *Event) { event.RunID = "run-作品" }},
		{"zero sequence", func(event *Event) { event.Sequence = 0 }},
		{"offset timestamp", func(event *Event) { event.OccurredAt = "2026-07-24T12:00:00+00:00" }},
		{"invalid timestamp", func(event *Event) { event.OccurredAt = "2026-07-24 12:00:00Z" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := validRunEvent()
			test.mutate(&event)
			if err := ValidateEvent(event); err == nil {
				t.Fatal("invalid event accepted")
			}
		})
	}
}

func validRunEvent() Event {
	return Event{
		Contract:   Contract{Kind: ContractKind, Version: ContractVersionV1},
		EventType:  EventWorkflowRunCreated,
		EventID:    "event-001",
		RunID:      "run-001",
		OccurredAt: "2026-07-24T12:00:00Z",
		Sequence:   1,
		Summary:    Summary{Code: "quality.event.workflow.run.created", Arguments: []SummaryArgument{}},
	}
}
