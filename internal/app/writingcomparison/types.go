// Package writingcomparison owns the Project-scoped comparison-task domain.
//
// A comparison task is deliberately separate from ordinary writing sessions:
// its journal is the only durable source of task state and its candidate
// assets live below the Project Store, away from formal chapter files.
package writingcomparison

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"denova/internal/agents/skills"
)

const (
	MinCandidates = 2
	MaxCandidates = 4
)

type Phase string

const (
	PhaseConfiguring        Phase = "configuring"
	PhaseGenerating         Phase = "generating"
	PhasePartial            Phase = "partial"
	PhaseGenerated          Phase = "generated"
	PhaseSynthesis          Phase = "synthesis" // reserved for #27
	PhasePendingApplication Phase = "pending_application"
	PhaseApplied            Phase = "applied"
	PhaseArchived           Phase = "archived" // reserved for #23
)

func (p Phase) Valid() bool {
	switch p {
	case PhaseConfiguring, PhaseGenerating, PhasePartial, PhaseGenerated,
		PhaseSynthesis, PhasePendingApplication, PhaseApplied, PhaseArchived:
		return true
	default:
		return false
	}
}

type Execution string

const (
	ExecutionIdle       Execution = "idle"
	ExecutionQueued     Execution = "queued"
	ExecutionRunning    Execution = "running"
	ExecutionPaused     Execution = "paused"    // reserved for #23
	ExecutionCancelled  Execution = "cancelled" // reserved for #23
	ExecutionIncomplete Execution = "incomplete"
	ExecutionDone       Execution = "done"
)

func (e Execution) Valid() bool {
	switch e {
	case ExecutionIdle, ExecutionQueued, ExecutionRunning, ExecutionPaused,
		ExecutionCancelled, ExecutionIncomplete, ExecutionDone:
		return true
	default:
		return false
	}
}

type CandidateStatus string

const (
	CandidatePending   CandidateStatus = "pending"
	CandidateRunning   CandidateStatus = "running"
	CandidateCompleted CandidateStatus = "completed"
	CandidateFailed    CandidateStatus = "failed"
)

type TargetRef struct {
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
	BaseRevision  string `json:"base_revision"`
	ChapterCursor string `json:"chapter_cursor,omitempty"`
}

type ChapterTask struct {
	Text       string `json:"text"`
	Source     string `json:"source"`
	GoalAnchor string `json:"goal_anchor,omitempty"`
}

