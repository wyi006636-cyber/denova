# P0-T01 Source and Dependency Matrix

This matrix prevents planning inputs from becoming a second engineering or product truth. It binds implementation decisions to the current Denova repository and the final consolidated solution without adding a dependency.

## Evidence classes and priority

| Class | Meaning | Authority |
|---|---|---|
| `source fact` | Directly verified in the current repository, Git state, dependency manifests, or tool output. | Authoritative for what exists now. |
| `final solution decision` | A product or architecture decision in `docs/project-design/final/小说写作工具-最终融合最优方案.md` v1.1. | Highest priority for what this project will build. |
| `source recommendation` | An argument, method, research observation, or external design option. | May inform an ADR or evaluation, but cannot override the final solution or current source facts. |

Conflict order: **final solution decision → current Denova source fact → source recommendation**. A recommendation that conflicts with the final solution is rejected, even if it appears in multiple reports.

## Source, license, and provenance

| Input | Class | Provenance captured in this repository | License / reuse boundary | P0-T01 decision |
|---|---|---|---|---|
| Current Denova repository | `source fact` | `D:/vibe/harness novel`, original source HEAD `91c6e509...`, foundation start HEAD `6548ab94...`, official upstream baseline `eb5e4ee...` | Repository `LICENSE` is Apache-2.0. Existing source is the sole engineering base. | Reuse and extend Denova in place; no second backend, runtime, event transport, editor stack, or version system. |
| Final consolidated solution v1.1 | `final solution decision` | `docs/project-design/final/小说写作工具-最终融合最优方案.md`, dated 2026-07-21 | Internal decision document, not a distributable software dependency. | Governs all implementation choices: modular monolith, file truth, Quality Harness, Profile separation, author finalization, SSE reuse, capability-based Skills, deferred Tauri/vector/worker work. |
| Kimi design and research set | `source recommendation` | Imported reports under `docs/project-design/sources/kimi/`, dated 2026-07-20, with their stated project links and captured evidence | The report prose has no independent software license grant recorded. Referenced projects retain their own licenses. | Use arguments about file-first data, context boundaries, source audits, and upstream sync. Reject parallel `.story-system` truth, fixed literary gates, and premature vector infrastructure. |
| WorkBuddy PRD, architecture, diagrams, and review | `source recommendation` | Imported deliverables under `docs/project-design/sources/workbuddy/` | No independent software license grant is asserted for the deliverable prose. It is planning input only. | Use user stories, acceptance structure, risk tracking, page information architecture, and web-fiction capability categories. Reject frontend/backend rewrites, WebSocket replacement, process-as-sandbox claims, and automatic Git finalization. |
| Competition/comparative research | `source recommendation` | Comparative sections and evidence preserved in the Kimi and WorkBuddy source sets | Product descriptions and screenshots remain attributable to their original sources; no code or asset reuse is authorized by the comparison itself. | Use only as an argument or evaluation prompt. It is not a second product-fact source and does not define contracts. |
| `webnovel-writer` | `source recommendation` | Referenced repository `https://github.com/lingfengQAQ/webnovel-writer` and local Kimi method report | GPL-3.0 per the final solution and imported report. | Clean-room methodology only: file truth, precise reads, thin/self-healing workflow, author finalization, writer/reviewer separation. Do not copy GPL code, templates, or prompts into Apache-2.0 Denova. |
| NovelForge Agent | `source recommendation` | Referenced repository `https://github.com/LvPengfei1/novelforge-agent` and local Kimi report | Apache-2.0 per the final solution and imported report. | Use arguments for source whitelists, hashes, task packets, fresh writer/reviewer contexts, and honest sandbox terminology. No runtime or package dependency is added. |
| Xiaping novel Skills | `source recommendation` | `https://xiaping.coze.com/?search=小说`, local research report, and captured page/API evidence dated 2026-07-20 | License and redistribution terms are package-specific and may be absent. Public download does not imply permission to redistribute. | First-priority external capability source for later tasks. Prefer original-source install with source URL, version/date, content hash, permissions, evaluation, update comparison, and rollback. P0-T01 installs or embeds nothing. |

