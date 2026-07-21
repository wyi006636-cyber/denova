# Xiaping Writing-Skill Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a resumable, read-only Xiaping discovery pipeline that snapshots the public catalog, finds comprehensive novel-writing candidates, de-duplicates and evidence-ranks them, and publishes a dual data-rich/exploration shortlist without claiming writing quality.

**Architecture:** Add a private `internal/quality/skilldiscovery` package and extend the existing `quality-eval` CLI with Xiaping discovery commands. Raw pages, comments, and reviewer identifiers remain in an OS user-cache directory; Git receives only normalized public metadata, hashes, aggregate evidence, duplicate clusters, Capability proposals, the shortlist, and a bilingual report. This is implementation plan 1 of 2: package static audits and paired Skill-arm blind evaluation begin only after the shortlist is reviewed.

**Tech Stack:** Go 1.26.5 standard library, existing `internal/skills` restricted HTTP transport, existing `cmd/quality-eval`, `github.com/santhosh-tekuri/jsonschema/v6` v6.0.2, JSON Schema draft 2020-12, `httptest`, Markdown, PowerShell verification commands.

## Global Constraints

- Scope is novel writing and editorial review only; exclude video, comics, illustration, audio, publishing operations, and unrelated content production.
- Keep the existing sixteen Capability IDs stable. New abilities are proposals until an explicit contract change is approved.
- Use public, unauthenticated HTTPS `GET` only. Never send reviews, authenticate, provide cookies/API keys, or call mutation endpoints.
- Raw comments, reviewer identifiers, agent documents, signed URLs, and third-party packages must not enter Git.
- Unknown licensing does not block discovery or internal analysis, but must not be represented as redistribution or preinstallation permission.
- Downloads, ratings, comments, platform status, security labels, and database-size claims are evidence signals, not quality results.
- Preserve a wide entrance: priority thresholds must not reject promising low-data candidates; retain a 20–30% exploration lane where the source pool permits it.
- Do not execute third-party scripts or download packages in this plan.
- Do not read regression task bodies or release-holdout task bodies in this plan.
- Every model-visible or committed fragment must have a source, purpose, hash, and explicit size boundary.
- Network pacing and retry limits are configurable; do not impose a fixed overall crawl or LLM timeout.
- Keep source files focused; do not grow `cmd/quality-eval/main.go` with the discovery implementation.
- Unit tests must each complete in less than one second.
- Every commit message is English, and `CHANGELOG.md` is updated before each commit.
- Do not push, merge, tag, release, or modify product UI/API as part of this plan.

## Plan Boundary

This plan delivers discovery through an evidence shortlist. A second plan will cover:

- public agent-document and package retrieval for approved shortlist entries;
- archive and static content audits;
- Skill-arm prompt assembly and bounded context accounting;
- tuning smoke runs, paired blind evaluation, regression, and minimum-portfolio comparison.

That split keeps the first deliverable useful and independently testable while avoiding model or human-review costs before the candidate pool is known.

## File Map

| Path | Responsibility |
|---|---|
| `internal/quality/skilldiscovery/types.go` | Versioned normalized records and closed status types |
| `internal/quality/skilldiscovery/artifact.go` | Strict JSON load/write, canonical hashes, artifact validation |
| `internal/quality/skilldiscovery/collector.go` | Xiaping list/detail/comment GET collection and response normalization |
| `internal/quality/skilldiscovery/cache.go` | OS cache paths, page receipts, atomic checkpoints, resume |
| `internal/quality/skilldiscovery/capability.go` | Lexicon loading, writing recall, stable/proposed Capability matching |
| `internal/quality/skilldiscovery/cluster.go` | Exact and explainable metadata near-duplicate groups |
| `internal/quality/skilldiscovery/reviews.go` | Reviewer/comment de-duplication, substantive evidence, anomaly facts |
| `internal/quality/skilldiscovery/evidence.go` | Capability-relative percentiles, Bayesian adjustment, evidence tiers |
| `internal/quality/skilldiscovery/shortlist.go` | Data-rich/exploration lane selection and diversity constraints |
| `internal/quality/skilldiscovery/report.go` | Deterministic bilingual evidence report |
| `cmd/quality-eval/skills_commands.go` | Discovery CLI parsing and orchestration only |
| `docs/project-design/implementation/skills/discovery/*` | Schemas, lexicon, generated committed artifacts, report |

---

### Task 1: Freeze Discovery Contracts and Synthetic Fixtures

**Files:**

- Create: `internal/quality/skilldiscovery/types.go`
- Create: `internal/quality/skilldiscovery/artifact.go`
- Create: `internal/quality/skilldiscovery/artifact_test.go`
- Create: `internal/quality/skilldiscovery/testdata/skills-page-direct.json`
- Create: `internal/quality/skilldiscovery/testdata/skills-page-envelope.json`
- Create: `internal/quality/skilldiscovery/testdata/skill-detail-envelope.json`
- Create: `internal/quality/skilldiscovery/testdata/comments-page-envelope.json`
- Modify: `docs/project-design/implementation/planning/MASTER_DEVELOPMENT_PLAN.md`
- Modify: `docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md`
- Modify: `docs/project-design/implementation/planning/REQUIREMENTS_TRACEABILITY_MATRIX.md`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Produces: `SnapshotManifest`, `LocalSnapshot`, `SkillRecord`, `CandidateRecord`, `CapabilityMatch`, `CapabilityProposal`, `DuplicateCluster`, `ReviewEvidence`, `EvidenceVector`, `Shortlist`, `StableSHA256`, `LoadStrictJSON`, and `WriteJSONArtifact`.
- Consumes: the approved design statuses and current sixteen Capability IDs from `xiaping-catalog-v1.json`.

- [ ] **Step 1: Add failing strict-contract tests**

Create `artifact_test.go` with tests that require strict decoding, lowercase SHA-256, complete/partial consistency, unique Skill IDs, and rejection of signed URLs or sensitive fields:

```go
func TestValidateSnapshotManifestRejectsCompletePageGap(t *testing.T) {
	manifest := validSnapshotManifest()
	manifest.Status = SnapshotPartial
	manifest.Failures = nil
	if err := ValidateSnapshotManifest(manifest); err == nil || !strings.Contains(err.Error(), "partial snapshot requires failures") {
		t.Fatalf("ValidateSnapshotManifest() error = %v", err)
	}
}

func TestValidateSkillRecordsRejectsDuplicateID(t *testing.T) {
	records := []SkillRecord{{ID: "skill-1", Name: "甲"}, {ID: "skill-1", Name: "乙"}}
	if err := ValidateSkillRecords(records); err == nil || !strings.Contains(err.Error(), "duplicate skill id") {
		t.Fatalf("ValidateSkillRecords() error = %v", err)
	}
}

func TestWriteJSONArtifactRejectsSignedURL(t *testing.T) {
	value := map[string]string{"avatar": "https://example.test/a.png?sign=secret"}
	err := WriteJSONArtifact(filepath.Join(t.TempDir(), "bad.json"), value)
	if err == nil || !strings.Contains(err.Error(), "signed URL") {
		t.Fatalf("WriteJSONArtifact() error = %v", err)
	}
}

func validSnapshotManifest() SnapshotManifest {
	return SnapshotManifest{
		Contract: "denova.xiaping-snapshot-manifest", Version: "v1",
		SnapshotID: "snapshot-0123456789abcdef", Status: SnapshotComplete,
		StartedAt: "2026-07-21T00:00:00Z", CompletedAt: "2026-07-21T00:01:00Z",
		BaseURL: "https://example.test", NormalizationVersion: "v1",
		ReportedTotal: 1, UniqueSkills: 1,
		Pages: []PageReceipt{{Kind: "catalog", Key: "1", URL: "https://example.test/api/skills?limit=50&page=1", HTTPStatus: 200, CapturedAt: "2026-07-21T00:00:30Z", SHA256: "sha256:" + strings.Repeat("a", 64), ItemCount: 1}},
		SkillRecordsSHA256: "sha256:" + strings.Repeat("b", 64),
	}
}
```

