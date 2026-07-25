package domain

import (
	"bytes"
	"fmt"
	"time"
)

// ReviewIssueReferences are canonical source bytes resolved outside the pure
// domain package. Revised and verifier maps are keyed by re-verification ID.
type ReviewIssueReferences struct {
	Workspace           []byte
	Artifact            []byte
	CandidateSet        []byte
	Candidate           []byte
	SourceManifest      []byte
	Profile             []byte
	QualitySpec         []byte
	ReviewedContent     []byte
	LocationAnchor      []byte
	ReviewerSource      []byte
	FinalizationReceipt []byte
	TransitionContents  map[string][]byte
	RevisedContents     map[string][]byte
	VerifierSources     map[string][]byte
}

// ValidateReviewIssue verifies exact reviewed bytes, localizable evidence,
// exhaustive lifecycle history, and the re-verification closure boundary.
func ValidateReviewIssue(record ReviewIssue, references ReviewIssueReferences) error {
	if record.Contract.Kind != ReviewIssueContractKind || record.Contract.Version != ContractVersionV1 || record.Contract.Schema != "review-issue-v1.schema.json" {
		return reviewError(CodeContractKind, "contract", record.Contract, "expected exact ReviewIssue v1 contract")
	}
	if err := validateReviewVocabulary(record); err != nil {
		return err
	}
	if err := validateReviewBindings(record, references); err != nil {
		return err
	}
	if err := validateReviewLocationAndEvidence(record, references); err != nil {
		return err
	}
	if err := validateReviewAttachment(record, references); err != nil {
		return err
	}
	if err := validateReviewHistory(record, references); err != nil {
		return err
	}
	return validateReverificationHistory(record, references)
}

func validateReviewVocabulary(record ReviewIssue) error {
	switch record.Status {
	case ReviewOpen, ReviewRevisionProposed, ReviewResolved, ReviewVerifiedClosed, ReviewReopened, ReviewDismissed:
	default:
		return reviewError(CodeStateVocabulary, "status", record.Status, "unknown ReviewIssue status")
	}
	switch record.CapabilityRouting.CapabilityID {
	case "revision.fact", "revision.structure", "revision.scene", "revision.character", "revision.dialogue", "revision.pacing", "revision.language":
	default:
		return reviewError(CodeReferenceInvalid, "capability_routing.capability_id", record.CapabilityRouting.CapabilityID, "unknown Capability ID must be rejected explicitly")
	}
	if record.CapabilityRouting.ContractVersion != ContractVersionV1 || record.CapabilityRouting.UnknownCapabilityID != RejectExplicitly {
		return reviewError(CodeReferenceInvalid, "capability_routing", record.CapabilityRouting, "Capability routing must use exact v1 and reject unknown IDs")
	}
	switch record.Severity {
	case "blocking", "major", "moderate", "minor":
	default:
		return reviewError(CodeStateVocabulary, "severity", record.Severity, "unknown ReviewIssue severity")
	}
	switch record.Cause.Category {
	case "fact", "story_structure", "character", "scene", "information", "causality", "dialogue", "pacing", "language_style", "profile_fit":
	default:
		return reviewError(CodeStateVocabulary, "cause.category", record.Cause.Category, "unknown ReviewIssue cause")
	}
	switch record.RevisionLayer {
	case "fact", "structure", "scene", "character", "dialogue", "pacing", "language":
	default:
		return reviewError(CodeStateVocabulary, "revision_layer", record.RevisionLayer, "unknown revision layer")
	}
	if record.ReviewerOutputPolicy.Output != "evidence_and_findings_only" || record.ReviewerOutputPolicy.WriterChainOfThought != "forbidden" || record.ReviewerOutputPolicy.FormalMutationAuthority {
		return reviewError(CodeAuthorityViolation, "reviewer_output_policy", record.ReviewerOutputPolicy, "reviewers have evidence-only output and no formal mutation authority")
	}
	return nil
}

