package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"denova/internal/quality/domain"
)

func TestArtifactRepositoryPathsArePortableIDDerivedAndContained(t *testing.T) {
	path, err := CandidateSetRelativePath("candidate-set-001")
	if err != nil || path != ".denova/quality/artifacts/candidate-sets/candidate-set-001.json" {
		t.Fatalf("CandidateSetRelativePath=%q err=%v", path, err)
	}
	path, err = ReviewIssueRelativePath("review-issue-001")
	if err != nil || path != ".denova/quality/artifacts/review-issues/review-issue-001.json" {
		t.Fatalf("ReviewIssueRelativePath=%q err=%v", path, err)
	}
	for _, id := range []string{"../escape", "a:b", "CON", "two/segments", ""} {
		if _, err := CandidateSetRelativePath(id); err == nil {
			t.Fatalf("unsafe ID %q produced a repository path", id)
		}
	}
}

func TestCandidateSetRepositoryCreateReadListAndCASUpdate(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	raw := candidateRepositoryRaw(t, provider.candidate)

	created, err := repository.Create(context.Background(), raw)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.RelativePath != ".denova/quality/artifacts/candidate-sets/candidate-set-001.json" || created.RawSHA256 != recordSHA256(raw) || created.WorkspaceRevision != 1 {
		t.Fatalf("created snapshot=%#v", created)
	}
	if _, err := repository.Create(context.Background(), raw); !errors.Is(err, ErrRecordExists) {
		t.Fatalf("duplicate Create error=%v, want ErrRecordExists", err)
	}

	read, err := repository.Read(context.Background(), "candidate-set-001")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(read.Parsed.RawBytes(), raw) || read.RawSHA256 != created.RawSHA256 {
		t.Fatalf("read snapshot changed raw bytes: %#v", read)
	}
	ids, err := repository.List(context.Background())
	if err != nil || !reflect.DeepEqual(ids, []string{"candidate-set-001"}) {
		t.Fatalf("List=%v err=%v", ids, err)
	}

	var updatedDocument map[string]any
	if err := json.Unmarshal(raw, &updatedDocument); err != nil {
		t.Fatal(err)
	}
	updatedDocument["current_state"] = "compared"
	updatedDocument["transition_history"] = []any{map[string]any{
		"transition_id": "transition-001", "from": "open", "to": "compared",
		"actor":  map[string]any{"actor_id": "reviewer-001", "actor_type": "reviewer"},
		"reason": "comparison complete", "at": "2026-07-21T00:00:00Z",
	}}
	updatedDocument["evaluation"] = map[string]any{
		"evaluation_id": "evaluation-001", "actor": map[string]any{"actor_id": "reviewer-001", "actor_type": "reviewer"}, "at": "2026-07-21T00:00:00Z",
		"candidate_evaluations": []any{map[string]any{
			"candidate_id": "candidate-001", "criteria": []any{map[string]any{
				"criterion_id": "criterion-001", "reader_observable_evidence": "clear opening", "source_ref": "artifact-001",
			}},
		}},
		"summary": "single candidate compared",
	}
	updatedRaw := marshalRecordFixture(t, updatedDocument)
	updated, err := repository.Update(context.Background(), "candidate-set-001", updatedRaw, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: created.WorkspaceRevision})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.RawSHA256 == created.RawSHA256 {
		t.Fatal("CAS update did not publish new complete bytes")
	}

	_, err = repository.Update(context.Background(), "candidate-set-001", raw, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("stale CAS error=%v, want ErrRecordConflict", err)
	}
	current, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(created.RelativePath)))
	if err != nil || !bytes.Equal(current, updatedRaw) {
		t.Fatalf("stale CAS changed current bytes=%q err=%v", current, err)
	}

	var rewritten map[string]any
	if err := json.Unmarshal(updatedRaw, &rewritten); err != nil {
		t.Fatal(err)
	}
	rewritten["transition_history"].([]any)[0].(map[string]any)["transition_id"] = "rewritten-history"
	_, err = repository.Update(context.Background(), "candidate-set-001", marshalRecordFixture(t, rewritten), ArtifactUpdateExpectation{RawSHA256: updated.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordEvolution) {
		t.Fatalf("history rewrite error=%v, want ErrRecordEvolution", err)
	}

	var selectedDocument map[string]any
	if err := json.Unmarshal(updatedRaw, &selectedDocument); err != nil {
		t.Fatal(err)
	}
	selectedDocument["current_state"] = "author_selected"
	selectedDocument["transition_history"] = append(selectedDocument["transition_history"].([]any), map[string]any{
		"transition_id": "transition-002", "from": "compared", "to": "author_selected",
		"actor":  map[string]any{"actor_id": "author-001", "actor_type": "author"},
		"reason": "author selected candidate", "at": "2026-07-21T00:00:01Z",
	})
	selectedDocument["author_decision"] = map[string]any{
		"decision_id": "decision-001", "kind": "selected",
		"actor": map[string]any{"actor_id": "author-001", "actor_type": "author"},
		"at":    "2026-07-21T00:00:01Z", "reason": "selected", "selected_candidate_ids": []any{"candidate-001"},
	}
	candidateHash := recordSHA256(provider.candidate.Candidates["candidate-001"])
	selectedDocument["finalization_handoff"] = map[string]any{"status": "ready", "content_hash": candidateHash}
	selectedRaw := marshalRecordFixture(t, selectedDocument)
	selected, err := repository.Update(context.Background(), "candidate-set-001", selectedRaw, ArtifactUpdateExpectation{RawSHA256: updated.RawSHA256, WorkspaceRevision: 1})
	if err != nil {
		t.Fatalf("Update selected: %v", err)
	}

	provider.candidate.FinalizationReceipt = []byte("finalization-receipt")
	var finalizedDocument map[string]any
	if err := json.Unmarshal(selectedRaw, &finalizedDocument); err != nil {
		t.Fatal(err)
	}
	finalizedDocument["current_state"] = "finalized"
	finalizedDocument["transition_history"] = append(finalizedDocument["transition_history"].([]any), map[string]any{
		"transition_id": "transition-003", "from": "author_selected", "to": "finalized",
		"actor":  map[string]any{"actor_id": "system-001", "actor_type": "system"},
		"reason": "receipt verified", "at": "2026-07-21T00:00:02Z",
	})
	receiptHash := recordSHA256(provider.candidate.FinalizationReceipt)
	finalizedDocument["binding_validation"] = append(finalizedDocument["binding_validation"].([]any), map[string]any{
		"binding_kind": "finalization_receipt", "expected_hash": receiptHash, "observed_hash": receiptHash,
		"status": "valid", "checked_at": "2026-07-21T00:00:02Z",
	})
	finalizedDocument["finalization_handoff"] = map[string]any{
		"status": "handed_off", "content_hash": candidateHash, "request_id": "request-001",
		"receipt_id": "receipt-001", "receipt_hash": receiptHash,
	}
	finalizedRaw := marshalRecordFixture(t, finalizedDocument)
	finalized, err := repository.Update(context.Background(), "candidate-set-001", finalizedRaw, ArtifactUpdateExpectation{RawSHA256: selected.RawSHA256, WorkspaceRevision: 1})
	if err != nil {
		t.Fatalf("Update finalized with appended receipt binding: %v", err)
	}
	finalizedDocument["finalization_handoff"].(map[string]any)["receipt_id"] = "rewritten-receipt"
	_, err = repository.Update(context.Background(), "candidate-set-001", marshalRecordFixture(t, finalizedDocument), ArtifactUpdateExpectation{RawSHA256: finalized.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordEvolution) {
		t.Fatalf("finalized handoff rewrite error=%v, want ErrRecordEvolution", err)
	}
}

func TestCandidateSetRepositoryPreservesExternalManualEditOnCASConflict(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	raw := candidateRepositoryRaw(t, provider.candidate)
	created, err := repository.Create(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	manual := append(append([]byte(nil), raw...), ' ')
	path := filepath.Join(workspace, filepath.FromSlash(created.RelativePath))
	if err := os.WriteFile(path, manual, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = repository.Update(context.Background(), "candidate-set-001", raw, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("external edit error=%v, want ErrRecordConflict", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(got, manual) {
		t.Fatalf("external author bytes were lost: got=%q err=%v", got, readErr)
	}
}

func TestCandidateSetRepositoryBlocksManagedWritesBeforeWorkspaceSchemaCompatibility(t *testing.T) {
	provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
	for _, test := range []struct {
		name      string
		workspace func(*testing.T) string
		version   string
	}{
		{"missing marker", func(t *testing.T) string { return t.TempDir() }, "1.6.2"},
		{"dev application", managedRepositoryWorkspace, "dev"},
		{"pre-1.0 application", managedRepositoryWorkspace, "0.9.9"},
		{"legacy only", func(t *testing.T) string {
			workspace := t.TempDir()
			writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[{"id":"legacy"}]}`)
			return workspace
		}, "1.6.2"},
		{"unknown required feature", func(t *testing.T) string {
			workspace := t.TempDir()
			writeSchemaMarker(t, workspace, newSchemaV1Marker(t, func(marker map[string]any) {
				marker["features"].(map[string]any)["future_required"] = map[string]any{"version": "2.0.0", "required": true}
			}))
			return workspace
		}, "1.6.2"},
		{"split roots", func(t *testing.T) string {
			workspace := managedRepositoryWorkspace(t)
			writeWorkspaceTestFile(t, workspace, ".nova/lore/items.json", `{"items":[{"id":"split"}]}`)
			return workspace
		}, "1.6.2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider.candidateCalls = 0
			workspace := test.workspace(t)
			repository := newCandidateRepositoryForTestVersion(t, workspace, provider, RecordRepositoryHooks{}, test.version)
			before := workspaceTreeDigest(t, workspace)
			_, err := repository.Create(context.Background(), candidateRepositoryRaw(t, provider.candidate))
			var blocked *MutationBlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("Create error=%T %v, want MutationBlockedError", err, err)
			}
			if after := workspaceTreeDigest(t, workspace); !reflect.DeepEqual(after, before) {
				t.Fatalf("blocked managed mutation changed workspace\nbefore=%v\nafter=%v", before, after)
			}
			if provider.candidateCalls != 0 {
				t.Fatalf("Workspace Schema guard ran after reference resolution: calls=%d", provider.candidateCalls)
			}
		})
	}
}

func TestReviewIssueRepositoryCreateReadListAndCASUpdate(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{issue: reviewRepositoryReferences()}
	repository := newReviewIssueRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	raw := reviewRepositoryRaw(t, provider.issue)

	created, err := repository.Create(context.Background(), raw)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.RelativePath != ".denova/quality/artifacts/review-issues/review-issue-001.json" || created.RawSHA256 != recordSHA256(raw) || created.WorkspaceRevision != 1 {
		t.Fatalf("created snapshot=%#v", created)
	}
	if _, err := repository.Create(context.Background(), raw); !errors.Is(err, ErrRecordExists) {
		t.Fatalf("duplicate Create error=%v, want ErrRecordExists", err)
	}

	read, err := repository.Read(context.Background(), "review-issue-001")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(read.Parsed.RawBytes(), raw) || read.RawSHA256 != created.RawSHA256 {
		t.Fatalf("read snapshot changed raw bytes: %#v", read)
	}
	ids, err := repository.List(context.Background())
	if err != nil || !reflect.DeepEqual(ids, []string{"review-issue-001"}) {
		t.Fatalf("List=%v err=%v", ids, err)
	}

	var updatedDocument map[string]any
	if err := json.Unmarshal(raw, &updatedDocument); err != nil {
		t.Fatal(err)
	}
	updatedDocument["recommendation"].(map[string]any)["minimum_impact_change"] = "add a precise place cue"
	updatedRaw := marshalRecordFixture(t, updatedDocument)
	updated, err := repository.Update(context.Background(), "review-issue-001", updatedRaw, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: created.WorkspaceRevision})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.RawSHA256 == created.RawSHA256 {
		t.Fatal("CAS update did not publish new complete bytes")
	}
	var rewritten map[string]any
	if err := json.Unmarshal(updatedRaw, &rewritten); err != nil {
		t.Fatal(err)
	}
	rewritten["status_history"].([]any)[0].(map[string]any)["transition_id"] = "rewritten-history"
	provider.issue.TransitionContents["rewritten-history"] = provider.issue.ReviewedContent
	_, err = repository.Update(context.Background(), "review-issue-001", marshalRecordFixture(t, rewritten), ArtifactUpdateExpectation{RawSHA256: updated.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordEvolution) {
		t.Fatalf("history rewrite error=%v, want ErrRecordEvolution", err)
	}

	_, err = repository.Update(context.Background(), "review-issue-001", raw, ArtifactUpdateExpectation{RawSHA256: created.RawSHA256, WorkspaceRevision: 1})
	if !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("stale CAS error=%v, want ErrRecordConflict", err)
	}
}

func TestReviewIssueRepositoryRejectsNonPortableReviewedArtifactPath(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	provider := &repositoryReferenceProvider{issue: reviewRepositoryReferences()}
	repository := newReviewIssueRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	var document map[string]any
	if err := json.Unmarshal(reviewRepositoryRaw(t, provider.issue), &document); err != nil {
		t.Fatal(err)
	}
	document["location"].(map[string]any)["artifact_path"] = "../chapters/chapter-001.md"
	if _, err := repository.Create(context.Background(), marshalRecordFixture(t, document)); err == nil {
		t.Fatal("Create accepted an escaping reviewed artifact path")
	}
}

func TestRecordRepositoriesRequireTrustedAuthorActionVerification(t *testing.T) {
	authorityErr := errors.New("untrusted author action")

	t.Run("CandidateSet author decision", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		provider := &repositoryReferenceProvider{candidate: candidateRepositoryReferences()}
		repository := newCandidateRepositoryForTestAuthority(t, workspace, provider, RecordRepositoryHooks{}, &repositoryAuthorityVerifier{candidateErr: authorityErr})
		raw := candidateAuthorSelectedRepositoryRaw(t, provider.candidate)
		if _, err := repository.Create(context.Background(), raw); !errors.Is(err, authorityErr) {
			t.Fatalf("Create error=%v, want trusted authority rejection", err)
		}
		path, _ := CandidateSetRelativePath("candidate-set-001")
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rejected author decision changed target: %v", err)
		}
	})

	t.Run("PreferenceSignal append", func(t *testing.T) {
		workspace := managedRepositoryWorkspace(t)
		provider := &repositoryReferenceProvider{preference: preferenceRepositoryReferences()}
		repository := newPreferenceRepositoryForTestAuthority(t, workspace, provider, RecordRepositoryHooks{}, &repositoryAuthorityVerifier{preferenceErr: authorityErr})
		raw := preferenceRepositoryRaw(t, provider.preference)
		if _, err := repository.Append(context.Background(), raw, PreferenceAppendExpectation{PriorRawSHA256: recordSHA256(nil)}); !errors.Is(err, authorityErr) {
			t.Fatalf("Append error=%v, want trusted authority rejection", err)
		}
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(preferenceJournalPath))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rejected PreferenceSignal changed journal: %v", err)
		}
	})
}

func TestRecordRepositoryRevalidatesReferencedBytesInsideMutationLock(t *testing.T) {
	workspace := managedRepositoryWorkspace(t)
	good := candidateRepositoryReferences()
	drifted := good
	drifted.Skills = map[string][]byte{"candidate-001": []byte("changed after preflight")}
	provider := &repositoryReferenceProvider{candidate: good, candidateSequence: []domain.CandidateSetReferences{drifted}}
	repository := newCandidateRepositoryForTest(t, workspace, provider, RecordRepositoryHooks{})
	if _, err := repository.Create(context.Background(), candidateRepositoryRaw(t, good)); err == nil {
		t.Fatal("Create persisted references that drifted before the locked commit")
	}
	path, _ := CandidateSetRelativePath("candidate-set-001")
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reference-drift rejection changed target: %v", err)
	}
}

type repositoryReferenceProvider struct {
	candidate          domain.CandidateSetReferences
	issue              domain.ReviewIssueReferences
	preference         domain.PreferenceSignalReferences
	candidateCalls     int
	issueCalls         int
	preferenceCalls    int
	candidateSequence  []domain.CandidateSetReferences
	issueSequence      []domain.ReviewIssueReferences
	preferenceSequence []domain.PreferenceSignalReferences
}

type repositoryAuthorityVerifier struct {
	candidateErr  error
	issueErr      error
	preferenceErr error
}

func (verifier *repositoryAuthorityVerifier) VerifyCandidateSetMutation(context.Context, *domain.CandidateSet, domain.CandidateSet) error {
	return verifier.candidateErr
}

func (verifier *repositoryAuthorityVerifier) VerifyReviewIssueMutation(context.Context, *domain.ReviewIssue, domain.ReviewIssue) error {
	return verifier.issueErr
}

func (verifier *repositoryAuthorityVerifier) VerifyPreferenceSignalAppend(context.Context, []domain.PreferenceSignal, domain.PreferenceSignal) error {
	return verifier.preferenceErr
}

func (provider *repositoryReferenceProvider) CandidateSetReferences(_ context.Context, scope RecordReferenceScope, _ domain.CandidateSet) (domain.CandidateSetReferences, error) {
	if scope.Root == nil {
		return domain.CandidateSetReferences{}, errors.New("unpinned reference scope")
	}
	provider.candidateCalls++
	if len(provider.candidateSequence) != 0 {
		index := provider.candidateCalls - 1
		if index >= len(provider.candidateSequence) {
			index = len(provider.candidateSequence) - 1
		}
		return provider.candidateSequence[index], nil
	}
	return provider.candidate, nil
}

func (provider *repositoryReferenceProvider) ReviewIssueReferences(_ context.Context, scope RecordReferenceScope, _ domain.ReviewIssue) (domain.ReviewIssueReferences, error) {
	if scope.Root == nil {
		return domain.ReviewIssueReferences{}, errors.New("unpinned reference scope")
	}
	provider.issueCalls++
	if len(provider.issueSequence) != 0 {
		index := provider.issueCalls - 1
		if index >= len(provider.issueSequence) {
			index = len(provider.issueSequence) - 1
		}
		return provider.issueSequence[index], nil
	}
	return provider.issue, nil
}

func (provider *repositoryReferenceProvider) PreferenceSignalReferences(_ context.Context, scope RecordReferenceScope, signal domain.PreferenceSignal) (domain.PreferenceSignalReferences, error) {
	if scope.Root == nil {
		return domain.PreferenceSignalReferences{}, errors.New("unpinned reference scope")
	}
	provider.preferenceCalls++
	if len(provider.preferenceSequence) != 0 {
		index := provider.preferenceCalls - 1
		if index >= len(provider.preferenceSequence) {
			index = len(provider.preferenceSequence) - 1
		}
		return provider.preferenceSequence[index], nil
	}
	references := provider.preference
	if signal.Event == "revocation" {
		references.Source = references.RevocationReason
	}
	return references, nil
}

func managedRepositoryWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	writeSchemaMarker(t, workspace, newSchemaV1Marker(t, nil))
	return workspace
}

func newCandidateRepositoryForTest(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks) *CandidateSetRepository {
	t.Helper()
	return newCandidateRepositoryForTestVersion(t, workspace, provider, hooks, "1.6.2")
}

func newCandidateRepositoryForTestVersion(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks, version string) *CandidateSetRepository {
	return newCandidateRepositoryForTestAuthorityVersion(t, workspace, provider, hooks, version, &repositoryAuthorityVerifier{})
}

func newCandidateRepositoryForTestAuthority(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks, authority RecordAuthorityVerifier) *CandidateSetRepository {
	t.Helper()
	return newCandidateRepositoryForTestAuthorityVersion(t, workspace, provider, hooks, "1.6.2", authority)
}

func newCandidateRepositoryForTestAuthorityVersion(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks, version string, authority RecordAuthorityVerifier) *CandidateSetRepository {
	t.Helper()
	repository, err := NewCandidateSetRepository(RecordRepositoryConfig{
		Workspace:  workspace,
		Inspector:  InspectorOptions{ApplicationVersion: version, SupportedFeatures: map[string]string{"quality_harness": ">=1.0.0 <2.0.0"}},
		Decoder:    newRecordDecoderForTest(t),
		Lease:      &migrationTestLease{invoke: true},
		References: provider,
		Authority:  authority,
		Hooks:      hooks,
	})
	if err != nil {
		t.Fatalf("NewCandidateSetRepository: %v", err)
	}
	return repository
}

func candidateAuthorSelectedRepositoryRaw(t *testing.T, references domain.CandidateSetReferences) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(candidateRepositoryRaw(t, references), &document); err != nil {
		t.Fatal(err)
	}
	document["current_state"] = "author_selected"
	document["transition_history"] = []any{
		map[string]any{"transition_id": "transition-001", "from": "open", "to": "compared", "actor": map[string]any{"actor_id": "reviewer-001", "actor_type": "reviewer"}, "reason": "compared", "at": "2026-07-21T00:00:00Z"},
		map[string]any{"transition_id": "transition-002", "from": "compared", "to": "author_selected", "actor": map[string]any{"actor_id": "author-001", "actor_type": "author"}, "reason": "selected", "at": "2026-07-21T00:00:01Z"},
	}
	document["evaluation"] = map[string]any{
		"evaluation_id": "evaluation-001", "actor": map[string]any{"actor_id": "reviewer-001", "actor_type": "reviewer"}, "at": "2026-07-21T00:00:00Z",
		"candidate_evaluations": []any{map[string]any{"candidate_id": "candidate-001", "criteria": []any{map[string]any{"criterion_id": "criterion-001", "reader_observable_evidence": "evidence", "source_ref": "artifact-001"}}}},
		"summary":               "compared",
	}
	document["author_decision"] = map[string]any{"decision_id": "decision-001", "kind": "selected", "actor": map[string]any{"actor_id": "author-001", "actor_type": "author"}, "at": "2026-07-21T00:00:01Z", "reason": "selected", "selected_candidate_ids": []any{"candidate-001"}}
	hash := recordSHA256(references.Candidates["candidate-001"])
	document["finalization_handoff"] = map[string]any{"status": "ready", "content_hash": hash}
	return marshalRecordFixture(t, document)
}

func candidateRepositoryReferences() domain.CandidateSetReferences {
	return domain.CandidateSetReferences{
		Workspace:       []byte("workspace"),
		Artifact:        []byte("candidate"),
		SourceManifest:  []byte("source-manifest"),
		Profile:         []byte("profile"),
		QualitySpec:     []byte("quality-spec"),
		CandidatePolicy: []byte("candidate-policy"),
		Candidates:      map[string][]byte{"candidate-001": []byte("candidate")},
		Skills:          map[string][]byte{"candidate-001": []byte("skill")},
	}
}

func candidateRepositoryRaw(t *testing.T, references domain.CandidateSetReferences) []byte {
	t.Helper()
	document := candidateSetFixture()
	hashes := map[string]string{
		"workspace":        recordSHA256(references.Workspace),
		"artifact":         recordSHA256(references.Artifact),
		"source_manifest":  recordSHA256(references.SourceManifest),
		"candidate":        recordSHA256(references.Candidates["candidate-001"]),
		"profile":          recordSHA256(references.Profile),
		"quality_spec":     recordSHA256(references.QualitySpec),
		"candidate_policy": recordSHA256(references.CandidatePolicy),
	}
	document["workspace"].(map[string]any)["hash"] = hashes["workspace"]
	document["artifact"].(map[string]any)["hash"] = hashes["artifact"]
	document["source_manifest"].(map[string]any)["hash"] = hashes["source_manifest"]
	document["profile"].(map[string]any)["hash"] = hashes["profile"]
	document["quality_spec"].(map[string]any)["hash"] = hashes["quality_spec"]
	candidate := document["candidates"].([]any)[0].(map[string]any)
	candidate["artifact"].(map[string]any)["hash"] = hashes["candidate"]
	candidate["content_hash"] = hashes["candidate"]
	candidate["source_manifest"].(map[string]any)["hash"] = hashes["source_manifest"]
	candidate["profile"].(map[string]any)["hash"] = hashes["profile"]
	candidate["quality_spec"].(map[string]any)["hash"] = hashes["quality_spec"]
	candidate["skill"].(map[string]any)["hash"] = recordSHA256(references.Skills["candidate-001"])
	checks := document["binding_validation"].([]any)
	for _, rawCheck := range checks {
		check := rawCheck.(map[string]any)
		hash := hashes[check["binding_kind"].(string)]
		check["expected_hash"], check["observed_hash"] = hash, hash
	}
	return marshalRecordFixture(t, document)
}

func newReviewIssueRepositoryForTest(t *testing.T, workspace string, provider RecordReferenceProvider, hooks RecordRepositoryHooks) *ReviewIssueRepository {
	t.Helper()
	repository, err := NewReviewIssueRepository(RecordRepositoryConfig{
		Workspace:  workspace,
		Inspector:  InspectorOptions{ApplicationVersion: "1.6.2", SupportedFeatures: map[string]string{"quality_harness": ">=1.0.0 <2.0.0"}},
		Decoder:    newRecordDecoderForTest(t),
		Lease:      &migrationTestLease{invoke: true},
		References: provider,
		Authority:  &repositoryAuthorityVerifier{},
		Hooks:      hooks,
	})
	if err != nil {
		t.Fatalf("NewReviewIssueRepository: %v", err)
	}
	return repository
}

func reviewRepositoryReferences() domain.ReviewIssueReferences {
	return domain.ReviewIssueReferences{
		Workspace:          []byte("workspace"),
		Artifact:           []byte("x"),
		CandidateSet:       []byte("candidate-set"),
		Candidate:          []byte("x"),
		SourceManifest:     []byte("source-manifest"),
		Profile:            []byte("profile"),
		QualitySpec:        []byte("quality-spec"),
		ReviewedContent:    []byte("x"),
		LocationAnchor:     []byte("x"),
		ReviewerSource:     []byte("reviewer"),
		TransitionContents: map[string][]byte{"transition-001": []byte("x")},
		RevisedContents:    map[string][]byte{},
		VerifierSources:    map[string][]byte{},
	}
}

func reviewRepositoryRaw(t *testing.T, references domain.ReviewIssueReferences) []byte {
	t.Helper()
	document := reviewIssueFixture()
	binding := document["binding"].(map[string]any)
	binding["workspace"].(map[string]any)["hash"] = recordSHA256(references.Workspace)
	binding["artifact"].(map[string]any)["hash"] = recordSHA256(references.Artifact)
	binding["candidate_set_hash"] = recordSHA256(references.CandidateSet)
	binding["candidate_content_hash"] = recordSHA256(references.Candidate)
	binding["source_manifest"].(map[string]any)["hash"] = recordSHA256(references.SourceManifest)
	binding["profile"].(map[string]any)["hash"] = recordSHA256(references.Profile)
	binding["quality_spec"].(map[string]any)["hash"] = recordSHA256(references.QualitySpec)
	binding["reviewed_content_hash"] = recordSHA256(references.ReviewedContent)
	document["attachment"].(map[string]any)["target_hash"] = recordSHA256(references.Candidate)
	document["location"].(map[string]any)["anchor_hash"] = recordSHA256(references.LocationAnchor)
	document["location"].(map[string]any)["quoted_text_hash"] = recordSHA256(references.ReviewedContent)
	document["reader_evidence"].(map[string]any)["excerpts"].([]any)[0].(map[string]any)["hash"] = recordSHA256(references.ReviewedContent)
	document["reviewer_provenance"].(map[string]any)["source_hash"] = recordSHA256(references.ReviewerSource)
	document["status_history"].([]any)[0].(map[string]any)["reviewed_content_hash"] = recordSHA256(references.ReviewedContent)
	return marshalRecordFixture(t, document)
}