The four JSON fixtures must be synthetic and use `example.test` URLs, stable UUID-shaped IDs, invented review text, and no copied Xiaping comments.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```powershell
go test ./internal/quality/skilldiscovery -run 'TestValidate|TestWriteJSONArtifact' -count=1
```

Expected: FAIL because the package and contract functions do not exist.

- [ ] **Step 3: Add the normalized types**

Create the types with these exact names and JSON fields:

```go
package skilldiscovery

type SnapshotStatus string

const (
	SnapshotComplete SnapshotStatus = "COMPLETE"
	SnapshotPartial  SnapshotStatus = "PARTIAL"
)

type PageReceipt struct {
	Kind       string `json:"kind"`
	Key        string `json:"key"`
	URL        string `json:"url"`
	HTTPStatus int    `json:"http_status"`
	CapturedAt string `json:"captured_at"`
	SHA256     string `json:"sha256"`
	ItemCount  int    `json:"item_count"`
	Error      string `json:"error,omitempty"`
}

type SnapshotFailure struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	Disposition string `json:"disposition"`
	Message     string `json:"message"`
}

type SnapshotManifest struct {
	Contract               string            `json:"contract"`
	Version                string            `json:"version"`
	SnapshotID             string            `json:"snapshot_id"`
	Status                 SnapshotStatus    `json:"status"`
	StartedAt              string            `json:"started_at"`
	CompletedAt            string            `json:"completed_at"`
	BaseURL                string            `json:"base_url"`
	NormalizationVersion   string            `json:"normalization_version"`
	ReportedTotal          int               `json:"reported_total"`
	UniqueSkills           int               `json:"unique_skills"`
	Pages                  []PageReceipt     `json:"pages"`
	Failures               []SnapshotFailure `json:"failures"`
	PreviousSnapshotSHA256 string            `json:"previous_snapshot_sha256,omitempty"`
	SkillRecordsSHA256     string            `json:"skill_records_sha256"`
}

type SkillRecord struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Triggers       []string `json:"triggers"`
	Categories     []string `json:"categories"`
	Tags           []string `json:"tags"`
	OwnerID        string   `json:"owner_id"`
	OwnerName      string   `json:"owner_name"`
	CurrentVersion string   `json:"current_version"`
	Downloads      int      `json:"downloads"`
	AverageStars   int      `json:"average_stars_x100"`
	StarCount      int      `json:"star_count"`
	CommentCount   int      `json:"comment_count"`
	Featured       bool     `json:"featured"`
	PlatformStatus string   `json:"platform_status"`
	SecurityStatus string   `json:"security_status"`
	VersionCount   int      `json:"version_count"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	DetailURL      string   `json:"detail_url"`
}

type LocalSnapshot struct {
	Manifest SnapshotManifest `json:"manifest"`
	Skills   []SkillRecord    `json:"skills"`
}

type MatchStatus string

const (
	MatchMatched    MatchStatus = "MATCHED"
	MatchAmbiguous  MatchStatus = "AMBIGUOUS"
	MatchNotMatched MatchStatus = "NOT-MATCHED"
)

type FieldEvidence struct {
	Field string `json:"field"`
	Term  string `json:"term"`
}

type CapabilityMatch struct {
	CapabilityID string          `json:"capability_id"`
	Status       MatchStatus     `json:"status"`
	Evidence     []FieldEvidence `json:"evidence"`
}

type CandidateRecord struct {
	Skill        SkillRecord      `json:"skill"`
	Profiles     []string         `json:"profiles"`
	Capabilities []CapabilityMatch `json:"capabilities"`
}

type CandidateIndex struct {
	Contract   string            `json:"contract"`
	Version    string            `json:"version"`
	SnapshotID string            `json:"snapshot_id"`
	Candidates []CandidateRecord `json:"candidates"`
}

type CapabilityProposal struct {
	CapabilityID     string   `json:"capability_id"`
	NameZH           string   `json:"name_zh"`
	NameEN           string   `json:"name_en"`
	Inputs           []string `json:"inputs"`
	Outputs          []string `json:"outputs"`
	LifecycleStage   string   `json:"lifecycle_stage"`
	MinimumPermission string  `json:"minimum_permission"`
	EvaluationMethod string   `json:"evaluation_method"`
	CandidateIDs     []string `json:"candidate_ids"`
}

type CapabilityProposalIndex struct {
	Contract   string               `json:"contract"`
	Version    string               `json:"version"`
	SnapshotID string               `json:"snapshot_id"`
	Proposals  []CapabilityProposal `json:"proposals"`
}

type DuplicateCluster struct {
	ClusterID       string   `json:"cluster_id"`
	Kind            string   `json:"kind"`
	RepresentativeID string  `json:"representative_id"`
	MemberIDs       []string `json:"member_ids"`
	Reasons         []string `json:"reasons"`
}

type DuplicateClusterIndex struct {
	Contract   string             `json:"contract"`
	Version    string             `json:"version"`
	SnapshotID string             `json:"snapshot_id"`
	Clusters   []DuplicateCluster `json:"clusters"`
}

type ReviewEvidence struct {
	EffectiveRaters      int      `json:"effective_raters"`
	SubstantiveComments  int      `json:"substantive_comments"`
	DuplicateComments    int      `json:"duplicate_comments"`
	OwnerSelfReviews     int      `json:"owner_self_reviews"`
	AverageStarsX100     int      `json:"average_stars_x100"`
	PlatformQualityMean *float64 `json:"platform_quality_mean,omitempty"`
	AnomalyFlags         []string `json:"anomaly_flags"`
}

type EvidenceVector struct {
	SkillID                 string         `json:"skill_id"`
	CapabilityID            string         `json:"capability_id"`
	DownloadPercentile      float64        `json:"download_percentile"`
	BayesianStarsX100       float64        `json:"bayesian_stars_x100"`
	Review                  ReviewEvidence `json:"review"`
	PlatformDataRich        bool           `json:"platform_data_rich"`
	MaturityVersionCount    int            `json:"maturity_version_count"`
	EvidenceCacheStatus     string         `json:"evidence_cache_status"`
}

type ShortlistLane string

const (
	LaneDataRich    ShortlistLane = "DATA-RICH"
	LaneExploration ShortlistLane = "EXPLORATION"
)

type ShortlistEntry struct {
	SkillID      string        `json:"skill_id"`
	CapabilityID string        `json:"capability_id"`
	Lane         ShortlistLane `json:"lane"`
	Rank         int           `json:"rank"`
	Reasons      []string      `json:"reasons"`
	Evidence     EvidenceVector `json:"evidence"`
}

type CapabilityGap struct {
	CapabilityID string `json:"capability_id"`
	Wanted       int    `json:"wanted"`
	Selected     int    `json:"selected"`
	Reason       string `json:"reason"`
}

