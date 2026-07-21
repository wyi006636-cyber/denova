# Xiaping Skill Discovery and Evidence Funnel Design

- **Status:** Approved design
- **Date:** 2026-07-21
- **Repository:** Harness Novel
**Phase boundary:** A separate post-P0-T08, pre-P0-T09 research increment. It does not rewrite the completed P0-T08 audit or claim that its original nine candidates are quality winners.

## 1. Purpose

P0-T08 established provenance, static safety, capability mapping, and integration red lines for nine fixed Xiaping candidates. It deliberately left every candidate at `PENDING-BLIND-REVIEW`. The nine-item set is too small and too preselected to answer either of these questions:

1. Which Xiaping Skills are supported by substantial public usage and review evidence?
2. Which Skills actually improve Harness Novel output relative to the no-Skill baseline?

This design adds a reproducible discovery and evidence funnel over Xiaping's public catalog. The funnel broadly recalls novel-writing candidates, keeps both data-rich and exploratory methods, audits only a bounded shortlist, and requires paired blind evaluation before integration.

The public Xiaping database is a reference source for discovery, prioritization, method analysis, and evaluation hypotheses. It is not a quality oracle and does not replace Harness Novel's file truth, QualitySpec, evaluation corpus, or author judgment.

## 2. Confirmed Product Decisions

- Scope includes novel writing and editorial review only. Video, comics, illustration, audio, publishing operations, and unrelated content-production Skills are excluded.
- Discovery must be broader than the current sixteen Capability IDs. The sixteen IDs remain the stable core; recurring uncovered abilities become proposed Capability records until separately approved.
- Publicly reachable metadata, comments, agent documents, and package contents may be read for research and internal evaluation.
- Unknown licensing does not block discovery, static analysis, or internal evaluation. It remains an explicit rights state and cannot be silently rewritten as redistribution or preinstallation permission.
- Raw comments and third-party package contents are not committed. The repository keeps normalized facts, hashes, aggregates, evidence references, and authored summaries.
- Selection is deliberately wide at the entrance. Popularity thresholds prioritize work but do not reject promising low-data candidates.
- Final integration depends on demonstrated quality uplift, not downloads, star ratings, marketing claims, platform status, or database size.

## 3. Scope

### 3.1 In scope

- A resumable snapshot of Xiaping's public Skill metadata.
- Broad novel-writing recall using names, descriptions, triggers, categories, tags, and approved query terms.
- Stable Capability mapping plus evidence-backed proposals for new Capabilities.
- Exact and near-duplicate clustering, including same-author variants and content-level forks.
- Separate platform-evidence, content-maturity, applicability, risk, and evaluation states.
- A dual-lane shortlist for each Capability: data-rich candidates and exploratory candidates.
- Temporary retrieval and static inspection of shortlisted packages without executing their scripts.
- Paired no-Skill versus Skill evaluation using the existing Quality Harness protocol.
- Auditable outputs suitable for the later P0-T09 gate decision.

### 3.2 Out of scope

- Product UI, product API, automatic installation, or automatic updates.
- Mirroring all raw comments or all packages into Git.
- Treating Xiaping rankings, security labels, or reviewer comments as verified quality.
- Reading regression task bodies during tuning or release-holdout bodies before final gate authorization.
- Executing third-party scripts, granting network access, or allowing a Skill to own Harness state transitions.
- Shipping copied third-party prompts, templates, cases, or reference databases as first-party assets.
- Expanding into adaptation, media generation, distribution, or platform operations.

## 4. Architecture

The implementation extends the existing local quality-evaluation tooling instead of adding a product subsystem.

```text
Xiaping public GET endpoints
  -> resumable collector and local cache
  -> normalized snapshot
  -> writing recall and Capability classifier
  -> exact/near-duplicate clustering
  -> evidence and anomaly analysis
  -> dual-lane Capability shortlist
  -> temporary package retrieval and static audit
  -> tuning smoke evaluation
  -> paired blind evaluation
  -> regression confirmation
  -> minimum effective Skill portfolio
```

### 4.1 `internal/quality/skilldiscovery`

A private Go package owns deterministic collection, normalization, classification inputs, evidence calculations, clustering records, shortlist construction, and validation. Its public API exposes stable request/result types to the CLI but does not expose transport or storage internals to product packages.

The package is split by responsibility rather than placed into one large file:

