package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestValidateCandidateSetAcceptsComparedSingleCandidateWithExactBindings(t *testing.T) {
	record, references := validComparedCandidateSet()
	if err := ValidateCandidateSet(record, references); err != nil {
		t.Fatalf("ValidateCandidateSet: %v", err)
	}
}

func TestValidateCandidateSetRejectsLifecycleAuthorityAndBindingViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CandidateSet, *CandidateSetReferences)
		code   ErrorCode
	}{
		{
			name: "unknown state",
			mutate: func(record *CandidateSet, _ *CandidateSetReferences) {
				record.CurrentState = "future"
			},
			code: CodeStateVocabulary,
		},
		{
			name: "single candidate skips comparison",
			mutate: func(record *CandidateSet, _ *CandidateSetReferences) {
				record.CurrentState = CandidateAuthorSelected
				record.TransitionHistory = []CandidateTransition{{TransitionID: "transition-001", From: CandidateOpen, To: CandidateAuthorSelected, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "skip", At: candidateTestTime}}
				record.AuthorDecision = &CandidateDecision{DecisionID: "decision-001", Kind: "selected", Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, At: candidateTestTime, Reason: "selected", SelectedCandidateIDs: []string{"candidate-001"}}
			},
			code: CodeStateTransition,
		},
		{
			name: "reviewer creates author decision",
			mutate: func(record *CandidateSet, _ *CandidateSetReferences) {
				makeCandidateSelected(record)
				record.AuthorDecision.Actor.ActorType = "reviewer"
			},
			code: CodeAuthorityViolation,
		},
		{
			name: "reviewer performs author decision transition",
			mutate: func(record *CandidateSet, _ *CandidateSetReferences) {
				makeCandidateSelected(record)
				record.TransitionHistory[len(record.TransitionHistory)-1].Actor = RecordActor{ActorID: "reviewer-001", ActorType: "reviewer"}
			},
			code: CodeAuthorityViolation,
		},
		{
			name: "candidate bytes changed",
			mutate: func(_ *CandidateSet, references *CandidateSetReferences) {
				references.Candidates["candidate-001"] = []byte("externally edited")
			},
			code: CodeHashMismatch,
		},
		{
			name: "Skill bytes changed",
			mutate: func(_ *CandidateSet, references *CandidateSetReferences) {
				references.Skills["candidate-001"] = []byte("changed Skill")
			},
			code: CodeHashMismatch,
		},
		{
			name: "stale binding remains comparable",
			mutate: func(record *CandidateSet, _ *CandidateSetReferences) {
				record.BindingValidation[0].Status = "stale"
				record.BindingValidation[0].Reason = "workspace changed"
			},
			code: CodeBindingMismatch,
		},
		{
			name: "rejected set finalizes",
			mutate: func(record *CandidateSet, _ *CandidateSetReferences) {
				record.CurrentState = CandidateFinalized
				record.TransitionHistory = append(record.TransitionHistory,
					CandidateTransition{TransitionID: "transition-002", From: CandidateCompared, To: CandidateRejected, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "reject", At: candidateTestTime},
					CandidateTransition{TransitionID: "transition-003", From: CandidateRejected, To: CandidateFinalized, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "illegal", At: candidateTestTime},
				)
			},
			code: CodeStateTransition,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, references := validComparedCandidateSet()
			test.mutate(&record, &references)
			assertCandidateContractCode(t, ValidateCandidateSet(record, references), test.code)
		})
	}
}