type Shortlist struct {
	Contract   string           `json:"contract"`
	Version    string           `json:"version"`
	SnapshotID string           `json:"snapshot_id"`
	Entries    []ShortlistEntry `json:"entries"`
	Gaps       []CapabilityGap  `json:"gaps"`
}
```

Keep the current sixteen IDs in one exported `CoreCapabilityIDs` slice in their catalog order.

- [ ] **Step 4: Implement strict artifacts and validation**

Implement these signatures in `artifact.go`:

```go
func StableSHA256(value any) string
func LoadStrictJSON(path string, target any) error
func WriteJSONArtifact(path string, value any) error
func ValidateSnapshotManifest(manifest SnapshotManifest) error
func ValidateSkillRecords(records []SkillRecord) error
```

`WriteJSONArtifact` must JSON-marshal first, reject URL values containing `?sign=`, `x-amz-signature`, `access_token=`, or equivalent signed-query parameters, reject bearer/secret values and private-key headers, then write indented JSON plus one newline through a same-directory temp file and rename. Field names such as `requires_api_key` are not secrets and must not be rejected by name alone. `LoadStrictJSON` uses `DisallowUnknownFields` and rejects trailing JSON. Hashes use `sha256:` plus 64 lowercase hexadecimal characters. `SkillRecordsSHA256` hashes only the canonical sorted `skills` array, avoiding a circular manifest hash.

- [ ] **Step 5: Record the follow-up task in Phase 0 planning**

Add a `P0-T08A` row/section between P0-T08 and P0-T09 in the three planning documents. State that it is the user-approved full-catalog discovery/evidence increment, depends on P0-T08, blocks P0-T09's Skill evidence closeout, and does not alter P0-T08 history. Trace it to `SKILL-001` through `SKILL-004`, `EVAL-001`, `EVAL-002`, and `SAFE-002`.

- [ ] **Step 6: Run GREEN and the focused time gate**

Run:

```powershell
go test ./internal/quality/skilldiscovery -count=1
$events = & go test -json ./internal/quality/skilldiscovery -count=1 | ForEach-Object { $_ | ConvertFrom-Json }
$slow = @($events | Where-Object { $_.Action -eq 'pass' -and $_.Test -and $_.Elapsed -gt 1 })
if ($slow.Count -gt 0) { throw "unit test exceeded one second: $($slow.Test -join ', ')" }
git diff --check
```

Expected: PASS; each individual test is below one second; the package run completes without network access.

- [ ] **Step 7: Update the changelog and commit**

Add bilingual `Added` bullets describing P0-T08A contracts, synthetic fixtures, and planning traceability, then run:

```powershell
git add -- CHANGELOG.md internal/quality/skilldiscovery docs/project-design/implementation/planning
git diff --cached --check
git commit -m "test: define Xiaping discovery contracts"
```

---

### Task 2: Add a Restricted Resumable Catalog Collector

**Files:**

- Modify: `internal/skills/remote.go`
- Modify: `internal/skills/remote_security_test.go`
- Create: `internal/quality/skilldiscovery/cache.go`
- Create: `internal/quality/skilldiscovery/collector.go`
- Create: `internal/quality/skilldiscovery/collector_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: `SnapshotManifest`, `PageReceipt`, `LocalSnapshot`, `SkillRecord`, `WriteJSONArtifact` from Task 1.
- Produces: `skills.NewRestrictedRemoteHTTPClient() *http.Client`, `CollectorOptions`, `Collector`, `NewCollector`, `CollectCatalog`, and `DefaultCacheRoot`.

- [ ] **Step 1: Write failing transport and resume tests**

Add tests for the exported restricted client and a TLS `httptest` catalog with two pages, direct/enveloped payloads, one `429` with `Retry-After: 0`, and a resumed run:

```go
func TestCollectCatalogResumesWithoutRefetchingCachedPage(t *testing.T) {
	server, calls := newCatalogTLSServer(t)
	collector := NewCollector(server.Client(), fixedClock())
	opts := CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 2}

	first, err := collector.CollectCatalog(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	before := calls[1]
	second, err := collector.CollectCatalog(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls[1] != before || second.Manifest.SnapshotID != first.Manifest.SnapshotID {
		t.Fatalf("resume refetched page 1 or changed snapshot: calls=%v", calls)
	}
}

func TestCollectCatalogMarksPartialAfterNonRetryablePage(t *testing.T) {
	collector, options := failingPageCollector(t, http.StatusBadRequest)
	got, err := collector.CollectCatalog(context.Background(), options)
	if err == nil || got.Manifest.Status != SnapshotPartial || len(got.Manifest.Failures) != 1 {
		t.Fatalf("result=%+v error=%v", got.Manifest, err)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) }
}

func newCatalogTLSServer(t *testing.T) (*httptest.Server, map[int]int) {
	t.Helper()
	calls := map[int]int{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		calls[page]++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			fmt.Fprint(w, `{"success":true,"data":{"skills":[{"id":"00000000-0000-0000-0000-000000000001","name":"小说助手"},{"id":"00000000-0000-0000-0000-000000000002","name":"对白助手"}],"total":3,"hasMore":true}}`)
			return
		}
		fmt.Fprint(w, `{"skills":[{"id":"00000000-0000-0000-0000-000000000003","name":"大纲助手"}],"total":3,"hasMore":false}`)
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func failingPageCollector(t *testing.T, status int) (*Collector, CollectorOptions) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(server.Close)
	return NewCollector(server.Client(), fixedClock()), CollectorOptions{BaseURL: server.URL, CacheRoot: t.TempDir(), PageSize: 2, RetryAttempts: 1, MaxRetryDelay: time.Millisecond}
}
```

- [ ] **Step 2: Run RED**

```powershell
go test ./internal/skills ./internal/quality/skilldiscovery -run 'Test(NewRestricted|CollectCatalog)' -count=1
```

Expected: FAIL because the exported client and collector do not exist.

- [ ] **Step 3: Export the existing restricted client without changing policy**

In `internal/skills/remote.go`, add:

```go
// NewRestrictedRemoteHTTPClient returns the HTTPS-only client used for remote
// Skill retrieval. It rejects non-public destinations on every redirect hop.
func NewRestrictedRemoteHTTPClient() *http.Client {
	return newSkillInstallHTTPClient()
}
```

Keep `newSkillInstallHTTPClient` as the private implementation used by current callers. Test that the exported constructor still rejects loopback, link-local, private IPv4/IPv6, non-HTTPS redirects, and excessive redirect chains.

- [ ] **Step 4: Implement cache ownership and atomic checkpoints**

Use these exact signatures:

```go
type Cache struct { Root string }

func DefaultCacheRoot() (string, error)
func (cache Cache) Initialize() error
func (cache Cache) ReadPage(kind, key string) ([]byte, PageReceipt, error)
func (cache Cache) WritePage(kind, key string, payload []byte, receipt PageReceipt) error
func (cache Cache) WriteLocalSnapshot(snapshot LocalSnapshot) error
func (cache Cache) LoadLocalSnapshot() (LocalSnapshot, error)
```

`DefaultCacheRoot` returns `filepath.Join(os.UserCacheDir(), "denova", "quality-eval", "xiaping")`. `Initialize` writes an ownership marker containing `denova.quality-eval.xiaping-cache/v1`. Page filenames are derived from a SHA-256 of `kind + "\n" + key`, never raw URLs. All writes use a same-directory temp, `Sync`, close, and rename; Windows directory-sync failure must not fail a successful file write.

- [ ] **Step 5: Implement GET pagination and envelope normalization**

Use:

```go
type CollectorOptions struct {
	BaseURL      string
	CacheRoot    string
	PageSize     int
	MinInterval  time.Duration
	RetryAttempts int
	MaxRetryDelay time.Duration
}

type Collector struct {
	client *http.Client
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
}

func NewCollector(client *http.Client, now func() time.Time) *Collector
func (collector *Collector) CollectCatalog(ctx context.Context, options CollectorOptions) (LocalSnapshot, error)
```

`CollectCatalog` must validate an HTTPS base URL, use `GET /api/skills?limit={PageSize}&page={N}`, accept both direct `{skills,total,hasMore}` and `{success,data:{skills,total,hasMore}}` responses, validate every returned ID, de-duplicate IDs, and sort final records by ID. Stop on `hasMore=false`; detect a repeated nonempty page hash as an error. `RetryAttempts` limits only one request's retries and is a required positive CLI/config value, not an overall crawl limit.

