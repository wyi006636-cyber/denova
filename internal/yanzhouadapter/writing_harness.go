package yanzhouadapter

import (
	"fmt"
	"regexp"
)

// WritingHarnessProfileID is the closed WP7 execution-profile set.
type WritingHarnessProfileID string

const (
	HarnessProfileNovelLite     WritingHarnessProfileID = "novel-lite"
	HarnessProfileNovelStandard WritingHarnessProfileID = "novel-standard"
	HarnessProfileNovelHeavy    WritingHarnessProfileID = "novel-heavy"
)

// WritingHarnessRoleID is a stage role, not a filesystem or writer authority.
type WritingHarnessRoleID string

const (
	HarnessRolePrimaryWriter        WritingHarnessRoleID = "primary-writer"
	HarnessRoleDeterministicChecker WritingHarnessRoleID = "deterministic-checker"
	HarnessRoleContextPlanner       WritingHarnessRoleID = "context-planner"
	HarnessRoleWriter               WritingHarnessRoleID = "writer"
	HarnessRoleReviewer             WritingHarnessRoleID = "reviewer"
	HarnessRoleFixer                WritingHarnessRoleID = "fixer"
	HarnessRoleFinalGate            WritingHarnessRoleID = "final-gate"
	HarnessRoleMemoryPatcher        WritingHarnessRoleID = "memory-patcher"
)

type WritingHarnessFailure string

const HarnessFailureStopPreserve WritingHarnessFailure = "stop_preserve"

type WritingHarnessBudget struct {
	SchemaVersion     string `json:"schemaVersion"`
	MaxModelCalls     int    `json:"maxModelCalls"`
	MaxToolRounds     int    `json:"maxToolRounds"`
	MaxDelegations    int    `json:"maxDelegations"`
	MaxRevisionRounds int    `json:"maxRevisionRounds"`
	MaxWallTimeMS     int    `json:"maxWallTimeMs"`
	MaxInputTokens    int    `json:"maxInputTokens"`
	MaxOutputTokens   int    `json:"maxOutputTokens"`
}

type WritingHarnessStage struct {
	ID          string                `json:"id"`
	RoleID      WritingHarnessRoleID  `json:"role"`
	Delegated   bool                  `json:"delegated"`
	Permissions []string              `json:"permissions"`
	OutputKind  string                `json:"outputKind"`
	NextStageID string                `json:"nextStageId,omitempty"`
	OnFailure   WritingHarnessFailure `json:"onFailure"`
}

type WritingHarnessProfile struct {
	SchemaVersion string                  `json:"schemaVersion"`
	ID            WritingHarnessProfileID `json:"id"`
	Stages        []WritingHarnessStage   `json:"stages"`
	Budget        WritingHarnessBudget    `json:"budget"`
}

var harnessProfileIDs = []WritingHarnessProfileID{
	HarnessProfileNovelLite,
	HarnessProfileNovelStandard,
	HarnessProfileNovelHeavy,
}

var harnessRoleIDs = []WritingHarnessRoleID{
	HarnessRolePrimaryWriter,
	HarnessRoleDeterministicChecker,
	HarnessRoleContextPlanner,
	HarnessRoleWriter,
	HarnessRoleReviewer,
	HarnessRoleFixer,
	HarnessRoleFinalGate,
	HarnessRoleMemoryPatcher,
}

var harnessArtifactKinds = map[string]bool{
	"plan": true, "conception": true, "outline": true, "draft": true,
	"transform": true, "review": true, "repair": true, "state_patch": true,
	"game_turn": true, "adaptation": true, "image": true, "report": true,
}

func harnessBudget(model, tool, delegation, revision, wall, input, output int) WritingHarnessBudget {
	return WritingHarnessBudget{
		SchemaVersion: "1", MaxModelCalls: model, MaxToolRounds: tool,
		MaxDelegations: delegation, MaxRevisionRounds: revision, MaxWallTimeMS: wall,
		MaxInputTokens: input, MaxOutputTokens: output,
	}
}