- `collector`: public HTTP GET pagination, checkpointing, cache validation, rate control, and source receipts.
- `normalize`: versioned conversion from upstream payloads to project-owned records.
- `capability`: stable-core matching, broad writing lexicon, and proposed-Capability evidence.
- `cluster`: exact IDs, package hashes, normalized text fingerprints, and explainable near-duplicate groups.
- `evidence`: percentiles, review confidence, reviewer/comment de-duplication, anomaly flags, and evidence tiers.
- `shortlist`: data-rich and exploration-lane selection with author and method diversity.
- `validate`: schema, closed-set, provenance, split, and forbidden-content checks.

### 4.2 `cmd/quality-eval`

The existing local evaluation command gains `skills` subcommands rather than a second CLI:

- `skills snapshot-xiaping`: collect or resume a normalized public snapshot.
- `skills classify-xiaping`: create the writing candidate pool and Capability proposals.
- `skills rank-xiaping`: calculate evidence vectors, clusters, anomalies, and the dual-lane shortlist.
- `skills validate-xiaping`: validate committed artifacts without network access.
- `skills prepare-audit`: retrieve only an approved shortlist into an owned temporary directory and produce static-audit inputs.

Commands emit machine-readable results to files and concise status/IDs to standard output. Diagnostic detail goes to standard error. No command imposes a fixed overall LLM or crawl timeout; cancellation remains explicit, and network pacing is configurable.

### 4.3 Existing evaluator reuse

Candidate generation and blind packaging continue to use `internal/quality/evaluation` and its existing run layout. Skill discovery does not create a second scoring or review system. A Skill arm records its source ID, version, package hash, selected capability, injected file hashes, and bounded context size in the private run provenance while blind packages remove all source labels.

## 5. Data Sources and Collection Policy

### 5.1 Allowed access

Collection uses public, unauthenticated HTTPS `GET` endpoints only:

- catalog search/list pages;
- Skill details;
- public comments;
- public agent documents;
- ephemeral package links exposed by public agent documents, and only for shortlisted candidates.

The collector never sends reviews, downloads through authenticated APIs, logs in, supplies cookies, uses an API key, or calls mutation endpoints. Ephemeral signed URLs are held in memory only and must not appear in logs, receipts, fixtures, errors, or committed artifacts.

### 5.2 Snapshot behavior

The upstream total is dynamic and must never be hard-coded. Each run records:

- capture start and finish timestamps;
- endpoint template and query plan;
- reported total, pages attempted, pages completed, and unique IDs;
- status and SHA-256 for every page;
- normalization version;
- previous snapshot identity and detected additions, removals, and version changes;
- partial/failure state with the exact failed page and retry disposition.

Every completed page produces a durable checkpoint. A resumed run reuses a cached page only when the receipt and content hash both validate. `429` responses honor `Retry-After`; otherwise the collector applies configurable capped backoff with jitter. Concurrency and minimum request interval are explicit configuration, with conservative defaults.

### 5.3 Local-only evidence

Raw payloads, comments, agent documents, and packages live below an OS user-cache directory owned by the evaluation tool. Package extraction uses a unique run directory plus an ownership marker, existing archive/path limits, and safe-path checks. Cleanup targets only the resolved owned run directory.

The cache is replaceable evidence, not formal project content. A new snapshot can reproduce current-source aggregates, but it cannot recreate historical comment bodies after upstream changes. If the exact locally hashed evidence for a historical aggregate is missing, that aggregate remains a dated receipt but is marked `EVIDENCE-CACHE-MISSING` and cannot support a new recommendation until refreshed.

## 6. Writing Recall and Capability Discovery

### 6.1 Broad writing lexicon

Recall is a union over the complete writing lifecycle, including:

- premise, ideation, genre, audience, theme, and platform-aware positioning;
- worldbuilding, setting, rules, research, and factual constraints;
- character, relationship, motivation, arc, voice, and choice;
- conflict, plot, structure, outline, chapter plan, scene, pacing, suspense, hook, payoff, foreshadowing, reversal, climax, and ending;
- prose, narration, dialogue, description, emotion, humor, style, naturalness, and anti-cliche work;
- drafting, continuation, consistency, continuity, fact tracking, editing, review, diagnosis, revision, and final quality checks;
- long serial, Fanqie short, Zhihu Salt short, and their common Chinese aliases.

Search terms are versioned data rather than code constants. Recall uses upstream query results plus deterministic matching over the normalized full snapshot, so a candidate need not contain the exact word used in the API query.

### 6.2 Stable and proposed Capabilities

The current sixteen Capability IDs remain a closed stable set for Harness routing. Discovery may emit a proposal when all of these are true:

- the behavior is directly related to writing or editorial quality;
- the stable set cannot describe it without distorting an existing Capability;
- at least two non-duplicate Skills or one Skill plus independent project requirements demonstrate the behavior;
- the proposal has a clear input, output, lifecycle stage, minimum permission, and evaluation method;
- it can remain optional and does not take ownership of Harness state.

A proposal never becomes routable merely because it appears in a snapshot. Promotion requires an explicit contract change, traceability update, tests, and approval.

### 6.3 Classification certainty

Every mapping stores `MATCHED`, `AMBIGUOUS`, or `NOT-MATCHED` plus field-level evidence. Deterministic lexical rules maximize recall; model assistance may review ambiguous records but must emit structured evidence and cannot silently change the stable Capability set. Human corrections are versioned overrides with reasons, not edits to upstream data.

## 7. Duplicate and Manipulation Controls

Popularity and review totals are counted only after explainable de-duplication.

- The same upstream ID across queries is one record.
- Versions of one Skill remain one lineage with version-specific hashes.
- Exact package/content hashes create an exact-duplicate group.
- Highly similar normalized instructions, metadata, file manifests, or database samples create a near-duplicate proposal for review.
- Same-author variants are not automatically duplicates, but they cannot fill every diversity slot in one Capability shortlist.
- Identical or near-identical comments are counted once for substantive-comment evidence.
- Reviewer IDs are pseudonymized before aggregation; owner self-reviews, concentrated reviewer groups, rating/comment count mismatches, and suspicious time bursts become anomaly flags.

Near-duplicate detection never deletes a candidate. It creates a cluster with a selected representative and a written reason, while preserving every member for audit.

## 8. Evidence Model

Evidence is stored as a vector. A composite ordering may be derived for convenience, but no opaque total is persisted as a quality score.

### 8.1 Platform evidence

- downloads and within-Capability percentile;
- rating count, average rating, and a reproducible Bayesian-adjusted mean using the Capability pool mean and its median nonzero effective-review count as the snapshot-frozen prior;
- unique and effective reviewer counts after de-duplication;
- substantive comment count, time distribution, and anomaly flags;
- official/trial and platform security states as upstream claims;
- version count, age, update span, and current-version freshness.

`PLATFORM-DATA-RICH` is a descriptive priority tier when a candidate has at least 50 downloads, is at or above the 75th download percentile in its Capability pool, has at least 10 effective raters, has at least 5 substantive de-duplicated comments, and has no unresolved severe manipulation flag. Failure to meet this tier never causes rejection.

A substantive comment contains at least one Skill-specific observation about inputs, outputs, behavior, failure, comparison, or a concrete writing result after boilerplate and near-duplicate text are removed. Generic praise, copied templates, owner self-reviews, and rating-only records do not count. When individual review stars are unavailable, the implementation reports the aggregate limitation and never fabricates a Wilson interval or rating distribution.

### 8.2 Content and method evidence

- required files are present and internally consistent;
- declared scripts, references, templates, and databases actually exist;
- method steps have usable inputs, outputs, and checks;
- examples or database entries are sufficiently varied rather than mass duplicates;
- claimed counts such as "210+ rules" or "100 writers" are verified against package contents before being recorded as facts;
- citations, provenance, contradictions, hidden text, missing dependencies, unsafe operations, and context-size costs are explicit findings.

Public reference databases may inform evaluation hypotheses and internal method analysis. Entry count alone is not evidence of correctness, originality, coverage, or writing uplift.

### 8.3 Risk evidence

Risk remains separate from quality:

- source and content-hash stability;
- license/rights state;
- file, network, shell, environment, credential, and external-tool requirements;
- overwrite, rename, Git, or destructive behavior;
- platform binding and missing dependencies;
- static security findings and upstream security claims.

Unknown license, trial status, low version count, or a static finding does not block internal evaluation. Direct rejection is reserved for irrelevance, empty/deceptive contents, an exact duplicate with no distinct value, credential theft, malicious behavior, or destructive behavior that cannot be isolated.

## 9. Wide-Entry Dual-Lane Shortlist

For each stable or proposed Capability, the shortlist aims for up to five non-duplicate candidates:

- **Data-rich lane:** up to three candidates with the strongest independent platform and content evidence, drawn from different authors and method clusters where available.
- **Exploration lane:** up to two candidates with lower platform volume but distinctive methods, unusually complete reference material, strong static design, or clear coverage of a neglected Profile.

