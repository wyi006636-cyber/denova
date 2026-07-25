package domain

import "testing"

func TestValidatePreferenceSignalAcceptsOnlySevenExplicitAuthorEvents(t *testing.T) {
	for _, event := range []string{"selection", "mixed_selection", "rejection", "author_rewrite", "rule_confirmation", "correction", "revocation"} {
		t.Run(event, func(t *testing.T) {
			signal, references := validPreferenceSignal(event)
			if err := ValidatePreferenceSignal(signal, references); err != nil {
				t.Fatalf("ValidatePreferenceSignal(%s): %v", event, err)
			}
		})
	}
}

func TestValidatePreferenceSignalRejectsPassiveOrNonAuthorSignals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PreferenceSignal, *PreferenceSignalReferences)
		code   ErrorCode
	}{
		{"model actor", func(signal *PreferenceSignal, _ *PreferenceSignalReferences) { signal.Author.ActorType = "model" }, CodeAuthorityViolation},
		{"reviewer actor", func(signal *PreferenceSignal, _ *PreferenceSignalReferences) { signal.Author.ActorType = "reviewer" }, CodeAuthorityViolation},
		{"automation actor", func(signal *PreferenceSignal, _ *PreferenceSignalReferences) { signal.Author.ActorType = "automation" }, CodeAuthorityViolation},
		{"telemetry event", func(signal *PreferenceSignal, _ *PreferenceSignalReferences) { signal.Event = "telemetry" }, CodeStateVocabulary},
		{"implicit confirmation", func(signal *PreferenceSignal, _ *PreferenceSignalReferences) { signal.Confirmation.Explicit = false }, CodeConfirmationRequired},
		{"source byte drift", func(_ *PreferenceSignal, references *PreferenceSignalReferences) {
			references.Source = []byte("model output")
		}, CodeHashMismatch},
		{"unrelated provenance source", func(signal *PreferenceSignal, references *PreferenceSignalReferences) {
			references.Source = []byte("unrelated but self-consistent")
			signal.Provenance.ContentHash = hashForTest(references.Source)
		}, CodeReferenceInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signal, references := validPreferenceSignal("selection")
			test.mutate(&signal, &references)
			assertCandidateContractCode(t, ValidatePreferenceSignal(signal, references), test.code)
		})
	}
}

func TestResolvePreferencesRejectsUnvalidatedNonAuthorSignal(t *testing.T) {
	signal, _ := validPreferenceSignal("selection")
	signal.Author.ActorType = "model"
	if _, err := ResolvePreferences([]PreferenceSignal{signal}, PreferenceQuery{AuthorID: signal.Author.ActorID, ProjectID: signal.Scope.ProjectID, WorkspaceID: signal.Scope.WorkspaceID, Dimension: signal.Preference.Dimension}); err == nil {
		t.Fatal("ResolvePreferences accepted a model-authored signal")
	}
}

func TestResolvePreferencesRejectsInvalidEventTaggedUnion(t *testing.T) {
	t.Run("selection source and reference", func(t *testing.T) {
		signal, _ := validPreferenceSignal("selection")
		signal.Provenance.SourceKind = "review_issue"
		signal.EventReference = PreferenceEventReference{}
		if _, err := ResolvePreferences([]PreferenceSignal{signal}, PreferenceQuery{AuthorID: signal.Author.ActorID, ProjectID: signal.Scope.ProjectID, WorkspaceID: signal.Scope.WorkspaceID, Dimension: signal.Preference.Dimension}); err == nil {
			t.Fatal("ResolvePreferences accepted an invalid selection tagged union")
		}
	})

	t.Run("selection carries an unrelated reference field", func(t *testing.T) {
		signal, _ := validPreferenceSignal("selection")
		signal.EventReference.IssueID = "review-issue-smuggled"
		if _, err := ResolvePreferences([]PreferenceSignal{signal}, PreferenceQuery{AuthorID: signal.Author.ActorID, ProjectID: signal.Scope.ProjectID, WorkspaceID: signal.Scope.WorkspaceID, Dimension: signal.Preference.Dimension}); err == nil {
			t.Fatal("ResolvePreferences accepted an open tagged union")
		}
	})

	t.Run("correction has an unknown source kind", func(t *testing.T) {
		target, _ := validPreferenceSignal("selection")
		target.SignalID = "signal-target"
		correction, _ := validPreferenceSignal("correction")
		correction.SignalID = "signal-correction"
		correction.Provenance.SourceKind = "model"
		correction.EventReference.CorrectedSignalID = target.SignalID
		correction.SupersedesSignalIDs = []string{target.SignalID}
		if _, err := ResolvePreferences([]PreferenceSignal{target, correction}, PreferenceQuery{AuthorID: target.Author.ActorID, ProjectID: target.Scope.ProjectID, WorkspaceID: target.Scope.WorkspaceID, Dimension: target.Preference.Dimension}); err == nil {
			t.Fatal("ResolvePreferences accepted an unknown correction source kind")
		}
	})
}