func validateReviewBindings(record ReviewIssue, references ReviewIssueReferences) error {
	checks := []struct {
		path string
		want string
		raw  []byte
	}{
		{"binding.workspace.hash", record.Binding.Workspace.Hash, references.Workspace},
		{"binding.artifact.hash", record.Binding.Artifact.Hash, references.Artifact},
		{"binding.candidate_set_hash", record.Binding.CandidateSetHash, references.CandidateSet},
		{"binding.candidate_content_hash", record.Binding.CandidateContentHash, references.Candidate},
		{"binding.source_manifest.hash", record.Binding.SourceManifest.Hash, references.SourceManifest},
		{"binding.profile.hash", record.Binding.Profile.Hash, references.Profile},
		{"binding.quality_spec.hash", record.Binding.QualitySpec.Hash, references.QualitySpec},
		{"binding.reviewed_content_hash", record.Binding.ReviewedContentHash, references.ReviewedContent},
		{"reviewer_provenance.source_hash", record.ReviewerProvenance.SourceHash, references.ReviewerSource},
	}
	for _, check := range checks {
		if actual := recordSHA256(check.raw); check.want != actual {
			return reviewError(CodeHashMismatch, check.path, map[string]string{"expected": check.want, "actual": actual}, "ReviewIssue binding does not match canonical bytes")
		}
	}
	if record.Binding.Artifact.Hash != record.Binding.ReviewedContentHash || record.Binding.CandidateContentHash != record.Binding.ReviewedContentHash {
		return reviewError(CodeBindingMismatch, "binding.reviewed_content_hash", record.Binding, "Artifact, candidate, and reviewed-content bindings must identify the same reviewed bytes")
	}
	return nil
}

func validateReviewLocationAndEvidence(record ReviewIssue, references ReviewIssueReferences) error {
	content := references.ReviewedContent
	if !validByteRange(record.Location.ByteRange, len(content)) {
		return reviewError(CodeRangeInvalid, "location.byte_range", record.Location.ByteRange, "issue location exceeds reviewed bytes")
	}
	quoted := content[record.Location.ByteRange.Start:record.Location.ByteRange.End]
	if record.Location.QuotedTextHash != recordSHA256(quoted) {
		return reviewError(CodeHashMismatch, "location.quoted_text_hash", record.Location.QuotedTextHash, "quoted hash must bind the exact located bytes")
	}
	if record.Location.AnchorHash != recordSHA256(references.LocationAnchor) {
		return reviewError(CodeHashMismatch, "location.anchor_hash", record.Location.AnchorHash, "anchor hash must bind caller-resolved canonical anchor bytes")
	}
	if record.ReaderEvidence.ObservableEffect == "" || record.ReaderEvidence.Summary == "" || len(record.ReaderEvidence.Excerpts) == 0 {
		return reviewError(CodeEvidenceInvalid, "reader_evidence", record.ReaderEvidence, "reader-observable prose evidence and excerpts are mandatory")
	}
	for index, excerpt := range record.ReaderEvidence.Excerpts {
		path := fmt.Sprintf("reader_evidence.excerpts[%d]", index)
		if !validByteRange(excerpt.Location, len(content)) {
			return reviewError(CodeRangeInvalid, path+".location", excerpt.Location, "evidence excerpt exceeds reviewed bytes")
		}
		actual := content[excerpt.Location.Start:excerpt.Location.End]
		if !bytes.Equal([]byte(excerpt.Quote), actual) {
			return reviewError(CodeEvidenceInvalid, path+".quote", excerpt.Quote, "excerpt quote must equal the exact reviewed byte range")
		}
		if excerpt.Hash != recordSHA256(actual) {
			return reviewError(CodeHashMismatch, path+".hash", excerpt.Hash, "excerpt hash must match exact reviewed bytes")
		}
	}
	if !validByteRange(record.Recommendation.AffectedRange, len(content)) || len(record.Recommendation.DimensionsToRecheck) == 0 || record.Recommendation.MinimumImpactChange == "" {
		return reviewError(CodeRangeInvalid, "recommendation", record.Recommendation, "recommendation must identify a bounded minimum-impact change")
	}
	return nil
}

func validateReviewAttachment(record ReviewIssue, references ReviewIssueReferences) error {
	attachment := record.Attachment
	switch attachment.Kind {
	case "candidate":
		if attachment.TargetID != record.Binding.CandidateID || attachment.TargetHash != recordSHA256(references.Candidate) || attachment.FinalizationReceiptID != "" || attachment.FinalizationReceiptHash != "" {
			return reviewError(CodeBindingMismatch, "attachment", attachment, "candidate attachment must bind the reviewed candidate only")
		}
	case "candidate_set":
		if attachment.TargetID != record.Binding.CandidateSetID || attachment.TargetHash != recordSHA256(references.CandidateSet) || attachment.FinalizationReceiptID != "" || attachment.FinalizationReceiptHash != "" {
			return reviewError(CodeBindingMismatch, "attachment", attachment, "CandidateSet attachment must bind the exact set only")
		}
	case "finalized_artifact":
		if attachment.TargetID != record.Binding.Artifact.ArtifactID || attachment.TargetHash != recordSHA256(references.Artifact) || attachment.FinalizationReceiptID == "" || attachment.FinalizationReceiptHash != recordSHA256(references.FinalizationReceipt) {
			return reviewError(CodeBindingMismatch, "attachment", attachment, "finalized attachment requires exact Artifact and Finalization receipt bindings")
		}
	default:
		return reviewError(CodeReferenceInvalid, "attachment.kind", attachment.Kind, "unknown attachment kind")
	}
	return nil
}

