package writingcomparison

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"denova/internal/agents/conversationjournal"
	"denova/internal/portablepath"
)

type journalEnvelope struct {
	V       int             `json:"v"`
	Kind    EventKind       `json:"kind"`
	TaskID  string          `json:"task_id"`
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

type taskProjection struct {
	task Task
}

func newTaskProjection() *taskProjection { return &taskProjection{} }

func (projection *taskProjection) Reset() error {
	projection.task = Task{}
	return nil
}

func (projection *taskProjection) Restore(raw json.RawMessage) error {
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return err
	}
	if task.ID == "" {
		return fmt.Errorf("comparison projection task id is required")
	}
	projection.task = task
	return nil
}

func (projection *taskProjection) Apply(record conversationjournal.Record) error {
	var envelope journalEnvelope
	if err := json.Unmarshal(record.Payload, &envelope); err != nil {
		return fmt.Errorf("decode comparison journal event: %w", err)
	}
	if envelope.V != 1 || envelope.Kind == "" || envelope.TaskID == "" {
		return fmt.Errorf("comparison journal event envelope is invalid")
	}
	event := Event{}
	if len(envelope.Payload) > 0 && string(envelope.Payload) != "null" {
		if err := json.Unmarshal(envelope.Payload, &event); err != nil {
			return fmt.Errorf("decode comparison event %s: %w", envelope.Kind, err)
		}
	}
	if projection.task.ID != "" && projection.task.ID != envelope.TaskID {
		return fmt.Errorf("comparison event task id mismatch")
	}
	if err := applyEvent(&projection.task, envelope.Kind, event); err != nil {
		return err
	}
	if !envelope.At.IsZero() {
		projection.task.UpdatedAt = envelope.At
		if projection.task.CreatedAt.IsZero() {
			projection.task.CreatedAt = envelope.At
		}
	}
	return nil
}

func (projection *taskProjection) Checkpoint() (json.RawMessage, error) {
	return json.Marshal(projection.task)
}

func (projection *taskProjection) CurrentCheckpoint() (json.RawMessage, error) {
	return projection.Checkpoint()
}