func TestPreferenceSignalCannotBeRecordedBeforeConfirmation(t *testing.T) {
	signal, references := validPreferenceSignal("selection")
	signal.Confirmation.ConfirmedAt = "2026-07-21T00:00:01Z"
	signal.RecordedAt = "2026-07-21T00:00:00Z"
	assertCandidateContractCode(t, ValidatePreferenceSignal(signal, references), CodeJournalInvalid)
}

func TestPreferenceJournalPreservesCorrectionRevocationAndResolvesDeterministically(t *testing.T) {
	authorSignal, _ := validPreferenceSignal("selection")
	authorSignal.SignalID = "signal-author"
	authorSignal.Scope.Kind = "author"
	authorSignal.Preference.Strength = "strong"
	authorSignal.RecordedAt = "2026-07-21T00:00:00Z"

	projectSignal := authorSignal
	projectSignal.SignalID = "signal-project"
	projectSignal.Scope.Kind = "project"
	projectSignal.Preference.Strength = "weak"
	projectSignal.RecordedAt = "2026-07-21T00:01:00Z"

	workspaceSignal := authorSignal
	workspaceSignal.SignalID = "signal-workspace"
	workspaceSignal.Scope.Kind = "workspace"
	workspaceSignal.Preference.Value = "workspace-value"
	workspaceSignal.Preference.Strength = "weak"
	workspaceSignal.RecordedAt = "2026-07-21T00:02:00Z"

	correction, _ := validPreferenceSignal("correction")
	correction.SignalID = "signal-correction"
	correction.EventReference.CorrectedSignalID = workspaceSignal.SignalID
	correction.SupersedesSignalIDs = []string{workspaceSignal.SignalID}
	correction.Preference.Value = "corrected-value"
	correction.Preference.Strength = "normal"
	correction.Confirmation.ConfirmedAt = "2026-07-21T00:03:00Z"
	correction.Confirmation.EvidenceHash = hashForTest([]byte("new correction confirmation"))
	correction.RecordedAt = "2026-07-21T00:03:00Z"

	revocation, _ := validPreferenceSignal("revocation")
	revocation.SignalID = "signal-revocation"
	revocation.EventReference.RevocationReasonHash = hashForTest([]byte("withdraw correction"))
	revocation.RevokesSignalIDs = []string{correction.SignalID}
	revocation.RecordedAt = "2026-07-21T00:04:00Z"

	journal := []PreferenceSignal{authorSignal, projectSignal, workspaceSignal, correction, revocation}
	if err := ValidatePreferenceJournal(journal); err != nil {
		t.Fatalf("ValidatePreferenceJournal: %v", err)
	}
	resolution, err := ResolvePreferences(journal, PreferenceQuery{AuthorID: "author-001", ProjectID: "project-001", WorkspaceID: "workspace-001", Dimension: "opening_dialogue"})
	if err != nil {
		t.Fatalf("ResolvePreferences: %v", err)
	}
	if resolution.Effective == nil || resolution.Effective.SignalID != projectSignal.SignalID {
		t.Fatalf("effective=%#v, want project fallback after workspace correction revocation", resolution.Effective)
	}
	if len(resolution.Suppressed) != 2 {
		t.Fatalf("suppressed=%#v, want superseded workspace and revoked correction", resolution.Suppressed)
	}
}