func validateReviewHistory(record ReviewIssue, references ReviewIssueReferences) error {
	if len(record.StatusHistory) == 0 || record.StatusHistory[0].From != nil || record.StatusHistory[0].To != ReviewOpen {
		return reviewError(CodeHistoryContinuity, "status_history[0]", record.StatusHistory, "ReviewIssue history must begin with creation to open")
	}
	seen := make(map[string]struct{}, len(record.StatusHistory))
	var previousAt time.Time
	for index, transition := range record.StatusHistory {
		path := fmt.Sprintf("status_history[%d]", index)
		transitionAt, err := time.Parse(time.RFC3339, transition.At)
		if err != nil || index > 0 && !transitionAt.After(previousAt) {
			return reviewError(CodeHistoryContinuity, path+".at", transition.At, "status history times must be valid and strictly increasing in append order")
		}
		previousAt = transitionAt
		content, exists := references.TransitionContents[transition.TransitionID]
		if !exists || transition.ReviewedContentHash != recordSHA256(content) {
			return reviewError(CodeHashMismatch, path+".reviewed_content_hash", transition.ReviewedContentHash, "status transition must bind caller-resolved reviewed or revised bytes")
		}
		if index == 0 && transition.ReviewedContentHash != record.Binding.ReviewedContentHash {
			return reviewError(CodeBindingMismatch, path+".reviewed_content_hash", transition.ReviewedContentHash, "creation transition must bind the originally reviewed bytes")
		}
		if _, duplicate := seen[transition.TransitionID]; duplicate {
			return reviewError(CodeDuplicateIdentity, path+".transition_id", transition.TransitionID, "status transition IDs must be unique")
		}
		seen[transition.TransitionID] = struct{}{}
		if index == 0 {
			continue
		}
		previous := record.StatusHistory[index-1].To
		if transition.From == nil || *transition.From != previous {
			return reviewError(CodeHistoryContinuity, path+".from", transition.From, "ReviewIssue history is not contiguous")
		}
		if !reviewTransitionAllowed(*transition.From, transition.To) {
			return reviewError(CodeStateTransition, path, transition, "illegal ReviewIssue transition")
		}
		if transition.To == ReviewDismissed && transition.Actor.ActorType != "author" && transition.Actor.ActorType != "review_lead" {
			return reviewError(CodeAuthorityViolation, path+".actor.actor_type", transition.Actor.ActorType, "dismissal requires author or review-lead authority")
		}
	}
	if record.StatusHistory[len(record.StatusHistory)-1].To != record.Status {
		return reviewError(CodeHistoryContinuity, "status", record.Status, "history must end at current status")
	}
	return nil
}

func reviewTransitionAllowed(from, to ReviewStatus) bool {
	switch from {
	case ReviewOpen:
		return to == ReviewRevisionProposed || to == ReviewDismissed
	case ReviewRevisionProposed:
		return to == ReviewResolved || to == ReviewOpen || to == ReviewDismissed
	case ReviewResolved:
		return to == ReviewVerifiedClosed || to == ReviewReopened
	case ReviewVerifiedClosed:
		return to == ReviewReopened
	case ReviewReopened:
		return to == ReviewRevisionProposed || to == ReviewDismissed
	case ReviewDismissed:
		return to == ReviewReopened
	}
	return false
}