On `429`, parse delta-seconds or HTTP-date `Retry-After`; on retryable `5xx`, use exponential delay capped by `MaxRetryDelay`; on cancellation, return `ctx.Err()`. A failed page produces a `PARTIAL` manifest and failure receipt. A partial result may be persisted but later ranking must reject it.

- [ ] **Step 6: Run GREEN and existing security tests**

```powershell
go test ./internal/quality/skilldiscovery -run 'TestCollectCatalog' -count=1
go test ./internal/skills -run 'Test.*(Restricted|Redirect|Remote)' -count=1
git diff --check
```

Expected: PASS without live network access.

- [ ] **Step 7: Update the changelog and commit**

Add bilingual bullets for the restricted, resumable, rate-aware collector, then:

```powershell
git add -- CHANGELOG.md internal/skills/remote.go internal/skills/remote_security_test.go internal/quality/skilldiscovery
git diff --cached --check
git commit -m "feat: add resumable Xiaping catalog collection"
```

---

### Task 3: Add Comprehensive Writing Recall and Capability Proposals

**Files:**

- Create: `internal/quality/skilldiscovery/capability.go`
- Create: `internal/quality/skilldiscovery/capability_test.go`
- Create: `docs/project-design/implementation/skills/discovery/xiaping-capability-lexicon-v1.json`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: sorted `[]SkillRecord` and the sixteen `CoreCapabilityIDs` from Task 1.
- Produces: `Lexicon`, `LoadLexicon`, `ClassifyWritingCandidates`, and `BuildCapabilityProposals`.

- [ ] **Step 1: Write failing recall and proposal tests**

```go
func TestClassifyWritingCandidatesRecallsLifecycleTerms(t *testing.T) {
	lexicon := testLexicon(t)
	records := []SkillRecord{
		{ID: "world-a", Name: "世界观规则库", Description: "维护魔法规则和设定约束"},
		{ID: "world-b", Name: "设定一致性助手", Description: "检查世界规则与能力体系约束"},
		{ID: "dialogue", Name: "对白声线", Tags: []string{"人物", "台词"}},
		{ID: "video", Name: "小说转视频", Description: "生成分镜和视频提示词"},
	}
	got, proposals := ClassifyWritingCandidates(records, lexicon)
	if idsOf(got) != "dialogue,world-a,world-b" {
		t.Fatalf("candidate ids = %s", idsOf(got))
	}
	if proposalIDs(proposals) != "worldbuilding.build-rules" {
		t.Fatalf("proposal ids = %s", proposalIDs(proposals))
	}
}

func testLexicon(t *testing.T) Lexicon {
	t.Helper()
	return Lexicon{
		Contract: "denova.xiaping-capability-lexicon", Version: "v1",
		IncludeTerms: []string{"小说", "对白", "台词", "世界观", "设定"},
		ExcludeTerms: []string{"转视频", "视频提示词"},
		CoreCapabilities: []CapabilityRule{{CapabilityID: "character.build-dialogue-voice", Terms: []string{"对白", "台词"}}},
		ProposalRules: []CapabilityRule{{CapabilityID: "worldbuilding.build-rules", Terms: []string{"世界观", "设定", "规则"}, NameZH: "世界规则构建", NameEN: "Worldbuilding rules", Inputs: []string{"premise"}, Outputs: []string{"world_rules"}, LifecycleStage: "planning", MinimumPermission: "read-bounded-input", EvaluationMethod: "rule-consistency paired review"}},
	}
}

func idsOf(records []CandidateRecord) string {
	ids := make([]string, 0, len(records))
	for _, record := range records { ids = append(ids, record.Skill.ID) }
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func proposalIDs(records []CapabilityProposal) string {
	ids := make([]string, 0, len(records))
	for _, record := range records { ids = append(ids, record.CapabilityID) }
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func TestCapabilityProposalRequiresTwoNonDuplicateSkills(t *testing.T) {
	lexicon := testLexicon(t)
	got, proposals := ClassifyWritingCandidates([]SkillRecord{{ID: "one", Name: "情感曲线"}}, lexicon)
	if len(got) != 1 || len(proposals) != 0 {
		t.Fatalf("got candidates=%d proposals=%d", len(got), len(proposals))
	}
}
```

- [ ] **Step 2: Run RED**

```powershell
go test ./internal/quality/skilldiscovery -run 'Test(ClassifyWriting|CapabilityProposal)' -count=1
```

Expected: FAIL because lexicon loading and classification do not exist.

- [ ] **Step 3: Create the versioned lexicon**

The JSON root is:

```json
{
  "contract": "denova.xiaping-capability-lexicon",
  "version": "v1",
  "include_terms": ["小说", "网文", "故事", "写作", "短篇", "长篇", "章节", "开篇", "人物", "角色", "对白", "台词", "世界观", "设定", "大纲", "情节", "剧情", "冲突", "节奏", "爽点", "伏笔", "悬念", "反转", "续写", "连续性", "结尾", "审稿", "审校", "修改", "润色", "文风", "去AI味"],
  "exclude_terms": ["转视频", "视频提示词", "漫画", "分镜", "配图", "插画", "有声书", "配音", "发布运营"],
  "core_capabilities": [],
  "proposal_rules": []
}
```

Populate `core_capabilities` with all sixteen current IDs and Chinese/English aliases. Populate proposal rules for `worldbuilding.build-rules`, `research.verify-story-facts`, `emotion.manage-character-arc`, `scene.draft-from-brief`, and `platform.adapt-writing-style`; each rule includes inputs, outputs, lifecycle stage, minimum permission, and one concrete evaluation method. Exclusion wins only when no direct writing term remains after removing the excluded phrase, so “把视频剧本改写成小说” is still recallable.

- [ ] **Step 4: Implement deterministic field-level classification**

Use these exact signatures:

```go
type Lexicon struct {
	Contract         string                 `json:"contract"`
	Version          string                 `json:"version"`
	IncludeTerms     []string               `json:"include_terms"`
	ExcludeTerms     []string               `json:"exclude_terms"`
	CoreCapabilities []CapabilityRule       `json:"core_capabilities"`
	ProposalRules    []CapabilityRule       `json:"proposal_rules"`
}

type CapabilityRule struct {
	CapabilityID      string   `json:"capability_id"`
	Terms             []string `json:"terms"`
	NameZH            string   `json:"name_zh,omitempty"`
	NameEN            string   `json:"name_en,omitempty"`
	Inputs            []string `json:"inputs,omitempty"`
	Outputs           []string `json:"outputs,omitempty"`
	LifecycleStage    string   `json:"lifecycle_stage,omitempty"`
	MinimumPermission string   `json:"minimum_permission,omitempty"`
	EvaluationMethod  string   `json:"evaluation_method,omitempty"`
}

func LoadLexicon(path string) (Lexicon, error)
func ClassifyWritingCandidates(records []SkillRecord, lexicon Lexicon) ([]CandidateRecord, []CapabilityProposal)
```

Normalize with Unicode NFKC, lowercase Latin text, collapse whitespace, and inspect name, description, triggers, categories, and tags separately. Store every matching field/term. `MATCHED` requires either a name/trigger match or two distinct evidence fields; a single description/tag match is `AMBIGUOUS`. Retain both states in the candidate pool. A proposal is emitted only when at least two distinct Skill IDs from distinct provisional metadata signatures support it.

- [ ] **Step 5: Run GREEN and validate stable IDs**

```powershell
go test ./internal/quality/skilldiscovery -run 'Test(ClassifyWriting|CapabilityProposal|LoadLexicon)' -count=1
go run ./cmd/quality-eval validate --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json
git diff --check
```

Expected: PASS; existing evaluation manifest remains valid.

- [ ] **Step 6: Update the changelog and commit**

