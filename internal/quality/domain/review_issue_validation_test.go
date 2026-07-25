package domain

import "testing"

func TestValidateReviewIssueAcceptsExactOpenEvidence(t *testing.T) {
	record, references := validOpenReviewIssue()
	if err := ValidateReviewIssue(record, references); err != nil {
		t.Fatalf("ValidateReviewIssue: %v", err)
	}
}

func TestValidateReviewIssueRejectsLocationCapabilityAndHistoryViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReviewIssue, *ReviewIssueReferences)
		code   ErrorCode
	}{
		{"unknown capability", func(record *ReviewIssue, _ *ReviewIssueReferences) {
			record.CapabilityRouting.CapabilityID = "revision.generic"
		}, CodeReferenceInvalid},
		{"quoted hash mismatch", func(record *ReviewIssue, _ *ReviewIssueReferences) {
			record.Location.QuotedTextHash = hashForTest([]byte("wrong"))
		}, CodeHashMismatch},
		{"excerpt is not reviewed bytes", func(record *ReviewIssue, _ *ReviewIssueReferences) { record.ReaderEvidence.Excerpts[0].Quote = "z" }, CodeEvidenceInvalid},
		{"range exceeds bytes", func(record *ReviewIssue, _ *ReviewIssueReferences) { record.Location.ByteRange.End = 9 }, CodeRangeInvalid},
		{"history does not begin open", func(record *ReviewIssue, _ *ReviewIssueReferences) {
			closed := ReviewResolved
			record.StatusHistory[0].From = &closed
		}, CodeHistoryContinuity},
		{"transition content hash drift", func(record *ReviewIssue, _ *ReviewIssueReferences) {
			record.StatusHistory[0].ReviewedContentHash = hashForTest([]byte("unrelated"))
		}, CodeHashMismatch},
		{"dismissed by reviewer", func(record *ReviewIssue, references *ReviewIssueReferences) {
			record.Status = ReviewDismissed
			record.StatusHistory = append(record.StatusHistory, ReviewStatusTransition{TransitionID: "transition-002", From: reviewStatusPointer(ReviewOpen), To: ReviewDismissed, Actor: RecordActor{ActorID: "reviewer-001", ActorType: "reviewer"}, Reason: "dismiss", At: "2026-07-21T00:00:01Z", ReviewedContentHash: record.Binding.ReviewedContentHash})
			references.TransitionContents["transition-002"] = references.ReviewedContent
		}, CodeAuthorityViolation},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, references := validOpenReviewIssue()
			test.mutate(&record, &references)
			assertCandidateContractCode(t, ValidateReviewIssue(record, references), test.code)
		})
	}
}