func TestCandidateSetTransitionVocabularyIsExhaustive(t *testing.T) {
	states := []CandidateState{CandidateOpen, CandidateCompared, CandidateAuthorSelected, CandidateMixed, CandidateRejected, CandidateFinalized, CandidateArchived}
	allowed := map[[2]CandidateState]bool{
		{CandidateOpen, CandidateCompared}:            true,
		{CandidateCompared, CandidateAuthorSelected}:  true,
		{CandidateCompared, CandidateMixed}:           true,
		{CandidateCompared, CandidateRejected}:        true,
		{CandidateAuthorSelected, CandidateFinalized}: true,
		{CandidateAuthorSelected, CandidateArchived}:  true,
		{CandidateMixed, CandidateFinalized}:          true,
		{CandidateMixed, CandidateArchived}:           true,
		{CandidateRejected, CandidateArchived}:        true,
		{CandidateFinalized, CandidateArchived}:       true,
	}
	for _, from := range states {
		for _, to := range states {
			if got, want := candidateTransitionAllowed(from, to), allowed[[2]CandidateState{from, to}]; got != want {
				t.Fatalf("candidateTransitionAllowed(%q,%q)=%v want %v", from, to, got, want)
			}
		}
	}
	if candidateTransitionAllowed("future", CandidateOpen) || candidateTransitionAllowed(CandidateOpen, "future") {
		t.Fatal("unknown CandidateSet state entered transition graph")
	}
}

func TestValidateCandidateSetArchivedRetainsDecisionAndHandoffSemantics(t *testing.T) {
	record, references := validComparedCandidateSet()
	makeCandidateSelected(&record)
	record.TransitionHistory = append(record.TransitionHistory, CandidateTransition{TransitionID: "transition-003", From: CandidateAuthorSelected, To: CandidateArchived, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "archive", At: candidateTestTime})
	record.CurrentState = CandidateArchived
	if err := ValidateCandidateSet(record, references); err != nil {
		t.Fatalf("ValidateCandidateSet archived selected: %v", err)
	}

	t.Run("handoff meaning cannot be erased", func(t *testing.T) {
		broken := record
		broken.FinalizationHandoff = FinalizationHandoff{Status: "not_ready"}
		assertCandidateContractCode(t, ValidateCandidateSet(broken, references), CodeBindingMismatch)
	})
	t.Run("decision cannot disagree with archived predecessor", func(t *testing.T) {
		broken := record
		decision := *record.AuthorDecision
		decision.Kind = "rejected"
		decision.SelectedCandidateIDs = nil
		broken.AuthorDecision = &decision
		broken.FinalizationHandoff = FinalizationHandoff{Status: "not_eligible"}
		assertCandidateContractCode(t, ValidateCandidateSet(broken, references), CodeAuthorityViolation)
	})
}

