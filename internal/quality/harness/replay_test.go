package harness

import (
	"reflect"
	"testing"
)

func TestValidateReplayAcceptsFullAndPartialContiguousStreamsWithoutMutation(t *testing.T) {
	first := validRunEvent()
	second := validRunEvent()
	second.EventType = EventWorkflowInputInvalidated
	second.EventID = "event-002"
	second.Sequence = 2
	second.OccurredAt = "2026-07-24T12:01:00Z"
	second.Summary.Code = "quality.event.workflow.input.invalidated"
	third := validRunEvent()
	third.EventType = EventFinalizationCompleted
	third.EventID = "event-003"
	third.Sequence = 3
	third.OccurredAt = "2026-07-24T12:02:00.123Z"
	third.StageID = "stage-001"
	third.ArtifactID = "artifact-001"
	third.Summary.Code = "quality.event.finalization.completed"
	events := []Event{first, second, third}
	want := append([]Event(nil), events...)

	if err := ValidateReplay(ReplayCursor{RunID: "run-001", Sequence: 0}, events); err != nil {
		t.Fatalf("full stream rejected: %v", err)
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatal("replay validation changed original identity, sequence, or time")
	}

	partial := third
	partial.Sequence = 8
	if err := ValidateReplay(ReplayCursor{RunID: "run-001", Sequence: 7}, []Event{partial}); err != nil {
		t.Fatalf("partial stream rejected: %v", err)
	}
}

func TestValidateReplayRejectsGapDuplicateRegressionIdentityAndCrossRun(t *testing.T) {
	event1 := validRunEvent()
	event2 := validRunEvent()
	event2.EventID = "event-002"
	event2.Sequence = 2
	tests := []struct {
		name   string
		cursor ReplayCursor
		events []Event
	}{
		{"first event does not start at one", ReplayCursor{RunID: "run-001"}, []Event{event2}},
		{"gap", ReplayCursor{RunID: "run-001"}, []Event{event1, eventWithSequence(event2, 3)}},
		{"duplicate sequence", ReplayCursor{RunID: "run-001"}, []Event{event1, eventWithSequence(event2, 1)}},
		{"regression", ReplayCursor{RunID: "run-001", Sequence: 2}, []Event{eventWithSequence(event1, 1)}},
		{"duplicate event id", ReplayCursor{RunID: "run-001"}, []Event{event1, eventWithSequence(event1, 2)}},
		{"cross run", ReplayCursor{RunID: "run-001"}, []Event{eventWithRun(event1, "run-002")}},
		{"invalid cursor run", ReplayCursor{RunID: "x"}, []Event{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateReplay(test.cursor, test.events); err == nil {
				t.Fatal("invalid replay accepted")
			}
		})
	}
}

func eventWithSequence(event Event, sequence uint64) Event {
	event.Sequence = sequence
	return event
}

func eventWithRun(event Event, runID string) Event {
	event.RunID = runID
	return event
}