Kimi, WorkBuddy, competition research, `webnovel-writer`, NovelForge, and Xiaping can supply arguments. None can override the final consolidated solution, establish a second fact source, or authorize copying material whose license/provenance has not been verified.

## Existing dependency reuse

All versions below are source facts from `go.mod`, `web/package.json`, and lockfile version `9.0`. The frontend “resolved” values are the direct versions selected by `web/pnpm-lock.yaml` at this snapshot.

### Backend

| Current dependency | Verified version | Existing responsibility | Decision |
|---|---:|---|---|
| Go | `1.26.5` | Backend language/toolchain contract | Reuse; Goal-local external provision only, with no repository or permanent PATH change. |
| `github.com/cloudwego/hertz` | `v0.10.5` | HTTP server and routing | Reuse; add only thin `/api/quality/*` handlers in later approved tasks. |
| `github.com/cloudwego/eino` | `v0.9.9` | Agent/model orchestration | Reuse through adapters; do not build a second Agent Runtime. |
| `github.com/cloudwego/eino-ext/adk/backend/local` | `v0.2.6` | Local ADK runner backend | Reuse for context isolation; do not equate a child process with a security sandbox. |
| `github.com/cloudwego/eino-ext/components/model/openai` | `v0.1.13` | OpenAI-compatible model integration | Reuse current provider/model configuration. |
| `github.com/openai/openai-go/v3` | `v3.41.0` | OpenAI-compatible API client | Reuse existing model/image seams; never log API keys. |
| `github.com/go-git/go-git/v5` | `v5.19.1` | Local workspace version history, diff, and restore | Reuse; future finalization composes with it but does not replace it. |
| `github.com/pelletier/go-toml/v2` | `v2.4.0` | Configuration parsing | Reuse existing user/workspace/runtime scope boundaries. |
| `github.com/sergi/go-diff` | `v1.4.0` | Text differences | Reuse existing review/version paths. |

### Frontend

| Current dependency | Resolved version | Existing responsibility | Decision |
|---|---:|---|---|
| React / React DOM | `19.2.6` | Frontend application and component runtime | Reuse; no replacement UI framework. |
| TipTap packages | `3.23.2` | Markdown-rich editing | Reuse; no custom editor implementation. |
| `shadcn` / `radix-ui` | `4.11.1` / `1.4.3` | Shared accessible UI primitives | Reuse before creating new primitives. |
| `@tanstack/react-query` | `5.100.10` | Server-state queries and mutations | Reuse for Quality API clients and views. |
| `zustand` | `5.0.13` | Local UI/workspace navigation state | Reuse; do not store server truth in the workspace store. |
| `ai` / `@ai-sdk/react` | `7.0.17` / `4.0.18` | Current AI UI stream protocol | Reuse with existing SSE; do not introduce WebSocket without separate evidence and approval. |
| `i18next` / `react-i18next` | `26.3.1` / `17.0.8` | Chinese/English localization | Extend the single existing translation system. |
| `react-resizable-panels` | `4.11.1` | Adaptive/resizable workbench layout | Reuse for layout; do not create a parallel panel system. |
| `lucide-react` | `1.16.0` | Shared icon set | Reuse; no second general icon library. |
| TypeScript / Vite / Vitest | `6.0.3` / `8.0.13` / `4.1.6` | Type checking, build, and frontend tests | Reuse current build/test chain. |

## Current seam decisions

