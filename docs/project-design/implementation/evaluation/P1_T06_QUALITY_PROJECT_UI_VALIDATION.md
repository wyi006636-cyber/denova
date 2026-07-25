# P1-T06 Quality Project UI Validation

## Scope and continuation

P1-T06 continues exactly from pushed commit
`98669428e52bdee1b1ba6a03e89486ad9a3eeb67` on
`feat/quality-harness-foundation`; its parent is
`bf23a968f3b99f12ac4de12f7a971e45b3defc86`, and the matching remote branch
was at the same P1-T05 commit before implementation. P1-T01 and P1-T05 are both
present in that history. No P1-T01–P1-T05 change was reset, rebased, replayed,
or replaced.

The UI consumes the four frozen read-only/zero-write Quality endpoints. It adds
no backend endpoint, Go production change, Accepted ADR change, normative
schema change, configuration option, dependency, or lockfile change.

## UI and route matrix

| Surface | Stable ID / route owner | Responsibility | Explicit exclusion |
|---|---|---|---|
| Shared primary navigation | `quality` / `WorkbenchShell` | One desktop/mobile entry named “作品质量” / “Project Quality”; active state is mutually exclusive with every other primary item | Does not switch `ide` / `interactive` |
| Thin route integration | `ModeRouter` | Lazy-mounts and shows `QualityProjectView`; preserves existing mounted-route behavior | No Quality query, mutation, or business rendering logic |
| Quality home | `QualityProjectView` | Composes overview, catalog, plan, and preview states | No parallel router, store, component library, or CSS system |
| Project overview | `QualityProjectOverview` | Reads compatibility, active root, marker/schema, and bounded issue codes | No workspace mutation |
| Profile catalog | `QualityProfileCatalog` | Shows exactly three bundled read-only references and switches local explanatory detail only | No install, selection, default, or author-confirmation claim |
| Author-readable plan | `QualityPlan` | Shows goals, purpose, provenance/source, scope, evidence, priority, and progressively disclosed metadata/settings | No raw JSON default view |
| Migration preview | `QualityMigrationPreview` | Sends only the frozen preview request and renders digest, compatibility/issues, files, proposed operations, and conflict summaries | No apply, confirm, commit, run, decision, or finalization action |

`App.tsx`, `ModeRouter.tsx`, and `WorkbenchShell.tsx` remain thin integration
points. Quality state, queries, contract interpretation, and presentation live
under `web/src/features/quality/`. The largest new production file is the
326-line contract guard; no new or modified Quality production file approaches
the 500-line split threshold.

## API and DTO matrix

| Endpoint | Method/body | Frontend function | Guarded result | Authority |
|---|---|---|---|---|
| `/api/quality/profiles` | `GET` | `getQualityProfiles` | `QualityProfileSummaryDTO[]`; empty or exactly the three frozen IDs, `read_only_catalog`, Profile/QualitySpec `v1` | Read-only reference catalog |
| `/api/quality/profiles/:profile_id` | `GET`; encoded path segment | `getQualityProfile` | `QualityProfileDetailDTO`; bounded public Profile and nested QualitySpec | Read-only reference detail, never installed/selected |
| `/api/quality/project` | `GET` | `getQualityProject` | `QualityProjectDTO` | Read-only workspace compatibility |
| `/api/quality/project/migration-preview` | `POST`; this UI sends `{}`; the frozen P1-T05 request contract also permits only bounded optional `offset` / `limit` paging | `previewQualityProjectMigration` | `QualityMigrationPreviewDTO` | Zero-write preview only |

There are five stable top-level DTO families: profile summary, profile detail,
project compatibility, the frozen optional `offset` / `limit`
migration-preview request, and migration-preview response. P1-T06's visible
action always invokes the default request and therefore emits exact body `{}`;
the exported request DTO does not add fields beyond P1-T05's already frozen
bounded paging contract. `APIError.status` and `APIError.code` remain intact. The contract
guard distinguishes unsupported versions from malformed values and never
renders the raw rejected payload. The client module exports only the four
functions above and has no install/select/update, apply, run, decision,
Candidate, Preference, or Finalization client.