Approximately 20–30% of the total shortlist remains exploratory. Download count, rating count, official status, license state, or version count affects priority and labeling but is not an entry filter. When a Capability has fewer credible candidates, the output records the shortfall instead of filling slots with unrelated items.

A broad, multi-stage Skill is evaluated per Capability. Breadth does not allow it to outrank a focused candidate without capability-specific evidence.

## 10. Evaluation and Decision Funnel

### 10.1 Static qualification

The first pass confirms applicability, package completeness, permissions, context cost, and executable surfaces without running unknown code. Static qualification is not a writing-quality result.

### 10.2 Tuning smoke evaluation

Each shortlisted candidate runs on one or two tuning tasks relevant to its claimed Capability. The two paired arms are:

- the existing no-Skill single-turn baseline;
- the same model, parameters, allowed inputs, and cost boundary with the bounded Skill content supplied.

Candidates that are clearly irrelevant, consistently fail, exceed the context/cost boundary, or materially degrade the target behavior do not proceed. A low-data exploratory candidate receives the same opportunity as a data-rich candidate.

### 10.3 Paired blind evaluation

Promising candidates run across the relevant tuning strata. Each run records model/provider, parameters, cost, source/version/package hash, injected file hashes, bounded context size, output hash, and failure type. Writer and reviewer contexts are fresh and isolated. Blind packages remove Skill, author, arm, filename, and reasoning labels.

At least two reviewers independently record pairwise preference and evidence. Disagreement follows the existing adjudication protocol. Metrics include win/loss/tie, bootstrap uncertainty, fact errors, instruction adherence, first-pass usability, author edit effort, latency/cost, and failure rate. Model-generated scores remain diagnostics only.

### 10.4 Regression and portfolio confirmation

Only individually promising Skills enter regression. Regression task bodies remain unread during discovery and tuning. `release_holdout` remains inaccessible until the final gate authorizes it.

Combinations are tested only after individual wins. The portfolio retains a Skill only when its incremental benefit survives comparison with the smaller combination. Conflicting instructions, duplicated context, higher edit effort, or disproportionate cost remove it even when its individual arm performed well.

The terminal objective is the smallest effective portfolio, including the valid possibility that no external Skill is better than the baseline for a Capability.

## 11. Status Model

The following statuses describe different claims and must not be collapsed:

- `DISCOVERED`: normalized public metadata exists.
- `WRITING-RELEVANT`: evidence maps the record to a stable or proposed Capability.
- `PLATFORM-DATA-RICH`: the descriptive public-data tier is satisfied.
- `STATIC-QUALIFIED`: bounded static applicability and risk review is complete.
- `EVAL-PROMISING`: tuning smoke/paired results justify regression.
- `EVAL-CONFIRMED`: blind evaluation and regression show stable benefit under the frozen gate.
- `REFERENCE-ONLY`: method/database evidence is useful, but direct installation is unsuitable or unnecessary.
- `REJECTED`: the record is irrelevant, deceptive/empty, redundant without distinct value, malicious, uncontrollably destructive, or empirically harmful.

Rights, source, security, evaluation, and recommendation remain independent status fields. Only `EVAL-CONFIRMED` candidates may be proposed for integration, and integration still requires an acceptable source/permission strategy.

## 12. Persistence and Repository Artifacts

Raw caches are local-only. The implementation plan should produce versioned, schema-validated committed artifacts under `docs/project-design/implementation/skills/discovery/`:

- `xiaping-snapshot-manifest-v1.json`: source, query plan, page receipts/hashes, counts, failures, and snapshot lineage.
- `xiaping-discovery-v1.schema.json`: shared JSON Schema definitions and artifact-specific roots for every committed discovery JSON file.
- `xiaping-writing-candidates-v1.json`: normalized writing-relevant candidate metadata and source hashes without raw comments or package bodies.
- `xiaping-capability-lexicon-v1.json`: broad query/matching lexicon and its version.
- `xiaping-capability-proposals-v1.json`: evidence-backed extension proposals; an empty valid array is allowed.
- `xiaping-duplicate-clusters-v1.json`: exact and near-duplicate groups with representative reasons.
- `xiaping-evidence-shortlist-v1.json`: evidence vectors, anomaly flags, lanes, and Capability slots.
- `XIAPING_EVIDENCE_REPORT.md`: bilingual human-readable methodology, counts, gaps, limitations, and candidate summary.

The existing `xiaping-catalog-v1.json` remains the deep-audit catalog. It is updated only for shortlisted candidates whose current packages were retrieved, hashed, and statically reviewed. It is not expanded with thousands of shallow records.