type ModelSnapshot struct {
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name,omitempty"`
	Model       string `json:"model"`
	Runtime     string `json:"runtime"`
	EndpointID  string `json:"endpoint_id,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
}

type WritingSourceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Version string `json:"content_version,omitempty"`
}

// UnmarshalJSON accepts both the numeric template content_version used by the
// public API and the string Skill directory revision used for Skills.
func (ref *WritingSourceRef) UnmarshalJSON(data []byte) error {
	type wire struct {
		Kind    string          `json:"kind"`
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Scope   string          `json:"scope"`
		Version json.RawMessage `json:"content_version"`
		Legacy  json.RawMessage `json:"version"`
	}
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	version := decoded.Version
	if len(version) == 0 {
		version = decoded.Legacy
	}
	parsed := ""
	if len(version) > 0 && string(version) != "null" {
		var text string
		if err := json.Unmarshal(version, &text); err == nil {
			parsed = strings.TrimSpace(text)
		} else {
			var number json.Number
			decoder := json.NewDecoder(strings.NewReader(string(version)))
			decoder.UseNumber()
			if err := decoder.Decode(&number); err != nil {
				return fmt.Errorf("invalid writing source version: %w", err)
			}
			integer, err := number.Int64()
			if err != nil {
				return fmt.Errorf("writing source version must be an integer: %w", err)
			}
			parsed = strconv.FormatInt(integer, 10)
		}
	}
	*ref = WritingSourceRef{Kind: decoded.Kind, ID: decoded.ID, Name: decoded.Name, Scope: decoded.Scope, Version: parsed}
	return nil
}

func (ref WritingSourceRef) identifier() string {
	if strings.TrimSpace(ref.ID) != "" {
		return strings.TrimSpace(ref.ID)
	}
	return strings.TrimSpace(ref.Name)
}

type WritingSourceSnapshot struct {
	Kind          string                  `json:"kind"`
	ID            string                  `json:"id"`
	Version       string                  `json:"version"`
	Scope         string                  `json:"scope"`
	Title         string                  `json:"title,omitempty"`
	ContentSHA256 string                  `json:"content_sha256"`
	Techniques    []WritingSourceSnapshot `json:"techniques,omitempty"` // reserved for #27
}

// WritingSourceResolver is intentionally tiny so T1's template catalog can
// be connected later without making this domain depend on that package.
type WritingSourceResolver interface {
	ResolveCandidateSource(ctx context.Context, ref WritingSourceRef) (WritingSourceSnapshot, error)
}

type CandidateRevision struct {
	Number      int           `json:"number"`
	Path        string        `json:"path"`
	SHA256      string        `json:"sha256"`
	Bytes       int           `json:"bytes"`
	InputSHA256 string        `json:"input_sha256"`
	Model       ModelSnapshot `json:"model"`
	CreatedAt   time.Time     `json:"created_at"`
	Usage       *Usage        `json:"usage,omitempty"`
	DurationMs  int64         `json:"duration_ms,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type Candidate struct {
	ID          string                `json:"id"`
	Position    int                   `json:"position"`
	Profile     ModelSnapshot         `json:"profile"`
	Source      WritingSourceSnapshot `json:"source"`
	Requirement string                `json:"requirement,omitempty"`
	Status      CandidateStatus       `json:"status"`
	Revision    *CandidateRevision    `json:"revision,omitempty"`
	RunID       string                `json:"run_id,omitempty"`
	Attempt     int                   `json:"attempt"` // reserved retry slot; first run is 1
	Error       *TaskError            `json:"error,omitempty"`
}

type SnapshotBudget struct {
	Bytes int `json:"bytes,omitempty"`
}

type SnapshotFragment struct {
	Source    string `json:"source"`
	Title     string `json:"title,omitempty"`
	Path      string `json:"path,omitempty"`
	Revision  string `json:"revision,omitempty"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
	AssetPath string `json:"asset_path"`
}

type OmittedRecord struct {
	Source string `json:"source"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
	Bytes  int    `json:"bytes,omitempty"`
}

type SnapshotRef struct {
	ID             string             `json:"id"`
	SHA256         string             `json:"sha256"`
	FrozenAt       time.Time          `json:"frozen_at"`
	TargetRevision string             `json:"target_revision"`
	Budget         SnapshotBudget     `json:"budget"`
	Fragments      []SnapshotFragment `json:"fragments,omitempty"`
	Omitted        []OmittedRecord    `json:"omitted,omitempty"`
}

type ExpectedCalls struct {
	Candidates  int `json:"candidates"`
	Reviews     int `json:"reviews"`      // reserved for #24
	Syntheses   int `json:"syntheses"`    // reserved for #27
	FinalWrites int `json:"final_writes"` // reserved for #28
}

type Composition struct {
	CandidateID string    `json:"candidate_id"`
	Revision    int       `json:"revision"`
	SHA256      string    `json:"sha256"`
	SelectedAt  time.Time `json:"selected_at"`
}

type AppliedChange struct {
	Path           string    `json:"path"`
	BaseRevision   string    `json:"base_revision"`
	ResultRevision string    `json:"result_revision"`
	ChangeGroupID  string    `json:"change_group_id"`
	ReviewThreadID string    `json:"review_thread_id"`
	AppliedAt      time.Time `json:"applied_at"`
}