func TestValidateReviewIssueRequiresPassedReverificationToClose(t *testing.T) {
	record, references := validOpenReviewIssue()
	revised := []byte("y")
	revisedHash := hashForTest(revised)
	references.RevisedContents = map[string][]byte{"attempt-001": revised}
	references.VerifierSources = map[string][]byte{"attempt-001": []byte("verifier")}
	record.Status = ReviewVerifiedClosed
	record.StatusHistory = append(record.StatusHistory,
		ReviewStatusTransition{TransitionID: "transition-002", From: reviewStatusPointer(ReviewOpen), To: ReviewRevisionProposed, Actor: RecordActor{ActorID: "reviewer-001", ActorType: "reviewer"}, Reason: "proposal", At: "2026-07-21T00:00:01Z", ReviewedContentHash: record.Binding.ReviewedContentHash},
		ReviewStatusTransition{TransitionID: "transition-003", From: reviewStatusPointer(ReviewRevisionProposed), To: ReviewResolved, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "revision produced", At: "2026-07-21T00:00:02Z", ReviewedContentHash: revisedHash},
		ReviewStatusTransition{TransitionID: "transition-004", From: reviewStatusPointer(ReviewResolved), To: ReviewVerifiedClosed, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "verified", At: "2026-07-21T00:00:04Z", ReviewedContentHash: revisedHash},
	)
	record.ReverificationHistory = []Reverification{{AttemptID: "attempt-001", Result: "passed", RevisedContentHash: revisedHash, Evidence: "reader-observable issue is gone", VerifierProvenance: ReviewerProvenance{SourceID: "reviewer-002", SourceKind: "reviewer", SourceVersion: "v1", SourceHash: hashForTest(references.VerifierSources["attempt-001"]), CreatedAt: "2026-07-21T00:00:03Z"}, At: "2026-07-21T00:00:03Z"}}
	references.TransitionContents["transition-002"] = references.ReviewedContent
	references.TransitionContents["transition-003"] = revised
	references.TransitionContents["transition-004"] = revised

	if err := ValidateReviewIssue(record, references); err != nil {
		t.Fatalf("ValidateReviewIssue closed: %v", err)
	}

	t.Run("resolved without verification stays resolved", func(t *testing.T) {
		resolved := record
		resolved.Status = ReviewResolved
		resolved.StatusHistory = append([]ReviewStatusTransition(nil), record.StatusHistory[:3]...)
		resolved.ReverificationHistory = nil
		if err := ValidateReviewIssue(resolved, references); err != nil {
			t.Fatalf("resolved must remain a valid non-closed state: %v", err)
		}
	})

	t.Run("failed verification cannot close", func(t *testing.T) {
		failed := record
		failed.ReverificationHistory = append([]Reverification(nil), record.ReverificationHistory...)
		failed.ReverificationHistory[0].Result = "failed"
		assertCandidateContractCode(t, ValidateReviewIssue(failed, references), CodeEvidenceInvalid)
	})

	t.Run("failed verification reopens", func(t *testing.T) {
		reopened := record
		reopened.Status = ReviewReopened
		reopened.StatusHistory = append([]ReviewStatusTransition(nil), record.StatusHistory[:3]...)
		reopened.StatusHistory = append(reopened.StatusHistory, ReviewStatusTransition{TransitionID: "transition-004", From: reviewStatusPointer(ReviewResolved), To: ReviewReopened, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "failed", At: "2026-07-21T00:00:04Z", ReviewedContentHash: revisedHash})
		reopened.ReverificationHistory = append([]Reverification(nil), record.ReverificationHistory...)
		reopened.ReverificationHistory[0].Result = "failed"
		if err := ValidateReviewIssue(reopened, references); err != nil {
			t.Fatalf("failed re-verification should support reopened: %v", err)
		}
	})

	t.Run("revised bytes drift", func(t *testing.T) {
		drifted := references
		drifted.RevisedContents = map[string][]byte{"attempt-001": []byte("externally changed")}
		assertCandidateContractCode(t, ValidateReviewIssue(record, drifted), CodeHashMismatch)
	})

	t.Run("missing verifier source", func(t *testing.T) {
		missing := references
		missing.VerifierSources = map[string][]byte{}
		assertCandidateContractCode(t, ValidateReviewIssue(record, missing), CodeEvidenceInvalid)
	})

	t.Run("old pass cannot close a later revision round", func(t *testing.T) {
		reclosed := record
		reclosed.StatusHistory = append([]ReviewStatusTransition(nil), record.StatusHistory...)
		reclosed.StatusHistory = append(reclosed.StatusHistory,
			ReviewStatusTransition{TransitionID: "transition-005", From: reviewStatusPointer(ReviewVerifiedClosed), To: ReviewReopened, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "drift", At: "2026-07-21T00:00:05Z", ReviewedContentHash: revisedHash},
			ReviewStatusTransition{TransitionID: "transition-006", From: reviewStatusPointer(ReviewReopened), To: ReviewRevisionProposed, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "second proposal", At: "2026-07-21T00:00:06Z", ReviewedContentHash: revisedHash},
			ReviewStatusTransition{TransitionID: "transition-007", From: reviewStatusPointer(ReviewRevisionProposed), To: ReviewResolved, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "second revision", At: "2026-07-21T00:00:07Z", ReviewedContentHash: revisedHash},
			ReviewStatusTransition{TransitionID: "transition-008", From: reviewStatusPointer(ReviewResolved), To: ReviewVerifiedClosed, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "reused pass", At: "2026-07-21T00:00:08Z", ReviewedContentHash: revisedHash},
		)
		reclosedReferences := references
		reclosedReferences.TransitionContents = cloneReviewTransitionContents(references.TransitionContents)
		for _, id := range []string{"transition-005", "transition-006", "transition-007", "transition-008"} {
			reclosedReferences.TransitionContents[id] = revised
		}
		assertCandidateContractCode(t, ValidateReviewIssue(reclosed, reclosedReferences), CodeEvidenceInvalid)
	})

	t.Run("failed attempt permits later revision proposal", func(t *testing.T) {
		continued := record
		continued.Status = ReviewRevisionProposed
		continued.StatusHistory = append([]ReviewStatusTransition(nil), record.StatusHistory[:3]...)
		continued.StatusHistory = append(continued.StatusHistory,
			ReviewStatusTransition{TransitionID: "transition-004", From: reviewStatusPointer(ReviewResolved), To: ReviewReopened, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "failed", At: "2026-07-21T00:00:04Z", ReviewedContentHash: revisedHash},
			ReviewStatusTransition{TransitionID: "transition-005", From: reviewStatusPointer(ReviewReopened), To: ReviewRevisionProposed, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "try again", At: "2026-07-21T00:00:05Z", ReviewedContentHash: revisedHash},
		)
		continued.ReverificationHistory = append([]Reverification(nil), record.ReverificationHistory...)
		continued.ReverificationHistory[0].Result = "failed"
		continuedReferences := references
		continuedReferences.TransitionContents = cloneReviewTransitionContents(references.TransitionContents)
		continuedReferences.TransitionContents["transition-005"] = revised
		if err := ValidateReviewIssue(continued, continuedReferences); err != nil {
			t.Fatalf("continued revision after failed attempt: %v", err)
		}
	})

	t.Run("backdated failed attempt cannot reuse an earlier reopen", func(t *testing.T) {
		laterRound := record
		laterRound.Status = ReviewResolved
		laterRound.StatusHistory = append([]ReviewStatusTransition(nil), record.StatusHistory...)
		laterRound.StatusHistory = append(laterRound.StatusHistory,
			ReviewStatusTransition{TransitionID: "transition-005", From: reviewStatusPointer(ReviewVerifiedClosed), To: ReviewReopened, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "drift", At: "2026-07-21T00:00:05Z", ReviewedContentHash: revisedHash},
			ReviewStatusTransition{TransitionID: "transition-006", From: reviewStatusPointer(ReviewReopened), To: ReviewRevisionProposed, Actor: RecordActor{ActorID: "reviewer-002", ActorType: "reviewer"}, Reason: "second proposal", At: "2026-07-21T00:00:06Z", ReviewedContentHash: revisedHash},
			ReviewStatusTransition{TransitionID: "transition-007", From: reviewStatusPointer(ReviewRevisionProposed), To: ReviewResolved, Actor: RecordActor{ActorID: "author-001", ActorType: "author"}, Reason: "second revision", At: "2026-07-21T00:00:07Z", ReviewedContentHash: revisedHash},
		)
		laterRound.ReverificationHistory = append([]Reverification(nil), record.ReverificationHistory...)
		laterRound.ReverificationHistory = append(laterRound.ReverificationHistory, Reverification{
			AttemptID: "attempt-002", Result: "failed", RevisedContentHash: revisedHash,
			Evidence: "backdated failure", VerifierProvenance: ReviewerProvenance{
				SourceID: "reviewer-002", SourceKind: "reviewer", SourceVersion: "v1",
				SourceHash: hashForTest([]byte("verifier-002")), CreatedAt: "2026-07-21T00:00:04.500Z",
			}, At: "2026-07-21T00:00:04.500Z",
		})
		laterReferences := references
		laterReferences.TransitionContents = cloneReviewTransitionContents(references.TransitionContents)
		for _, id := range []string{"transition-005", "transition-006", "transition-007"} {
			laterReferences.TransitionContents[id] = revised
		}
		laterReferences.RevisedContents = map[string][]byte{"attempt-001": revised, "attempt-002": revised}
		laterReferences.VerifierSources = map[string][]byte{"attempt-001": []byte("verifier"), "attempt-002": []byte("verifier-002")}
		assertCandidateContractCode(t, ValidateReviewIssue(laterRound, laterReferences), CodeEvidenceInvalid)
	})
}

