# Xiaping Skill Deep Audit and Evaluation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the noisy Xiaping metadata shortlist into a content-verified, capability-scoped novel-writing reference set, then prepare fair tuning-only quality comparisons without claiming quality before paired evidence exists.

**Architecture:** Public `agent.md` documents and their current signed ZIPs are retrieved only into an owned OS temporary/cache directory. Package bytes are hashed and statically inspected without executing third-party code; Git stores only bounded facts, hashes, capability-level method summaries, rejection reasons, and evaluation records. Quality screening separates platform evidence, content audit, model prescreen, and human blind review.

**Tech Stack:** Go 1.26 repository tooling, PowerShell 7, public HTTPS GET, SHA-256, existing P0-T07 evaluation contracts, JSON/Markdown artifacts.

## Global Constraints

- Do not execute third-party Python, Shell, JavaScript, binaries, installers, hooks, or package-defined tools.
- Raw ZIPs, full `SKILL.md`, references, assets, signed URLs, reviewer identities, and review bodies stay outside Git.
- Refresh `agent.md` immediately before each ZIP GET; never persist signed query strings in logs or committed artifacts.
- Maximum package size is 4 MiB each and 32 MiB for the complete batch; maximum expanded size is 8 MiB and 5,000 files per package.
- Capability extraction may read at most 512 KiB per text file. A future model call may receive at most 256 KiB total Skill-derived context and at most 48 KiB for one capability slice, always with source, purpose, and hash.
- Only P0-T07 `tuning` inputs may guide selection or prompt adjustment. `regression` remains frozen for the selected approach; `release_holdout` remains unopened until the release gate.
- Platform counts are source evidence, not writing-quality results. Static audit may produce `DEEP-AUDIT-PASS`, `REFERENCE-ONLY`, or `REJECT`; it may not produce `EVAL-PROMISING` or `EVAL-CONFIRMED`.
- Model prescreen requires the same configured model and runtime-only credential for both arms. Missing credentials produce `MODEL-EVAL-BLOCKED`, not substituted Codex output.
- Do not modify the historical P0-T08 catalog or pretend the 65-entry automated shortlist is manually approved.
- Update `CHANGELOG.md` before every commit. Commit messages are English. Do not push, merge, rebase, tag, release, install a Skill, or change product APIs/UI.

---

### Task 1: Freeze the Human-Curated Batch and False-Positive Record

