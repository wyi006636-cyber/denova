package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// CandidateSetReferences are canonical bytes supplied by an owning reader.
// The domain package never discovers disk paths or treats persisted hash text
// as proof of a binding.
type CandidateSetReferences struct {
	Workspace           []byte
	Artifact            []byte
	SourceManifest      []byte
	Profile             []byte
	QualitySpec         []byte
	CandidatePolicy     []byte
	Candidates          map[string][]byte
	Skills              map[string][]byte
	MixedOutput         []byte
	FinalizationReceipt []byte
}

// ValidateCandidateSet verifies exact bytes, exhaustive lifecycle history,
// author authority, comparison coverage, and mixed-output lineage.
func ValidateCandidateSet(record CandidateSet, references CandidateSetReferences) error {
	if record.Contract.Kind != CandidateSetContractKind || record.Contract.Version != ContractVersionV1 || record.Contract.Schema != "candidate-set-v1.schema.json" {
		return candidateError(CodeContractKind, "contract", record.Contract, "expected exact CandidateSet v1 contract")
	}
	if err := validateCandidateState(record.CurrentState); err != nil {
		return err
	}
	candidates, err := validateCandidateIdentities(record, references)
	if err != nil {
		return err
	}
	if err := validateCandidateReferenceHashes(record, references); err != nil {
		return err
	}
	if err := validateCandidateBindingChecks(record, references); err != nil {
		return err
	}
	if err := validateCandidateHistory(record); err != nil {
		return err
	}
	if err := validateCandidateEvaluation(record, candidates); err != nil {
		return err
	}
	if err := validateCandidateDecision(record, candidates); err != nil {
		return err
	}
	if err := validateMixedCandidateOutput(record, references, candidates); err != nil {
		return err
	}
	return validateCandidateHandoff(record, references, candidates)
}

func validateCandidateState(state CandidateState) error {
	switch state {
	case CandidateOpen, CandidateCompared, CandidateAuthorSelected, CandidateMixed, CandidateRejected, CandidateFinalized, CandidateArchived:
		return nil
	}
	return candidateError(CodeStateVocabulary, "current_state", state, "unknown CandidateSet state")
}

func validateCandidateIdentities(record CandidateSet, references CandidateSetReferences) (map[string]Candidate, error) {
	if len(record.Candidates) == 0 || record.CandidatePolicy.RequestedCount != len(record.Candidates) {
		return nil, candidateError(CodeReferenceInvalid, "candidate_policy.requested_count", record.CandidatePolicy.RequestedCount, "requested count must equal the complete candidate set")
	}
	result := make(map[string]Candidate, len(record.Candidates))
	for index, candidate := range record.Candidates {
		path := fmt.Sprintf("candidates[%d]", index)
		if _, duplicate := result[candidate.CandidateID]; duplicate {
			return nil, candidateError(CodeDuplicateIdentity, path+".candidate_id", candidate.CandidateID, "candidate IDs must be unique")
		}
		raw, exists := references.Candidates[candidate.CandidateID]
		if !exists {
			return nil, candidateError(CodeReferenceInvalid, path+".candidate_id", candidate.CandidateID, "candidate canonical bytes are unavailable")
		}
		hash := recordSHA256(raw)
		if candidate.ContentHash != hash || candidate.Artifact.Hash != hash {
			return nil, candidateError(CodeHashMismatch, path+".content_hash", candidate.ContentHash, "candidate and Artifact hashes must match canonical candidate bytes")
		}
		skill, exists := references.Skills[candidate.CandidateID]
		if !exists || candidate.Skill.Hash != recordSHA256(skill) {
			return nil, candidateError(CodeHashMismatch, path+".skill.hash", candidate.Skill.Hash, "candidate Skill hash must match canonical Skill bytes")
		}
		if candidate.SourceManifest != record.SourceManifest || candidate.Profile != record.Profile || candidate.QualitySpec != record.QualitySpec {
			return nil, candidateError(CodeBindingMismatch, path, candidate.CandidateID, "candidate bindings must equal the enclosing set bindings")
		}
		result[candidate.CandidateID] = candidate
	}
	return result, nil
}