type TaskError struct {
	Code      string `json:"code"`
	Class     string `json:"class,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	Message   string `json:"message,omitempty"`
}

type Task struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	Revision      uint64         `json:"revision"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Phase         Phase          `json:"phase"`
	Execution     Execution      `json:"execution"`
	Target        TargetRef      `json:"target"`
	ChapterTask   ChapterTask    `json:"chapter_task"`
	Candidates    []Candidate    `json:"candidates"`
	ReviewModel   ModelSnapshot  `json:"review_model"`
	FinalWriter   ModelSnapshot  `json:"final_writer"`
	ExpectedCalls ExpectedCalls  `json:"expected_calls"`
	Snapshot      *SnapshotRef   `json:"snapshot,omitempty"`
	Composition   *Composition   `json:"composition,omitempty"`
	Applied       *AppliedChange `json:"applied,omitempty"`
	LastError     *TaskError     `json:"last_error,omitempty"`
}

type TaskSummary struct {
	ID             string    `json:"id"`
	TargetPath     string    `json:"target_path"`
	Phase          Phase     `json:"phase"`
	Execution      Execution `json:"execution"`
	CandidateCount int       `json:"candidate_count"`
	Completed      int       `json:"completed"`
	Failed         int       `json:"failed"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (task Task) Summary() TaskSummary {
	summary := TaskSummary{ID: task.ID, TargetPath: task.Target.Path, Phase: task.Phase, Execution: task.Execution, CandidateCount: len(task.Candidates), UpdatedAt: task.UpdatedAt}
	for _, candidate := range task.Candidates {
		switch candidate.Status {
		case CandidateCompleted:
			summary.Completed++
		case CandidateFailed:
			summary.Failed++
		}
	}
	return summary
}

type CandidateInput struct {
	ProfileID   string           `json:"profile_id"`
	Source      WritingSourceRef `json:"source"`
	Requirement string           `json:"requirement"`
}

type CreateInput struct {
	ID                   string           `json:"id,omitempty"`
	TargetPath           string           `json:"target_path"`
	ChapterTask          ChapterTask      `json:"chapter_task"`
	Candidates           []CandidateInput `json:"candidates"`
	ReviewModelProfileID string           `json:"review_model_profile_id"`
	FinalWriterProfileID string           `json:"final_writer_profile_id"`
	ExpectedCalls        *ExpectedCalls   `json:"expected_calls,omitempty"`
}

type CandidateOptions struct {
	Profiles      []CandidateOption `json:"profiles"`
	MaxCandidates int               `json:"max_candidates"`
}

type CandidateOption struct {
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Runtime   string `json:"runtime"`
	Eligible  bool   `json:"eligible"`
	Reason    string `json:"reason,omitempty"`
}

type ListOptions struct {
	Phase          Phase
	IncludeApplied bool
}

type EventKind string

const (
	EventTaskCreated         EventKind = "task.created"
	EventSnapshotFrozen      EventKind = "snapshot.frozen"
	EventPhaseChanged        EventKind = "phase.changed"
	EventExecutionChanged    EventKind = "execution.changed"
	EventCandidateStarted    EventKind = "candidate.started"
	EventCandidateCompleted  EventKind = "candidate.completed"
	EventCandidateFailed     EventKind = "candidate.failed"
	EventSelectionSet        EventKind = "selection.set"
	EventApplicationApplied  EventKind = "application.applied"
	EventApplicationRejected EventKind = "application.rejected"
)

type Event struct {
	Kind          EventKind          `json:"kind"`
	FromPhase     Phase              `json:"from_phase,omitempty"`
	ToPhase       Phase              `json:"to_phase,omitempty"`
	FromExecution Execution          `json:"from_execution,omitempty"`
	ToExecution   Execution          `json:"to_execution,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	Candidate     *Candidate         `json:"candidate,omitempty"`
	CandidateID   string             `json:"candidate_id,omitempty"`
	Revision      *CandidateRevision `json:"revision,omitempty"`
	Error         *TaskError         `json:"error,omitempty"`
	Snapshot      *SnapshotRef       `json:"snapshot,omitempty"`
	Composition   *Composition       `json:"composition,omitempty"`
	Applied       *AppliedChange     `json:"applied,omitempty"`
	Task          *Task              `json:"task,omitempty"`
}

