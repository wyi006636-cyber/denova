package writingcomparison

import (
	"errors"
	"testing"
)

func stateTask(phase Phase, execution Execution) Task {
	return Task{ID: "comparison-1", ProjectID: "book-1", Phase: phase, Execution: execution, Target: TargetRef{Path: "chapters/ch00001.md", BaseRevision: "missing"}, Candidates: []Candidate{{ID: "candidate-1", Status: CandidatePending, Attempt: 1}, {ID: "candidate-2", Status: CandidatePending, Attempt: 1}}}
}

func completedRevision() *CandidateRevision {
	return &CandidateRevision{Number: 1, Path: "assets/candidate-1.md", SHA256: "hash"}
}

func TestStateMachineExpectationTable(t *testing.T) {
	allCompleted := stateTask(PhaseGenerating, ExecutionIdle)
	allCompleted.Candidates[0].Status = CandidateCompleted
	allCompleted.Candidates[0].Revision = completedRevision()
	allCompleted.Candidates[1].Status = CandidateCompleted
	allCompleted.Candidates[1].Revision = completedRevision()
	failed := stateTask(PhaseGenerating, ExecutionIdle)
	failed.Candidates[0].Status = CandidateFailed
	running := stateTask(PhaseGenerating, ExecutionRunning)
	running.Candidates[0].Status = CandidateRunning
	completed := stateTask(PhaseGenerated, ExecutionDone)
	completed.Candidates[0].Status = CandidateCompleted
	completed.Candidates[0].Revision = completedRevision()

	tests := []struct {
		name    string
		task    Task
		event   Event
		allowed bool
	}{
		{"configuring to generating", stateTask(PhaseConfiguring, ExecutionIdle), Event{Kind: EventPhaseChanged, FromPhase: PhaseConfiguring, ToPhase: PhaseGenerating}, true},
		{"generating queues", stateTask(PhaseGenerating, ExecutionIdle), Event{Kind: EventExecutionChanged, FromExecution: ExecutionIdle, ToExecution: ExecutionQueued}, true},
		{"partial rerun queues done execution", stateTask(PhaseGenerating, ExecutionDone), Event{Kind: EventExecutionChanged, FromExecution: ExecutionDone, ToExecution: ExecutionQueued}, true},
		{"partial rerun is generating only", stateTask(PhasePartial, ExecutionDone), Event{Kind: EventExecutionChanged, FromExecution: ExecutionDone, ToExecution: ExecutionQueued}, false},
		{"generated requires completed candidates", stateTask(PhaseGenerating, ExecutionIdle), Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhaseGenerated}, false},
		{"generated accepts all completed", allCompleted, Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhaseGenerated}, true},
		{"partial requires failed candidate", stateTask(PhaseGenerating, ExecutionIdle), Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhasePartial}, false},
		{"partial accepts failed candidate", failed, Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhasePartial}, true},
		{"completed requires generating", stateTask(PhaseConfiguring, ExecutionRunning), Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, false},
		{"completed requires started candidate", stateTask(PhaseGenerating, ExecutionRunning), Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, false},
		{"completed accepts running candidate", running, Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, true},
		{"selection rejects failed candidate", failed, Event{Kind: EventSelectionSet, Composition: &Composition{CandidateID: "candidate-1", Revision: 1}}, false},
		{"selection accepts completed candidate", completed, Event{Kind: EventSelectionSet, Composition: &Composition{CandidateID: "candidate-1", Revision: 1}}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTransition(test.task, test.event)
			if test.allowed && err != nil {
				t.Fatalf("allowed event rejected: %v", err)
			}
			if !test.allowed && !errors.Is(err, ErrStateConflict) {
				t.Fatalf("illegal event error=%v, want state conflict", err)
			}
		})
	}
}

func TestStateMachineRejectsDuplicateSnapshotAndIncompleteRevision(t *testing.T) {
	task := stateTask(PhaseGenerating, ExecutionRunning)
	snapshot := &SnapshotRef{ID: "snapshot-1", SHA256: "hash"}
	if err := ValidateTransition(task, Event{Kind: EventSnapshotFrozen, Snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	task.Snapshot = snapshot
	if err := ValidateTransition(task, Event{Kind: EventSnapshotFrozen, Snapshot: snapshot}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("duplicate snapshot error=%v", err)
	}
	task.Candidates[0].Status = CandidateRunning
	if err := ValidateTransition(task, Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: &CandidateRevision{Number: 1}}); err == nil {
		t.Fatal("incomplete candidate revision was accepted")
	}
}