func validateCandidateReferenceHashes(record CandidateSet, references CandidateSetReferences) error {
	checks := []struct {
		path string
		want string
		raw  []byte
	}{
		{"workspace.hash", record.Workspace.Hash, references.Workspace},
		{"artifact.hash", record.Artifact.Hash, references.Artifact},
		{"source_manifest.hash", record.SourceManifest.Hash, references.SourceManifest},
		{"profile.hash", record.Profile.Hash, references.Profile},
		{"quality_spec.hash", record.QualitySpec.Hash, references.QualitySpec},
	}
	for _, check := range checks {
		if actual := recordSHA256(check.raw); check.want != actual {
			return candidateError(CodeHashMismatch, check.path, map[string]string{"expected": check.want, "actual": actual}, "binding hash does not match canonical bytes")
		}
	}
	return nil
}

func validateCandidateBindingChecks(record CandidateSet, references CandidateSetReferences) error {
	expected := map[string][]string{
		"workspace":        {recordSHA256(references.Workspace)},
		"artifact":         {recordSHA256(references.Artifact)},
		"source_manifest":  {recordSHA256(references.SourceManifest)},
		"profile":          {recordSHA256(references.Profile)},
		"quality_spec":     {recordSHA256(references.QualitySpec)},
		"candidate_policy": {recordSHA256(references.CandidatePolicy)},
	}
	for _, candidate := range record.Candidates {
		expected["candidate"] = append(expected["candidate"], recordSHA256(references.Candidates[candidate.CandidateID]))
	}
	if record.FinalizationHandoff.Status == "handed_off" {
		expected["finalization_receipt"] = []string{recordSHA256(references.FinalizationReceipt)}
	}
	for kind := range expected {
		sort.Strings(expected[kind])
	}
	observed := make(map[string][]string, len(expected))
	for index, check := range record.BindingValidation {
		if check.Status != "valid" || check.ExpectedHash != check.ObservedHash {
			return candidateError(CodeBindingMismatch, fmt.Sprintf("binding_validation[%d]", index), check.Status, "stale or mismatched bindings cannot support managed CandidateSet use")
		}
		observed[check.BindingKind] = append(observed[check.BindingKind], check.ObservedHash)
	}
	for kind, hashes := range observed {
		sort.Strings(hashes)
		observed[kind] = hashes
	}
	if !hashGroupsEqual(expected, observed) {
		return candidateError(CodeBindingMismatch, "binding_validation", observed, "binding checks must cover every canonical referenced byte set exactly")
	}
	return nil
}

func hashGroupsEqual(first, second map[string][]string) bool {
	if len(first) != len(second) {
		return false
	}
	for kind, hashes := range first {
		other, exists := second[kind]
		if !exists || len(hashes) != len(other) {
			return false
		}
		for index := range hashes {
			if hashes[index] != other[index] {
				return false
			}
		}
	}
	return true
}

func validateCandidateHistory(record CandidateSet) error {
	if record.CurrentState == CandidateOpen {
		if len(record.TransitionHistory) != 0 {
			return candidateError(CodeHistoryContinuity, "transition_history", len(record.TransitionHistory), "open CandidateSet has no transitions")
		}
		return nil
	}
	if len(record.TransitionHistory) == 0 || record.TransitionHistory[0].From != CandidateOpen {
		return candidateError(CodeHistoryContinuity, "transition_history", record.TransitionHistory, "history must start at open")
	}
	seen := make(map[string]struct{}, len(record.TransitionHistory))
	for index, transition := range record.TransitionHistory {
		path := fmt.Sprintf("transition_history[%d]", index)
		if _, duplicate := seen[transition.TransitionID]; duplicate {
			return candidateError(CodeDuplicateIdentity, path+".transition_id", transition.TransitionID, "transition IDs must be unique")
		}
		seen[transition.TransitionID] = struct{}{}
		if index > 0 && record.TransitionHistory[index-1].To != transition.From {
			return candidateError(CodeHistoryContinuity, path+".from", transition.From, "transition history is not contiguous")
		}
		if !candidateTransitionAllowed(transition.From, transition.To) {
			return candidateError(CodeStateTransition, path, transition, "illegal CandidateSet transition")
		}
		if transition.From == CandidateCompared && (transition.To == CandidateAuthorSelected || transition.To == CandidateMixed || transition.To == CandidateRejected) && transition.Actor.ActorType != "author" {
			return candidateError(CodeAuthorityViolation, path+".actor.actor_type", transition.Actor.ActorType, "author decision transitions require explicit author authority")
		}
	}
	if record.TransitionHistory[len(record.TransitionHistory)-1].To != record.CurrentState {
		return candidateError(CodeHistoryContinuity, "current_state", record.CurrentState, "history must end at current state")
	}
	return nil
}