// ProfileCheckResult is used by both candidate-options and Create. It never
// contains credentials or endpoint URLs.
type ProfileCheckResult struct {
	Option   CandidateOption
	Snapshot ModelSnapshot
}

var (
	ErrNotFound         = errors.New("writing comparison task not found")
	ErrStateConflict    = errors.New("writing comparison state conflict")
	ErrRevisionConflict = errors.New("writing comparison revision conflict")
	ErrInvalidRequest   = errors.New("invalid writing comparison request")
	ErrAssetCorrupt     = errors.New("writing comparison asset is corrupt")
)

type Error struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	if err.Message != "" {
		return err.Message
	}
	return err.Code
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func domainError(code, message string, cause error, details map[string]any) error {
	return &Error{Code: code, Message: message, Cause: cause, Details: details}
}

// NewSkillSourceResolver creates a resolver over the existing Skill catalog.
// It intentionally exposes only metadata and content hashes to the task.
func NewSkillSourceResolver(dirs []skills.Directory) WritingSourceResolver {
	return &skillSourceResolver{dirs: dirs}
}

type skillSourceResolver struct{ dirs []skills.Directory }

func (resolver *skillSourceResolver) ResolveCandidateSource(ctx context.Context, ref WritingSourceRef) (WritingSourceSnapshot, error) {
	if resolver == nil {
		return WritingSourceSnapshot{}, domainError("skill_missing", "Skill directory is unavailable", nil, nil)
	}
	if !strings.EqualFold(strings.TrimSpace(ref.Kind), "skill") {
		return WritingSourceSnapshot{}, domainError("template_catalog_unavailable", "Template catalog is unavailable", nil, nil)
	}
	name := ref.identifier()
	if name == "" {
		return WritingSourceSnapshot{}, domainError("skill_missing", "Skill name is required", nil, nil)
	}
	scopes := []skills.Scope{}
	if strings.TrimSpace(ref.Scope) != "" {
		scopes = append(scopes, skills.Scope(strings.TrimSpace(ref.Scope)))
	} else {
		scopes = append(scopes, skills.ScopeBuiltin, skills.ScopeUser, skills.ScopeWorkspace)
	}
	var lastErr error
	for _, scope := range scopes {
		document, err := skills.ReadDocument(ctx, resolver.dirs, scope, name)
		if err != nil {
			lastErr = err
			continue
		}
		if strings.TrimSpace(document.Content) == "" {
			return WritingSourceSnapshot{}, domainError("skill_not_usable", "Skill content is empty", lastErr, nil)
		}
		if wanted := strings.TrimSpace(ref.Version); wanted != "" && wanted != document.Revision {
			return WritingSourceSnapshot{}, domainError("skill_version_changed", "Skill version changed", nil, map[string]any{"expected": wanted, "actual": document.Revision})
		}
		return WritingSourceSnapshot{
			Kind: "skill", ID: name, Version: document.Revision, Scope: string(scope),
			Title: document.Name, ContentSHA256: sha256Text(document.Content),
		}, nil
	}
	return WritingSourceSnapshot{}, domainError("skill_missing", "Skill was not found", lastErr, map[string]any{"name": name})
}

func sha256Text(value string) string {
	return fmt.Sprintf("%x", sha256Bytes([]byte(value)))
}

func sha256Bytes(value []byte) [32]byte {
	// Kept here rather than exposing a general hash helper from the domain.
	// The fixed SHA-256 is part of the persisted asset contract.
	return sha256.Sum256(value)
}
