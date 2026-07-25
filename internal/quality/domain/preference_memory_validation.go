package domain

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"time"
)

// PreferenceSignalReferences are canonical bytes and stable identities
// resolved by an author-facing service before a signal can be persisted.
type PreferenceSignalReferences struct {
	Workspace            []byte
	Profile              []byte
	QualitySpec          []byte
	Source               []byte
	ConfirmationEvidence []byte
	CandidateSetID       string
	CandidateID          string
	Candidate            []byte
	ComposedArtifactID   string
	ComposedArtifact     []byte
	ParentCandidateIDs   []string
	SegmentMap           []byte
	IssueID              string
	ReviewIssue          []byte
	OriginalArtifactID   string
	OriginalArtifact     []byte
	RewrittenArtifactID  string
	RewrittenArtifact    []byte
	RuleID               string
	Rule                 []byte
	ReplacementEvidence  []byte
	RevocationReason     []byte
}

// ValidatePreferenceSignal verifies the explicit-author boundary and every
// event-specific identity/hash combination before journal persistence.
func ValidatePreferenceSignal(signal PreferenceSignal, references PreferenceSignalReferences) error {
	if err := validatePreferenceSignalEnvelope(signal); err != nil {
		return err
	}
	if signal.Workspace.ContentHash != recordSHA256(references.Workspace) || signal.Provenance.Profile.Hash != recordSHA256(references.Profile) || signal.Provenance.QualitySpec.Hash != recordSHA256(references.QualitySpec) || signal.Provenance.ContentHash != recordSHA256(references.Source) {
		return preferenceError(CodeHashMismatch, "provenance", signal.Provenance, "workspace, Profile, QualitySpec, and source hashes must match canonical bytes")
	}
	if signal.Confirmation.EvidenceHash != recordSHA256(references.ConfirmationEvidence) {
		return preferenceError(CodeHashMismatch, "confirmation.evidence_hash", signal.Confirmation.EvidenceHash, "confirmation evidence hash does not match explicit author evidence")
	}
	return validatePreferenceEvent(signal, references)
}

func validatePreferenceSignalEnvelope(signal PreferenceSignal) error {
	if signal.Contract.Kind != PreferenceSignalContractKind || signal.Contract.Version != ContractVersionV1 || signal.Contract.SchemaID != "preference-memory-v1.schema.json" {
		return preferenceError(CodeContractKind, "contract", signal.Contract, "expected exact PreferenceSignal v1 contract")
	}
	if signal.Author.ActorType != "author" || signal.Author.ActorID == "" || signal.Author.ActorID != signal.Scope.AuthorID {
		return preferenceError(CodeAuthorityViolation, "author", signal.Author, "only an explicit author actor may create PreferenceMemory")
	}
	if signal.Workspace.WorkspaceID != signal.Scope.WorkspaceID || signal.Workspace.ProjectID != signal.Scope.ProjectID {
		return preferenceError(CodeBindingMismatch, "scope", signal.Scope, "scope must bind the same project and workspace record")
	}
	switch signal.Event {
	case "selection", "mixed_selection", "rejection", "author_rewrite", "rule_confirmation", "correction", "revocation":
	default:
		return preferenceError(CodeStateVocabulary, "event", signal.Event, "only seven explicit author events exist in v1")
	}
	switch signal.Scope.Kind {
	case "workspace", "project", "author":
	default:
		return preferenceError(CodeStateVocabulary, "scope.kind", signal.Scope.Kind, "unknown PreferenceMemory scope")
	}
	if !signal.Confirmation.Explicit || signal.Confirmation.Method != signal.Event {
		return preferenceError(CodeConfirmationRequired, "confirmation", signal.Confirmation, "event requires a matching explicit author confirmation")
	}
	confirmedAt, err := time.Parse(time.RFC3339, signal.Confirmation.ConfirmedAt)
	if err != nil {
		return preferenceError(CodeConfirmationRequired, "confirmation.confirmed_at", signal.Confirmation.ConfirmedAt, "confirmation time must be RFC3339")
	}
	recordedAt, err := time.Parse(time.RFC3339, signal.RecordedAt)
	if err != nil || recordedAt.Before(confirmedAt) {
		return preferenceError(CodeJournalInvalid, "recorded_at", signal.RecordedAt, "recorded time must be RFC3339")
	}
	if signal.Preference.Dimension == "" || signal.Preference.Value == "" || signal.Preference.Reason == "" || signal.Preference.Confidence < 0 || signal.Preference.Confidence > 1 {
		return preferenceError(CodeEvidenceInvalid, "preference", signal.Preference, "preference value, reason, and bounded confidence are required")
	}
	switch signal.Preference.Strength {
	case "weak", "normal", "strong":
	default:
		return preferenceError(CodeStateVocabulary, "preference.strength", signal.Preference.Strength, "unknown preference strength")
	}
	return validatePreferenceEventShape(signal)
}