func candidateTransitionAllowed(from, to CandidateState) bool {
	switch from {
	case CandidateOpen:
		return to == CandidateCompared
	case CandidateCompared:
		return to == CandidateAuthorSelected || to == CandidateMixed || to == CandidateRejected
	case CandidateAuthorSelected, CandidateMixed:
		return to == CandidateFinalized || to == CandidateArchived
	case CandidateRejected, CandidateFinalized:
		return to == CandidateArchived
	case CandidateArchived:
		return false
	}
	return false
}

func validateCandidateEvaluation(record CandidateSet, candidates map[string]Candidate) error {
	if record.CurrentState == CandidateOpen {
		if record.Evaluation != nil {
			return candidateError(CodeEvidenceInvalid, "evaluation", record.Evaluation, "open CandidateSet cannot contain an evaluation")
		}
		return nil
	}
	if record.Evaluation == nil {
		return candidateError(CodeEvidenceInvalid, "evaluation", nil, "compared and later states require evaluation evidence")
	}
	seen := make(map[string]struct{}, len(record.Evaluation.CandidateEvaluations))
	for index, evaluation := range record.Evaluation.CandidateEvaluations {
		if _, exists := candidates[evaluation.CandidateID]; !exists || len(evaluation.Criteria) == 0 {
			return candidateError(CodeEvidenceInvalid, fmt.Sprintf("evaluation.candidate_evaluations[%d]", index), evaluation.CandidateID, "evaluation must name a candidate and reader-observable criteria")
		}
		if _, duplicate := seen[evaluation.CandidateID]; duplicate {
			return candidateError(CodeDuplicateIdentity, "evaluation.candidate_evaluations", evaluation.CandidateID, "candidate evaluation IDs must be unique")
		}
		seen[evaluation.CandidateID] = struct{}{}
		for _, criterion := range evaluation.Criteria {
			if criterion.ReaderObservableEvidence == "" || criterion.SourceRef == "" {
				return candidateError(CodeEvidenceInvalid, "evaluation.candidate_evaluations.criteria", criterion, "numeric scores cannot replace reader-observable evidence")
			}
		}
	}
	if len(seen) != len(candidates) {
		return candidateError(CodeEvidenceInvalid, "evaluation.candidate_evaluations", seen, "every candidate, including a single candidate, must be compared")
	}
	return nil
}

func validateCandidateDecision(record CandidateSet, candidates map[string]Candidate) error {
	requiresDecision := record.CurrentState == CandidateAuthorSelected || record.CurrentState == CandidateMixed || record.CurrentState == CandidateRejected || record.CurrentState == CandidateFinalized
	if record.CurrentState == CandidateArchived && len(record.TransitionHistory) != 0 {
		requiresDecision = record.TransitionHistory[len(record.TransitionHistory)-1].From != CandidateOpen && record.TransitionHistory[len(record.TransitionHistory)-1].From != CandidateCompared
	}
	if !requiresDecision {
		if record.AuthorDecision != nil {
			return candidateError(CodeAuthorityViolation, "author_decision", record.AuthorDecision, "decision is not valid before the author decision transition")
		}
		return nil
	}
	decision := record.AuthorDecision
	if decision == nil || decision.Actor.ActorType != "author" {
		return candidateError(CodeAuthorityViolation, "author_decision.actor.actor_type", decision, "only an explicit author action may create a decision")
	}
	for _, id := range decision.SelectedCandidateIDs {
		if _, exists := candidates[id]; !exists {
			return candidateError(CodeReferenceInvalid, "author_decision.selected_candidate_ids", id, "decision references an unknown candidate")
		}
	}
	switch decision.Kind {
	case "selected":
		if len(decision.SelectedCandidateIDs) != 1 || record.CurrentState == CandidateMixed || record.CurrentState == CandidateRejected {
			return candidateError(CodeAuthorityViolation, "author_decision", decision, "selected decision requires exactly one candidate and matching state")
		}
	case "mixed":
		if len(decision.SelectedCandidateIDs) < 2 || record.CurrentState == CandidateAuthorSelected || record.CurrentState == CandidateRejected {
			return candidateError(CodeAuthorityViolation, "author_decision", decision, "mixed decision requires at least two candidates and matching state")
		}
	case "rejected":
		if len(decision.SelectedCandidateIDs) != 0 || record.CurrentState != CandidateRejected && record.CurrentState != CandidateArchived {
			return candidateError(CodeAuthorityViolation, "author_decision", decision, "rejected decision selects no candidate and cannot finalize")
		}
	default:
		return candidateError(CodeAuthorityViolation, "author_decision.kind", decision.Kind, "unknown author decision kind")
	}
	if record.CurrentState == CandidateArchived {
		basis := archivedCandidateBasis(record)
		matches := basis == CandidateAuthorSelected && decision.Kind == "selected" ||
			basis == CandidateMixed && decision.Kind == "mixed" ||
			basis == CandidateRejected && decision.Kind == "rejected"
		if !matches {
			return candidateError(CodeAuthorityViolation, "author_decision.kind", decision.Kind, "archived decision must retain the lifecycle predecessor's authority semantics")
		}
	}
	return nil
}