**Files:**
- Create: `docs/project-design/implementation/planning/XIAPING_SKILL_DEEP_AUDIT_AND_EVALUATION_PLAN.md`
- Create: `docs/project-design/implementation/skills/deep-audit/xiaping-deep-audit-batch-v1.json`
- Create: `docs/project-design/implementation/skills/deep-audit/XIAPING_RELEVANCE_AUDIT.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the 65 shortlist entries, 1,072 candidate records, the original nine-candidate catalog, and current platform evidence.
- Produces: a stable 15-candidate batch with `wave`, `selection_basis`, `target_capabilities`, `target_profiles`, and explicit false-positive exclusions.

- [ ] **Step 1: Record the selected candidates**

Wave A content audit starts with six high-evidence, directly novel-writing Skills:

```text
a3504d81-736d-49b3-9a98-2a4a3cd3aaf1  小说助手
5a7d600a-1ff2-4d11-818c-e080f405deb9  深度小说写作法
56ed7d78-5636-4c78-83cc-0aee4802b0b7  小说爽点架构生成器
f47e299c-3861-421d-9f1d-971de45bf622  小说审校员
8643f73c-7eeb-4409-9136-09c9a125c2ff  人味写作引擎
8f2aeb09-5135-489f-9d40-b57cb07cb18a  小说创作大神·100+位作家风格引擎
```

Wave B preserves breadth and fills profile/capability gaps:

```text
6a436d60-f180-4d09-8d23-87666fc2ea2b  AI文本人性化
d4f61b34-aa84-4d75-aa43-fa2ae81823c2  小说框架维度化器
12c7fc40-3ae0-474a-a21f-a9b8aff4b572  多米的长篇小说创作
7e465336-639f-469e-a268-9633cfd7d448  黄金开篇大师
95687260-d5ad-43d3-b083-008d00a9dfa3  人物塑造大师
5330e3ad-e596-459d-827e-79e580e053f0  番茄短故事创作法
f4b6f5b7-c284-4df3-a252-f4ce301a221c  盐言小说创作
3a5d8b5d-c319-41b8-8a3f-3c7c87ff83ce  情节架构大师
b71b0d23-9bb4-4e45-9328-8f596efc67cf  网文写作前必检八项
```

- [ ] **Step 2: Preserve the rejected evidence**

Document every rejected automated shortlist entry by stable Skill ID and one of `NON_WRITING_DOMAIN`, `MEDIA_ONLY`, `GENERIC_TOOLING`, `SOCIAL_CONTENT_ONLY`, or `INSUFFICIENT_NOVEL_LINK`. The report must explicitly call out known false positives such as Skill security scanning, Feishu tables, Agent tracking, finance, force calculation, layout, and e-commerce imagery.

- [ ] **Step 3: Validate and commit**

```powershell
$batch = Get-Content -Raw docs/project-design/implementation/skills/deep-audit/xiaping-deep-audit-batch-v1.json | ConvertFrom-Json -Depth 100
if ($batch.candidates.Count -ne 15) { throw 'expected 15 candidates' }
if (($batch.candidates.id | Sort-Object -Unique).Count -ne 15) { throw 'candidate IDs must be unique' }
git diff --check
git add CHANGELOG.md docs/project-design/implementation/planning/XIAPING_SKILL_DEEP_AUDIT_AND_EVALUATION_PLAN.md docs/project-design/implementation/skills/deep-audit/xiaping-deep-audit-batch-v1.json docs/project-design/implementation/skills/deep-audit/XIAPING_RELEVANCE_AUDIT.md
git commit -m "docs: curate Xiaping novel skill audit batch"
```

Expected: 15 unique candidates, false positives remain auditable, and only the four allowlisted paths are committed.

### Task 2: Retrieve and Statically Inspect Wave A Packages

**Files:**
- Create: `internal/skills/archive_audit.go`
- Create: `internal/skills/archive_audit_test.go`
- Modify: `internal/skills/install.go`
- Create: `docs/project-design/implementation/skills/deep-audit/xiaping-content-audit-v1.json`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the Wave A IDs and public `GET /api/skills/{id}` plus `GET /skill/{id}/agent.md` sources.
- Produces: `InspectArchive(data []byte, limits ArchiveAuditLimits) (ArchiveAudit, error)` in the existing shared Skills package and a committed summary containing stable URLs without queries, current version, hashes, file inventory facts, permissions, and status. The installer and auditor share path/symlink/size validation instead of maintaining two ZIP security implementations.

- [ ] **Step 1: Write archive-boundary tests**

Tests must create local synthetic ZIPs and require rejection of traversal, absolute paths, symlinks, duplicate normalized paths, more than 5,000 files, more than 8 MiB expanded bytes, scripts mislabeled as text, and archives over 4 MiB. They must require stable SHA-256 and a sorted text-file inventory without executing content.

- [ ] **Step 2: Run RED**

```powershell
& .\.tools\go\bin\go.exe test ./internal/skills -run 'TestInspectArchive' -count=1
```

Expected: FAIL because the deep-audit archive API does not exist.

- [ ] **Step 3: Implement the bounded inspector and run GREEN**

Implement `ArchiveAuditLimits{MaxArchiveBytes: 4<<20, MaxExpandedBytes: 8<<20, MaxFiles: 5000, MaxTextFileBytes: 512<<10}` and return metadata only. Extract the existing installer ZIP-entry validation into one private helper used by both install and audit paths. Then rerun the focused test and `go test ./internal/skills -count=1`.

- [ ] **Step 4: Retrieve Wave A into an owned temporary root**

Create exactly one root under `$env:TEMP\denova-xiaping-content\<run-id>`. For each ID, GET metadata, refresh `agent.md`, extract the ZIP URL in memory, reject non-HTTPS/non-approved hosts, download within the size bound, hash it, and inspect it. Do not print or save the signed URL. Do not execute package content.

- [ ] **Step 5: Publish summaries and commit**

The committed JSON records source/version/hash/file/permission/license facts and `CONTENT-VERIFIED`, `SOURCE-UNAVAILABLE`, or `REJECTED-UNSAFE`; it contains no third-party body. Update the changelog, run focused tests and `git diff --check`, then commit:

```powershell
git commit -m "feat: audit Xiaping novel skill packages"
```

### Task 3: Extract Capability-Level Reference Findings

**Files:**
- Create: `docs/project-design/implementation/skills/deep-audit/xiaping-capability-reference-v1.json`
- Create: `docs/project-design/implementation/skills/deep-audit/XIAPING_CONTENT_QUALITY_REPORT.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: verified Wave A text files from the owned temporary root and the 16 stable Capability IDs.
- Produces: bounded original summaries of methods, inputs, outputs, constraints, strengths, failure modes, and evidence locations (`archive_sha256`, file path, line span hash); no copied full instructions.

- [ ] **Step 1: Score content, not marketing**