func TestValidateCandidateSetRecomposesMixedOutputFromOrderedParentRanges(t *testing.T) {
	record, references := validComparedCandidateSet()
	secondBytes := []byte("CD")
	record.Candidates = append(record.Candidates, candidateForTest("candidate-002", secondBytes, record))
	record.CandidatePolicy.RequestedCount = 2
	record.CandidatePolicy.KeyNode = true
	record.Evaluation.CandidateEvaluations = append(record.Evaluation.CandidateEvaluations, CandidateEvaluation{CandidateID: "candidate-002", Criteria: []CriterionEvidence{{CriterionID: "criterion-001", ReaderObservableEvidence: "second evidence", SourceRef: "artifact:candidate-002"}}})
	references.Candidates["candidate-002"] = secondBytes
	references.Skills["candidate-002"] = []byte("skill")
	record.BindingValidation = append(record.BindingValidation, validBindingCheck("candidate", hashForTest(secondBytes)))
	record.CurrentState = CandidateMixed
	record.TransitionHistory = append(record.TransitionHistory, CandidateTransition{TransitionID: "transition-002", From: CandidateCompared, To: CandidateMixed, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "mix", At: candidateTestTime})
	record.AuthorDecision = &CandidateDecision{DecisionID: "decision-001", Kind: "mixed", Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, At: candidateTestTime, Reason: "best segments", SelectedCandidateIDs: []string{"candidate-001", "candidate-002"}}
	mixed := []byte("aCDb")
	references.MixedOutput = mixed
	record.MixedOutput = &MixedOutput{
		ArtifactID:  "mixed-artifact-001",
		ContentHash: hashForTest(mixed),
		Segments: []MixedSegment{
			{SegmentID: "segment-001", ParentCandidateID: "candidate-001", ParentContentHash: hashForTest([]byte("ab")), ParentRange: ByteRange{Start: 0, End: 1}, OutputRange: ByteRange{Start: 0, End: 1}, SegmentContentHash: hashForTest([]byte("a"))},
			{SegmentID: "segment-002", ParentCandidateID: "candidate-002", ParentContentHash: hashForTest(secondBytes), ParentRange: ByteRange{Start: 0, End: 2}, OutputRange: ByteRange{Start: 1, End: 3}, SegmentContentHash: hashForTest(secondBytes)},
			{SegmentID: "segment-003", ParentCandidateID: "candidate-001", ParentContentHash: hashForTest([]byte("ab")), ParentRange: ByteRange{Start: 1, End: 2}, OutputRange: ByteRange{Start: 3, End: 4}, SegmentContentHash: hashForTest([]byte("b"))},
		},
	}
	mixedHash := record.MixedOutput.ContentHash
	record.FinalizationHandoff = FinalizationHandoff{Status: "ready", ContentHash: &mixedHash}

	if err := ValidateCandidateSet(record, references); err != nil {
		t.Fatalf("ValidateCandidateSet mixed: %v", err)
	}

	t.Run("segment overlap", func(t *testing.T) {
		broken := record
		output := *record.MixedOutput
		output.Segments = append([]MixedSegment(nil), record.MixedOutput.Segments...)
		output.Segments[2].OutputRange.Start = 2
		broken.MixedOutput = &output
		assertCandidateContractCode(t, ValidateCandidateSet(broken, references), CodeLineageInvalid)
	})

	t.Run("segment hash mismatch", func(t *testing.T) {
		broken := record
		output := *record.MixedOutput
		output.Segments = append([]MixedSegment(nil), record.MixedOutput.Segments...)
		output.Segments[1].SegmentContentHash = hashForTest([]byte("wrong"))
		broken.MixedOutput = &output
		assertCandidateContractCode(t, ValidateCandidateSet(broken, references), CodeHashMismatch)
	})

	t.Run("parent ranges out of order", func(t *testing.T) {
		broken := record
		output := *record.MixedOutput
		output.Segments = append([]MixedSegment(nil), record.MixedOutput.Segments...)
		output.Segments[0].ParentRange = ByteRange{Start: 1, End: 2}
		output.Segments[0].SegmentContentHash = hashForTest([]byte("b"))
		output.Segments[2].ParentRange = ByteRange{Start: 0, End: 1}
		output.Segments[2].SegmentContentHash = hashForTest([]byte("a"))
		outOfOrder := []byte("bCDa")
		output.ContentHash = hashForTest(outOfOrder)
		broken.MixedOutput = &output
		broken.FinalizationHandoff.ContentHash = &output.ContentHash
		outOfOrderReferences := references
		outOfOrderReferences.MixedOutput = outOfOrder
		assertCandidateContractCode(t, ValidateCandidateSet(broken, outOfOrderReferences), CodeLineageInvalid)
	})
}

const candidateTestTime = "2026-07-21T00:00:00Z"