func archivedCandidateBasis(record CandidateSet) CandidateState {
	if len(record.TransitionHistory) == 0 {
		return ""
	}
	basis := record.TransitionHistory[len(record.TransitionHistory)-1].From
	if basis != CandidateFinalized {
		return basis
	}
	for index := len(record.TransitionHistory) - 2; index >= 0; index-- {
		if record.TransitionHistory[index].To == CandidateFinalized {
			return record.TransitionHistory[index].From
		}
	}
	return ""
}

func validateMixedCandidateOutput(record CandidateSet, references CandidateSetReferences, candidates map[string]Candidate) error {
	if record.AuthorDecision == nil || record.AuthorDecision.Kind != "mixed" {
		if record.MixedOutput != nil {
			return candidateError(CodeLineageInvalid, "mixed_output", record.MixedOutput, "only a mixed author decision may carry mixed output")
		}
		return nil
	}
	if record.MixedOutput == nil || len(record.MixedOutput.Segments) == 0 {
		return candidateError(CodeLineageInvalid, "mixed_output", nil, "mixed decision requires ordered segment lineage")
	}
	if hash := recordSHA256(references.MixedOutput); record.MixedOutput.ContentHash != hash {
		return candidateError(CodeHashMismatch, "mixed_output.content_hash", record.MixedOutput.ContentHash, "mixed output hash does not match canonical composed bytes")
	}
	output := make([]byte, 0, len(references.MixedOutput))
	cursor := 0
	parentRanges := make(map[string][]ByteRange)
	usedParents := make(map[string]struct{})
	seenSegments := make(map[string]struct{})
	for index, segment := range record.MixedOutput.Segments {
		path := fmt.Sprintf("mixed_output.segments[%d]", index)
		if _, duplicate := seenSegments[segment.SegmentID]; duplicate {
			return candidateError(CodeDuplicateIdentity, path+".segment_id", segment.SegmentID, "segment IDs must be unique")
		}
		seenSegments[segment.SegmentID] = struct{}{}
		candidate, exists := candidates[segment.ParentCandidateID]
		parent := references.Candidates[segment.ParentCandidateID]
		if !exists || segment.ParentContentHash != candidate.ContentHash {
			return candidateError(CodeLineageInvalid, path+".parent_candidate_id", segment.ParentCandidateID, "segment parent is not bound to this CandidateSet")
		}
		if !validByteRange(segment.ParentRange, len(parent)) || segment.OutputRange.Start != cursor || segment.OutputRange.End-segment.OutputRange.Start != segment.ParentRange.End-segment.ParentRange.Start {
			return candidateError(CodeLineageInvalid, path, segment, "segment ranges must be valid, ordered, contiguous, and length preserving")
		}
		piece := parent[segment.ParentRange.Start:segment.ParentRange.End]
		if segment.SegmentContentHash != recordSHA256(piece) {
			return candidateError(CodeHashMismatch, path+".segment_content_hash", segment.SegmentContentHash, "segment hash does not match the exact parent byte range")
		}
		orderedRanges := parentRanges[segment.ParentCandidateID]
		if len(orderedRanges) != 0 && segment.ParentRange.Start < orderedRanges[len(orderedRanges)-1].End {
			return candidateError(CodeLineageInvalid, path+".parent_range", segment.ParentRange, "each parent's byte ranges must be ordered and non-overlapping")
		}
		for _, existing := range orderedRanges {
			if segment.ParentRange.Start < existing.End && existing.Start < segment.ParentRange.End {
				return candidateError(CodeLineageInvalid, path+".parent_range", segment.ParentRange, "parent byte ranges must not overlap")
			}
		}
		parentRanges[segment.ParentCandidateID] = append(parentRanges[segment.ParentCandidateID], segment.ParentRange)
		usedParents[segment.ParentCandidateID] = struct{}{}
		output = append(output, piece...)
		cursor = segment.OutputRange.End
	}
	if cursor != len(references.MixedOutput) || !bytes.Equal(output, references.MixedOutput) {
		return candidateError(CodeLineageInvalid, "mixed_output.segments", output, "segments must cover and recompose the complete output bytes")
	}
	selected := append([]string(nil), record.AuthorDecision.SelectedCandidateIDs...)
	sort.Strings(selected)
	parents := make([]string, 0, len(usedParents))
	for id := range usedParents {
		parents = append(parents, id)
	}
	sort.Strings(parents)
	if len(parents) < 2 || !stringSlicesEqual(selected, parents) {
		return candidateError(CodeLineageInvalid, "author_decision.selected_candidate_ids", selected, "mixed decision must name exactly the segment parents")
	}
	return nil
}

