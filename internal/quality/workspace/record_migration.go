package workspace

import (
	"bytes"
	"errors"
	"fmt"

	"denova/internal/quality/domain"
)

var ErrNoAcceptedRecordMigration = errors.New("no accepted quality record migration path")

type RecordKind string

const (
	RecordKindCandidateSet     RecordKind = "candidate_set"
	RecordKindReviewIssue      RecordKind = "review_issue"
	RecordKindPreferenceSignal RecordKind = "preference_signal"
)

type RecordMigrationAction string

const (
	RecordMigrationNoOp        RecordMigrationAction = "no_op"
	RecordMigrationUnavailable RecordMigrationAction = "unavailable"
)

// RecordMigrationPreview is a zero-write inspection result. No conversion
// action exists until a superseding Accepted ADR defines one.
type RecordMigrationPreview struct {
	Kind          RecordKind
	SourceVersion string
	Action        RecordMigrationAction
	Reason        string
	WritesPlanned int
	raw           []byte
}

func (preview RecordMigrationPreview) RawBytes() []byte {
	return bytes.Clone(preview.raw)
}

type RecordMigrationPlanner struct {
	decoder *RecordDecoder
}

func NewRecordMigrationPlanner(decoder *RecordDecoder) (*RecordMigrationPlanner, error) {
	if decoder == nil {
		return nil, errors.New("record decoder is required")
	}
	return &RecordMigrationPlanner{decoder: decoder}, nil
}

func (planner *RecordMigrationPlanner) PreviewCandidateSet(raw []byte) (RecordMigrationPreview, error) {
	parsed, err := planner.decoder.ParseCandidateSet(raw)
	if err != nil {
		return RecordMigrationPreview{}, err
	}
	return newRecordMigrationPreview(RecordKindCandidateSet, parsed.contract.Version, parsed.CanManagedMutate(), raw), nil
}

func (planner *RecordMigrationPlanner) PreviewReviewIssue(raw []byte) (RecordMigrationPreview, error) {
	parsed, err := planner.decoder.ParseReviewIssue(raw)
	if err != nil {
		return RecordMigrationPreview{}, err
	}
	return newRecordMigrationPreview(RecordKindReviewIssue, parsed.contract.Version, parsed.CanManagedMutate(), raw), nil
}

func (planner *RecordMigrationPlanner) PreviewPreferenceSignal(raw []byte) (RecordMigrationPreview, error) {
	parsed, err := planner.decoder.ParsePreferenceSignal(raw)
	if err != nil {
		return RecordMigrationPreview{}, err
	}
	return newRecordMigrationPreview(RecordKindPreferenceSignal, parsed.contract.Version, parsed.CanManagedMutate(), raw), nil
}

func newRecordMigrationPreview(kind RecordKind, version string, exactV1 bool, raw []byte) RecordMigrationPreview {
	preview := RecordMigrationPreview{Kind: kind, SourceVersion: version, WritesPlanned: 0, raw: bytes.Clone(raw)}
	if exactV1 {
		preview.Action = RecordMigrationNoOp
		preview.Reason = "exact_v1"
		return preview
	}
	preview.Action = RecordMigrationUnavailable
	preview.Reason = "no_accepted_migration_path"
	return preview
}

// Execute accepts only an exact-v1 no-op. Unknown/newer versions always fail
// before any repository or filesystem write is possible.
func (planner *RecordMigrationPlanner) Execute(preview RecordMigrationPreview) error {
	if preview.WritesPlanned != 0 || preview.Action != RecordMigrationNoOp || preview.Reason != "exact_v1" || preview.SourceVersion != domain.ContractVersionV1 {
		return ErrNoAcceptedRecordMigration
	}
	managed := false
	var err error
	switch preview.Kind {
	case RecordKindCandidateSet:
		var parsed ParsedCandidateSet
		parsed, err = planner.decoder.ParseCandidateSet(preview.raw)
		managed = parsed.CanManagedMutate()
	case RecordKindReviewIssue:
		var parsed ParsedReviewIssue
		parsed, err = planner.decoder.ParseReviewIssue(preview.raw)
		managed = parsed.CanManagedMutate()
	case RecordKindPreferenceSignal:
		var parsed ParsedPreferenceSignal
		parsed, err = planner.decoder.ParsePreferenceSignal(preview.raw)
		managed = parsed.CanManagedMutate()
	default:
		return fmt.Errorf("%w: unknown record kind %q", ErrNoAcceptedRecordMigration, preview.Kind)
	}
	if err != nil || !managed {
		return fmt.Errorf("%w: preview bytes are no longer exact v1: %v", ErrNoAcceptedRecordMigration, err)
	}
	return nil
}
