# P0 Offline Harness Evaluation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a cohort-aware, evaluation-only `p0-offline-harness-v1` runner that produces real S/H pairs without entering product runtime or touching release holdout.

**Architecture:** Extend the existing `internal/quality/evaluation` contracts rather than creating a second evaluation stack. A selected cohort gets a stable run identity; H executes two independent candidates, structured review, and final revision through the existing OpenAI-compatible Provider adapter, persists private stage evidence atomically, and feeds the existing blind-review pipeline only when both arms are complete.

**Tech Stack:** Go 1.26.5, existing Eino OpenAI-compatible model adapter, strict JSON contracts, SHA-256, PowerShell for explicit live execution, existing Go/Vitest/build gates.

## Global Constraints

- No product API, SSE event, React page, menu, setting, Automation, workspace migration, Author Finalization, or formal novel Markdown write.
- No production CandidateSet, ReviewIssue repository, PreferenceMemory, Capability Router, Skill installer, or third-party script execution.
- Use the same task facts, Provider, `deepseek-v4-pro`, model profile, temperature, output-token ceiling, and task-level QualitySpec for S and H.
- H is exactly two independent candidates, one structured review, and one final revision; all four calls count toward usage and cost.
- K remains a separate capability-reference isolation cohort and must never be renamed or substituted as H.
- `release_holdout` metadata may be validated, but Phase 0 cannot generate, review, package, or tune against its outputs.
- Runtime credentials never enter Git, run JSON, logs, prompts, blind packages, generated documentation, or test fixtures.
- Raw prose, private blind maps, reviewer identity, and free-form review evidence remain local private data by default.
- Do not add a fixed LLM timeout. Caller cancellation is allowed; retries, if added, must be explicit and versioned.
- Do not add dependencies unless an existing standard-library or repository abstraction cannot satisfy the contract.
- No individual leaf unit test may exceed one second.
- Add bilingual `CHANGELOG.md` entries before every commit; Commit Messages remain English.
- Preserve the existing committed P0-T07 run and legacy `StableRunID(manifest)` behavior.

---

### Task 1: Add cohort-aware run identity without breaking P0-T07

**Files:**
- Modify: `internal/quality/evaluation/types.go`
- Modify: `internal/quality/evaluation/run.go`
- Modify: `internal/quality/evaluation/run_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: `RunSelection`, `NewRunSelection([]DataSplit, []string) (RunSelection, error)`
- Produces: `SelectTasks(CorpusManifest, RunSelection) ([]EvaluationTask, error)`
- Produces: `StableCohortRunID(CorpusManifest, RunSelection, string) string`
- Extends: `CreateRunOptions.Selection *RunSelection`, `CreateRunOptions.HarnessPolicySHA256 string`
- Preserves: `StableRunID(CorpusManifest) string` and legacy nil-selection behavior

- [ ] **Step 1: Write failing selection and identity tests**

Add tests proving sort/deduplication, holdout rejection, exact task filtering, policy-sensitive IDs, and legacy ID stability:

```go
func TestNewRunSelectionNormalizesAndRejectsReleaseHoldout(t *testing.T) {
	selection, err := NewRunSelection(
		[]DataSplit{SplitRegression, SplitTuning, SplitTuning},
		[]string{"task-b", "task-a", "task-a"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]DataSplit{SplitRegression, SplitTuning}, selection.DataSplits) {
		t.Fatalf("splits=%v", selection.DataSplits)
	}
	if !reflect.DeepEqual([]string{"task-a", "task-b"}, selection.TaskIDs) {
		t.Fatalf("task IDs=%v", selection.TaskIDs)
	}
	if _, err := NewRunSelection([]DataSplit{SplitReleaseHoldout}, nil); err == nil {
		t.Fatal("release holdout selection must fail in Phase 0")
	}
}

