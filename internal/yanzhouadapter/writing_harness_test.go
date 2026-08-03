package yanzhouadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWritingHarnessProfilesMatchWP7Contract(t *testing.T) {
	started := time.Now()
	profiles := WritingHarnessProfiles()
	contractJSON, err := json.Marshal(profiles)
	if err != nil {
		t.Fatalf("marshal WritingHarness contract: %v", err)
	}
	contractHash := sha256.Sum256(contractJSON)
	if got, want := hex.EncodeToString(contractHash[:]), "b731d9e7e542b26187caa0fdba3948f4e51f24c87fa4cafb86fb520566815ce9"; got != want {
		t.Fatalf("cross-language WritingHarness contract digest = %s, want %s", got, want)
	}
	if got, want := profileIDs(profiles), []WritingHarnessProfileID{HarnessProfileNovelLite, HarnessProfileNovelStandard, HarnessProfileNovelHeavy}; !reflect.DeepEqual(got, want) {
		t.Fatalf("profile ids = %v, want %v", got, want)
	}
	if got, want := profiles[0].Budget.MaxModelCalls, 2; got != want {
		t.Fatalf("lite max model calls = %d, want %d", got, want)
	}
	if got, want := profiles[1].Budget.MaxModelCalls, 5; got != want {
		t.Fatalf("standard max model calls = %d, want %d", got, want)
	}
	roles := map[WritingHarnessProfileID][]WritingHarnessRoleID{
		HarnessProfileNovelLite:     {HarnessRolePrimaryWriter, HarnessRoleDeterministicChecker},
		HarnessProfileNovelStandard: {HarnessRolePrimaryWriter, HarnessRoleReviewer, HarnessRolePrimaryWriter, HarnessRoleDeterministicChecker},
		HarnessProfileNovelHeavy:    {HarnessRoleContextPlanner, HarnessRoleWriter, HarnessRoleReviewer, HarnessRoleFixer, HarnessRoleFinalGate, HarnessRoleMemoryPatcher},
	}
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			t.Fatalf("profile %s is invalid: %v", profile.ID, err)
		}
		if got := stageRoles(profile.Stages); !reflect.DeepEqual(got, roles[profile.ID]) {
			t.Fatalf("profile %s roles = %v, want %v", profile.ID, got, roles[profile.ID])
		}
		if profile.Budget.MaxWallTimeMS < 1 || profile.Budget.MaxInputTokens < 1 || profile.Budget.MaxOutputTokens < 1 {
			t.Fatalf("profile %s has an unbounded time/token budget", profile.ID)
		}
		for _, stage := range profile.Stages {
			if stage.OnFailure != HarnessFailureStopPreserve {
				t.Fatalf("profile %s stage %s does not preserve completed Artifacts", profile.ID, stage.ID)
			}
			for _, capability := range stage.Permissions {
				if strings.Contains(capability, "filesystem") || strings.Contains(capability, "shell") || strings.Contains(capability, "workspace") {
					t.Fatalf("stage %s grants forbidden authority %s", stage.ID, capability)
				}
			}
		}
	}
	heavy := profiles[2]
	for _, role := range []WritingHarnessRoleID{HarnessRoleReviewer, HarnessRoleFinalGate} {
		stage := stageByRole(heavy.Stages, role)
		if harnessContainsString(stage.Permissions, "writing.create_artifact") || harnessContainsString(stage.Permissions, "setting.create_patch_proposal") {
			t.Fatalf("read-only role %s can propose", role)
		}
	}
	memory := stageByRole(heavy.Stages, HarnessRoleMemoryPatcher)
	if got, want := memory.Permissions, []string{"story.get_target", "setting.create_patch_proposal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("memory-patcher permissions = %v, want %v", got, want)
	}
	if time.Since(started) >= time.Second {
		t.Fatalf("WritingHarness profile test exceeded one second")
	}
}