func harnessStage(id string, role WritingHarnessRoleID, delegated bool, permissions []string, outputKind string) WritingHarnessStage {
	return WritingHarnessStage{
		ID: id, RoleID: role, Delegated: delegated,
		Permissions: append([]string(nil), permissions...), OutputKind: outputKind,
		OnFailure: HarnessFailureStopPreserve,
	}
}

func linkedHarnessStages(stages []WritingHarnessStage) []WritingHarnessStage {
	cloned := cloneHarnessStages(stages)
	for index := range cloned {
		if index+1 < len(cloned) {
			cloned[index].NextStageID = cloned[index+1].ID
		}
	}
	return cloned
}

// WritingHarnessProfiles returns independent copies so callers cannot mutate the adapter contract.
func WritingHarnessProfiles() []WritingHarnessProfile {
	readTarget := []string{"story.get_target"}
	readReview := []string{"story.get_target", "story.search_chapters", "story.get_outline"}
	proposeText := []string{"story.get_target", "writing.create_artifact"}
	profiles := []WritingHarnessProfile{
		{
			SchemaVersion: "1", ID: HarnessProfileNovelLite,
			Stages: linkedHarnessStages([]WritingHarnessStage{
				harnessStage("draft", HarnessRolePrimaryWriter, false, proposeText, "draft"),
				harnessStage("deterministic-checks", HarnessRoleDeterministicChecker, false, readTarget, "report"),
			}),
			Budget: harnessBudget(1, 2, 0, 0, 60_000, 32_000, 8_000),
		},
		{
			SchemaVersion: "1", ID: HarnessProfileNovelStandard,
			Stages: linkedHarnessStages([]WritingHarnessStage{
				harnessStage("draft", HarnessRolePrimaryWriter, false, proposeText, "draft"),
				harnessStage("review", HarnessRoleReviewer, true, readReview, "review"),
				harnessStage("primary-revision", HarnessRolePrimaryWriter, false, proposeText, "transform"),
				harnessStage("deterministic-checks", HarnessRoleDeterministicChecker, false, readTarget, "report"),
			}),
			Budget: harnessBudget(3, 4, 1, 1, 120_000, 64_000, 16_000),
		},
		{
			SchemaVersion: "1", ID: HarnessProfileNovelHeavy,
			Stages: linkedHarnessStages([]WritingHarnessStage{
				harnessStage("context-plan", HarnessRoleContextPlanner, true, readReview, "plan"),
				harnessStage("draft", HarnessRoleWriter, true, proposeText, "draft"),
				harnessStage("review", HarnessRoleReviewer, true, readReview, "review"),
				harnessStage("repair", HarnessRoleFixer, true, proposeText, "repair"),
				harnessStage("final-gate", HarnessRoleFinalGate, true, readReview, "report"),
				harnessStage("state-patch", HarnessRoleMemoryPatcher, true, []string{"story.get_target", "setting.create_patch_proposal"}, "state_patch"),
			}),
			Budget: harnessBudget(7, 8, 7, 2, 240_000, 128_000, 32_000),
		},
	}
	return cloneHarnessProfiles(profiles)
}

func (budget WritingHarnessBudget) Validate() error {
	if budget.SchemaVersion != "1" || budget.MaxModelCalls < 0 || budget.MaxToolRounds < 0 || budget.MaxDelegations < 0 || budget.MaxRevisionRounds < 0 || budget.MaxWallTimeMS < 1 || budget.MaxInputTokens < 1 || budget.MaxOutputTokens < 1 {
		return fmt.Errorf("WritingHarness budget is invalid")
	}
	return nil
}