Add bilingual bullets for broad writing recall and non-routable proposals, then:

```powershell
git add -- CHANGELOG.md internal/quality/skilldiscovery docs/project-design/implementation/skills/discovery/xiaping-capability-lexicon-v1.json
git diff --cached --check
git commit -m "feat: classify Xiaping writing capabilities"
```

---

### Task 4: Cluster Exact and Near-Duplicate Candidates

**Files:**

- Create: `internal/quality/skilldiscovery/cluster.go`
- Create: `internal/quality/skilldiscovery/cluster_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: `[]CandidateRecord` from Task 3.
- Produces: `MetadataSignature`, `TokenJaccard`, and `ClusterCandidates`.

- [ ] **Step 1: Write failing clustering tests**

```go
func TestClusterCandidatesGroupsMetadataCopiesButNotSameAuthorAlone(t *testing.T) {
	candidates := []CandidateRecord{
		{Skill: SkillRecord{ID: "a", OwnerID: "owner", Name: "长篇小说助手", Description: "维护人物时间线伏笔并续写章节", Downloads: 10}},
		{Skill: SkillRecord{ID: "b", OwnerID: "other", Name: "长篇小说写作助手", Description: "维护人物、时间线、伏笔并续写章节", Downloads: 20}},
		{Skill: SkillRecord{ID: "c", OwnerID: "owner", Name: "对白助手", Description: "塑造人物独特声线", Downloads: 30}},
	}
	clusters := ClusterCandidates(candidates, 0.90)
	if len(clusters) != 1 || strings.Join(clusters[0].MemberIDs, ",") != "a,b" || clusters[0].RepresentativeID != "b" {
		t.Fatalf("clusters = %#v", clusters)
	}
}
```

- [ ] **Step 2: Run RED**

```powershell
go test ./internal/quality/skilldiscovery -run 'TestClusterCandidates' -count=1
```

Expected: FAIL because clustering functions do not exist.

- [ ] **Step 3: Implement explainable metadata clustering**

Use:

```go
func MetadataSignature(candidate CandidateRecord) string
func TokenJaccard(left, right CandidateRecord) float64
func ClusterCandidates(candidates []CandidateRecord, threshold float64) []DuplicateCluster
```

`MetadataSignature` hashes NFKC-normalized name, description, sorted triggers, and sorted tags. `TokenJaccard` uses unique Unicode bigrams plus normalized trigger/tag tokens. Union candidates with identical signatures or Jaccard greater than or equal to the versioned `0.90` threshold. Same author is evidence recorded in `Reasons`, never a union condition by itself. Choose the representative by downloads descending, star count descending, version count descending, then ID ascending. Sort cluster members and clusters deterministically.

Do not remove cluster members. Downstream selection receives a representative map and the full cluster artifact.

- [ ] **Step 4: Run GREEN and determinism checks**

```powershell
go test ./internal/quality/skilldiscovery -run 'Test(ClusterCandidates|TokenJaccard|MetadataSignature)' -count=1
go test ./internal/quality/skilldiscovery -count=10
git diff --check
```

Expected: PASS on all ten runs with byte-identical JSON fixtures.

- [ ] **Step 5: Update the changelog and commit**

Add bilingual bullets for explainable duplicate clustering, then:

```powershell
git add -- CHANGELOG.md internal/quality/skilldiscovery/cluster.go internal/quality/skilldiscovery/cluster_test.go
git diff --cached --check
git commit -m "feat: cluster duplicate Xiaping skills"
```

---

### Task 5: Collect and Calculate Review Evidence

**Files:**

- Create: `internal/quality/skilldiscovery/reviews.go`
- Create: `internal/quality/skilldiscovery/reviews_test.go`
- Create: `internal/quality/skilldiscovery/evidence.go`
- Create: `internal/quality/skilldiscovery/evidence_test.go`
- Modify: `internal/quality/skilldiscovery/collector.go`
- Modify: `internal/quality/skilldiscovery/collector_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: complete `LocalSnapshot`, writing candidates, cache, collector retry policy, and duplicate clusters.
- Produces: `CollectCandidateEvidence`, `SummarizeReviews`, `DownloadPercentiles`, `BayesianAdjustedStars`, and `BuildEvidenceVectors`.

- [ ] **Step 1: Write failing comment privacy, de-duplication, and scoring tests**

```go
func TestSummarizeReviewsExcludesOwnerGenericAndDuplicates(t *testing.T) {
	reviews := []ReviewRecord{
		{ID: "1", UserID: "owner", Stars: 5, Content: "非常好用", CreatedAt: "2026-07-01T00:00:00Z"},
		{ID: "2", UserID: "u2", Stars: 5, Content: "实际续写三章后，人物声线保持稳定，时间线没有漂移。", CreatedAt: "2026-07-02T00:00:00Z"},
		{ID: "3", UserID: "u3", Stars: 5, Content: "实际续写三章后人物声线保持稳定，时间线没有漂移", CreatedAt: "2026-07-02T00:01:00Z"},
	}
	policy := ReviewPolicy{MinimumRunes: 40, NearDuplicateJaccard: 0.90, ObservationTerms: []string{"续写", "人物", "时间线", "稳定", "漂移"}, GenericPhrases: []string{"非常好用", "效果不错", "推荐使用"}}
	got := SummarizeReviews("owner", reviews, policy)
	if got.EffectiveRaters != 1 || got.SubstantiveComments != 1 || got.OwnerSelfReviews != 1 || got.DuplicateComments != 1 {
		t.Fatalf("evidence = %#v", got)
	}
}

func TestCommittedEvidenceContainsNoReviewerOrSignedAvatar(t *testing.T) {
	raw := apiReview{ID: "review-1", UserID: "reviewer-1", UserName: "评审者", UserAvatarURL: "https://example.test/a.png?sign=ephemeral", Stars: 5, Content: "实际续写三章后，人物声线保持稳定，时间线没有漂移。"}
	review := normalizeAPIReview(raw)
	policy := ReviewPolicy{MinimumRunes: 20, NearDuplicateJaccard: 0.90, ObservationTerms: []string{"续写", "人物", "时间线"}}
	vector := EvidenceVector{SkillID: "skill-1", CapabilityID: "continuity.review-facts", Review: SummarizeReviews("owner", []ReviewRecord{review}, policy)}
	data, err := json.Marshal(vector)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"user_id", "user_name", "avatar", "?sign="} {
		if bytes.Contains(bytes.ToLower(data), []byte(forbidden)) {
			t.Fatalf("committed evidence leaked %q: %s", forbidden, data)
		}
	}
}
```

- [ ] **Step 2: Run RED**

```powershell
go test ./internal/quality/skilldiscovery -run 'Test(SummarizeReviews|CommittedEvidence|Bayesian|DownloadPercentiles)' -count=1
```

Expected: FAIL because review and evidence functions do not exist.

- [ ] **Step 3: Add detail/comment collection with cache resume**

Add these signatures:

```go
type EvidenceCollectionOptions struct {
	CollectorOptions
	CommentPageSize int
}

type SkillDetail struct {
	SkillRecord
	WeightedScore  float64 `json:"weighted_score"`
	SecurityReport string  `json:"security_report"`
}

func (collector *Collector) CollectCandidateEvidence(
	ctx context.Context,
	options EvidenceCollectionOptions,
	candidates []CandidateRecord,
) (map[string]SkillDetail, map[string][]ReviewRecord, []SnapshotFailure, error)
```

Normalize both `{success,data:{...}}` and direct detail/comment shapes. Fetch `/api/skills/{id}` and paginated `/api/skills/{id}/comments?limit={N}&page={P}` only for writing candidates. Raw cache may contain avatar URLs and reviewer IDs; normalized committed types must not have fields for them. Never write request or response bodies to logs.

Use these local-only raw types:

```go
type ReviewRecord struct {
	ID                   string
	UserID               string
	Stars                int
	Content              string
	Pros                 string
	Cons                 string
	UseCase              string
	ReviewerQualityScore float64
	PlatformQualityTotal float64
	CreatedAt            string
}

type apiReview struct {
	ID            string  `json:"id"`
	UserID        string  `json:"user_id"`
	UserName      string  `json:"user_name"`
	UserAvatarURL string  `json:"user_avatar_url"`
	Stars         int     `json:"stars"`
	Content       string  `json:"content"`
	Pros          string  `json:"pros"`
	Cons          string  `json:"cons"`
	UseCase       string  `json:"use_case"`
	CreatedAt     string  `json:"created_at"`
}

func normalizeAPIReview(raw apiReview) ReviewRecord
```

Store them only inside the cache snapshot. The returned values exist in memory for aggregation.

If a detail or comment page remains unavailable after configured retries, preserve its `SnapshotFailure`, set the affected vector's `EvidenceCacheStatus` to `EVIDENCE-CACHE-MISSING`, force `PlatformDataRich=false`, and continue ranking it only through the exploration path. Never substitute platform aggregate counts for unavailable independent-review evidence.

- [ ] **Step 4: Implement substantive review and anomaly rules**

Use:

```go
type ReviewPolicy struct {
	MinimumRunes          int
	NearDuplicateJaccard float64
	ObservationTerms     []string
	GenericPhrases       []string
}

func SummarizeReviews(ownerID string, reviews []ReviewRecord, policy ReviewPolicy) ReviewEvidence
```

A review is substantive when it is not an owner self-review, has at least `40` Unicode runes after normalization, is not a near duplicate at `0.90`, and contains an observation term covering input, output, behavior, failure, comparison, or concrete writing result. `pros`, `cons`, and `use_case` count as evidence text. Generic praise such as “很好用”, “效果不错”, or “推荐使用” alone does not count.

Add exact anomaly flags:

- `RATING-COMMENT-COUNT-MISMATCH` when absolute count difference exceeds both 5 and 20% of the larger count;
- `REVIEW-BURST` when at least 10 effective reviews exist and 60% were created in the same rolling 24-hour window;
- `DUPLICATE-COMMENT-CONCENTRATION` when at least 30% of non-owner comments are duplicates;
- `LOW-SUBSTANTIVE-RATIO` when at least 10 comments exist and fewer than 20% are substantive.

- [ ] **Step 5: Implement evidence calculations without inventing unavailable data**

Use:

```go
func DownloadPercentiles(candidates []CandidateRecord) map[string]map[string]float64
func BayesianAdjustedStars(averageX100 int, effectiveRaters int, poolMeanX100 float64, priorStrength int) float64
func BuildEvidenceVectors(candidates []CandidateRecord, reviews map[string]ReviewEvidence, clusters []DuplicateCluster) []EvidenceVector
```

Calculate percentiles independently per Capability from non-duplicate representatives using average rank for ties. For each Capability pool, derive the prior mean from effective-review-weighted stars and the prior strength from the median nonzero effective-review count; use `1` only when the median is empty. The adjustment is:

```go
adjusted := (float64(effectiveRaters)*float64(averageX100) + float64(priorStrength)*poolMeanX100) /
	(float64(effectiveRaters) + float64(priorStrength))
```

Never emit a Wilson interval or rating distribution when individual data is missing. Mark `PlatformDataRich=true` only when downloads are at least 50, percentile is at least 0.75, effective raters are at least 10, substantive comments are at least 5, and no severe manipulation flag is present. This label changes ranking priority only.

- [ ] **Step 6: Run GREEN including privacy scans**

```powershell
go test ./internal/quality/skilldiscovery -run 'Test(SummarizeReviews|CommittedEvidence|Bayesian|DownloadPercentiles|CollectCandidateEvidence)' -count=1
go test ./internal/quality/skilldiscovery -count=1
git diff --check
```

Expected: PASS; tests use synthetic comments only and make no live calls.

- [ ] **Step 7: Update the changelog and commit**

Add bilingual bullets for privacy-safe review evidence and descriptive data-rich tiers, then:

```powershell
git add -- CHANGELOG.md internal/quality/skilldiscovery
git diff --cached --check
git commit -m "feat: rank Xiaping evidence signals"
```

---

### Task 6: Build the Dual-Lane Shortlist and Committed Artifacts

**Files:**

- Create: `internal/quality/skilldiscovery/shortlist.go`
- Create: `internal/quality/skilldiscovery/shortlist_test.go`
- Create: `internal/quality/skilldiscovery/report.go`
- Create: `internal/quality/skilldiscovery/report_test.go`
- Create: `docs/project-design/implementation/skills/discovery/xiaping-discovery-v1.schema.json`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: complete snapshot, candidates, proposals, clusters, and evidence vectors.
- Produces: `BuildShortlist`, `WriteDiscoveryArtifacts`, `ValidateArtifactSchema`, and `RenderEvidenceReport`.

- [ ] **Step 1: Write failing wide-entry and diversity tests**

```go
func TestBuildShortlistKeepsThreeDataRichAndTwoExplorationSlots(t *testing.T) {
	candidates, vectors := shortlistFixture()
	got, err := BuildShortlist("snapshot-1", candidates, vectors, nil)
	if err != nil {
		t.Fatal(err)
	}
	dataRich, exploration, keptCold := 0, 0, false
	for _, entry := range got.Entries {
		if entry.CapabilityID != "style.revise-prose" { continue }
		if entry.Lane == LaneDataRich { dataRich++ }
		if entry.Lane == LaneExploration { exploration++ }
		if entry.SkillID == "cold-but-distinct" && entry.Lane == LaneExploration { keptCold = true }
	}
	if dataRich != 3 || exploration != 2 || !keptCold {
		t.Fatalf("entries = %#v", got.Entries)
	}
}

func TestBuildShortlistRejectsPartialSnapshot(t *testing.T) {
	_, err := BuildShortlistFromSnapshot(LocalSnapshot{Manifest: SnapshotManifest{Status: SnapshotPartial}}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "complete snapshot") {
		t.Fatalf("error = %v", err)
	}
}

func shortlistFixture() ([]CandidateRecord, []EvidenceVector) {
	ids := []string{"rich-a", "rich-b", "rich-c", "cold-but-distinct", "cold-second"}
	candidates := make([]CandidateRecord, 0, len(ids))
	vectors := make([]EvidenceVector, 0, len(ids))
	for index, id := range ids {
		candidates = append(candidates, CandidateRecord{Skill: SkillRecord{ID: id, OwnerID: "owner-" + id, Name: id}, Capabilities: []CapabilityMatch{{CapabilityID: "style.revise-prose", Status: MatchMatched, Evidence: []FieldEvidence{{Field: "name", Term: "文风"}}}}})
		vectors = append(vectors, EvidenceVector{SkillID: id, CapabilityID: "style.revise-prose", DownloadPercentile: float64(5-index) / 5, BayesianStarsX100: float64(450-index), Review: ReviewEvidence{EffectiveRaters: 20-index, SubstantiveComments: 8-index}, PlatformDataRich: index < 3, MaturityVersionCount: index + 1, EvidenceCacheStatus: "AVAILABLE"})
	}
	return candidates, vectors
}
```

- [ ] **Step 2: Run RED**

```powershell
go test ./internal/quality/skilldiscovery -run 'TestBuildShortlist' -count=1
```

Expected: FAIL because shortlist functions do not exist.

- [ ] **Step 3: Implement deterministic dual-lane selection**

Use:

```go
func BuildShortlist(
	snapshotID string,
	candidates []CandidateRecord,
	vectors []EvidenceVector,
	clusters []DuplicateCluster,
) (Shortlist, error)

func BuildShortlistFromSnapshot(
	snapshot LocalSnapshot,
	candidates []CandidateRecord,
	vectors []EvidenceVector,
	clusters []DuplicateCluster,
) (Shortlist, error)
```

`BuildShortlistFromSnapshot` rejects any status other than `COMPLETE` and then delegates to `BuildShortlist` with the validated snapshot ID.

For each Capability:

1. Collapse exact/near-duplicate clusters to representatives for slot selection while retaining member references in reasons.
2. Rank the data-rich lane by severe anomaly absence, `PlatformDataRich`, Bayesian stars, effective raters, download percentile, version count, then ID.
3. Select up to three data-rich candidates, preferring distinct owners. Reuse an owner only when no unused owner can fill the slot.
4. Rank remaining candidates for exploration by method-cluster distance, number of matched evidence fields, Profile coverage novelty, content metadata completeness, then ID.
5. Select up to two exploration candidates, again preferring distinct owners and clusters.
6. Record an explicit gap when fewer than five credible candidates exist. Never fill a slot with `NOT-MATCHED` or excluded media-production candidates.

Each `ShortlistEntry.Evidence` is the exact vector used for that selection, so anomaly facts and metric inputs remain inspectable in the committed shortlist. The shortlist contains no `EVAL-PROMISING`, `EVAL-CONFIRMED`, or recommendation claim.

- [ ] **Step 4: Implement schema-bound artifact and report output**

Add the mature JSON Schema validator dependency before writing validation code:

```powershell
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.2
```

Add:

```go
type DiscoveryArtifacts struct {
	Manifest   SnapshotManifest
	Candidates []CandidateRecord
	Proposals  []CapabilityProposal
	Clusters   []DuplicateCluster
	Evidence   []EvidenceVector
	Shortlist  Shortlist
}

func WriteDiscoveryArtifacts(root string, artifacts DiscoveryArtifacts) error
func ValidateArtifactSchema(schemaPath string, artifactPaths []string) error
func RenderEvidenceReport(artifacts DiscoveryArtifacts) ([]byte, error)
```

`WriteDiscoveryArtifacts` wraps slices in `CandidateIndex`, `CapabilityProposalIndex`, and `DuplicateClusterIndex` so every JSON document has `contract`, `version`, and `snapshot_id` at its root. The schema's root `oneOf` dispatches on those contract constants plus the snapshot manifest, lexicon, and shortlist contracts.

`ValidateArtifactSchema` uses `github.com/santhosh-tekuri/jsonschema/v6`: decode the schema into `any`, add it to a new compiler under `schema.json`, compile it, decode each artifact JSON into `any`, and call `schema.Validate(document)`. Compilation and validation are offline; the schema must not contain remote `$ref` values.

Write exactly these paths:

- `xiaping-snapshot-manifest-v1.json`
- `xiaping-writing-candidates-v1.json`
- `xiaping-capability-proposals-v1.json`
- `xiaping-duplicate-clusters-v1.json`
- `xiaping-evidence-shortlist-v1.json`
- `XIAPING_EVIDENCE_REPORT.md`

The JSON Schema file uses draft 2020-12, rejects unknown top-level properties, and has artifact-specific roots selected by the `contract` field. The bilingual report must show snapshot completeness, candidate counts, Capability coverage/gaps, proposal counts, duplicate/anomaly counts, lane counts, limitations, and the exact statement that platform evidence is not a writing-quality result.

- [ ] **Step 5: Run GREEN and generated-content scans**

```powershell
go test ./internal/quality/skilldiscovery -run 'Test(BuildShortlist|WriteDiscoveryArtifacts|RenderEvidenceReport)' -count=1
go test ./internal/quality/skilldiscovery -count=1
git diff --check
```

Expected: PASS; generated fixtures contain no reviewer IDs, raw comments, signed URLs, or third-party package contents.

- [ ] **Step 6: Update the changelog and commit**

Add bilingual bullets for the wide-entry dual-lane shortlist and schema-bound report, then:

```powershell
git add -- CHANGELOG.md go.mod go.sum internal/quality/skilldiscovery docs/project-design/implementation/skills/discovery/xiaping-discovery-v1.schema.json
git diff --cached --check
git commit -m "feat: select Xiaping writing skill shortlists"
```

---

### Task 7: Extend `quality-eval` with Discovery Commands

**Files:**