func applyEvent(task *Task, kind EventKind, event Event) error {
	if task == nil {
		return fmt.Errorf("comparison task is nil")
	}
	switch kind {
	case EventTaskCreated:
		if event.Task == nil {
			return fmt.Errorf("task.created payload is missing task")
		}
		if task.ID != "" {
			return stateConflict(task, "task.created cannot replace an existing task")
		}
		*task = cloneTask(*event.Task)
		return validateTask(*task)
	case EventSnapshotFrozen:
		if event.Snapshot == nil {
			return fmt.Errorf("snapshot.frozen payload is missing snapshot")
		}
		if task.ID == "" {
			return fmt.Errorf("snapshot.frozen requires an existing task")
		}
		if task.Snapshot != nil {
			return stateConflict(task, "snapshot is already frozen")
		}
		task.Snapshot = cloneSnapshot(event.Snapshot)
		return nil
	case EventPhaseChanged:
		if task.Phase != event.FromPhase {
			return stateConflict(task, fmt.Sprintf("phase source is %q, task is %q", event.FromPhase, task.Phase))
		}
		if !event.ToPhase.Valid() {
			return stateConflict(task, "phase target is invalid")
		}
		if err := validatePhaseTransitionForTask(task, event.ToPhase); err != nil {
			return err
		}
		task.Phase = event.ToPhase
		return nil
	case EventExecutionChanged:
		if task.Execution != event.FromExecution {
			return stateConflict(task, fmt.Sprintf("execution source is %q, task is %q", event.FromExecution, task.Execution))
		}
		if !event.ToExecution.Valid() {
			return stateConflict(task, "execution target is invalid")
		}
		if err := validateExecutionTransition(task.Phase, task.Execution, event.ToExecution); err != nil {
			return err
		}
		task.Execution = event.ToExecution
		return nil
	case EventCandidateStarted:
		candidate := candidateByID(task, event.CandidateID)
		if candidate == nil {
			return fmt.Errorf("candidate %q not found", event.CandidateID)
		}
		if task.Phase != PhaseGenerating {
			return stateConflict(task, "candidate.started requires generating phase")
		}
		candidate.Status = CandidateRunning
		if candidate.Attempt == 0 {
			candidate.Attempt = 1
		}
		return nil
	case EventCandidateCompleted:
		candidate := candidateByID(task, event.CandidateID)
		if candidate == nil {
			return fmt.Errorf("candidate %q not found", event.CandidateID)
		}
		if task.Phase != PhaseGenerating {
			return stateConflict(task, "candidate.completed requires generating phase")
		}
		if candidate.Status != CandidateRunning {
			return stateConflict(task, "candidate.completed requires a running candidate")
		}
		if event.Revision == nil || event.Revision.Number < 1 || strings.TrimSpace(event.Revision.Path) == "" || strings.TrimSpace(event.Revision.SHA256) == "" {
			return stateConflict(task, "candidate.completed revision is incomplete")
		}
		if err := portablepath.Validate(event.Revision.Path); err != nil {
			return stateConflict(task, "candidate.completed revision path is invalid")
		}
		candidate.Status = CandidateCompleted
		candidate.Revision = cloneCandidateRevision(event.Revision)
		candidate.Error = nil
		return nil
	case EventCandidateFailed:
		candidate := candidateByID(task, event.CandidateID)
		if candidate == nil {
			return fmt.Errorf("candidate %q not found", event.CandidateID)
		}
		if task.Phase != PhaseGenerating {
			return stateConflict(task, "candidate.failed requires generating phase")
		}
		if candidate.Status != CandidateRunning {
			return stateConflict(task, "candidate.failed requires a running candidate")
		}
		candidate.Status = CandidateFailed
		candidate.Error = cloneTaskError(event.Error)
		return nil
	case EventSelectionSet:
		if event.Composition == nil {
			return fmt.Errorf("selection.set composition is missing")
		}
		if task.Phase != PhaseGenerated && task.Phase != PhasePartial && task.Phase != PhasePendingApplication {
			return stateConflict(task, "selection requires generated or partial phase")
		}
		candidate := candidateByID(task, event.Composition.CandidateID)
		if candidate == nil {
			return fmt.Errorf("selected candidate %q not found", event.Composition.CandidateID)
		}
		if candidate.Status != CandidateCompleted || candidate.Revision == nil {
			return stateConflict(task, "selection requires a completed candidate")
		}
		task.Composition = cloneComposition(event.Composition)
		if task.Phase != PhasePendingApplication {
			task.Phase = PhasePendingApplication
		}
		return nil
	case EventApplicationApplied:
		if event.Applied == nil {
			return fmt.Errorf("application.applied payload is missing change")
		}
		if task.Phase != PhasePendingApplication {
			return stateConflict(task, "application requires pending_application phase")
		}
		task.Applied = cloneAppliedChange(event.Applied)
		task.Phase = PhaseApplied
		return nil
	case EventApplicationRejected:
		if task.Phase != PhasePendingApplication {
			return stateConflict(task, "application.rejected requires pending_application phase")
		}
		task.LastError = cloneTaskError(event.Error)
		return nil
	default:
		return fmt.Errorf("unsupported comparison journal event %q", kind)
	}
}

// ValidateTransition is the table-driven state-machine gate used by storage
// and exposed for exhaustive tests. Every finite state combination is handled
// explicitly; no unknown state is silently accepted.
func ValidateTransition(task Task, event Event) error {
	copy := cloneTask(task)
	return applyEvent(&copy, event.Kind, event)
}

// ApplyTransition is the mutating form used by reducers and domain tests.
func ApplyTransition(task *Task, event Event) error { return applyEvent(task, event.Kind, event) }

func validatePhaseTransition(from, to Phase) error {
	allowed := map[Phase]map[Phase]bool{
		PhaseConfiguring:        {PhaseGenerating: true},
		PhaseGenerating:         {PhaseGenerated: true, PhasePartial: true},
		PhasePartial:            {PhaseGenerating: true, PhasePendingApplication: true},
		PhaseGenerated:          {PhasePendingApplication: true, PhaseSynthesis: true},
		PhaseSynthesis:          {PhasePendingApplication: true},
		PhasePendingApplication: {PhaseGenerated: true, PhaseApplied: true},
		PhaseApplied:            {},
		PhaseArchived:           {},
	}
	if allowed[from][to] {
		return nil
	}
	return &StateConflictError{FromPhase: from, ToPhase: to, Reason: "phase transition is not allowed"}
}