func validatePreferenceEventShape(signal PreferenceSignal) error {
	reference := signal.EventReference
	emptyRelations := len(signal.SupersedesSignalIDs) == 0 && len(signal.RevokesSignalIDs) == 0
	switch signal.Event {
	case "selection":
		expected := PreferenceEventReference{Kind: reference.Kind, CandidateSetID: reference.CandidateSetID, CandidateID: reference.CandidateID, CandidateHash: reference.CandidateHash}
		if signal.Provenance.SourceKind != "candidate" || reference.Kind != "selection" || reference.CandidateSetID == "" || reference.CandidateID == "" || reference.CandidateHash == "" || !reflect.DeepEqual(reference, expected) || !emptyRelations {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "selection shape is not the closed v1 tagged union")
		}
	case "mixed_selection":
		expected := PreferenceEventReference{Kind: reference.Kind, CandidateSetID: reference.CandidateSetID, ComposedArtifactID: reference.ComposedArtifactID, ComposedHash: reference.ComposedHash, ParentCandidateIDs: reference.ParentCandidateIDs, SegmentMapHash: reference.SegmentMapHash}
		if signal.Provenance.SourceKind != "candidate_segment" || reference.Kind != "mixed_selection" || reference.CandidateSetID == "" || reference.ComposedArtifactID == "" || reference.ComposedHash == "" || reference.SegmentMapHash == "" || len(reference.ParentCandidateIDs) < 2 || hasDuplicateStrings(reference.ParentCandidateIDs) || !reflect.DeepEqual(reference, expected) || !emptyRelations {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "mixed selection shape is not the closed v1 tagged union")
		}
	case "rejection":
		candidateReference := PreferenceEventReference{Kind: reference.Kind, CandidateSetID: reference.CandidateSetID, CandidateID: reference.CandidateID, CandidateHash: reference.CandidateHash}
		issueReference := PreferenceEventReference{Kind: reference.Kind, IssueID: reference.IssueID, IssueHash: reference.IssueHash}
		candidate := signal.Provenance.SourceKind == "candidate" && reference.Kind == "candidate_rejection" && reference.CandidateSetID != "" && reference.CandidateID != "" && reference.CandidateHash != "" && reflect.DeepEqual(reference, candidateReference)
		issue := signal.Provenance.SourceKind == "review_issue" && reference.Kind == "issue_rejection" && reference.IssueID != "" && reference.IssueHash != "" && reflect.DeepEqual(reference, issueReference)
		if (!candidate && !issue) || !emptyRelations {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "rejection shape is not the closed v1 tagged union")
		}
	case "author_rewrite":
		expected := PreferenceEventReference{Kind: reference.Kind, OriginalArtifactID: reference.OriginalArtifactID, OriginalHash: reference.OriginalHash, RewrittenArtifactID: reference.RewrittenArtifactID, RewrittenHash: reference.RewrittenHash}
		if signal.Provenance.SourceKind != "author_rewrite" || reference.Kind != "author_rewrite" || reference.OriginalArtifactID == "" || reference.OriginalHash == "" || reference.RewrittenArtifactID == "" || reference.RewrittenHash == "" || !reflect.DeepEqual(reference, expected) || !emptyRelations {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "author rewrite shape is not the closed v1 tagged union")
		}
	case "rule_confirmation":
		expected := PreferenceEventReference{Kind: reference.Kind, RuleID: reference.RuleID, RuleHash: reference.RuleHash}
		if signal.Provenance.SourceKind != "author_rule" || reference.Kind != "rule" || reference.RuleID == "" || reference.RuleHash == "" || !reflect.DeepEqual(reference, expected) || !emptyRelations {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "rule confirmation shape is not the closed v1 tagged union")
		}
	case "correction":
		expected := PreferenceEventReference{Kind: reference.Kind, CorrectedSignalID: reference.CorrectedSignalID, ReplacementEvidenceHash: reference.ReplacementEvidenceHash}
		if !validCorrectionSourceKind(signal.Provenance.SourceKind) || reference.Kind != "correction" || reference.CorrectedSignalID == "" || reference.ReplacementEvidenceHash == "" || !reflect.DeepEqual(reference, expected) || len(signal.SupersedesSignalIDs) == 0 || hasDuplicateStrings(signal.SupersedesSignalIDs) || !containsString(signal.SupersedesSignalIDs, reference.CorrectedSignalID) || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "correction shape is not the closed v1 tagged union")
		}
	case "revocation":
		expected := PreferenceEventReference{Kind: reference.Kind, RevocationReasonHash: reference.RevocationReasonHash}
		if signal.Provenance.SourceKind != "author_revocation" || reference.Kind != "revocation" || reference.RevocationReasonHash == "" || !reflect.DeepEqual(reference, expected) || len(signal.RevokesSignalIDs) == 0 || hasDuplicateStrings(signal.RevokesSignalIDs) || len(signal.SupersedesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "revocation shape is not the closed v1 tagged union")
		}
	default:
		return preferenceError(CodeStateVocabulary, "event", signal.Event, "only seven explicit author events exist in v1")
	}
	return nil
}