## Read-only authority

- Bundled Profiles are labelled “只读参考目录” / “Read-only reference
  catalog” in both the catalog and active detail.
- The page states that a reference is not enabled, installed, or
  author-confirmed for the current work.
- Selecting a catalog row changes only the explanation shown in this page.
- The project overview reads compatibility only and states that folders,
  markers, and creative content are unchanged.
- Preview is labelled “仅预览，未写入” / “Preview only, not written”. There is
  no apply/confirm/commit control or request.
- Technical identifiers, revisions, versions, and hashes are secondary,
  progressively disclosed information; author-readable goals remain primary.

## Mode-preservation evidence

Automated navigation tests cover both entry directions, return behavior, and
desktop/mobile exclusivity. The live ego lite page additionally observed:

| Sequence | `nova:content-mode` | Visible result |
|---|---:|---|
| `ide` → Quality | `ide` | Quality is the only active desktop primary item |
| Quality → return | `ide` | Writing workspace restored |
| Explicit Game Mode action | `interactive` | Game workspace shown |
| `interactive` → Quality | `interactive` | Quality is the only active desktop/mobile primary item |
| Quality → return | `interactive` | Game workspace restored |

The final live page was restored to `ide`, zh-CN, light, 1440×900, with Quality
visible and the single active primary item.

## RED/GREEN record

All RED runs preceded their corresponding implementation. Failures were kept
as behavior evidence rather than removed or converted to skipped tests.

| Slice | RED evidence | GREEN evidence |
|---|---|---|
| API client + contract guard | exit 1: `quality-projects` and `contract-guards` modules did not exist | first GREEN attempt exposed two incorrect `v2` classifications; after guard-order correction, exit 0, 2 files / 16 tests |
| View states + migration preview | exit 1: three suites could not resolve `QualityProjectView` | first GREEN attempt exposed three asynchronous-render assertions; after waiting on public UI state, exit 0, 3 files / 13 tests |
| Shared navigation + mode preservation | exit 1: store rejected `quality`, navigation entry was absent, and App overwrote the return mode (6 failures) | exit 0, 2 files / 12 tests |
| TypeScript/i18n integration | first typecheck exit 2 for two unused imports | unused imports removed; typecheck/build exit 0 and locale-key audit aligned |

No `.skip`, `it.skip`, `describe.skip`, `test.skip`, disabled test, deleted case,
or weakened product assertion was introduced.

## Locale, theme, viewport, and state matrix

Visible validation used the currently running Denova frontend/backend through
an isolated ego lite task space. Normal-workspace checks used the live
read-only API. Controlled states intercepted only the same four Quality paths
inside that isolated page; they did not change the real workspace or start a
second frontend/backend.