func (profile WritingHarnessProfile) Validate() error {
	if profile.SchemaVersion != "1" || !knownHarnessProfileID(profile.ID) || len(profile.Stages) < 2 || len(profile.Stages) > 16 || profile.Budget.Validate() != nil {
		return fmt.Errorf("WritingHarness profile is invalid")
	}
	seenStages := map[string]bool{}
	for index, stage := range profile.Stages {
		if !validPlanSchemaID(stage.ID) || seenStages[stage.ID] || !knownHarnessRoleID(stage.RoleID) || !harnessArtifactKinds[stage.OutputKind] || stage.OnFailure != HarnessFailureStopPreserve || len(stage.Permissions) > 32 {
			return fmt.Errorf("WritingHarness stage is invalid")
		}
		seenStages[stage.ID] = true
		seenCapabilities := map[string]bool{}
		for _, capability := range stage.Permissions {
			if _, ok := knownToolMode(capability); !ok || seenCapabilities[capability] {
				return fmt.Errorf("WritingHarness stage permission is invalid")
			}
			seenCapabilities[capability] = true
		}
		expectedNext := ""
		if index+1 < len(profile.Stages) {
			expectedNext = profile.Stages[index+1].ID
		}
		if stage.NextStageID != expectedNext {
			return fmt.Errorf("WritingHarness transition is invalid")
		}
	}
	return nil
}

func knownHarnessProfileID(value WritingHarnessProfileID) bool {
	for _, candidate := range harnessProfileIDs {
		if value == candidate {
			return true
		}
	}
	return false
}

func knownHarnessRoleID(value WritingHarnessRoleID) bool {
	for _, candidate := range harnessRoleIDs {
		if value == candidate {
			return true
		}
	}
	return false
}

type WritingHarnessScopeKind string

const (
	HarnessScopeParagraph WritingHarnessScopeKind = "paragraph"
	HarnessScopeScene     WritingHarnessScopeKind = "scene"
	HarnessScopeChapter   WritingHarnessScopeKind = "chapter"
	HarnessScopeNChapters WritingHarnessScopeKind = "n_chapters"
	HarnessScopeArc       WritingHarnessScopeKind = "arc"
)

type WritingHarnessScope struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Kind          WritingHarnessScopeKind `json:"kind"`
	BookID        string                  `json:"bookId"`
	Target        *ToolTarget             `json:"target,omitempty"`
	Targets       []ToolTarget            `json:"targets,omitempty"`
	SceneID       string                  `json:"sceneId,omitempty"`
	ArcID         string                  `json:"arcId,omitempty"`
	Start         int                     `json:"start,omitempty"`
	End           int                     `json:"end,omitempty"`
}

func (scope WritingHarnessScope) Validate() error {
	if scope.SchemaVersion != "1" || !validPlanSchemaID(scope.BookID) {
		return fmt.Errorf("WritingHarness scope is invalid")
	}
	validateTarget := func(target ToolTarget, kind string) bool {
		return target.Validate() == nil && target.Kind == kind && target.BookID == scope.BookID
	}
	switch scope.Kind {
	case HarnessScopeParagraph:
		if scope.Target == nil || !validateTarget(*scope.Target, "text_selection") || scope.Start < 0 || scope.End <= scope.Start || len(scope.Targets) != 0 || scope.SceneID != "" || scope.ArcID != "" {
			return fmt.Errorf("WritingHarness paragraph scope is invalid")
		}
	case HarnessScopeScene:
		if scope.Target == nil || !validateTarget(*scope.Target, "chapter") || !validPlanSchemaID(scope.SceneID) || len(scope.Targets) != 0 || scope.ArcID != "" || scope.Start != 0 || scope.End != 0 {
			return fmt.Errorf("WritingHarness scene scope is invalid")
		}
	case HarnessScopeChapter:
		if scope.Target == nil || !validateTarget(*scope.Target, "chapter") || len(scope.Targets) != 0 || scope.SceneID != "" || scope.ArcID != "" || scope.Start != 0 || scope.End != 0 {
			return fmt.Errorf("WritingHarness chapter scope is invalid")
		}
	case HarnessScopeNChapters, HarnessScopeArc:
		minimum := 2
		if scope.Kind == HarnessScopeArc {
			minimum = 1
			if !validPlanSchemaID(scope.ArcID) {
				return fmt.Errorf("WritingHarness arc scope is invalid")
			}
		} else if scope.ArcID != "" {
			return fmt.Errorf("WritingHarness N-chapter scope is invalid")
		}
		if scope.Target != nil || scope.SceneID != "" || scope.Start != 0 || scope.End != 0 || len(scope.Targets) < minimum || len(scope.Targets) > 64 {
			return fmt.Errorf("WritingHarness multi-target scope is invalid")
		}
		seen := map[string]bool{}
		for _, target := range scope.Targets {
			if !validateTarget(target, "chapter") || seen[target.TargetID] {
				return fmt.Errorf("WritingHarness multi-target scope is invalid")
			}
			seen[target.TargetID] = true
		}
	default:
		return fmt.Errorf("WritingHarness scope kind is invalid")
	}
	return nil
}