Committed candidate records may retain public author display names because they are required for source attribution and diversity checks. Reviewer identifiers and raw review bodies are excluded; only aggregate counts, de-duplication statistics, anomaly labels, and hashes of locally held evidence are committed.

## 13. Error Handling and Observability

- Every network error reports endpoint type, page or Skill ID, attempt disposition, and checkpoint state without exposing query secrets or signed URLs.
- Partial snapshots are marked `PARTIAL` and cannot silently produce a complete shortlist.
- Missing or malformed upstream fields remain explicit `UNKNOWN` values with validation findings.
- A source version or content-hash change invalidates dependent static findings and evaluation statuses until refreshed.
- Collection logs include run ID, source record, cache hit/miss, page count, normalized count, and retry category.
- Classification logs include deterministic rule/version and the reason for ambiguous mappings.
- Ranking logs include metric inputs, percentile population, exclusions caused by duplicate clusters, and lane selection reasons.
- Evaluation failures distinguish environment, provider, content, validation, and Skill-induced errors.

## 14. Testing Strategy

### 14.1 Unit tests

- upstream payload normalization and unknown fields;
- stable page hashing and canonical snapshot identity;
- pagination, duplicate IDs, and resume checkpoints;
- query/metadata recall and field-level mapping evidence;
- stable versus proposed Capability boundaries;
- exact and near-duplicate cluster explanations;
- download percentiles, the frozen-prior Bayesian adjustment, missing individual-rating behavior, comment de-duplication, and anomaly flags;
- data-rich and exploration-lane quotas, author diversity, and sparse-pool behavior;
- content/hash invalidation and independent status transitions.

No individual unit test may run longer than one second.

### 14.2 HTTP integration tests

A local test server covers success pagination, malformed JSON, `429` with `Retry-After`, transient `5xx`, interrupted runs, resume without duplicate requests, changed page contents, and missing pages. Tests do not depend on the live Xiaping service.

### 14.3 Artifact and security tests

- all committed JSON validates against its schema and deterministic regeneration is diff-free;
- every source hash uses lowercase SHA-256 and every candidate ID is unique;
- signed URLs, credentials, raw comments, package files, and third-party prompt/database bodies are absent from Git;
- archive traversal, symlink, file-count, size, binary, secret, and hidden-character checks reuse existing Skills safety boundaries;
- only an owned, resolved local cache/run directory can be cleaned.

### 14.4 Evaluation isolation tests

- tuning discovery never reads regression or release-holdout task bodies;
- regression starts only from approved `EVAL-PROMISING` candidates;
- blind packages contain no arm, author, Skill, filename, or reasoning leakage;
- baseline and Skill arms use identical allowed inputs and model configuration;
- missing arms or reviews are reported, never fabricated;
- portfolio evaluation records every included Skill hash and incremental comparison.

### 14.5 Repository gates

The implementation must run focused package tests, existing Skills security tests, full `go test ./...`, `go vet ./...`, project build, JSON/schema validation, forbidden-content scans, and `git diff --check`. Live-source tests are separate read-only evidence runs and are never required for deterministic CI.

## 15. Acceptance Criteria

The increment is complete only when:

1. A public snapshot can finish or resume with an auditable manifest and no silent page loss.
2. Broad writing recall is reproducible and explains why each candidate is included or excluded.
3. The stable sixteen Capabilities remain unchanged unless a separate proposal is approved.
4. Every proposed Capability has the required independent evidence and evaluation definition.
5. Duplicate clusters and manipulation indicators are visible rather than deleted or hidden.
6. Every populated Capability shortlist contains both evidence-rich and exploratory representation where the source pool permits it.
7. Shortlisting never claims quality; only blind evaluation can produce `EVAL-PROMISING` or `EVAL-CONFIRMED`.
8. Raw comments, signed URLs, third-party packages, and third-party reference bodies are absent from committed files.
9. Regression and release-holdout isolation is machine-verified.
10. The resulting report states coverage gaps, upstream limitations, evaluation uncertainty, and the valid no-external-Skill outcome.

## 16. Rollback

The discovery package, CLI subcommands, schemas, and committed discovery artifacts are additive and isolated from product runtime. Rollback removes those additions and their changelog entries without touching user workspaces, the completed P0-T08 catalog, evaluation corpus inputs, or locally owned evidence outside the repository. Local cache cleanup remains an explicit, exact-target operation rather than an automatic repository rollback side effect.