func TestStableCohortRunIDIncludesSelectionAndHarnessPolicy(t *testing.T) {
	_, manifest := writeValidManifest(t)
	tuning, _ := NewRunSelection([]DataSplit{SplitTuning}, nil)
	regression, _ := NewRunSelection([]DataSplit{SplitRegression}, nil)
	left := StableCohortRunID(manifest, tuning, "sha256:"+strings.Repeat("1", 64))
	right := StableCohortRunID(manifest, regression, "sha256:"+strings.Repeat("1", 64))
	changedPolicy := StableCohortRunID(manifest, tuning, "sha256:"+strings.Repeat("2", 64))
	if left == right || left == changedPolicy || !strings.HasPrefix(left, "run-") {
		t.Fatalf("cohort identities are not isolated: %q %q %q", left, right, changedPolicy)
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test(NewRunSelection|StableCohortRunID)' -count=1
```

Expected: build failure because `RunSelection`, `NewRunSelection`, and `StableCohortRunID` do not exist.

- [ ] **Step 3: Implement the selection contract and stable cohort identity**

Add the exact public shape:

```go
type RunSelection struct {
	DataSplits []DataSplit `json:"data_splits"`
	TaskIDs    []string    `json:"task_ids,omitempty"`
}
```

`NewRunSelection` must sort, deduplicate, reject empty/unknown splits, reject `SplitReleaseHoldout`, and validate task IDs with `idPattern`. `SelectTasks` must return manifest order filtered by the normalized selection, fail if any requested task ID is absent, and fail if an explicit task does not belong to a selected split.

`StableCohortRunID` must hash this exact identity payload:

```go
payload := struct {
	Contract            string         `json:"contract"`
	Version             string         `json:"version"`
	Baseline            BaselineProtocol `json:"baseline"`
	Selection           RunSelection   `json:"selection"`
	HarnessPolicySHA256 string         `json:"harness_policy_sha256"`
	Tasks               []taskIdentity `json:"tasks"`
}{manifest.Contract, manifest.Version, manifest.Baseline, selection, policySHA256, identities}
```

Extend `RunRecord` with optional `Selection` and `HarnessPolicySHA256`. When `CreateRunOptions.Selection == nil`, retain every current v1 field and `StableRunID(manifest)`. When non-nil, select tasks, require a valid `sha256:` policy hash, use `StableCohortRunID`, and create only selected `RunTask` entries.

- [ ] **Step 4: Run focused and package tests**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test(NewRunSelection|SelectTasks|StableCohortRunID|CreateRun)' -count=1
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -count=1
```

Expected: PASS; the pre-existing legacy stable-run test remains unchanged.

- [ ] **Step 5: Update the changelog and commit**

```powershell
git add -- CHANGELOG.md internal/quality/evaluation/types.go internal/quality/evaluation/run.go internal/quality/evaluation/run_test.go
git commit -m "feat: add cohort-aware quality evaluation runs"
```

---

### Task 2: Freeze the H policy and stage templates

**Files:**
- Create: `internal/quality/evaluation/harness_policy.go`
- Create: `internal/quality/evaluation/harness_policy_test.go`
- Create: `docs/project-design/implementation/evaluation/runs/templates/harness-candidate-v1.md`
- Create: `docs/project-design/implementation/evaluation/runs/templates/harness-review-v1.md`
- Create: `docs/project-design/implementation/evaluation/runs/templates/harness-revision-v1.md`
- Create: `docs/project-design/implementation/evaluation/harness-policy-v1.json`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: `HarnessStage`, `HarnessPolicy`, `HarnessStagePolicy`
- Produces: `LoadHarnessPolicy(string) (HarnessPolicy, error)`
- Produces: `HarnessPolicySHA256(HarnessPolicy) string`

- [ ] **Step 1: Write failing policy validation tests**

```go
func TestLoadHarnessPolicyRequiresFourFrozenStages(t *testing.T) {
	path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["stages"] = []any{}
	})
	if _, err := LoadHarnessPolicy(path); err == nil || !strings.Contains(err.Error(), "candidate_a, candidate_b, review, revision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHarnessPolicyRejectsReleaseHoldoutAndHashDrift(t *testing.T) {
	path := writeHarnessPolicyFixture(t, func(policy map[string]any) {
		policy["allowed_splits"] = []any{"release_holdout"}
	})
	if _, err := LoadHarnessPolicy(path); err == nil {
		t.Fatal("release holdout policy must fail")
	}
}
```

- [ ] **Step 2: Run policy tests and verify RED**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'TestLoadHarnessPolicy' -count=1
```

Expected: build failure because the policy API is absent.

- [ ] **Step 3: Add exact policy types and strict loader**

```go
type HarnessStage string

const (
	HarnessStageCandidateA HarnessStage = "candidate_a"
	HarnessStageCandidateB HarnessStage = "candidate_b"
	HarnessStageReview     HarnessStage = "review"
	HarnessStageRevision   HarnessStage = "revision"
)

type HarnessStagePolicy struct {
	Stage           HarnessStage `json:"stage"`
	TemplateFile    string       `json:"template_file"`
	TemplateSHA256  string       `json:"template_sha256"`
	MaxOutputTokens int          `json:"max_output_tokens"`
	MaxOutputBytes  int          `json:"max_output_bytes"`
}

type HarnessPolicy struct {
	Contract       string               `json:"contract"`
	Version        string               `json:"version"`
	PolicyID       string               `json:"policy_id"`
	AllowedSplits  []DataSplit          `json:"allowed_splits"`
	CandidateCount int                  `json:"candidate_count"`
	ThinkingStored bool                 `json:"thinking_persisted"`
	Stages         []HarnessStagePolicy `json:"stages"`
}
```

The strict loader must reject unknown JSON fields, duplicate/missing/out-of-order stages, non-two candidate counts, thinking persistence, release holdout, paths escaping the policy directory, template hash drift, output tokens above the task's frozen ceiling, and output bytes outside `1..49152`. `HarnessPolicySHA256` hashes the validated policy object and is the policy identity stored in cohort runs.

- [ ] **Step 4: Add the exact templates and freeze the policy JSON**

Write `harness-candidate-v1.md` with exactly this UTF-8 text and a final LF:

```markdown
You are writing one independent fiction candidate for a blinded quality evaluation.

Use only the supplied task facts and QualitySpec goals. Return fiction prose only. Do not mention evaluation, Harness, prompts, candidate labels, or hidden reasoning. Do not imitate a named living author. Preserve factual constraints even when increasing tension, clarity, or narrative momentum.
```

Candidate A and B share this template and differ only through the stage label stored outside prose.

Write `harness-review-v1.md` with exactly this UTF-8 text and a final LF:

```markdown
You are the structured reviewer in a blinded fiction workflow.

Read both candidates completely against the supplied task facts and QualitySpec goals. Return exactly one JSON object with keys preferred_candidate, issues, and preserve. preferred_candidate must be A or B. Each issue must contain goal_id, severity, location, evidence, and action. Do not add unknown facts, prose, markdown fences, source guesses, model commentary, or hidden reasoning.
```

Write `harness-revision-v1.md` with exactly this UTF-8 text and a final LF:

```markdown
You are producing the final fiction revision for a blinded quality evaluation.

Use the original task facts, QualitySpec goals, both complete candidates, and the validated structured review. Revise the preferred candidate while preserving its successful passages and correcting actionable issues. Do not introduce a new factual premise solely because the review suggests it. Return fiction prose only and do not mention evaluation, Harness, candidates, review, prompts, or hidden reasoning.
```

Review template requirements: compare both complete candidates, return one strict JSON object with `preferred_candidate`, `issues`, and `preserve`; every issue contains `goal_id`, `severity`, `location`, `evidence`, and `action`.

Revision template requirements: return fiction prose only; use original task facts, QualitySpec, both candidates, and validated review JSON; introduce no new factual premise solely because the reviewer suggested it.

After adding the three files, verify their lowercase SHA-256 values with:

```powershell
Get-FileHash -Algorithm SHA256 docs/project-design/implementation/evaluation/runs/templates/harness-*-v1.md |
  Select-Object Path,@{n='SHA256';e={'sha256:'+$_.Hash.ToLowerInvariant()}}
```

Expected hashes are candidate `sha256:c73b2a7ae645c7c5b3b067fb41316a6d035945685fc92eeff1542c8f6b47db51`, review `sha256:5f42ab2ba44021d9481fe36538424d04f5d2d06a71f91d307493bea98d767b80`, and revision `sha256:28de666b55001e40fc859d327e9c5edbd79ba4c5439488a23aa0e2f843a5a2c8`. Create `harness-policy-v1.json` with exactly this structure:

```json
{
  "contract": "denova.quality-harness-policy",
  "version": "v1",
  "policy_id": "p0-offline-harness-v1",
  "allowed_splits": ["tuning", "regression"],
  "candidate_count": 2,
  "thinking_persisted": false,
  "stages": [
    {
      "stage": "candidate_a",
      "template_file": "runs/templates/harness-candidate-v1.md",
      "template_sha256": "sha256:c73b2a7ae645c7c5b3b067fb41316a6d035945685fc92eeff1542c8f6b47db51",
      "max_output_tokens": 4096,
      "max_output_bytes": 49152
    },
    {
      "stage": "candidate_b",
      "template_file": "runs/templates/harness-candidate-v1.md",
      "template_sha256": "sha256:c73b2a7ae645c7c5b3b067fb41316a6d035945685fc92eeff1542c8f6b47db51",
      "max_output_tokens": 4096,
      "max_output_bytes": 49152
    },
    {
      "stage": "review",
      "template_file": "runs/templates/harness-review-v1.md",
      "template_sha256": "sha256:5f42ab2ba44021d9481fe36538424d04f5d2d06a71f91d307493bea98d767b80",
      "max_output_tokens": 4096,
      "max_output_bytes": 49152
    },
    {
      "stage": "revision",
      "template_file": "runs/templates/harness-revision-v1.md",
      "template_sha256": "sha256:28de666b55001e40fc859d327e9c5edbd79ba4c5439488a23aa0e2f843a5a2c8",
      "max_output_tokens": 4096,
      "max_output_bytes": 49152
    }
  ]
}
```

- [ ] **Step 5: Run policy tests and commit**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test.*HarnessPolicy' -count=1
git add -- CHANGELOG.md internal/quality/evaluation/harness_policy.go internal/quality/evaluation/harness_policy_test.go
git add -f -- docs/project-design/implementation/evaluation/runs/templates/harness-candidate-v1.md docs/project-design/implementation/evaluation/runs/templates/harness-review-v1.md docs/project-design/implementation/evaluation/runs/templates/harness-revision-v1.md docs/project-design/implementation/evaluation/harness-policy-v1.json
git commit -m "feat: freeze offline Harness evaluation policy"
```

---

### Task 3: Assemble bounded H requests and structured review

**Files:**
- Create: `internal/quality/evaluation/harness_request.go`
- Create: `internal/quality/evaluation/harness_request_test.go`
- Modify: `internal/quality/evaluation/types.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: `HarnessRequest`, `HarnessReview`, `HarnessReviewIssue`
- Produces: `BuildHarnessRequest(HarnessStage, HarnessRequestInputs) (HarnessRequest, error)`
- Produces: `ParseHarnessReview([]byte, int, []string) (HarnessReview, error)`

- [ ] **Step 1: Write failing boundary tests**

```go
func TestBuildHarnessRequestKeepsStageInputsIsolated(t *testing.T) {
	inputs := validHarnessRequestInputs(t)
	candidate, err := BuildHarnessRequest(HarnessStageCandidateA, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(candidate.UserInput, inputs.CandidateB) || strings.Contains(candidate.UserInput, inputs.ReviewJSON) {
		t.Fatal("candidate request received downstream artifacts")
	}
	revision, err := BuildHarnessRequest(HarnessStageRevision, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{inputs.OriginalInput, inputs.CandidateA, inputs.CandidateB, inputs.ReviewJSON} {
		if !strings.Contains(revision.UserInput, required) {
			t.Fatalf("revision request omitted required bounded input")
		}
	}
}

func TestParseHarnessReviewRejectsUnknownFieldsAndOversize(t *testing.T) {
	if _, err := ParseHarnessReview([]byte(`{"preferred_candidate":"A","issues":[],"preserve":[],"reasoning":"secret"}`), 49152, []string{"goal.opening"}); err == nil {
		t.Fatal("unknown review fields must fail")
	}
	if _, err := ParseHarnessReview(bytes.Repeat([]byte("x"), 49153), 49152, []string{"goal.opening"}); err == nil {
		t.Fatal("oversized review must fail")
	}
}
```

- [ ] **Step 2: Verify RED**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test(BuildHarnessRequest|ParseHarnessReview)' -count=1
```

Expected: build failure for missing request/review APIs.

- [ ] **Step 3: Implement exact request and review contracts**

```go
type HarnessRequest struct {
	TaskID         string
	Stage          HarnessStage
	SystemTemplate string
	UserInput      string
	InputSHA256    string
	Model          ModelConfigSnapshot
}

type HarnessRequestInputs struct {
	TaskID         string
	OriginalInput  string
	InputSHA256    string
	QualityGoals   []string
	CandidateA     string
	CandidateB     string
	ReviewJSON     string
	StagePolicy    HarnessStagePolicy
	SystemTemplate string
	Model          ModelConfigSnapshot
}

type HarnessReviewIssue struct {
	GoalID   string `json:"goal_id"`
	Severity string `json:"severity"`
	Location string `json:"location"`
	Evidence string `json:"evidence"`
	Action   string `json:"action"`
}

type HarnessReview struct {
	PreferredCandidate string               `json:"preferred_candidate"`
	Issues             []HarnessReviewIssue `json:"issues"`
	Preserve           []string             `json:"preserve"`
}
```

Canonical JSON serialization, not string concatenation of maps, must feed stage hashes. `BuildHarnessRequest` must enforce the stage allowlist from the design and reject any assembled UTF-8 input above 131072 bytes. `ParseHarnessReview` must use `DisallowUnknownFields`, require exactly one JSON value, allow only candidate `A` or `B`, require every issue goal to exist in the task QualitySpec, cap issues at 64, and reject credential/reasoning fields through the existing sensitive JSON walker.

- [ ] **Step 4: Run tests and commit**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test(BuildHarnessRequest|ParseHarnessReview)' -count=1
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -count=1
git add -- CHANGELOG.md internal/quality/evaluation/types.go internal/quality/evaluation/harness_request.go internal/quality/evaluation/harness_request_test.go
git commit -m "feat: assemble bounded offline Harness requests"
```

---

### Task 4: Execute and resume the four H stages atomically

**Files:**
- Create: `internal/quality/evaluation/harness.go`
- Create: `internal/quality/evaluation/harness_test.go`
- Modify: `internal/quality/evaluation/run.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: `HarnessTextGenerator.GenerateHarness(context.Context, HarnessRequest) (GenerationResult, error)`
- Produces: `ExecuteOfflineHarness(context.Context, string, string, string, string, HarnessTextGenerator) (RunRecord, error)`
- Adds: `HarnessStageRecord` and `ArmRecord.Stages []HarnessStageRecord`

- [ ] **Step 1: Write failing execution, resume, and failure tests**

```go
func TestExecuteOfflineHarnessRunsFourStagesAndProducesFinalH(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	generator := &recordingHarnessGenerator{}
	run, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, generator)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(generator.Requests); got != 4*len(run.Tasks) {
		t.Fatalf("calls=%d want=%d", got, 4*len(run.Tasks))
	}
	for _, task := range run.Tasks {
		h := task.Arms["H"]
		if h.Status != StatusReady || len(h.Stages) != 4 || h.Usage.ModelCalls != 4 || h.OutputFile == "" {
			t.Fatalf("task %s H=%#v", task.TaskID, h)
		}
	}
}

func TestExecuteOfflineHarnessResumesMatchingStagesWithoutPaidRepeats(t *testing.T) {
	fixture := newHarnessExecutionFixture(t)
	first := &recordingHarnessGenerator{FailStage: HarnessStageReview}
	_, _ = ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, first)
	second := &recordingHarnessGenerator{}
	_, err := ExecuteOfflineHarness(context.Background(), fixture.ManifestPath, fixture.RunRoot, fixture.RunID, fixture.PolicyPath, second)
	if err != nil {
		t.Fatal(err)
	}
	if second.Count(HarnessStageCandidateA) != 0 || second.Count(HarnessStageCandidateB) != 0 {
		t.Fatal("valid persisted candidates were repeated")
	}
}
```

- [ ] **Step 2: Verify RED**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'TestExecuteOfflineHarness' -count=1
```

Expected: build failure for missing executor and stage records.

- [ ] **Step 3: Implement atomic stage records and executor**

```go
type HarnessStageRecord struct {
	Stage        HarnessStage `json:"stage"`
	Status       ResultStatus `json:"status"`
	InputSHA256  string       `json:"input_sha256"`
	OutputSHA256 string       `json:"output_sha256,omitempty"`
	OutputFile   string       `json:"output_file,omitempty"`
	Usage        UsageRecord  `json:"usage"`
	Cost         CostRecord   `json:"cost"`
	FailureType  string       `json:"failure_type,omitempty"`
}

type HarnessTextGenerator interface {
	GenerateHarness(context.Context, HarnessRequest) (GenerationResult, error)
}
```

Add this field to `ArmRecord`:

```go
Stages []HarnessStageRecord `json:"stages,omitempty"`
```

Persist stage files as mode `0600` under `private/outputs/<task-id>/h/<stage>.txt` and the final revision as the H arm output. Save each successful stage and updated run record before starting the next call. Resume only when the stored file hash, input hash, model hash, policy hash, and template hash all match. Aggregate stage usage/cost into H; a failed stage sets H and run Harness status to `FAILED`, clears the public H final pointer, records `harness_<stage>_failed`, and leaves later stages absent.

- [ ] **Step 4: Prove no partial blind package and run package tests**

Extend `blind` tests so a task with two candidates but no valid revision remains `NOT-READY` and produces no A/B files.

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test(ExecuteOfflineHarness|PackageBlind)' -count=1
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -count=1
```

Expected: PASS.

- [ ] **Step 5: Update changelog and commit**

```powershell
git add -- CHANGELOG.md internal/quality/evaluation/run.go internal/quality/evaluation/harness.go internal/quality/evaluation/harness_test.go internal/quality/evaluation/blind.go internal/quality/evaluation/run_test.go
git commit -m "feat: execute resumable offline Harness runs"
```

---

### Task 5: Expose secure cohort and H commands through `quality-eval`

**Files:**
- Modify: `cmd/quality-eval/main.go`
- Create: `cmd/quality-eval/evaluation_commands_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Adds command: `create-run --splits tuning|regression --tasks id[,id] --harness-policy FILE --run-root DIR`
- Adds command: `execute-harness --run ID --manifest FILE --harness-policy FILE --run-root DIR`
- Extends: `package-blind` and `summarize` with `--run-root DIR`
- Adds method: `einoGenerator.GenerateHarness(context.Context, evaluation.HarnessRequest) (evaluation.GenerationResult, error)`
- Adds local interface: `evaluationGenerator`, embedding both generation interfaces

- [ ] **Step 1: Write failing CLI tests with injected fake generators**

```go
func TestCreateRunRequiresExplicitPrivateRunRootForCohorts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"create-run", "--manifest", testManifestPath(t), "--splits", "tuning",
		"--harness-policy", testPolicyPath(t),
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--run-root") || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, stdout.String())
	}
}

func TestCreateRunRejectsReleaseHoldoutBeforeProviderCall(t *testing.T) {
	calls := 0
	previous := newEvaluationGenerator
	newEvaluationGenerator = func(string) evaluationGenerator { calls++; return &fakeGenerator{} }
	t.Cleanup(func() { newEvaluationGenerator = previous })
	err := run(context.Background(), cohortArgs(t, "release_holdout"), io.Discard, io.Discard)
	if err == nil || calls != 0 {
		t.Fatalf("err=%v provider factory calls=%d", err, calls)
	}
}
```

- [ ] **Step 2: Verify RED**

```powershell
& .\.tools\go\bin\go.exe test ./cmd/quality-eval -run 'Test(CreateRun|ExecuteHarness|PackageBlind).*' -count=1
```

Expected: missing options/commands or assertions fail.

- [ ] **Step 3: Implement parsing and shared Provider adapter**

Parse comma-separated splits/tasks with exact trimming and duplicate rejection through `NewRunSelection`. Cohort mode requires absolute `--run-root`; reject paths inside the repository so private generated prose cannot be staged accidentally. Legacy `create-run --manifest FILE` remains available for P0-T07 reproduction.

Refactor the adapter through one private method used by both interfaces:

```go
func (generator *einoGenerator) generate(
	ctx context.Context,
	model evaluation.ModelConfigSnapshot,
	systemTemplate string,
	userInput string,
) (evaluation.GenerationResult, error)
```

Use this exact factory boundary so tests can replace both arms without a network call:

```go
type evaluationGenerator interface {
	evaluation.TextGenerator
	evaluation.HarnessTextGenerator
}

var newEvaluationGenerator = func(apiKey string) evaluationGenerator {
	return &einoGenerator{apiKey: apiKey}
}
```

`GenerateHarness` passes the frozen stage template and assembled user input. It must never print headers, Key values, full prompts, provider reasoning, or raw responses to stdout/stderr. CLI success output contains only run ID, arm status, selected task count, model-call count, and aggregate token count.

- [ ] **Step 4: Run command tests**

```powershell
& .\.tools\go\bin\go.exe test ./cmd/quality-eval -run 'Test(CreateRun|ExecuteHarness|PackageBlind|Summarize).*' -count=1
& .\.tools\go\bin\go.exe test ./cmd/quality-eval -count=1
```

Expected: PASS; no test uses the network.

- [ ] **Step 5: Update changelog and commit**

```powershell
git add -- CHANGELOG.md cmd/quality-eval/main.go cmd/quality-eval/evaluation_commands_test.go
git commit -m "feat: expose offline Harness evaluation commands"
```

---

### Task 6: Close privacy, blind-review, and planning contracts

**Files:**
- Modify: `internal/quality/evaluation/blind.go`
- Modify: `internal/quality/evaluation/summary.go`
- Modify: `internal/quality/evaluation/summary_test.go`
- Create: `internal/quality/evaluation/run_index.go`
- Create: `internal/quality/evaluation/run_index_test.go`
- Modify: `cmd/quality-eval/main.go`
- Modify: `cmd/quality-eval/evaluation_commands_test.go`
- Modify: `docs/project-design/implementation/evaluation/EVALUATION_PROTOCOL.md`
- Modify: `docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md`
- Modify: `docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md`
- Modify: `docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Preserves: existing blind review and summary contracts
- Adds: cohort/policy fields to blind index and summary reproducibility metadata
- Adds: `ExportRunIndex(string, []string, string) (RunIndex, error)` and CLI `export-run-index`
- Corrects: Phase 0 blanket prohibition while preserving product-runtime exclusions

- [ ] **Step 1: Write failing privacy and cohort summary tests**

```go
func TestBlindPackageContainsOnlySelectedCompletePairs(t *testing.T) {
	fixture := writeReadyCohortRun(t, SplitRegression)
	index, err := PackageBlind(fixture.RunRoot, fixture.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Samples) != 12 {
		t.Fatalf("samples=%d want=12", len(index.Samples))
	}
	for _, sample := range index.Samples {
		if sample.DataSplit != SplitRegression || sample.Status != StatusReady {
			t.Fatalf("unexpected sample=%#v", sample)
		}
	}
}

func TestCommittedMetadataRejectsHarnessSecretsAndReasoning(t *testing.T) {
	for _, field := range []string{"api_key", "authorization", "thinking_content", "reasoning_content", "reviewer_id", "raw_comments"} {
		if err := rejectSensitiveJSON([]byte(`{"` + field + `":"not-allowed"}`)); err == nil {
			t.Fatalf("field %s was accepted", field)
		}
	}
}

func TestExportRunIndexContainsHashesAndAggregatesOnly(t *testing.T) {
	fixture := writeReadyCohortRun(t, SplitRegression)
	output := filepath.Join(t.TempDir(), "index.json")
	index, err := ExportRunIndex(fixture.RunRoot, []string{fixture.RunID}, output)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{fixture.RunRoot, "output_file", "reviewer_id", "raw_comments", "authorization"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("bounded index contains %q", forbidden)
		}
	}
	if len(index.Runs) != 1 || index.Runs[0].RunID != fixture.RunID {
		t.Fatalf("index=%#v", index)
	}
}
```

- [ ] **Step 2: Verify RED**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -run 'Test(BlindPackageContainsOnlySelected|CommittedMetadataRejectsHarness)' -count=1
```

Expected: failure until cohort metadata and the extended sensitive-field rejection exist.

- [ ] **Step 3: Implement metadata and update the four authoritative documents**

Blind index and summary must record selection, policy ID/hash, S template hash, and model hashes without adding private text. Keep reviewer IDs only in private review files; committed aggregate export uses stable anonymous review counts. `RunIndex` contains contract/version, generated-at time, and `Runs []RunIndexEntry`; each entry contains only run ID, selection, task count, policy/template/model hashes, S/H status counts, aggregate usage/cost, local-evidence availability, and blind-package hash. `export-run-index` requires explicit `--run-root`, comma-separated `--runs`, and `--output`.

Use these exact bounded export types:

```go
type RunIndex struct {
	Contract    string          `json:"contract"`
	Version     string          `json:"version"`
	GeneratedAt string          `json:"generated_at"`
	Runs        []RunIndexEntry `json:"runs"`
}

type RunIndexEntry struct {
	RunID                  string                            `json:"run_id"`
	Selection              RunSelection                      `json:"selection"`
	TaskCount              int                               `json:"task_count"`
	HarnessPolicySHA256    string                            `json:"harness_policy_sha256"`
	BaselineTemplateSHA256 string                            `json:"baseline_template_sha256"`
	ModelConfigSHA256      []string                          `json:"model_config_sha256"`
	ArmStatusCounts        map[string]map[ResultStatus]int   `json:"arm_status_counts"`
	Usage                  map[string]UsageRecord            `json:"usage"`
	Cost                   map[string]CostRecord             `json:"cost"`
	LocalEvidenceAvailable bool                              `json:"local_evidence_available"`
	BlindPackageSHA256     string                            `json:"blind_package_sha256,omitempty"`
}
```

Replace the Phase 0 sentence with this approved boundary in Chinese and English:

```text
Phase 0 may implement a versioned, evaluation-only offline Harness runner. It must not add product runtime integration, user-facing Harness workflows, formal workspace writes, automatic publication, or third-party script execution.
```

Update P0-T09 to use tuning for runner shakeout, regression for the paired pilot, and release holdout only as registered hashes. State that Phase 0 freezes future gate rules and does not claim a Phase 2/3/5 quality PASS.

- [ ] **Step 4: Run privacy, summary, and document checks**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation -count=1
$matches = rg -n -i '\?sign=|authorization:\s*\S|bearer\s+[A-Za-z0-9._~+/-]{12,}|BEGIN .*PRIVATE KEY|raw_comments\s*[:=]' docs/project-design/implementation/evaluation/harness-policy-v1.json docs/project-design/implementation/evaluation/runs/templates/harness-*-v1.md docs/project-design/implementation/planning
if ($LASTEXITCODE -eq 0) { $matches; exit 1 }
if ($LASTEXITCODE -ne 1) { exit $LASTEXITCODE }
git diff --check
```

Expected: tests PASS, scan produces no match, diff check exits zero.

- [ ] **Step 5: Update changelog and commit**

```powershell
git add -- CHANGELOG.md internal/quality/evaluation/blind.go internal/quality/evaluation/summary.go internal/quality/evaluation/summary_test.go internal/quality/evaluation/run_index.go internal/quality/evaluation/run_index_test.go cmd/quality-eval/main.go cmd/quality-eval/evaluation_commands_test.go docs/project-design/implementation/evaluation/EVALUATION_PROTOCOL.md docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md docs/project-design/implementation/planning/VALIDATION_AND_RELEASE_GATES.md
git commit -m "docs: permit evaluation-only Harness in Phase 0"
```

---

### Task 7: Run the real smoke, tuning, and regression cohorts safely

**Files:**
- Create locally only: `%LOCALAPPDATA%\Denova\quality-eval\p0-offline-harness-v1\`
- Create committed aggregate: `docs/project-design/implementation/evaluation/p0-harness-run-index-v1.json`
- Modify: `docs/project-design/implementation/evaluation/EVALUATION_PROTOCOL.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: completed Tasks 1–6 and the existing runtime DeepSeek credential
- Produces: one tuning smoke run, one full tuning run, one frozen regression run, blind packages for complete pairs, and a hash-only committed run index
- Does not produce: human review decisions or a P0-T09 quality PASS

- [ ] **Step 1: Run all offline gates before spending model calls**

```powershell
& .\.tools\go\bin\go.exe mod tidy -diff
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation ./cmd/quality-eval -count=1
& .\.tools\go\bin\go.exe test ./... -count=1
& .\.tools\go\bin\go.exe vet ./...
& 'C:\Program Files\Git\bin\bash.exe' -lc 'PATH="$(pwd)/.tools/go/bin:$PATH" ./scripts/build.sh'
```

Expected: every command exits zero. Record the existing Vite chunk warning as non-failing if unchanged.

- [ ] **Step 2: Load the existing Key into this process without displaying or persisting it**

```powershell
$line = Get-Content -LiteralPath 'C:\Users\yiwan\AppData\Local\hermes\.env' |
  Where-Object { $_ -match '^\s*DEEPSEEK_API_KEY\s*=' } |
  Select-Object -First 1
if ($line -notmatch '^\s*DEEPSEEK_API_KEY\s*=\s*(.*)$') { throw 'DeepSeek credential not found' }
$env:OPENAI_API_KEY = $Matches[1].Trim().Trim('"').Trim("'")
if ([string]::IsNullOrWhiteSpace($env:OPENAI_API_KEY)) { throw 'DeepSeek credential is empty' }
$models = Invoke-RestMethod -Method Get -Uri 'https://api.deepseek.com/models' -Headers @{Authorization="Bearer $env:OPENAI_API_KEY"}
if (@($models.data.id) -notcontains 'deepseek-v4-pro') { throw 'Frozen model is unavailable' }
Remove-Item Env:OPENAI_API_KEY -ErrorAction SilentlyContinue
```

Do not print `$env:OPENAI_API_KEY`, its prefix, or the matching source line. Every later live-call block reloads the Key into its own process and removes it in `finally`.

- [ ] **Step 3: Execute one five-call tuning smoke task**

Use `fq-urban-opening-01` because it is a tuning task and already participates in the preregistered capability-reference matrix:

```powershell
$line = Get-Content -LiteralPath 'C:\Users\yiwan\AppData\Local\hermes\.env' | Where-Object { $_ -match '^\s*DEEPSEEK_API_KEY\s*=' } | Select-Object -First 1
if ($line -notmatch '^\s*DEEPSEEK_API_KEY\s*=\s*(.*)$') { throw 'DeepSeek credential not found' }
$env:OPENAI_API_KEY = $Matches[1].Trim().Trim('"').Trim("'")
$runRoot = Join-Path $env:LOCALAPPDATA 'Denova\quality-eval\p0-offline-harness-v1'
try {
  $smokeRun = (& .\.tools\go\bin\go.exe run ./cmd/quality-eval create-run --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --splits tuning --tasks fq-urban-opening-01 --harness-policy docs/project-design/implementation/evaluation/harness-policy-v1.json --run-root $runRoot).Trim()
  if ($smokeRun -notmatch '^run-[a-f0-9]{24}$') { throw 'invalid smoke run id' }
  & .\.tools\go\bin\go.exe run ./cmd/quality-eval execute-harness --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --run $smokeRun --harness-policy docs/project-design/implementation/evaluation/harness-policy-v1.json --run-root $runRoot
  $record = Get-Content -Raw -LiteralPath (Join-Path $runRoot "$smokeRun\run.json") | ConvertFrom-Json
  $h = $record.tasks[0].arms.H
  if ($record.baseline_status -ne 'READY' -or $record.harness_status -ne 'READY' -or $h.status -ne 'READY' -or $h.stages.Count -ne 4 -or $h.usage.model_calls -ne 4) { throw 'smoke run is not a complete S/H pair' }
} finally {
  Remove-Item Env:OPENAI_API_KEY -ErrorAction SilentlyContinue
}
```

The implementation must return the run ID as the only stdout line for `create-run`. Stop before the full cohort if S/H are not both `READY`, H call count is not four, any stage hash is missing, output exceeds policy, or a secret scan finds a match.

- [ ] **Step 4: Freeze templates after smoke and run the full tuning cohort**

If smoke reveals a template defect, change the template, regenerate the policy hash, commit that change, and create a new run ID. Never mutate a completed run to match a new template.

Then run `--splits tuning` without `--tasks`, producing 18 S calls and 72 H calls. Package complete pairs but do not use tuning outcomes as release evidence:

```powershell
$line = Get-Content -LiteralPath 'C:\Users\yiwan\AppData\Local\hermes\.env' | Where-Object { $_ -match '^\s*DEEPSEEK_API_KEY\s*=' } | Select-Object -First 1
if ($line -notmatch '^\s*DEEPSEEK_API_KEY\s*=\s*(.*)$') { throw 'DeepSeek credential not found' }
$env:OPENAI_API_KEY = $Matches[1].Trim().Trim('"').Trim("'")
$runRoot = Join-Path $env:LOCALAPPDATA 'Denova\quality-eval\p0-offline-harness-v1'
try {
  $tuningRun = (& .\.tools\go\bin\go.exe run ./cmd/quality-eval create-run --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --splits tuning --harness-policy docs/project-design/implementation/evaluation/harness-policy-v1.json --run-root $runRoot).Trim()
  & .\.tools\go\bin\go.exe run ./cmd/quality-eval execute-harness --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --run $tuningRun --harness-policy docs/project-design/implementation/evaluation/harness-policy-v1.json --run-root $runRoot
  & .\.tools\go\bin\go.exe run ./cmd/quality-eval package-blind --run $tuningRun --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --run-root $runRoot
} finally {
  Remove-Item Env:OPENAI_API_KEY -ErrorAction SilentlyContinue
}
```

- [ ] **Step 5: Run the frozen regression cohort**

With unchanged template, policy, and model hashes, run `--splits regression`, producing 12 S calls and 48 H calls. Package the 12 complete S/H pairs. Do not generate the six `release_holdout` tasks:

```powershell
$line = Get-Content -LiteralPath 'C:\Users\yiwan\AppData\Local\hermes\.env' | Where-Object { $_ -match '^\s*DEEPSEEK_API_KEY\s*=' } | Select-Object -First 1
if ($line -notmatch '^\s*DEEPSEEK_API_KEY\s*=\s*(.*)$') { throw 'DeepSeek credential not found' }
$env:OPENAI_API_KEY = $Matches[1].Trim().Trim('"').Trim("'")
$runRoot = Join-Path $env:LOCALAPPDATA 'Denova\quality-eval\p0-offline-harness-v1'
try {
  $regressionRun = (& .\.tools\go\bin\go.exe run ./cmd/quality-eval create-run --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --splits regression --harness-policy docs/project-design/implementation/evaluation/harness-policy-v1.json --run-root $runRoot).Trim()
  & .\.tools\go\bin\go.exe run ./cmd/quality-eval execute-harness --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --run $regressionRun --harness-policy docs/project-design/implementation/evaluation/harness-policy-v1.json --run-root $runRoot
  & .\.tools\go\bin\go.exe run ./cmd/quality-eval package-blind --run $regressionRun --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json --run-root $runRoot
} finally {
  Remove-Item Env:OPENAI_API_KEY -ErrorAction SilentlyContinue
}
```

- [ ] **Step 6: Commit only the bounded run index and status update**

`p0-harness-run-index-v1.json` contains only run IDs, selection, task counts, policy/template/model hashes, arm status counts, usage/cost aggregates, local evidence availability, and blind-package hashes. It contains no local absolute path, prose, prompts, review JSON, Key, reviewer identity, or signed URL.

```powershell
$runRoot = Join-Path $env:LOCALAPPDATA 'Denova\quality-eval\p0-offline-harness-v1'
$records = @(Get-ChildItem -LiteralPath $runRoot -Filter run.json -Recurse -File | ForEach-Object { Get-Content -Raw -LiteralPath $_.FullName | ConvertFrom-Json })
$smokeRun = ($records | Where-Object { $_.selection.data_splits.Count -eq 1 -and $_.selection.data_splits[0] -eq 'tuning' -and $_.selection.task_ids.Count -eq 1 }).run_id
$tuningRun = ($records | Where-Object { $_.selection.data_splits.Count -eq 1 -and $_.selection.data_splits[0] -eq 'tuning' -and $_.tasks.Count -eq 18 }).run_id
$regressionRun = ($records | Where-Object { $_.selection.data_splits.Count -eq 1 -and $_.selection.data_splits[0] -eq 'regression' -and $_.tasks.Count -eq 12 }).run_id
if (@($smokeRun).Count -ne 1 -or @($tuningRun).Count -ne 1 -or @($regressionRun).Count -ne 1) { throw 'expected exactly one smoke, tuning, and regression run' }
& .\.tools\go\bin\go.exe run ./cmd/quality-eval export-run-index --run-root $runRoot --runs "$smokeRun,$tuningRun,$regressionRun" --output docs/project-design/implementation/evaluation/p0-harness-run-index-v1.json
$matches = rg -n -i 'api[_-]?key|authorization|bearer |[A-Za-z]:\\|reviewer_id|raw_comments|thinking_content|reasoning_content' docs/project-design/implementation/evaluation/p0-harness-run-index-v1.json
if ($LASTEXITCODE -eq 0) { $matches; exit 1 }
if ($LASTEXITCODE -ne 1) { exit $LASTEXITCODE }
git diff --check
git status --short -uall
git add -- CHANGELOG.md docs/project-design/implementation/evaluation/EVALUATION_PROTOCOL.md
git add -f -- docs/project-design/implementation/evaluation/p0-harness-run-index-v1.json
git commit -m "test: record offline Harness evaluation cohorts"
```

- [ ] **Step 7: Run final verification**

```powershell
& .\.tools\go\bin\go.exe test ./internal/quality/evaluation ./cmd/quality-eval -count=1
& .\.tools\go\bin\go.exe test ./... -count=1
& .\.tools\go\bin\go.exe vet ./...
git show --check --stat --oneline HEAD
git status --short --branch
```

Expected: all tests/vet pass, commit check is clean, worktree is clean. Report paired-output readiness separately from human-review readiness; P0-T09 remains incomplete until independent reviews, adjudication where needed, Projection ADR, and the full gate manifest are finished.

## Completion Boundary

This plan is complete when the evaluation-only H runner is implemented, non-holdout S/H cohorts are reproducible, private evidence remains outside Git, and blind packages are ready for human review. It does not complete P0-T09 or authorize Phase 1 by itself.