type WritingHarnessSkillEvidence struct {
	RunID         string
	SkillID       string
	CapabilityID  string
	ScopeKind     WritingHarnessScopeKind
	CatalogEntry  WritingHarnessCatalogEntry
	LoadReceipt   SkillLoadReceipt
	SkillSnapshot SkillSnapshot
}

type WritingHarnessCatalogEntry struct {
	ID               string   `json:"id"`
	CompatibleSkills []string `json:"compatibleSkills"`
}

type WritingHarnessSkillResolution struct {
	SkillID      string           `json:"skillId"`
	CapabilityID string           `json:"capabilityId"`
	Receipt      SkillLoadReceipt `json:"receipt"`
}

var midWritingSkillCapabilities = map[string][]string{
	"continue":             {"chapter.continue"},
	"rewrite":              {"chapter.rewrite"},
	"outline":              {"outline.main.create", "outline.main.rewrite", "outline.volume.create", "outline.volume.rewrite", "outline.chapter.create", "outline.chapter.expand", "outline.chapter.rewrite"},
	"group-plan":           {"outline.main.create", "outline.volume.create"},
	"chapter-illustration": {"image.generate"},
}

func ResolveWritingHarnessSkill(evidence WritingHarnessSkillEvidence) (WritingHarnessSkillResolution, error) {
	capabilities, known := midWritingSkillCapabilities[evidence.SkillID]
	if !known || !validPlanSchemaID(evidence.RunID) || !validPlanSchemaID(evidence.CapabilityID) || evidence.CatalogEntry.Validate() != nil || evidence.CatalogEntry.ID != evidence.CapabilityID || evidence.LoadReceipt.Validate() != nil || evidence.LoadReceipt.ID != evidence.SkillID || evidence.SkillSnapshot.SchemaVersion != "1" || evidence.SkillSnapshot.RunID != evidence.RunID {
		return WritingHarnessSkillResolution{}, fmt.Errorf("WritingHarness Skill evidence is invalid")
	}
	catalogAllowsSkill := false
	for _, skillID := range evidence.CatalogEntry.CompatibleSkills {
		if skillID == evidence.SkillID {
			catalogAllowsSkill = true
			break
		}
	}
	if !catalogAllowsSkill {
		return WritingHarnessSkillResolution{}, fmt.Errorf("WritingHarness Skill Catalog entry does not authorize the Skill")
	}
	allowed := false
	for _, capability := range capabilities {
		if capability == evidence.CapabilityID {
			allowed = true
			break
		}
	}
	if !allowed || (evidence.SkillID == "group-plan" && evidence.ScopeKind != HarnessScopeNChapters) {
		return WritingHarnessSkillResolution{}, fmt.Errorf("WritingHarness Skill capability is invalid")
	}
	matched := false
	for _, receipt := range evidence.SkillSnapshot.Skills {
		if receipt.ID == evidence.SkillID && receipt.Revision == evidence.LoadReceipt.Revision && receipt.Checksum == evidence.LoadReceipt.Checksum && receipt.Source == evidence.LoadReceipt.Source {
			matched = true
			break
		}
	}
	if !matched {
		return WritingHarnessSkillResolution{}, fmt.Errorf("WritingHarness Skill receipt does not match the run snapshot")
	}
	return WritingHarnessSkillResolution{SkillID: evidence.SkillID, CapabilityID: evidence.CapabilityID, Receipt: cloneSkillReceipt(evidence.LoadReceipt)}, nil
}