func validCorrectionSourceKind(kind string) bool {
	switch kind {
	case "candidate", "candidate_segment", "review_issue", "author_rewrite", "author_rule", "author_revocation", "finalization_receipt":
		return true
	}
	return false
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func validatePreferenceEvent(signal PreferenceSignal, references PreferenceSignalReferences) error {
	reference := signal.EventReference
	switch signal.Event {
	case "selection":
		if signal.Provenance.SourceKind != "candidate" || !bytes.Equal(references.Source, references.Candidate) || reference.Kind != "selection" || reference.CandidateSetID != references.CandidateSetID || reference.CandidateID != references.CandidateID || reference.CandidateHash != recordSHA256(references.Candidate) || len(signal.SupersedesSignalIDs) != 0 || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "selection must bind one exact CandidateSet candidate")
		}
	case "mixed_selection":
		parents := append([]string(nil), reference.ParentCandidateIDs...)
		wantParents := append([]string(nil), references.ParentCandidateIDs...)
		sort.Strings(parents)
		sort.Strings(wantParents)
		if signal.Provenance.SourceKind != "candidate_segment" || !bytes.Equal(references.Source, references.ComposedArtifact) || reference.Kind != "mixed_selection" || reference.CandidateSetID != references.CandidateSetID || reference.ComposedArtifactID != references.ComposedArtifactID || reference.ComposedHash != recordSHA256(references.ComposedArtifact) || reference.SegmentMapHash != recordSHA256(references.SegmentMap) || len(parents) < 2 || !stringSlicesEqual(parents, wantParents) || len(signal.SupersedesSignalIDs) != 0 || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "mixed selection must bind composed bytes and every parent segment")
		}
	case "rejection":
		candidate := signal.Provenance.SourceKind == "candidate" && bytes.Equal(references.Source, references.Candidate) && reference.Kind == "candidate_rejection" && reference.CandidateSetID == references.CandidateSetID && reference.CandidateID == references.CandidateID && reference.CandidateHash == recordSHA256(references.Candidate)
		issue := signal.Provenance.SourceKind == "review_issue" && bytes.Equal(references.Source, references.ReviewIssue) && reference.Kind == "issue_rejection" && reference.IssueID == references.IssueID && reference.IssueHash == recordSHA256(references.ReviewIssue)
		if !candidate && !issue || len(signal.SupersedesSignalIDs) != 0 || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "rejection must bind one exact candidate or ReviewIssue")
		}
	case "author_rewrite":
		if signal.Provenance.SourceKind != "author_rewrite" || !bytes.Equal(references.Source, references.RewrittenArtifact) || reference.Kind != "author_rewrite" || reference.OriginalArtifactID != references.OriginalArtifactID || reference.OriginalHash != recordSHA256(references.OriginalArtifact) || reference.RewrittenArtifactID != references.RewrittenArtifactID || reference.RewrittenHash != recordSHA256(references.RewrittenArtifact) || len(signal.SupersedesSignalIDs) != 0 || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "author rewrite must bind exact before and after Artifact bytes")
		}
	case "rule_confirmation":
		if signal.Provenance.SourceKind != "author_rule" || !bytes.Equal(references.Source, references.Rule) || reference.Kind != "rule" || reference.RuleID != references.RuleID || reference.RuleHash != recordSHA256(references.Rule) || len(signal.SupersedesSignalIDs) != 0 || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "rule confirmation must bind one exact authored rule")
		}
	case "correction":
		if reference.Kind != "correction" || reference.CorrectedSignalID == "" || reference.ReplacementEvidenceHash != recordSHA256(references.ReplacementEvidence) || len(signal.SupersedesSignalIDs) == 0 || !containsString(signal.SupersedesSignalIDs, reference.CorrectedSignalID) || len(signal.RevokesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "correction must append replacement evidence and superseded targets")
		}
	case "revocation":
		if signal.Provenance.SourceKind != "author_revocation" || !bytes.Equal(references.Source, references.RevocationReason) || reference.Kind != "revocation" || reference.RevocationReasonHash != recordSHA256(references.RevocationReason) || len(signal.RevokesSignalIDs) == 0 || len(signal.SupersedesSignalIDs) != 0 {
			return preferenceError(CodeReferenceInvalid, "event_reference", reference, "revocation must append at least one target and an exact reason hash")
		}
	default:
		return preferenceError(CodeStateVocabulary, "event", signal.Event, "only seven explicit author events exist in v1")
	}
	return nil
}

