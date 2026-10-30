package writingcomparison

import (
	"errors"
	"testing"
)

func stateTask(phase Phase, execution Execution) Task {
	return Task{ID: "comparison-1", ProjectID: "book-1", Phase: phase, Execution: execution, Target: TargetRef{Path: "chapters/ch00001.md", BaseRevision: "missing"}, Candidates: []Candidate{{ID: "candidate-1", Status: CandidatePending, Attempt: 1}, {ID: "candidate-2", Status: CandidatePending, Attempt: 1}}}
}

func TestStateMachineEnumeratesPhaseExecutionEvents(t *testing.T) {
	phases := []Phase{PhaseConfiguring, PhaseGenerating, PhasePartial, PhaseGenerated, PhaseSynthesis, PhasePendingApplication, PhaseApplied, PhaseArchived}
	executions := []Execution{ExecutionIdle, ExecutionQueued, ExecutionRunning, ExecutionPaused, ExecutionCancelled, ExecutionIncomplete, ExecutionDone}
	events := []Event{
		{Kind: EventPhaseChanged, FromPhase: PhaseConfiguring, ToPhase: PhaseGenerating},
		{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhaseGenerated},
		{Kind: EventExecutionChanged, FromExecution: ExecutionIdle, ToExecution: ExecutionQueued},
		{Kind: EventExecutionChanged, FromExecution: ExecutionQueued, ToExecution: ExecutionRunning},
		{Kind: EventExecutionChanged, FromExecution: ExecutionRunning, ToExecution: ExecutionDone},
		{Kind: EventCandidateStarted, CandidateID: "candidate-1"},
		{Kind: EventCandidateFailed, CandidateID: "candidate-1", Error: &TaskError{Code: "model_failed"}},
		{Kind: EventSelectionSet, Composition: &Composition{CandidateID: "candidate-1", Revision: 1}},
		{Kind: EventApplicationApplied, Applied: &AppliedChange{Path: "chapters/ch00001.md", BaseRevision: "missing"}},
	}
	for _, phase := range phases {
		for _, execution := range executions {
			for _, event := range events {
				_ = ValidateTransition(stateTask(phase, execution), event)
			}
		}
	}
	if err := ValidateTransition(stateTask(PhaseConfiguring, ExecutionIdle), events[0]); err != nil {
		t.Fatalf("legal phase transition rejected: %v", err)
	}
	if err := ValidateTransition(stateTask(PhaseGenerating, ExecutionIdle), events[2]); err != nil {
		t.Fatalf("legal execution transition rejected: %v", err)
	}
	if err := ValidateTransition(stateTask(PhaseApplied, ExecutionDone), Event{Kind: EventPhaseChanged, FromPhase: PhaseApplied, ToPhase: PhaseGenerating}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("illegal transition error = %v", err)
	}
}