| Seam | Source fact | Final solution decision | Boundary for later tasks |
|---|---|---|---|
| Task and SSE | `internal/app/task.go`, `internal/api/sse/task.go`, and `internal/api/agentui/stream.go` provide background execution, snapshot/live subscription, and AI SDK stream encoding. | Keep SSE; event transport is not Harness state truth. | Reuse transport. Persist Quality Run state separately and send bounded IDs/status summaries. |
| Session and model context | `internal/session/` separates display events, context messages, clear markers, and effective messages; `internal/agent/context/` has source/purpose/placement/limit metadata. | Context must be source-bound, purpose-bound, hashed, and bounded; writer/reviewer contexts are isolated. | Extend via a Quality context-pack adapter. Do not inject display history, thinking, logs, all Skills, or full workspaces by default. |
| Workspace changes | `internal/workspacechange/` has revision checks, leases, durable operations, atomic file visibility, review, undo, and redo. The multi-path operation is currently private. | Author Finalization is the only Harness formal-write boundary. | Reuse existing service and later expose a named recoverable batch through an approved ADR; do not loop over direct file writes. |
| Versions | `internal/book/versions/` uses go-git for file collection, create, diff, restore, and protected exclusions. | Formal files stay versioned; run/cache/index data must be precisely excluded. | Extend filters only after Workspace Schema ADR and full include/exclude tests. Do not rewrite the git store. |
| Workspace path | `internal/workspacepath/workspacepath.go` chooses `.denova`/legacy `.nova` targets based on real data. | Existing physical paths evolve through a versioned schema without forced destructive renames. | Reuse path semantics and add migrations only with preview, backup, atomic switch, and rollback. |
| Routes and runtime assembly | `internal/api/routes.go` and `internal/app/runtime_builder.go` are the current registration/assembly seams. | Denova remains a modular monolith. | Later add thin quality routes and workspace-scoped assembly only; keep domain/state-machine logic out. |
| Agent runtime | `internal/agent/builder.go`, `runner.go`, and `chat.go` build and run current Eino agents; the builder/chat files are already large. | Reuse model/provider/tool/Skill runtime; logical roles do not require separate OS processes. | Isolate Quality adapters and fresh message construction; do not enlarge existing high-risk files with Harness state. |
| Skills | `internal/skills/` already handles scope/catalog, safe archive preview/install, remote URLs, and GitHub sources. | Harness depends on Capability IDs, not Skill names; Xiaping is a governed source. | Extend provenance/hash/license/permission/evaluation/update/rollback in focused files or packages; do not replace the installer. |
| Automation | `internal/automation/` and `internal/app/automation_app_service.go` provide catalogs, schedules, triggers, runs, and Inbox behavior. | Automation can create pending artifacts but cannot finalize formal content. | Reuse triggers; isolate Harness state and enforce author confirmation above existing `auto_write`. |
| Frontend workspace/navigation | `workspace-store.ts`, `ModeRouter.tsx`, and `WorkbenchShell.tsx` separate visible mode/content mode and shared primary activities; the two workbench components exceed 800 lines. | Shared menus must never switch Writing/Game content mode, and exactly one primary item is active. | Add only thin feature boundaries with regression coverage; keep Quality state in `features/quality`. |
| Editor, review, and versions UI | TipTap editor, draft persistence, Change Review, document comments, and Version Panel already exist. | Reuse mature components and keep AI suggestions distinct from formal content. | Compose future candidate/review/finalization UX from these seams; do not duplicate editors, diff viewers, or version controls. |

## Explicit no-add decisions

| Candidate dependency/technology | Current fact | Decision |
|---|---|---|
| SQLite/FTS driver | No SQLite driver exists in `go.mod`. | Do not add in P0-T01. P0-T09 must approve `ADR-PROJECTION-001` with pure-Go/CGO, Windows, release, FTS, and license analysis before Phase 1. |
| Vector database / embedding runtime | No such dependency exists. | Deferred until real FTS/precise-read evaluation proves insufficiency. |
| WebSocket | Existing streaming uses SSE. | Do not add; only reconsider for an evidenced bidirectional real-time need. |
| Independent Agent worker | Current Eino runtime already supports fresh contexts and subagent workflows. | Do not add before evidence that context isolation is insufficient for a defined fault/resource boundary. |
| Tauri/Rust | No Tauri/Rust project dependency exists. | Deferred until Phase 3 quality gate passes and `ADR-TAURI-001` is accepted. |
| Third-party Skill code | P0-T01 only records source recommendations. | Do not vendor, execute, or redistribute. Later original-source installation requires source/hash/license/permission and rollback evidence. |

P0-T01 dependency delta: **zero**. `go.mod`, `go.sum`, `web/package.json`, and `web/pnpm-lock.yaml` remain unchanged.