func cloneReviewTransitionContents(source map[string][]byte) map[string][]byte {
	clone := make(map[string][]byte, len(source))
	for id, raw := range source {
		clone[id] = append([]byte(nil), raw...)
	}
	return clone
}

func TestReviewIssueTransitionVocabularyIsExhaustive(t *testing.T) {
	states := []ReviewStatus{ReviewOpen, ReviewRevisionProposed, ReviewResolved, ReviewVerifiedClosed, ReviewReopened, ReviewDismissed}
	allowed := map[[2]ReviewStatus]bool{
		{ReviewOpen, ReviewRevisionProposed}:      true,
		{ReviewOpen, ReviewDismissed}:             true,
		{ReviewRevisionProposed, ReviewResolved}:  true,
		{ReviewRevisionProposed, ReviewOpen}:      true,
		{ReviewRevisionProposed, ReviewDismissed}: true,
		{ReviewResolved, ReviewVerifiedClosed}:    true,
		{ReviewResolved, ReviewReopened}:          true,
		{ReviewVerifiedClosed, ReviewReopened}:    true,
		{ReviewReopened, ReviewRevisionProposed}:  true,
		{ReviewReopened, ReviewDismissed}:         true,
		{ReviewDismissed, ReviewReopened}:         true,
	}
	for _, from := range states {
		for _, to := range states {
			if got, want := reviewTransitionAllowed(from, to), allowed[[2]ReviewStatus{from, to}]; got != want {
				t.Fatalf("reviewTransitionAllowed(%q,%q)=%v want %v", from, to, got, want)
			}
		}
	}
	if reviewTransitionAllowed("future", ReviewOpen) || reviewTransitionAllowed(ReviewOpen, "future") {
		t.Fatal("unknown ReviewIssue status entered transition graph")
	}
}

