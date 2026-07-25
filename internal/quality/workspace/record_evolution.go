package workspace

import (
	"fmt"
	"reflect"

	"denova/internal/quality/domain"
)

// CandidateSet identity, generated candidates, bindings, and existing
// lifecycle evidence are immutable. Updates may only append valid lifecycle
// state and fill the corresponding decision/handoff fields.
func validateCandidateSetEvolution(current, next domain.CandidateSet) error {
	if current.CurrentState == domain.CandidateArchived {
		return fmt.Errorf("%w: archived CandidateSet is terminal", ErrRecordEvolution)
	}
	stable := reflect.DeepEqual(current.Contract, next.Contract) &&
		current.CandidateSetID == next.CandidateSetID &&
		reflect.DeepEqual(current.Workspace, next.Workspace) &&
		reflect.DeepEqual(current.Run, next.Run) &&
		reflect.DeepEqual(current.Stage, next.Stage) &&
		reflect.DeepEqual(current.Artifact, next.Artifact) &&
		reflect.DeepEqual(current.SourceManifest, next.SourceManifest) &&
		reflect.DeepEqual(current.Profile, next.Profile) &&
		reflect.DeepEqual(current.QualitySpec, next.QualitySpec) &&
		reflect.DeepEqual(current.CandidatePolicy, next.CandidatePolicy) &&
		reflect.DeepEqual(current.Candidates, next.Candidates)
	if !stable {
		return fmt.Errorf("%w: CandidateSet identity, candidates, or binding evidence changed", ErrRecordEvolution)
	}
	if !candidateBindingEvolutionAllowed(current, next) {
		return fmt.Errorf("%w: CandidateSet binding evidence was rewritten or appended outside finalization", ErrRecordEvolution)
	}
	if !slicePrefixEqual(current.TransitionHistory, next.TransitionHistory) {
		return fmt.Errorf("%w: CandidateSet transition history is not an exact prefix", ErrRecordEvolution)
	}
	if current.Evaluation != nil && !reflect.DeepEqual(current.Evaluation, next.Evaluation) {
		return fmt.Errorf("%w: CandidateSet evaluation was rewritten", ErrRecordEvolution)
	}
	if current.AuthorDecision != nil && !reflect.DeepEqual(current.AuthorDecision, next.AuthorDecision) {
		return fmt.Errorf("%w: CandidateSet author decision was rewritten", ErrRecordEvolution)
	}
	if current.MixedOutput != nil && !reflect.DeepEqual(current.MixedOutput, next.MixedOutput) {
		return fmt.Errorf("%w: CandidateSet mixed lineage was rewritten", ErrRecordEvolution)
	}
	if current.CurrentState == domain.CandidateFinalized && !reflect.DeepEqual(current.FinalizationHandoff, next.FinalizationHandoff) {
		return fmt.Errorf("%w: finalized CandidateSet handoff receipt was rewritten", ErrRecordEvolution)
	}
	return nil
}

func candidateBindingEvolutionAllowed(current, next domain.CandidateSet) bool {
	if !slicePrefixEqual(current.BindingValidation, next.BindingValidation) {
		return false
	}
	added := next.BindingValidation[len(current.BindingValidation):]
	if len(added) == 0 {
		return true
	}
	return len(added) == 1 &&
		added[0].BindingKind == "finalization_receipt" &&
		next.CurrentState == domain.CandidateFinalized &&
		(current.CurrentState == domain.CandidateAuthorSelected || current.CurrentState == domain.CandidateMixed)
}

// ReviewIssue identity and existing lifecycle/re-verification attempts are
// immutable. Findings and recommendations may be refined by later valid
// ReviewIssue updates, but prior status evidence cannot be rewritten.
func validateReviewIssueEvolution(current, next domain.ReviewIssue) error {
	stable := reflect.DeepEqual(current.Contract, next.Contract) &&
		current.IssueID == next.IssueID &&
		reflect.DeepEqual(current.CapabilityRouting, next.CapabilityRouting) &&
		reflect.DeepEqual(current.Binding, next.Binding) &&
		reflect.DeepEqual(current.Attachment, next.Attachment) &&
		reflect.DeepEqual(current.Location, next.Location) &&
		reflect.DeepEqual(current.ReviewerProvenance, next.ReviewerProvenance) &&
		reflect.DeepEqual(current.ReviewerOutputPolicy, next.ReviewerOutputPolicy)
	if !stable {
		return fmt.Errorf("%w: ReviewIssue identity or reviewed-byte binding changed", ErrRecordEvolution)
	}
	if !slicePrefixEqual(current.StatusHistory, next.StatusHistory) {
		return fmt.Errorf("%w: ReviewIssue status history is not an exact prefix", ErrRecordEvolution)
	}
	if !slicePrefixEqual(current.ReverificationHistory, next.ReverificationHistory) {
		return fmt.Errorf("%w: ReviewIssue re-verification history is not an exact prefix", ErrRecordEvolution)
	}
	return nil
}

func slicePrefixEqual[T any](prefix, whole []T) bool {
	return len(whole) >= len(prefix) && reflect.DeepEqual(prefix, whole[:len(prefix)])
}
