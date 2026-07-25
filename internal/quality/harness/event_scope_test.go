package harness

import "testing"

func TestValidateEventEnforcesExhaustiveScopeMatrix(t *testing.T) {
	tests := []struct {
		eventType EventType
		scope     string
	}{
		{EventWorkflowRunCreated, "run"},
		{EventWorkflowStageStarted, "stage"},
		{EventWorkflowStageCompleted, "stage"},
		{EventWorkflowStageFailed, "stage"},
		{EventWorkflowInputInvalidated, "run"},
		{EventWorkflowDecisionRequired, "stage"},
		{EventArtifactCreated, "artifact"},
		{EventCandidateCreated, "artifact"},
		{EventCandidateCompared, "artifact"},
		{EventCandidateSelected, "artifact"},
		{EventReviewIssueCreated, "artifact"},
		{EventReviewCompleted, "artifact"},
		{EventRevisionCompleted, "artifact"},
		{EventPreferenceConfirmed, "run"},
		{EventPreferenceRevoked, "run"},
		{EventFinalizationStarted, "artifact"},
		{EventFinalizationCompleted, "artifact"},
		{EventFinalizationRolledBack, "artifact"},
	}

	for _, test := range tests {
		t.Run(string(test.eventType), func(t *testing.T) {
			event := validRunEvent()
			event.EventType = test.eventType
			event.Summary.Code = "quality.event." + string(test.eventType)
			switch test.scope {
			case "run":
				assertValidEvent(t, event)
				event.StageID = "stage-001"
				assertInvalidEvent(t, event)
				event.StageID = ""
				event.ArtifactID = "artifact-001"
				assertInvalidEvent(t, event)
			case "stage":
				event.StageID = "stage-001"
				assertValidEvent(t, event)
				event.StageID = ""
				assertInvalidEvent(t, event)
				event.StageID = "stage-001"
				event.ArtifactID = "artifact-001"
				assertInvalidEvent(t, event)
			case "artifact":
				event.StageID = "stage-001"
				event.ArtifactID = "artifact-001"
				assertValidEvent(t, event)
				event.StageID = ""
				assertInvalidEvent(t, event)
				event.StageID = "stage-001"
				event.ArtifactID = ""
				assertInvalidEvent(t, event)
			}
		})
	}
}

func assertValidEvent(t *testing.T, event Event) {
	t.Helper()
	if err := ValidateEvent(event); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
}

func assertInvalidEvent(t *testing.T, event Event) {
	t.Helper()
	if err := ValidateEvent(event); err == nil {
		t.Fatal("invalid event accepted")
	}
}