func (entry WritingHarnessCatalogEntry) Validate() error {
	if !validPlanSchemaID(entry.ID) || len(entry.CompatibleSkills) == 0 || len(entry.CompatibleSkills) > 64 {
		return fmt.Errorf("WritingHarness Skill Catalog entry is invalid")
	}
	seen := map[string]bool{}
	for _, skillID := range entry.CompatibleSkills {
		if !validPlanSchemaID(skillID) || seen[skillID] {
			return fmt.Errorf("WritingHarness Skill Catalog entry is invalid")
		}
		seen[skillID] = true
	}
	return nil
}

type WritingHarnessArtifactProjection struct {
	SchemaVersion      string               `json:"schemaVersion"`
	RunID              string               `json:"runId"`
	StageID            string               `json:"stageId"`
	RoleID             WritingHarnessRoleID `json:"role"`
	ArtifactID         string               `json:"artifactId"`
	ArtifactKind       string               `json:"artifactKind"`
	Status             string               `json:"status"`
	ParentArtifactRefs []string             `json:"parentArtifactRefs"`
	ContextPackID      string               `json:"contextPackId"`
	SkillSnapshotID    string               `json:"skillSnapshotId"`
	ModelProfileID     string               `json:"modelProfileId"`
	BaseRevisions      map[string]string    `json:"baseRevisions"`
}

var harnessRevisionPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (projection WritingHarnessArtifactProjection) Validate() error {
	if projection.SchemaVersion != "1" || !validPlanSchemaID(projection.RunID) || !validPlanSchemaID(projection.StageID) || !knownHarnessRoleID(projection.RoleID) || !validPlanSchemaID(projection.ArtifactID) || !harnessArtifactKinds[projection.ArtifactKind] || (projection.Status != "partial" && projection.Status != "complete" && projection.Status != "invalid" && projection.Status != "superseded") || !validPlanSchemaID(projection.ContextPackID) || !validPlanSchemaID(projection.SkillSnapshotID) || !validPlanSchemaID(projection.ModelProfileID) || len(projection.BaseRevisions) == 0 || len(projection.BaseRevisions) > 64 || len(projection.ParentArtifactRefs) > 32 {
		return fmt.Errorf("WritingHarness Artifact projection is invalid")
	}
	seen := map[string]bool{}
	for _, ref := range projection.ParentArtifactRefs {
		if !validPlanSchemaID(ref) || seen[ref] || ref == projection.ArtifactID {
			return fmt.Errorf("WritingHarness Artifact parent is invalid")
		}
		seen[ref] = true
	}
	for key, revision := range projection.BaseRevisions {
		if !validPlanSchemaID(key) || !harnessRevisionPattern.MatchString(revision) {
			return fmt.Errorf("WritingHarness Artifact revision is invalid")
		}
	}
	return nil
}

func cloneHarnessStages(stages []WritingHarnessStage) []WritingHarnessStage {
	cloned := make([]WritingHarnessStage, len(stages))
	for index, stage := range stages {
		cloned[index] = stage
		cloned[index].Permissions = append([]string(nil), stage.Permissions...)
	}
	return cloned
}

func cloneHarnessProfiles(profiles []WritingHarnessProfile) []WritingHarnessProfile {
	cloned := make([]WritingHarnessProfile, len(profiles))
	for index, profile := range profiles {
		cloned[index] = profile
		cloned[index].Stages = cloneHarnessStages(profile.Stages)
	}
	return cloned
}