// ValidatePreferenceJournal validates immutable append relationships without
// deleting, rewriting, or inferring any record.
func ValidatePreferenceJournal(signals []PreferenceSignal) error {
	byID := make(map[string]PreferenceSignal, len(signals))
	indexByID := make(map[string]int, len(signals))
	for index, signal := range signals {
		if err := validatePreferenceSignalEnvelope(signal); err != nil {
			return err
		}
		if _, duplicate := byID[signal.SignalID]; duplicate {
			return preferenceError(CodeDuplicateIdentity, fmt.Sprintf("signals[%d].signal_id", index), signal.SignalID, "PreferenceSignal IDs must be unique")
		}
		byID[signal.SignalID] = signal
		indexByID[signal.SignalID] = index
	}
	graph := make(map[string][]string, len(signals))
	for _, signal := range signals {
		graph[signal.SignalID] = append(append([]string(nil), signal.SupersedesSignalIDs...), signal.RevokesSignalIDs...)
	}
	if cycle := preferenceCycle(graph); cycle != "" {
		return preferenceError(CodeResolutionCycle, "signals", cycle, "PreferenceMemory reference graph contains a cycle")
	}
	for index, signal := range signals {
		targets := graph[signal.SignalID]
		if signal.Event == "revocation" && len(targets) == 0 {
			return preferenceError(CodeReferenceInvalid, fmt.Sprintf("signals[%d].revokes_signal_ids", index), nil, "revocation must name at least one target")
		}
		for _, targetID := range targets {
			target, exists := byID[targetID]
			if !exists {
				return preferenceError(CodeReferenceInvalid, fmt.Sprintf("signals[%d]", index), targetID, "correction or revocation target does not exist")
			}
			if target.Author.ActorID != signal.Author.ActorID || target.Scope.AuthorID != signal.Scope.AuthorID {
				return preferenceError(CodeAuthorityViolation, fmt.Sprintf("signals[%d]", index), targetID, "PreferenceMemory targets cannot cross authors")
			}
			if target.Scope.ProjectID != signal.Scope.ProjectID || target.Scope.WorkspaceID != signal.Scope.WorkspaceID {
				return preferenceError(CodeReferenceInvalid, fmt.Sprintf("signals[%d]", index), targetID, "PreferenceMemory targets cannot cross project/workspace identity")
			}
			if preferenceScopeRank(signal.Scope.Kind) < preferenceScopeRank(target.Scope.Kind) {
				signalAt, signalErr := time.Parse(time.RFC3339, signal.Confirmation.ConfirmedAt)
				targetAt, targetErr := time.Parse(time.RFC3339, target.Confirmation.ConfirmedAt)
				if signalErr != nil || targetErr != nil || signal.Confirmation.EvidenceHash == target.Confirmation.EvidenceHash || !signalAt.After(targetAt) {
					return preferenceError(CodeScopeExpansion, fmt.Sprintf("signals[%d].scope.kind", index), signal.Scope.Kind, "broader scope requires fresh explicit author confirmation")
				}
			}
			if indexByID[targetID] >= index {
				return preferenceError(CodeJournalInvalid, fmt.Sprintf("signals[%d]", index), targetID, "append-only relations must target an earlier journal record")
			}
		}
	}
	return nil
}

func preferenceCycle(graph map[string][]string) string {
	const (
		unseen = iota
		visiting
		done
	)
	state := make(map[string]int, len(graph))
	var visit func(string) string
	visit = func(id string) string {
		switch state[id] {
		case visiting:
			return id
		case done:
			return ""
		}
		state[id] = visiting
		for _, target := range graph[id] {
			if _, exists := graph[target]; !exists {
				continue
			}
			if cycle := visit(target); cycle != "" {
				return cycle
			}
		}
		state[id] = done
		return ""
	}
	ids := make([]string, 0, len(graph))
	for id := range graph {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if cycle := visit(id); cycle != "" {
			return cycle
		}
	}
	return ""
}

func preferenceScopeRank(kind string) int {
	switch kind {
	case "workspace":
		return 3
	case "project":
		return 2
	case "author":
		return 1
	}
	return 0
}

func preferenceError(code ErrorCode, path string, value any, message string) error {
	return &ContractError{Code: code, Path: path, Value: value, Message: message}
}