func TestPreferenceJournalRejectsCyclesCrossAuthorTargetsAndUnconfirmedScopeExpansion(t *testing.T) {
	tests := []struct {
		name    string
		journal func() []PreferenceSignal
		code    ErrorCode
	}{
		{
			name: "cycle",
			journal: func() []PreferenceSignal {
				first, _ := validPreferenceSignal("correction")
				second, _ := validPreferenceSignal("correction")
				first.SignalID, second.SignalID = "signal-first", "signal-second"
				first.EventReference.CorrectedSignalID, first.SupersedesSignalIDs = second.SignalID, []string{second.SignalID}
				second.EventReference.CorrectedSignalID, second.SupersedesSignalIDs = first.SignalID, []string{first.SignalID}
				return []PreferenceSignal{first, second}
			},
			code: CodeResolutionCycle,
		},
		{
			name: "cross author target",
			journal: func() []PreferenceSignal {
				target, _ := validPreferenceSignal("selection")
				target.SignalID = "signal-target"
				correction, _ := validPreferenceSignal("correction")
				correction.SignalID = "signal-correction"
				correction.Author.ActorID, correction.Scope.AuthorID = "author-002", "author-002"
				correction.EventReference.CorrectedSignalID, correction.SupersedesSignalIDs = target.SignalID, []string{target.SignalID}
				return []PreferenceSignal{target, correction}
			},
			code: CodeAuthorityViolation,
		},
		{
			name: "scope expansion reuses confirmation",
			journal: func() []PreferenceSignal {
				target, _ := validPreferenceSignal("selection")
				target.SignalID = "signal-target"
				correction, _ := validPreferenceSignal("correction")
				correction.SignalID = "signal-correction"
				correction.Scope.Kind = "author"
				correction.EventReference.CorrectedSignalID, correction.SupersedesSignalIDs = target.SignalID, []string{target.SignalID}
				correction.Confirmation.ConfirmedAt = target.Confirmation.ConfirmedAt
				correction.Confirmation.EvidenceHash = target.Confirmation.EvidenceHash
				return []PreferenceSignal{target, correction}
			},
			code: CodeScopeExpansion,
		},
		{
			name: "revocation has no target",
			journal: func() []PreferenceSignal {
				revocation, _ := validPreferenceSignal("revocation")
				revocation.RevokesSignalIDs = nil
				return []PreferenceSignal{revocation}
			},
			code: CodeReferenceInvalid,
		},
		{
			name: "revocation repeats a target",
			journal: func() []PreferenceSignal {
				target, _ := validPreferenceSignal("selection")
				target.SignalID = "signal-target"
				revocation, _ := validPreferenceSignal("revocation")
				revocation.SignalID = "signal-revocation"
				revocation.RevokesSignalIDs = []string{target.SignalID, target.SignalID}
				return []PreferenceSignal{target, revocation}
			},
			code: CodeReferenceInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCandidateContractCode(t, ValidatePreferenceJournal(test.journal()), test.code)
		})
	}
}

func TestPreferenceJournalScopeExpansionUsesAbsoluteConfirmationTime(t *testing.T) {
	target, _ := validPreferenceSignal("selection")
	target.SignalID = "signal-target"
	target.Confirmation.ConfirmedAt = "2026-07-21T00:30:00Z"
	target.RecordedAt = "2026-07-21T00:31:00Z"
	correction, _ := validPreferenceSignal("correction")
	correction.SignalID = "signal-correction"
	correction.Scope.Kind = "author"
	correction.EventReference.CorrectedSignalID = target.SignalID
	correction.SupersedesSignalIDs = []string{target.SignalID}
	correction.Confirmation.ConfirmedAt = "2026-07-21T01:00:00+02:00"
	correction.Confirmation.EvidenceHash = hashForTest([]byte("fresh-looking but earlier confirmation"))
	assertCandidateContractCode(t, ValidatePreferenceJournal([]PreferenceSignal{target, correction}), CodeScopeExpansion)
}