- Modify: `cmd/quality-eval/main.go`
- Create: `cmd/quality-eval/skills_commands.go`
- Create: `cmd/quality-eval/skills_commands_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: all Task 1–6 package functions and `skills.NewRestrictedRemoteHTTPClient`.
- Produces: CLI subcommands `skills snapshot-xiaping`, `skills classify-xiaping`, `skills rank-xiaping`, and `skills validate-xiaping`.

- [ ] **Step 1: Write failing CLI dispatch tests**

```go
func TestRunSkillsRequiresKnownSubcommand(t *testing.T) {
	err := run(context.Background(), []string{"skills", "unknown"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown skills command") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunSkillsValidateIsOffline(t *testing.T) {
	root := writeValidDiscoveryArtifacts(t)
	schema := filepath.Join("..", "..", "docs", "project-design", "implementation", "skills", "discovery", "xiaping-discovery-v1.schema.json")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"skills", "validate-xiaping", "--root", root, "--schema", schema}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "VALID") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func writeValidDiscoveryArtifacts(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifest := skilldiscovery.SnapshotManifest{Contract: "denova.xiaping-snapshot-manifest", Version: "v1", SnapshotID: "snapshot-test", Status: skilldiscovery.SnapshotComplete, StartedAt: "2026-07-21T00:00:00Z", CompletedAt: "2026-07-21T00:01:00Z", BaseURL: "https://example.test", NormalizationVersion: "v1", ReportedTotal: 1, UniqueSkills: 1, Pages: []skilldiscovery.PageReceipt{{Kind: "catalog", Key: "1", URL: "https://example.test/api/skills?limit=50&page=1", HTTPStatus: 200, CapturedAt: "2026-07-21T00:00:30Z", SHA256: "sha256:" + strings.Repeat("a", 64), ItemCount: 1}}, SkillRecordsSHA256: "sha256:" + strings.Repeat("b", 64)}
	artifacts := skilldiscovery.DiscoveryArtifacts{Manifest: manifest, Candidates: []skilldiscovery.CandidateRecord{}, Proposals: []skilldiscovery.CapabilityProposal{}, Clusters: []skilldiscovery.DuplicateCluster{}, Evidence: []skilldiscovery.EvidenceVector{}, Shortlist: skilldiscovery.Shortlist{Contract: "denova.xiaping-evidence-shortlist", Version: "v1", SnapshotID: "snapshot-test", Entries: []skilldiscovery.ShortlistEntry{}, Gaps: []skilldiscovery.CapabilityGap{}}}
	if err := skilldiscovery.WriteDiscoveryArtifacts(root, artifacts); err != nil { t.Fatal(err) }
	return root
}
```

- [ ] **Step 2: Run RED**

```powershell
go test ./cmd/quality-eval -run 'TestRunSkills' -count=1
```

Expected: FAIL because `skills` dispatch is unknown.

- [ ] **Step 3: Add thin command dispatch**

Modify the main switch only as follows:

```go
case "skills":
	return runSkills(ctx, args[1:], stdout, stderr)
```

Update the no-argument error to include `skills`. Put all flags and orchestration in `skills_commands.go`:

```go
const defaultDiscoveryRoot = "docs/project-design/implementation/skills/discovery"
const defaultXiapingBaseURL = "https://xiaping.coze.com"

func runSkills(ctx context.Context, args []string, stdout, stderr io.Writer) error
func runXiapingSnapshot(ctx context.Context, args []string, stdout, stderr io.Writer) error
func runXiapingClassify(args []string, stdout, stderr io.Writer) error
func runXiapingRank(ctx context.Context, args []string, stdout, stderr io.Writer) error
func runXiapingValidate(args []string, stdout, stderr io.Writer) error
```

Flags are explicit:

- snapshot: `--base-url`, `--cache-root`, `--root`, `--page-size`, `--min-interval`, `--retry-attempts`, `--max-retry-delay`;
- classify: `--cache-root`, `--root`, `--lexicon`;
- rank: `--base-url`, `--cache-root`, `--root`, `--comment-page-size`, `--min-interval`, `--retry-attempts`, `--max-retry-delay`;
- validate: `--root`, `--schema`.

All durations use `time.ParseDuration`. `snapshot-xiaping` and `rank-xiaping` require HTTPS unless a test injects a TLS server. Standard output contains one concise line such as `SNAPSHOT id=... status=COMPLETE skills=...`; progress and retry logs go to standard error without URLs containing query strings.

- [ ] **Step 4: Ensure partial data cannot rank**

Add a CLI test that writes a `PARTIAL` local snapshot and asserts:

```text
rank-xiaping requires a COMPLETE snapshot; failures=1
```

The command must leave existing committed artifacts unchanged on failure by generating into a sibling staging directory and renaming files only after every validation passes.

- [ ] **Step 5: Run GREEN and existing CLI regression**

```powershell
go test ./cmd/quality-eval -count=1
go test ./internal/quality/evaluation ./internal/quality/skilldiscovery -count=1
go run ./cmd/quality-eval validate --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json
git diff --check
```

Expected: PASS; existing four commands retain their output contracts.

- [ ] **Step 6: Update the changelog and commit**

Add bilingual bullets for the offline-validatable discovery CLI, then:

```powershell
git add -- CHANGELOG.md cmd/quality-eval internal/quality/skilldiscovery
git diff --cached --check
git commit -m "feat: add Xiaping discovery commands"
```

---

### Task 8: Run the Live Public Discovery and Publish the Shortlist

**Files:**

- Create: `docs/project-design/implementation/skills/discovery/xiaping-snapshot-manifest-v1.json`
- Create: `docs/project-design/implementation/skills/discovery/xiaping-writing-candidates-v1.json`
- Create: `docs/project-design/implementation/skills/discovery/xiaping-capability-proposals-v1.json`
- Create: `docs/project-design/implementation/skills/discovery/xiaping-duplicate-clusters-v1.json`
- Create: `docs/project-design/implementation/skills/discovery/xiaping-evidence-shortlist-v1.json`
- Create: `docs/project-design/implementation/skills/discovery/XIAPING_EVIDENCE_REPORT.md`
- Modify: `docs/project-design/implementation/skills/XIAPING_SOURCE_MATRIX.md`
- Modify: `docs/project-design/implementation/planning/PHASE_0_DETAILED_PLAN.md`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: the live public Xiaping API through Task 7 commands and local user cache.
- Produces: a current, complete, auditable writing-candidate evidence snapshot and shortlist for plan 2.

- [ ] **Step 1: Reconfirm machine and repository truth**

```powershell
git status --short --branch -uall
git rev-parse HEAD
go version
$cacheRoot = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Denova\quality-eval\xiaping'
$cacheRoot
```

Expected: expected feature branch, clean worktree, Go 1.26.5, and a cache path outside the repository. Stop if unrelated dirty files overlap this task.

- [ ] **Step 2: Collect the full public catalog with conservative pacing**

```powershell
go run ./cmd/quality-eval skills snapshot-xiaping `
  --cache-root $cacheRoot `
  --root docs/project-design/implementation/skills/discovery `
  --page-size 50 `
  --min-interval 750ms `
  --retry-attempts 5 `
  --max-retry-delay 30s
```

Expected: `SNAPSHOT ... status=COMPLETE`; the reported total is recorded from the live API rather than asserted as 2,251. If rate-limited or interrupted, rerun the identical command to resume. Do not rank a partial snapshot.

- [ ] **Step 3: Classify the complete writing pool**

```powershell
go run ./cmd/quality-eval skills classify-xiaping `
  --cache-root $cacheRoot `
  --root docs/project-design/implementation/skills/discovery `
  --lexicon docs/project-design/implementation/skills/discovery/xiaping-capability-lexicon-v1.json
```

Expected: one `CLASSIFIED` line with total writing candidates, matched/ambiguous counts, stable Capability coverage, and proposal count. Manually inspect at least 20 deterministic samples across included, ambiguous, proposed, and excluded records; record corrections as lexicon changes and rerun rather than editing generated candidates.

- [ ] **Step 4: Collect review evidence and generate the dual-lane shortlist**

```powershell
go run ./cmd/quality-eval skills rank-xiaping `
  --cache-root $cacheRoot `
  --root docs/project-design/implementation/skills/discovery `
  --comment-page-size 50 `
  --min-interval 750ms `
  --retry-attempts 5 `
  --max-retry-delay 30s
```

Expected: `RANKED ...` with candidate, cluster, data-rich, exploration, proposal, and gap counts. Resume after `429` or interruption. No agent document or package is downloaded.

- [ ] **Step 5: Validate committed artifacts and forbidden content**

```powershell
go run ./cmd/quality-eval skills validate-xiaping `
  --root docs/project-design/implementation/skills/discovery `
  --schema docs/project-design/implementation/skills/discovery/xiaping-discovery-v1.schema.json

rg -n -i 'user_id|user_name|avatar_url|\?sign=|x-amz-signature|authorization|api_key|access_token|BEGIN .*PRIVATE KEY' docs/project-design/implementation/skills/discovery
```

Expected: `VALID`; `rg` returns no forbidden raw-review, signed-URL, credential, or private-key values. Field names in schema descriptions must avoid the forbidden raw-source names so the scan remains exact.

- [ ] **Step 6: Review evidence rather than accepting platform rank**

Open the generated report and verify:

- every core Capability has either candidates or an explicit gap;
- exploration entries are 20–30% where the pool permits it;
- no author or duplicate cluster monopolizes one Capability;
- high-download but generic/irrelevant records are excluded with evidence;
- low-data distinctive records survive in exploration slots;
- platform metrics are labeled source claims and no entry is called quality-confirmed;
- Capability proposals meet the two-nonduplicate-source rule.

Record the exact live counts and any source limitations in `XIAPING_SOURCE_MATRIX.md` and the P0-T08A section. Do not update the deep-audit catalog because packages were not inspected in this plan.

- [ ] **Step 7: Run full repository gates**

```powershell
go mod tidy -diff
go test ./internal/quality/skilldiscovery ./internal/skills ./cmd/quality-eval -count=1
go test ./... -count=1
go vet ./...
& 'C:\Program Files\Git\bin\bash.exe' ./scripts/build.sh
git diff --check
git status --short -uall
```

Expected: all commands exit 0; only planned source, test, planning, discovery artifact, report, and changelog files are changed.

- [ ] **Step 8: Update the changelog and commit the live evidence artifacts**

Add bilingual bullets with the actual snapshot/candidate/cluster/shortlist counts and the explicit `not a quality result` limitation. Stage an exact allowlist, inspect `git diff --cached --name-only`, then:

```powershell
git commit -m "docs: publish Xiaping writing skill shortlist"
```

After the commit, verify a clean worktree and record the SHA. Do not push.

## Completion Handoff to Plan 2

Plan 1 is complete when Task 8 has a clean, committed shortlist and report. Before package retrieval or model calls, review the shortlist with the user and write a separate implementation plan that names the approved candidates, maximum package/context budgets, tuning tasks, evaluation arms, reviewer workflow, and regression gate. No `EVAL-PROMISING` or `EVAL-CONFIRMED` state is permitted before that second plan executes.