func TestImageGenerationUsesItsRequestedProviderWindow(t *testing.T) {
	profile := WritingHarnessProfiles()[0]
	request := planRunRequest{
		CapabilityID: "image.generate",
		Budgets:      planRunBudget{MaxWallTimeMS: 300_000},
	}
	if got, want := writingRunWallTimeMS(request, profile), 300_000; got != want {
		t.Fatalf("image wall time = %d, want %d", got, want)
	}
	request.CapabilityID = "command.run"
	if got, want := writingRunWallTimeMS(request, profile), profile.Budget.MaxWallTimeMS; got != want {
		t.Fatalf("command wall time = %d, want %d", got, want)
	}
}

func TestWritingHarnessScopeAndSkillEvidenceFailClosed(t *testing.T) {
	target := ToolTarget{SchemaVersion: "1", Kind: "chapter", BookID: "book-1", TargetID: "chapter-1"}
	scopes := []WritingHarnessScope{
		{SchemaVersion: "1", Kind: HarnessScopeParagraph, BookID: "book-1", Target: &ToolTarget{SchemaVersion: "1", Kind: "text_selection", BookID: "book-1", TargetID: "selection-1"}, Start: 1, End: 8},
		{SchemaVersion: "1", Kind: HarnessScopeScene, BookID: "book-1", Target: &target, SceneID: "scene-1"},
		{SchemaVersion: "1", Kind: HarnessScopeChapter, BookID: "book-1", Target: &target},
		{SchemaVersion: "1", Kind: HarnessScopeNChapters, BookID: "book-1", Targets: []ToolTarget{target, {SchemaVersion: "1", Kind: "chapter", BookID: "book-1", TargetID: "chapter-2"}}},
		{SchemaVersion: "1", Kind: HarnessScopeArc, BookID: "book-1", ArcID: "arc-1", Targets: []ToolTarget{target}},
	}
	for _, scope := range scopes {
		if err := scope.Validate(); err != nil {
			t.Fatalf("scope %s is invalid: %v", scope.Kind, err)
		}
	}
	invalid := []WritingHarnessScope{
		{},
		{SchemaVersion: "1", Kind: HarnessScopeChapter, BookID: "book-1"},
		{SchemaVersion: "1", Kind: HarnessScopeNChapters, BookID: "book-1", Targets: []ToolTarget{target}},
		{SchemaVersion: "1", Kind: HarnessScopeNChapters, BookID: "book-1", Targets: []ToolTarget{target, {SchemaVersion: "1", Kind: "chapter", BookID: "book-2", TargetID: "chapter-2"}}},
	}
	for _, scope := range invalid {
		if err := scope.Validate(); err == nil {
			t.Fatalf("invalid scope unexpectedly passed: %+v", scope)
		}
	}

	checksum := "sha256:" + strings.Repeat("a", 64)
	receipt := SkillLoadReceipt{SchemaVersion: "1", ID: "group-plan", Revision: 1, Checksum: checksum, Source: SkillSourceBuiltin}
	snapshot := SkillSnapshot{SchemaVersion: "1", RunID: "run-1", Skills: []SkillLoadReceipt{receipt}}
	catalog := WritingHarnessCatalogEntry{ID: "outline.volume.create", CompatibleSkills: []string{"group-plan"}}
	resolution, err := ResolveWritingHarnessSkill(WritingHarnessSkillEvidence{RunID: "run-1", SkillID: "group-plan", CapabilityID: "outline.volume.create", ScopeKind: HarnessScopeNChapters, CatalogEntry: catalog, LoadReceipt: receipt, SkillSnapshot: snapshot})
	if err != nil || resolution.CapabilityID != "outline.volume.create" {
		t.Fatalf("group-plan resolution failed: resolution=%+v err=%v", resolution, err)
	}
	for _, evidence := range []WritingHarnessSkillEvidence{
		{SkillID: "continue"},
		{RunID: "run-1", SkillID: "group-plan", CapabilityID: "outline.volume.create", ScopeKind: HarnessScopeNChapters, LoadReceipt: receipt, SkillSnapshot: snapshot},
		{RunID: "run-1", SkillID: "group-plan", CapabilityID: "outline.volume.create", ScopeKind: HarnessScopeNChapters, CatalogEntry: WritingHarnessCatalogEntry{ID: "outline.main.create", CompatibleSkills: []string{"group-plan"}}, LoadReceipt: receipt, SkillSnapshot: snapshot},
		{RunID: "run-1", SkillID: "group-plan", CapabilityID: "outline.volume.create", ScopeKind: HarnessScopeNChapters, CatalogEntry: WritingHarnessCatalogEntry{ID: "outline.volume.create", CompatibleSkills: []string{"outline"}}, LoadReceipt: receipt, SkillSnapshot: snapshot},
		{RunID: "run-1", SkillID: "group-plan", CapabilityID: "outline.volume.create", ScopeKind: HarnessScopeChapter, CatalogEntry: catalog, LoadReceipt: receipt, SkillSnapshot: snapshot},
		{RunID: "run-1", SkillID: "chapter-illustration", CapabilityID: "chapter.continue", ScopeKind: HarnessScopeChapter, CatalogEntry: WritingHarnessCatalogEntry{ID: "chapter.continue", CompatibleSkills: []string{"chapter-illustration"}}, LoadReceipt: receipt, SkillSnapshot: snapshot},
	} {
		if _, err := ResolveWritingHarnessSkill(evidence); err == nil {
			t.Fatalf("invalid Skill evidence unexpectedly passed: %+v", evidence)
		}
	}
}

