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
	// incompleteRerun is a recovered task queued again: candidate-1 was
	// interrupted mid-call and candidate-2 never started.
	incompleteRerun := stateTask(PhaseGenerating, ExecutionRunning)
	incompleteRerun.Candidates[0].Status = CandidateIncomplete
	incompleteIdle := stateTask(PhaseGenerating, ExecutionIncomplete)
	incompleteIdle.Candidates[0].Status = CandidateIncomplete

	tests := []struct {
		name          string
		task          Task
		event         Event
		allowed       bool
		wantPhase     Phase
		wantExecution Execution
		wantCandidate CandidateStatus
	}{
		{"configuring to generating", stateTask(PhaseConfiguring, ExecutionIdle), Event{Kind: EventPhaseChanged, FromPhase: PhaseConfiguring, ToPhase: PhaseGenerating}, true, PhaseGenerating, ExecutionIdle, CandidatePending},
		{"generating queues", stateTask(PhaseGenerating, ExecutionIdle), Event{Kind: EventExecutionChanged, FromExecution: ExecutionIdle, ToExecution: ExecutionQueued}, true, PhaseGenerating, ExecutionQueued, CandidatePending},
		{"partial rerun queues done execution", stateTask(PhaseGenerating, ExecutionDone), Event{Kind: EventExecutionChanged, FromExecution: ExecutionDone, ToExecution: ExecutionQueued}, true, PhaseGenerating, ExecutionQueued, CandidatePending},
		{"partial rerun is generating only", stateTask(PhasePartial, ExecutionDone), Event{Kind: EventExecutionChanged, FromExecution: ExecutionDone, ToExecution: ExecutionQueued}, false, "", "", ""},
		{"generated requires completed candidates", stateTask(PhaseGenerating, ExecutionIdle), Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhaseGenerated}, false, "", "", ""},
		{"generated accepts all completed", allCompleted, Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhaseGenerated}, true, PhaseGenerated, ExecutionIdle, CandidateCompleted},
		{"partial requires failed candidate", stateTask(PhaseGenerating, ExecutionIdle), Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhasePartial}, false, "", "", ""},
		{"partial accepts failed candidate", failed, Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhasePartial}, true, PhasePartial, ExecutionIdle, CandidateFailed},
		{"completed requires generating", stateTask(PhaseConfiguring, ExecutionRunning), Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, false, "", "", ""},
		{"completed requires started candidate", stateTask(PhaseGenerating, ExecutionRunning), Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, false, "", "", ""},
		{"completed accepts running candidate", running, Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, true, PhaseGenerating, ExecutionRunning, CandidateCompleted},
		{"selection rejects failed candidate", failed, Event{Kind: EventSelectionSet, Composition: &Composition{CandidateID: "candidate-1", Revision: 1}}, false, "", "", ""},
		{"selection accepts completed candidate", completed, Event{Kind: EventSelectionSet, Composition: &Composition{CandidateID: "candidate-1", Revision: 1}}, true, PhasePendingApplication, ExecutionDone, CandidateCompleted},
		{"incomplete candidate restarts", incompleteRerun, Event{Kind: EventCandidateStarted, CandidateID: "candidate-1"}, true, PhaseGenerating, ExecutionRunning, CandidateRunning},
		{"incomplete candidate cannot complete without restart", incompleteRerun, Event{Kind: EventCandidateCompleted, CandidateID: "candidate-1", Revision: completedRevision()}, false, "", "", ""},
		{"incomplete and pending candidates are not partial", incompleteIdle, Event{Kind: EventPhaseChanged, FromPhase: PhaseGenerating, ToPhase: PhasePartial}, false, "", "", ""},
		{"interrupted execution marks running candidate incomplete", running, Event{Kind: EventExecutionChanged, FromExecution: ExecutionRunning, ToExecution: ExecutionIncomplete}, true, PhaseGenerating, ExecutionIncomplete, CandidateIncomplete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := cloneTask(test.task)
			err := ApplyTransition(&actual, test.event)
			if test.allowed && err != nil {
				t.Fatalf("allowed event rejected: %v", err)
			}
			if !test.allowed && !errors.Is(err, ErrStateConflict) {
				t.Fatalf("illegal event error=%v, want state conflict", err)
			}
			if test.allowed {
				if actual.Phase != test.wantPhase || actual.Execution != test.wantExecution {
					t.Fatalf("state after %s = phase=%s execution=%s, want phase=%s execution=%s", test.name, actual.Phase, actual.Execution, test.wantPhase, test.wantExecution)
				}
				if test.wantCandidate != "" && actual.Candidates[0].Status != test.wantCandidate {
					t.Fatalf("candidate after %s = %s, want %s", test.name, actual.Candidates[0].Status, test.wantCandidate)
				}
				// Only the addressed or running candidate may change; an
				// untouched pending candidate must never be rewritten.
				if actual.Candidates[1].Status != test.task.Candidates[1].Status {
					t.Fatalf("untouched candidate after %s = %s, want %s", test.name, actual.Candidates[1].Status, test.task.Candidates[1].Status)
				}
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