func validateReverificationHistory(record ReviewIssue, references ReviewIssueReferences) error {
	seen := make(map[string]struct{}, len(record.ReverificationHistory))
	var previousAt time.Time
	for index, attempt := range record.ReverificationHistory {
		path := fmt.Sprintf("reverification_history[%d]", index)
		if _, duplicate := seen[attempt.AttemptID]; duplicate {
			return reviewError(CodeDuplicateIdentity, path+".attempt_id", attempt.AttemptID, "re-verification attempt IDs must be unique")
		}
		seen[attempt.AttemptID] = struct{}{}
		if attempt.Result != "passed" && attempt.Result != "failed" {
			return reviewError(CodeStateVocabulary, path+".result", attempt.Result, "unknown re-verification result")
		}
		revised, exists := references.RevisedContents[attempt.AttemptID]
		if !exists {
			return reviewError(CodeReferenceInvalid, path+".revised_content_hash", attempt.AttemptID, "revised canonical bytes are unavailable")
		}
		if attempt.RevisedContentHash != recordSHA256(revised) {
			return reviewError(CodeHashMismatch, path+".revised_content_hash", attempt.RevisedContentHash, "re-verification must bind exact revised bytes")
		}
		verifier, exists := references.VerifierSources[attempt.AttemptID]
		if !exists || attempt.VerifierProvenance.SourceHash != recordSHA256(verifier) || attempt.Evidence == "" {
			return reviewError(CodeEvidenceInvalid, path, attempt, "re-verification requires evidence and exact verifier provenance")
		}
		attemptAt, err := time.Parse(time.RFC3339, attempt.At)
		if err != nil || index > 0 && !attemptAt.After(previousAt) {
			return reviewError(CodeEvidenceInvalid, path+".at", attempt.At, "re-verification time must be RFC3339")
		}
		previousAt = attemptAt
		resolved, next, inResolvedRound := reviewAttemptRound(record, attemptAt)
		if !inResolvedRound || resolved.ReviewedContentHash != attempt.RevisedContentHash {
			return reviewError(CodeEvidenceInvalid, path, attempt, "re-verification must occur in the matching resolved-byte lifecycle round")
		}
		if attempt.Result == "failed" && (next == nil || next.To != ReviewReopened || next.ReviewedContentHash != attempt.RevisedContentHash) {
			return reviewError(CodeEvidenceInvalid, path, attempt, "failed re-verification must produce the next reopened transition for the same revised bytes")
		}
	}
	if record.Status == ReviewVerifiedClosed {
		if len(record.ReverificationHistory) == 0 {
			return reviewError(CodeEvidenceInvalid, "reverification_history", nil, "verified_closed requires a passed re-verification")
		}
		latest := record.ReverificationHistory[len(record.ReverificationHistory)-1]
		lastTransition := record.StatusHistory[len(record.StatusHistory)-1]
		resolved, exists := latestReviewTransition(record, ReviewResolved)
		resolvedAt, resolvedErr := time.Parse(time.RFC3339, resolved.At)
		attemptAt, attemptErr := time.Parse(time.RFC3339, latest.At)
		closedAt, closedErr := time.Parse(time.RFC3339, lastTransition.At)
		if latest.Result != "passed" || lastTransition.To != ReviewVerifiedClosed || lastTransition.ReviewedContentHash != latest.RevisedContentHash || !exists || resolved.ReviewedContentHash != latest.RevisedContentHash || resolvedErr != nil || attemptErr != nil || closedErr != nil || !attemptAt.After(resolvedAt) || closedAt.Before(attemptAt) {
			return reviewError(CodeEvidenceInvalid, "status", record.Status, "only passed re-verification against the revised bytes may close an issue")
		}
	}
	return nil
}

func reviewAttemptRound(record ReviewIssue, attemptAt time.Time) (ReviewStatusTransition, *ReviewStatusTransition, bool) {
	latestIndex := -1
	for index := range record.StatusHistory {
		transitionAt, err := time.Parse(time.RFC3339, record.StatusHistory[index].At)
		if err != nil || !transitionAt.Before(attemptAt) {
			break
		}
		latestIndex = index
	}
	if latestIndex < 0 || record.StatusHistory[latestIndex].To != ReviewResolved {
		return ReviewStatusTransition{}, nil, false
	}
	if latestIndex+1 == len(record.StatusHistory) {
		return record.StatusHistory[latestIndex], nil, true
	}
	next := record.StatusHistory[latestIndex+1]
	return record.StatusHistory[latestIndex], &next, true
}

func latestReviewTransition(record ReviewIssue, status ReviewStatus) (ReviewStatusTransition, bool) {
	for index := len(record.StatusHistory) - 1; index >= 0; index-- {
		if record.StatusHistory[index].To == status {
			return record.StatusHistory[index], true
		}
	}
	return ReviewStatusTransition{}, false
}

func reviewError(code ErrorCode, path string, value any, message string) error {
	return &ContractError{Code: code, Path: path, Value: value, Message: message}
}