| Matrix item | Observation |
|---|---|
| zh-CN / light / 1440×900 | Overview, three Profiles, author-readable QualitySpec, metadata disclosure, compatibility, and preview rendered on a white/black editorial surface; document width equalled viewport width |
| en-US / dark / 1440×900 | “Project Quality”, all navigation/content copy, and read-only labels rendered in English on an RGB `0,0,0` background with restrained status accents; exactly one desktop primary item active |
| zh-CN / light / 390×844 | Adaptive single-column cards, reachable return action, fixed mobile navigation, and exactly one `aria-current="page"` item; document and Quality view widths both equalled 390 |
| en-US / dark / 390×844 | Complete English header/overview/read-only copy, black surface, no raw key, no clipping, and exactly one “Project Quality” mobile active item |
| Long Profile/QualitySpec text | Controlled 390×844 fixture repeated long localized names, walkthrough, goal, purpose, and evidence text; document and Quality widths stayed 390 with natural wrapping |
| Loading | Bounded skeleton and localized status text; never a blank page |
| Empty catalog | Localized empty title and “no automatic fallback” explanation; read-only label remains visible |
| No workspace / 409 | Localized “open a work first” alert; Profile catalog remains readable and preview action is disabled |
| Profile 404 | Localized bounded unavailable-reference alert; no replacement/install fallback |
| Catalog/project 500 | Localized catalog or service alert; internal fixture detail and absolute path were not displayed |
| Network failure | Two bounded service alerts for independent overview reads; no raw transport text |
| Newer contract | Localized unsupported-contract alert; `v2` data was not accepted as v1 |
| Malformed contract | Localized malformed-response alert; raw payload was not displayed |
| legacy-only | `.nova`, `safe_read_open`, and `legacy_only_workspace` rendered as compatibility information |
| split-root | `.denova`, blocked safe-read state, and `split_root_workspace` rendered without mutation |
| Unknown required feature | `unknown_required_feature` rendered as a bounded compatibility issue; no fallback contract was invented |
| Preview error | Exact `POST` body `{}` observed; localized “not written” alert; real project response remained byte-equal before/after |
| Real preview success | Digest, compatibility issue, file summary, proposed operations, and conflict summary rendered; project response remained byte-equal before/after; no apply/confirm/start/finalize button |

The normal real-preview route produced zero new console errors. No raw i18n key
or horizontal page overflow was observed in the matrix.

## Verification gates

| Gate | Result |
|---|---|
| Exact requested targeted Vitest command | exit 0; 6 files / 30 tests |
| JSON reporter audit over all added/modified tests | exit 0; 41/41 passed, failed 0, skipped 0, leaves over 1.0s: 0; slowest leaf about 166ms |
| `PATH=/opt/homebrew/opt/node@22/bin:$PATH pnpm --dir web test` | exit 0; 130 files / 682 tests |
| `PATH=/opt/homebrew/opt/node@22/bin:$PATH pnpm --dir web check:i18n` | exit 0; 3,082 keys aligned |
| `PATH=/opt/homebrew/opt/node@22/bin:$PATH pnpm --dir web build` | exit 0; existing chunk-size warning only; `QualityProjectView` remains a separate lazy chunk |
| Requested targeted Go package command | exit 0 |
| `go test ./... -count=1` | exit 0 |
| `go vet ./...` | exit 0 |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | shell exit 1 (`govulncheck` exit 3); only pre-existing reachable `GO-2026-5970` in `golang.org/x/text v0.38.0`; no new vulnerability ID |
| `./scripts/build.sh` | exit 0 |
| `go mod tidy -diff` | exit 0; no output |
| Protected manifest diff from `98669428e52bdee1b1ba6a03e89486ad9a3eeb67` | exit 0; `go.mod`, `go.sum`, `web/package.json`, and `pnpm-lock.yaml` unchanged |
| `git diff --check` | exit 0 |

## Independent review

The independent whole-diff reviewer was `gpt-5.5` with `xhigh` reasoning
(Minimax M3 was not available in the environment). Final disposition:
Critical 0, Important 0, Minor 0.

The initial review raised one Important question about the exported
`offset` / `limit` preview request. Re-review against the authoritative P1-T05
request struct, strict handler decoder, API tests, and validation record showed
that bounded optional paging was already frozen in P1-T05, while P1-T06's
visible action correctly emits `{}`. The reviewer withdrew the finding after
the UI/default-call distinction was clarified above.

## Deferred boundary

P1-T07, Quality Run UI/runtime, persistent Run repositories, P2 Harness
runtime, Candidate/ReviewIssue/Preference/Finalization UI, formal-content
writes, Profile/QualitySpec install/select/update, migration apply, Agent,
Automation, Projection, Tauri, and vector work remain deferred. This validation
does not claim push, merge, release, product-quality acceptance, or publication
readiness.