For each Skill and each claimed capability, score `specificity`, `operational_detail`, `novel_fit`, `profile_fit`, `state_boundary_fit`, `testability`, and `safety` from 0–4 with evidence. A package passes only when its relevant capability has no zero, average at least 2.5, and no requirement to surrender Harness state, hidden files, network, or irreversible writes.

- [ ] **Step 2: Assign a content decision**

Use only `DEEP-AUDIT-PASS`, `REFERENCE-ONLY`, or `REJECT`. A high platform score cannot override vague, irrelevant, unsafe, or non-decomposable content. Record disagreements between two Terra/medium content auditors as `AUDIT-DISAGREEMENT`; resolve from cited content, not a third review loop.

- [ ] **Step 3: Validate and commit**

Validate the 16-Capability closed set, source hashes, line-span hashes, 256 KiB aggregate per-Skill summary bound, and absence of signed URLs/full third-party documents. Update the changelog and commit:

```powershell
git commit -m "docs: assess Xiaping novel skill content quality"
```

### Task 4: Prepare Tuning-Only Paired Evaluation

**Files:**
- Create: `docs/project-design/implementation/skills/deep-audit/xiaping-evaluation-matrix-v1.json`
- Create: `docs/project-design/implementation/skills/deep-audit/XIAPING_EVALUATION_STATUS.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: capability references that passed Task 3 and six P0-T07 tuning tasks.
- Produces: preregistered S/K pairings where S is the frozen single-turn baseline and K differs only by one bounded capability slice.

- [ ] **Step 1: Freeze six tuning tasks**

Use these task IDs and no regression/release-holdout input:

```text
ls-mystery-dialogue-03
ls-mystery-turn-05
fq-urban-opening-01
fq-urban-dialogue-03
zh-marriage-opening-01
zh-workplace-turn-05
```

- [ ] **Step 2: Freeze the fair arms**

Both S and K use the same provider, model, model profile, temperature, output-token ceiling, task facts, and QualitySpec. K receives exactly one relevant capability slice, capped at 48 KiB. All calls, tokens, costs, failures, inputs, outputs, and hashes are recorded. Full Skills, scripts, references, prior outputs, and reviewer feedback are excluded.

- [ ] **Step 3: Record the current execution state**

If the configured runtime credential is absent, write `MODEL-EVAL-BLOCKED` with reason `provider_credentials_missing`; do not substitute Codex/subagent prose and do not generate fake A/B files. When credentials exist, generate paired outputs, package them anonymously, obtain two independent human blind decisions, and use a third adjudicator only for disagreement.

- [ ] **Step 4: Commit the preregistration**

Update the changelog, validate that every matrix row points to a tuning task and passed content hash, run `git diff --check`, and commit:

```powershell
git commit -m "docs: preregister Xiaping skill quality evaluation"
```

### Task 5: Close the Audit Without Installing Skills

**Files:**
- Modify: `docs/project-design/implementation/skills/deep-audit/XIAPING_CONTENT_QUALITY_REPORT.md`
- Modify: `docs/project-design/implementation/skills/deep-audit/XIAPING_EVALUATION_STATUS.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: all content decisions, validation results, and any real paired evaluation evidence.
- Produces: an ordered `adopt as bounded reference`, `keep exploring`, and `reject` list, with unresolved model/human evidence clearly separated.

- [ ] **Step 1: Run repository and privacy gates**

```powershell
& .\.tools\go\bin\go.exe mod tidy -diff
& .\.tools\go\bin\go.exe test ./internal/quality/skilldiscovery ./internal/skills ./cmd/quality-eval -count=1
& .\.tools\go\bin\go.exe test ./... -count=1
& .\.tools\go\bin\go.exe vet ./...
& 'C:\Program Files\Git\bin\bash.exe' ./scripts/build.sh
rg -n -i '\?sign=|x-amz-signature|authorization:|bearer |BEGIN .*PRIVATE KEY|reviewer_id|raw_comments' docs/project-design/implementation/skills/deep-audit
git diff --check
```

Expected: all Go/build/diff gates exit 0; the forbidden-content scan returns no matches.

- [ ] **Step 2: Clean only the owned temporary root**

Resolve the exact absolute path, verify it is a child of `$env:TEMP\denova-xiaping-content`, count files, remove only that run root, and verify it no longer exists. Never remove the shared Xiaping evidence cache.

- [ ] **Step 3: Commit the final audit status**

Update the changelog and commit with an English message. Leave the branch clean and unpushed. A Skill is not installable or quality-confirmed until its package hash, permissions, capability slice, paired model evidence, and human blind result all pass.