func TestWritingHarnessArtifactProjectionUsesExistingTerminalContract(t *testing.T) {
	projection := WritingHarnessArtifactProjection{
		SchemaVersion:      "1",
		RunID:              "run-1",
		StageID:            "review",
		RoleID:             HarnessRoleReviewer,
		ArtifactID:         "artifact-review-1",
		ArtifactKind:       "review",
		Status:             "complete",
		ParentArtifactRefs: []string{"artifact-draft-1"},
		ContextPackID:      "context-pack-1",
		SkillSnapshotID:    "skill-snapshot-1",
		ModelProfileID:     "model-profile-1",
		BaseRevisions:      map[string]string{"chapter-1": "sha256:" + strings.Repeat("b", 64)},
	}
	if err := projection.Validate(); err != nil {
		t.Fatalf("Artifact projection is invalid: %v", err)
	}
	decision, err := ClassifyTermination(TerminationInput{Cause: TerminationCauseBudgetExhausted, PartialArtifactRefs: []string{projection.ArtifactID}})
	if err != nil || decision.EventType != RunEventTypeRunBudgetExhausted || decision.State != TerminalRunStateBudgetExhausted {
		t.Fatalf("budget terminal does not reuse WP3 contract: decision=%+v err=%v", decision, err)
	}
	if strings.Contains(strings.ToLower(strings.Join(projection.ParentArtifactRefs, " ")), "path") {
		t.Fatalf("Artifact projection leaked a path")
	}
}

func profileIDs(profiles []WritingHarnessProfile) []WritingHarnessProfileID {
	ids := make([]WritingHarnessProfileID, len(profiles))
	for index, profile := range profiles {
		ids[index] = profile.ID
	}
	return ids
}

func stageRoles(stages []WritingHarnessStage) []WritingHarnessRoleID {
	roles := make([]WritingHarnessRoleID, len(stages))
	for index, stage := range stages {
		roles[index] = stage.RoleID
	}
	return roles
}

func stageByRole(stages []WritingHarnessStage, role WritingHarnessRoleID) WritingHarnessStage {
	for _, stage := range stages {
		if stage.RoleID == role {
			return stage
		}
	}
	return WritingHarnessStage{}
}

func harnessContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