func validatePhaseTransitionForTask(task *Task, to Phase) error {
	if task == nil {
		return fmt.Errorf("comparison task is nil")
	}
	if err := validatePhaseTransition(task.Phase, to); err != nil {
		return err
	}
	if task.Phase == PhaseGenerating {
		switch to {
		case PhaseGenerated:
			if !allCandidatesCompleted(*task) {
				return stateConflict(task, "generated requires every candidate to be completed")
			}
		case PhasePartial:
			if !hasCandidateFailure(*task) {
				return stateConflict(task, "partial requires a failed candidate")
			}
		}
	}
	return nil
}

func validateExecutionTransition(phase Phase, from, to Execution) error {
	allowed := map[Execution]map[Execution]bool{
		ExecutionIdle:       {ExecutionQueued: true},
		ExecutionQueued:     {ExecutionRunning: true, ExecutionIncomplete: true},
		ExecutionRunning:    {ExecutionDone: true, ExecutionIncomplete: true},
		ExecutionIncomplete: {ExecutionQueued: true},
		ExecutionDone:       {ExecutionQueued: true},
		ExecutionPaused:     {ExecutionQueued: true, ExecutionCancelled: true},
		ExecutionCancelled:  {},
	}
	if phase != PhaseGenerating && from != to {
		return &StateConflictError{FromExecution: from, ToExecution: to, Reason: "execution changes only while generating"}
	}
	if allowed[from][to] {
		if from == ExecutionDone && to == ExecutionQueued && phase != PhaseGenerating {
			return &StateConflictError{FromExecution: from, ToExecution: to, Reason: "completed execution can be queued again only while generating"}
		}
		return nil
	}
	return &StateConflictError{FromExecution: from, ToExecution: to, Reason: "execution transition is not allowed"}
}

type StateConflictError struct {
	FromPhase     Phase
	ToPhase       Phase
	FromExecution Execution
	ToExecution   Execution
	Reason        string
}

func (err *StateConflictError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v: %s", ErrStateConflict, err.Reason)
}
func (err *StateConflictError) Unwrap() error { return ErrStateConflict }

func stateConflict(task *Task, reason string) error {
	return &StateConflictError{FromPhase: task.Phase, FromExecution: task.Execution, Reason: reason}
}

func candidateByID(task *Task, id string) *Candidate {
	for index := range task.Candidates {
		if task.Candidates[index].ID == id {
			return &task.Candidates[index]
		}
	}
	return nil
}

func allCandidatesCompleted(task Task) bool {
	if len(task.Candidates) == 0 {
		return false
	}
	for _, candidate := range task.Candidates {
		if candidate.Status != CandidateCompleted {
			return false
		}
	}
	return true
}

func hasCandidateFailure(task Task) bool {
	for _, candidate := range task.Candidates {
		if candidate.Status == CandidateFailed {
			return true
		}
	}
	return false
}

func validateTask(task Task) error {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.ProjectID) == "" {
		return fmt.Errorf("comparison task identity is required")
	}
	if !task.Phase.Valid() || !task.Execution.Valid() {
		return fmt.Errorf("comparison task state is invalid")
	}
	if len(task.Candidates) < MinCandidates || len(task.Candidates) > MaxCandidates {
		return fmt.Errorf("comparison task candidate count must be between %d and %d", MinCandidates, MaxCandidates)
	}
	if task.Target.Path == "" || task.Target.BaseRevision == "" {
		return fmt.Errorf("comparison task target is incomplete")
	}
	return nil
}

func cloneTask(task Task) Task {
	raw, _ := json.Marshal(task)
	var copy Task
	_ = json.Unmarshal(raw, &copy)
	return copy
}

func cloneSnapshot(value *SnapshotRef) *SnapshotRef {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Fragments = append([]SnapshotFragment(nil), value.Fragments...)
	copy.Omitted = append([]OmittedRecord(nil), value.Omitted...)
	return &copy
}
func cloneComposition(value *Composition) *Composition {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneAppliedChange(value *AppliedChange) *AppliedChange {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneTaskError(value *TaskError) *TaskError {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneCandidateRevision(value *CandidateRevision) *CandidateRevision {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Usage = nil
	if value.Usage != nil {
		usage := *value.Usage
		copy.Usage = &usage
	}
	return &copy
}