func validComparedCandidateSet() (CandidateSet, CandidateSetReferences) {
	references := CandidateSetReferences{
		Workspace:       []byte("workspace"),
		Artifact:        []byte("candidate-set-artifact"),
		SourceManifest:  []byte("source-manifest"),
		Profile:         []byte("profile"),
		QualitySpec:     []byte("quality-spec"),
		CandidatePolicy: []byte("candidate-policy"),
		Candidates:      map[string][]byte{"candidate-001": []byte("ab")},
		Skills:          map[string][]byte{"candidate-001": []byte("skill")},
	}
	record := CandidateSet{
		Contract:          ArtifactRecordContract{Kind: CandidateSetContractKind, Version: ContractVersionV1, Schema: "candidate-set-v1.schema.json"},
		CandidateSetID:    "candidate-set-001",
		Workspace:         WorkspaceBinding{WorkspaceID: "workspace-001", Revision: 1, Hash: hashForTest(references.Workspace)},
		Run:               RunBinding{RunID: "run-001", ContractVersion: "v1"},
		Stage:             StageBinding{StageID: "stage-001", StageType: "draft", ContractVersion: "v1"},
		Artifact:          ArtifactBinding{ArtifactID: "artifact-set-001", ArtifactType: "candidate_set", ContractVersion: "v1", Hash: hashForTest(references.Artifact)},
		SourceManifest:    VersionedHashBinding{ID: "source-manifest-001", Version: "v1", Hash: hashForTest(references.SourceManifest)},
		Profile:           ProfileBinding{ProfileID: ProfileLongSerial, ContractVersion: "v1", Hash: hashForTest(references.Profile)},
		QualitySpec:       QualitySpecBinding{SpecID: "quality-spec-001", Revision: 1, ContractVersion: "v1", Hash: hashForTest(references.QualitySpec)},
		CandidatePolicy:   CandidatePolicy{PolicyID: "candidate-policy-001", Version: "v1", RequestedCount: 1, Rationale: "ordinary stage"},
		CurrentState:      CandidateCompared,
		TransitionHistory: []CandidateTransition{{TransitionID: "transition-001", From: CandidateOpen, To: CandidateCompared, Actor: RecordActor{ActorID: "reviewer-001", ActorType: "reviewer"}, Reason: "comparison complete", At: candidateTestTime}},
		Evaluation:        &CandidateSetEvaluation{EvaluationID: "evaluation-001", Actor: RecordActor{ActorID: "reviewer-001", ActorType: "reviewer"}, At: candidateTestTime, CandidateEvaluations: []CandidateEvaluation{{CandidateID: "candidate-001", Criteria: []CriterionEvidence{{CriterionID: "criterion-001", ReaderObservableEvidence: "observable evidence", SourceRef: "artifact:candidate-001"}}}}, Summary: "compared"},
		BindingValidation: []BindingCheck{
			validBindingCheck("workspace", hashForTest(references.Workspace)),
			validBindingCheck("artifact", hashForTest(references.Artifact)),
			validBindingCheck("source_manifest", hashForTest(references.SourceManifest)),
			validBindingCheck("candidate", hashForTest(references.Candidates["candidate-001"])),
			validBindingCheck("profile", hashForTest(references.Profile)),
			validBindingCheck("quality_spec", hashForTest(references.QualitySpec)),
			validBindingCheck("candidate_policy", hashForTest(references.CandidatePolicy)),
		},
		FinalizationHandoff: FinalizationHandoff{Status: "not_ready"},
	}
	record.Candidates = []Candidate{candidateForTest("candidate-001", references.Candidates["candidate-001"], record)}
	return record, references
}

func candidateForTest(id string, raw []byte, record CandidateSet) Candidate {
	hash := hashForTest(raw)
	return Candidate{
		CandidateID:    id,
		Artifact:       ArtifactBinding{ArtifactID: "artifact-" + id, ArtifactType: "chapter", ContractVersion: "v1", Hash: hash},
		ContentHash:    hash,
		SourceManifest: record.SourceManifest,
		Model:          CandidateModel{Provider: "provider", ModelID: "model-001", Version: "v1"},
		Skill:          CandidateSkill{SkillID: "skill-001", Version: "v1", Hash: hashForTest([]byte("skill")), CapabilityID: "draft.chapter"},
		Profile:        record.Profile,
		QualitySpec:    record.QualitySpec,
		CreatedAt:      candidateTestTime,
	}
}

func makeCandidateSelected(record *CandidateSet) {
	record.CurrentState = CandidateAuthorSelected
	record.TransitionHistory = append(record.TransitionHistory, CandidateTransition{TransitionID: "transition-002", From: CandidateCompared, To: CandidateAuthorSelected, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "selected", At: candidateTestTime})
	record.AuthorDecision = &CandidateDecision{DecisionID: "decision-001", Kind: "selected", Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, At: candidateTestTime, Reason: "selected", SelectedCandidateIDs: []string{"candidate-001"}}
	hash := record.Candidates[0].ContentHash
	record.FinalizationHandoff = FinalizationHandoff{Status: "ready", ContentHash: &hash}
}

func validBindingCheck(kind, hash string) BindingCheck {
	return BindingCheck{BindingKind: kind, ExpectedHash: hash, ObservedHash: hash, Status: "valid", CheckedAt: candidateTestTime}
}

func hashForTest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func assertCandidateContractCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	var contractErr *ContractError
	if !errors.As(err, &contractErr) || contractErr.Code != code {
		t.Fatalf("error=%T %v, want ContractError code=%s", err, err, code)
	}
}
