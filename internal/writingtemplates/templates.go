// Package writingtemplates owns the user-level writing instruction catalog.
// Templates are deliberately independent from Skills: a template is model
// visible prose, while a Skill is a workflow with tools and references.
package writingtemplates

import (
	"context"
	"errors"
	"strings"

	"denova/internal/agents/prompts"
)

const SchemaVersion = 1

const (
	KindTemplate  = "template"
	KindTechnique = "technique"

	TaskChapterDraft      = "chapter_full_draft"
	TaskChapterOpening    = "chapter_opening"
	TaskLocalRewrite      = "local_rewrite"
	TaskConsistencyReview = "consistency_review"
	TaskComposition       = "composition"
	TaskPlanning          = "planning"
)

var (
	ErrNotFound              = errors.New("writing template not found")
	ErrCategoryMissing       = errors.New("writing template category missing")
	ErrNotUsableForCandidate = errors.New("writing template cannot drive a candidate")
	ErrVersionRequired       = errors.New("writing template content version is required")
	ErrVersionChanged        = errors.New("writing template content version changed")
	ErrRevisionConflict      = errors.New("writing template changed elsewhere; reload before saving")
	ErrDataRootRequired      = errors.New("writing template data root is required")
)

// Localized contains the two product locales. Template bodies are English
// instructions; quoted Chinese terms, word lists, and example sentences are
// allowed as data because the model writes Chinese prose.
type Localized struct {
	ZH string `json:"zh-CN"`
	EN string `json:"en-US"`
}

// Template is a versioned Denova writing instruction resource.
//
// Kind only answers whether one candidate can independently produce a complete
// chapter. "template" can; "technique" covers every other consumer, including
// prose techniques, review checklists, planning instructions, and composition
// instructions. This closes the inventory's gap without adding another kind.
// The comparison task supplies context reference ranges, output contract, and
// quality checks. They are intentionally not repeated as context_refs,
// output_schema, or quality_checks fields: the frozen candidate snapshot owns
// context, candidate_contract owns output shape, and the body plus review/lint
// steps own quality checks. Tags and deprecated/supersedes metadata are also
// intentionally absent; Tasks/RecommendedFor/search cover the current need.
type Template struct {
	Version        int       `json:"version"`
	ID             string    `json:"id"`
	ContentVersion int       `json:"content_version"`
	Kind           string    `json:"kind"`
	Title          Localized `json:"title"`
	Summary        Localized `json:"summary"`
	Category       []string  `json:"category"`
	Tasks          []string  `json:"tasks"`
	// Body is English. Quoted Chinese terms, word lists, and example sentences
	// may appear as data; the instruction itself stays in English.
	Body           string   `json:"body,omitempty"`
	Preview        string   `json:"preview,omitempty"`
	RecommendedFor []string `json:"recommended_for,omitempty"`

	// The fields below are derived from the catalog file and are never written
	// back. Revision is the CAS hash of the canonical JSON bytes.
	Source            string `json:"source,omitempty"`
	Revision          string `json:"revision,omitempty"`
	Path              string `json:"path,omitempty"`
	Invalid           bool   `json:"invalid,omitempty"`
	Error             string `json:"error,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
	BuiltinOverridden bool   `json:"builtin_overridden,omitempty"`
	// BuiltinContentVersion is derived for a user override so clients can tell
	// that the release copy has advanced without replacing the author's text.
	BuiltinContentVersion int `json:"builtin_content_version,omitempty"`
}

// Category is one node in the flat, at-most-two-level category table.
type Category struct {
	ID       string    `json:"id"`
	Parent   string    `json:"parent,omitempty"`
	Title    Localized `json:"title"`
	Order    int       `json:"order"`
	TaskHint string    `json:"task_hint,omitempty"`
}

// ListFilter controls catalog listing. Empty fields do not filter.
type ListFilter struct {
	Kind     string
	Task     string
	Category string
	Query    string
}

// Snapshot is the immutable source a candidate freezes before generation.
type Snapshot struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Scope and Source are both retained for consumers that call the field by
	// either vocabulary. They always carry builtin or user and are identical.
	Scope          string    `json:"scope"`
	Source         string    `json:"source"`
	ContentVersion int       `json:"content_version"`
	Revision       string    `json:"revision"`
	Title          Localized `json:"title"`
	Body           string    `json:"body"`
}

type PreviewSegment struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type PreviewResponse struct {
	Segments []PreviewSegment `json:"segments"`
}

const CandidateContractText = prompts.CandidateContractText
const TargetHintText = prompts.TargetHintText

// ResolveForCandidate freezes a complete-chapter template at one content
// version. It is the only resolver intended for candidate execution.
func (c *Catalog) ResolveForCandidate(ctx context.Context, id string, contentVersion int) (Snapshot, error) {
	if err := contextErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if contentVersion <= 0 {
		return Snapshot{}, ErrVersionRequired
	}
	item, err := c.Get(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if item.Kind != KindTemplate || !hasTask(item.Tasks, TaskChapterDraft) && !hasTask(item.Tasks, TaskChapterOpening) {
		return Snapshot{}, ErrNotUsableForCandidate
	}
	if contentVersion != item.ContentVersion {
		return Snapshot{}, ErrVersionChanged
	}
	return Snapshot{
		ID: item.ID, Kind: item.Kind, Scope: item.Source, Source: item.Source, ContentVersion: item.ContentVersion,
		Revision: item.Revision, Title: item.Title, Body: item.Body,
	}, nil
}

func (c *Catalog) Preview(ctx context.Context, id string, targetHint string) (PreviewResponse, error) {
	if err := contextErr(ctx); err != nil {
		return PreviewResponse{}, err
	}
	item, err := c.Get(ctx, id)
	if err != nil {
		return PreviewResponse{}, err
	}
	if strings.TrimSpace(targetHint) == "" {
		targetHint = TargetHintText
	}
	return PreviewResponse{Segments: []PreviewSegment{
		{Kind: "candidate_contract", Text: CandidateContractText},
		{Kind: "template_body", Text: item.Body},
		{Kind: "target_hint", Text: targetHint},
	}}, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func hasTask(tasks []string, target string) bool {
	for _, task := range tasks {
		if task == target {
			return true
		}
	}
	return false
}