func validPreferenceSignal(event string) (PreferenceSignal, PreferenceSignalReferences) {
	references := PreferenceSignalReferences{
		Workspace:            []byte("workspace"),
		Profile:              []byte("profile"),
		QualitySpec:          []byte("quality-spec"),
		Source:               []byte("candidate"),
		ConfirmationEvidence: []byte("confirmation"),
		CandidateSetID:       "candidate-set-001",
		CandidateID:          "candidate-001",
		Candidate:            []byte("candidate"),
		ComposedArtifactID:   "composed-artifact-001",
		ComposedArtifact:     []byte("composed"),
		ParentCandidateIDs:   []string{"candidate-001", "candidate-002"},
		SegmentMap:           []byte("segment-map"),
		IssueID:              "review-issue-001",
		ReviewIssue:          []byte("review-issue"),
		OriginalArtifactID:   "artifact-original",
		OriginalArtifact:     []byte("original"),
		RewrittenArtifactID:  "artifact-rewritten",
		RewrittenArtifact:    []byte("rewritten"),
		RuleID:               "rule-001",
		Rule:                 []byte("rule"),
		ReplacementEvidence:  []byte("replacement"),
		RevocationReason:     []byte("revocation"),
	}
	signal := PreferenceSignal{
		Contract: PreferenceContract{Kind: PreferenceSignalContractKind, Version: ContractVersionV1, SchemaID: "preference-memory-v1.schema.json"},
		SignalID: "preference-signal-001", Event: event,
		Scope:        PreferenceScope{Kind: "workspace", AuthorID: "author-001", ProjectID: "project-001", WorkspaceID: "workspace-001"},
		Author:       PreferenceAuthor{ActorID: "author-001", ActorType: "author"},
		Workspace:    PreferenceWorkspace{WorkspaceID: "workspace-001", ProjectID: "project-001", CanonicalPath: "/workspace", Revision: "revision-001", ContentHash: hashForTest(references.Workspace)},
		Provenance:   PreferenceProvenance{SourceKind: "candidate", OperationID: "operation-001", Profile: PreferenceProfileBinding{ID: "long_serial", Version: "v1", Hash: hashForTest(references.Profile)}, QualitySpec: PreferenceQualitySpecBinding{ID: "quality-spec-001", Revision: "1", Version: "v1", Hash: hashForTest(references.QualitySpec)}, ContentHash: hashForTest(references.Source)},
		Preference:   PreferenceValue{Dimension: "opening_dialogue", Value: "shorter", Reason: "faster entry", Strength: "normal", Confidence: 1},
		Confirmation: PreferenceConfirmation{Explicit: true, Method: event, ConfirmedAt: candidateTestTime, EvidenceHash: hashForTest(references.ConfirmationEvidence)},
		RecordedAt:   candidateTestTime,
	}
	switch event {
	case "selection":
		signal.Provenance.SourceKind = "candidate"
		signal.EventReference = PreferenceEventReference{Kind: "selection", CandidateSetID: references.CandidateSetID, CandidateID: references.CandidateID, CandidateHash: hashForTest(references.Candidate)}
	case "mixed_selection":
		signal.Provenance.SourceKind = "candidate_segment"
		references.Source = references.ComposedArtifact
		signal.Provenance.ContentHash = hashForTest(references.Source)
		signal.EventReference = PreferenceEventReference{Kind: "mixed_selection", CandidateSetID: references.CandidateSetID, ComposedArtifactID: references.ComposedArtifactID, ComposedHash: hashForTest(references.ComposedArtifact), ParentCandidateIDs: append([]string(nil), references.ParentCandidateIDs...), SegmentMapHash: hashForTest(references.SegmentMap)}
	case "rejection":
		signal.Provenance.SourceKind = "review_issue"
		references.Source = references.ReviewIssue
		signal.Provenance.ContentHash = hashForTest(references.Source)
		signal.EventReference = PreferenceEventReference{Kind: "issue_rejection", IssueID: references.IssueID, IssueHash: hashForTest(references.ReviewIssue)}
	case "author_rewrite":
		signal.Provenance.SourceKind = "author_rewrite"
		references.Source = references.RewrittenArtifact
		signal.Provenance.ContentHash = hashForTest(references.Source)
		signal.EventReference = PreferenceEventReference{Kind: "author_rewrite", OriginalArtifactID: references.OriginalArtifactID, OriginalHash: hashForTest(references.OriginalArtifact), RewrittenArtifactID: references.RewrittenArtifactID, RewrittenHash: hashForTest(references.RewrittenArtifact)}
	case "rule_confirmation":
		signal.Provenance.SourceKind = "author_rule"
		references.Source = references.Rule
		signal.Provenance.ContentHash = hashForTest(references.Source)
		signal.EventReference = PreferenceEventReference{Kind: "rule", RuleID: references.RuleID, RuleHash: hashForTest(references.Rule)}
	case "correction":
		signal.Provenance.SourceKind = "candidate"
		signal.EventReference = PreferenceEventReference{Kind: "correction", CorrectedSignalID: "preference-target-001", ReplacementEvidenceHash: hashForTest(references.ReplacementEvidence)}
		signal.SupersedesSignalIDs = []string{"preference-target-001"}
	case "revocation":
		signal.Provenance.SourceKind = "author_revocation"
		references.Source = references.RevocationReason
		signal.Provenance.ContentHash = hashForTest(references.Source)
		signal.EventReference = PreferenceEventReference{Kind: "revocation", RevocationReasonHash: hashForTest(references.RevocationReason)}
		signal.RevokesSignalIDs = []string{"preference-target-001"}
	}
	return signal, references
}