func validateCandidateHandoff(record CandidateSet, references CandidateSetReferences, candidates map[string]Candidate) error {
	handoff := record.FinalizationHandoff
	var selectedHash string
	if record.AuthorDecision != nil {
		switch record.AuthorDecision.Kind {
		case "selected":
			selectedHash = candidates[record.AuthorDecision.SelectedCandidateIDs[0]].ContentHash
		case "mixed":
			selectedHash = record.MixedOutput.ContentHash
		}
	}
	handoffState := record.CurrentState
	if handoffState == CandidateArchived && len(record.TransitionHistory) != 0 {
		handoffState = record.TransitionHistory[len(record.TransitionHistory)-1].From
	}
	switch handoffState {
	case CandidateOpen, CandidateCompared:
		if handoff.Status != "not_ready" || handoff.ContentHash != nil {
			return candidateError(CodeAuthorityViolation, "finalization_handoff", handoff, "pre-decision CandidateSet is not ready")
		}
	case CandidateAuthorSelected, CandidateMixed:
		if handoff.Status != "ready" || handoff.ContentHash == nil || *handoff.ContentHash != selectedHash {
			return candidateError(CodeBindingMismatch, "finalization_handoff", handoff, "ready handoff must bind selected or composed bytes")
		}
	case CandidateRejected:
		if handoff.Status != "not_eligible" || handoff.ContentHash != nil {
			return candidateError(CodeAuthorityViolation, "finalization_handoff", handoff, "rejected CandidateSet can never finalize")
		}
	case CandidateFinalized:
		if handoff.Status != "handed_off" || handoff.ContentHash == nil || *handoff.ContentHash != selectedHash || handoff.RequestID == "" || handoff.ReceiptID == "" || handoff.ReceiptHash != recordSHA256(references.FinalizationReceipt) {
			return candidateError(CodeBindingMismatch, "finalization_handoff", handoff, "finalized CandidateSet requires an exact receipt-bound handoff")
		}
	case CandidateArchived:
		return candidateError(CodeHistoryContinuity, "transition_history", record.TransitionHistory, "archived CandidateSet must retain a valid predecessor")
	}
	return nil
}

func validByteRange(value ByteRange, size int) bool {
	return value.Start >= 0 && value.End > value.Start && value.End <= size
}

func stringSlicesEqual(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func recordSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func candidateError(code ErrorCode, path string, value any, message string) error {
	return &ContractError{Code: code, Path: path, Value: value, Message: message}
}