func validOpenReviewIssue() (ReviewIssue, ReviewIssueReferences) {
	content := []byte("x")
	references := ReviewIssueReferences{
		Workspace:          []byte("workspace"),
		Artifact:           content,
		CandidateSet:       []byte("candidate-set"),
		Candidate:          content,
		SourceManifest:     []byte("source-manifest"),
		Profile:            []byte("profile"),
		QualitySpec:        []byte("quality-spec"),
		ReviewedContent:    content,
		LocationAnchor:     content,
		ReviewerSource:     []byte("reviewer"),
		TransitionContents: map[string][]byte{"transition-001": content},
	}
	record := ReviewIssue{
		Contract:          ArtifactRecordContract{Kind: ReviewIssueContractKind, Version: ContractVersionV1, Schema: "review-issue-v1.schema.json"},
		IssueID:           "review-issue-001",
		CapabilityRouting: CapabilityRouting{CapabilityID: "revision.scene", ContractVersion: "v1", UnknownCapabilityID: RejectExplicitly},
		Binding: ReviewBinding{
			Workspace: WorkspaceBinding{WorkspaceID: "workspace-001", Revision: 1, Hash: hashForTest(references.Workspace)},
			Run:       RunBinding{RunID: "run-001", ContractVersion: "v1"}, Stage: StageBinding{StageID: "stage-001", StageType: "review", ContractVersion: "v1"},
			Artifact:       ArtifactBinding{ArtifactID: "artifact-001", ArtifactType: "chapter", ContractVersion: "v1", Hash: hashForTest(references.Artifact)},
			CandidateSetID: "candidate-set-001", CandidateSetHash: hashForTest(references.CandidateSet), CandidateID: "candidate-001", CandidateContentHash: hashForTest(references.Candidate),
			SourceManifest:      VersionedHashBinding{ID: "source-manifest-001", Version: "v1", Hash: hashForTest(references.SourceManifest)},
			Profile:             ProfileBinding{ProfileID: ProfileLongSerial, ContractVersion: "v1", Hash: hashForTest(references.Profile)},
			QualitySpec:         QualitySpecBinding{SpecID: "quality-spec-001", Revision: 1, ContractVersion: "v1", Hash: hashForTest(references.QualitySpec)},
			ReviewedContentHash: hashForTest(content),
		},
		Attachment:     ReviewAttachment{Kind: "candidate", TargetID: "candidate-001", TargetHash: hashForTest(content)},
		Location:       ReviewLocation{ArtifactPath: "chapters/chapter-001.md", ByteRange: ByteRange{Start: 0, End: 1}, AnchorHash: hashForTest(references.LocationAnchor), QuotedTextHash: hashForTest(content)},
		ReaderEvidence: ReaderEvidence{ObservableEffect: "reader loses orientation", Summary: "scene location is unclear", Excerpts: []EvidenceExcerpt{{Quote: "x", Location: ByteRange{Start: 0, End: 1}, Hash: hashForTest(content)}}},
		Cause:          ReviewCause{Category: "scene", Explanation: "location cue is missing"}, Severity: "major", RevisionLayer: "scene",
		Recommendation:       ReviewRecommendation{MinimumImpactChange: "add location cue", AffectedRange: ByteRange{Start: 0, End: 1}, DimensionsToRecheck: []string{"scene_orientation"}},
		ReviewerProvenance:   ReviewerProvenance{SourceID: "reviewer-001", SourceKind: "reviewer", SourceVersion: "v1", SourceHash: hashForTest(references.ReviewerSource), CreatedAt: candidateTestTime},
		ReviewerOutputPolicy: ReviewerOutputPolicy{Output: "evidence_and_findings_only", WriterChainOfThought: "forbidden"},
		Status:               ReviewOpen,
		StatusHistory:        []ReviewStatusTransition{{TransitionID: "transition-001", From: nil, To: ReviewOpen, Actor: RecordActor{ActorID: "reviewer-001", ActorType: "reviewer"}, Reason: "created", At: candidateTestTime, ReviewedContentHash: hashForTest(content)}},
	}
	return record, references
}

func reviewStatusPointer(status ReviewStatus) *ReviewStatus { return &status }
