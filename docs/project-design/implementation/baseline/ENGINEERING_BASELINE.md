# P0-T01 Engineering Baseline

This snapshot freezes the machine-verifiable Denova engineering baseline used by the Quality Harness implementation plan. It records facts observed before this task's documentation commit; later task commits do not redefine the source baseline.

## Snapshot identity

| Field | Value |
|---|---|
| Repository / current directory | `D:/vibe/harness novel` |
| Branch | `feat/quality-harness-foundation` |
| Original source HEAD | `91c6e509a6beea98e8d025777c97b34b2bc6ac9f` |
| Foundation start HEAD after the planning checkpoint | `6548ab94a26c6aa992192065b8a93903ab38e350` |
| Upstream baseline | `upstream/master@eb5e4ee53ad158fe88dfb7148408edc6558e481a` |
| Merge base at task start | `eb5e4ee53ad158fe88dfb7148408edc6558e481a` |
| Tracking branch | `origin/feat/quality-harness-foundation` |
| Manifest binding | Source bytes present at foundation start HEAD `6548ab94...`; the planning checkpoint changed only `CHANGELOG.md` and six planning documents, with no Go, TypeScript, TSX, or CSS source difference from `91c6e509...` |

The task-start command `git status --short --untracked-files=all` produced no output. The complete task-start worktree state was therefore clean: no staged, unstaged, or untracked paths.

## Remote evidence

Direct remote query time: `2026-07-20T19:01:22.073Z` (UTC).

| Remote/ref | URL | Direct SHA |
|---|---|---|
| `origin` / `refs/heads/feat/quality-harness-foundation` | `https://github.com/wyi006636-cyber/denova.git` | `91c6e509a6beea98e8d025777c97b34b2bc6ac9f` |
| `origin` / `refs/heads/master` | `https://github.com/wyi006636-cyber/denova.git` | `eb5e4ee53ad158fe88dfb7148408edc6558e481a` |
| `upstream` / `refs/heads/master` | `https://github.com/alfredxw/denova.git` | `eb5e4ee53ad158fe88dfb7148408edc6558e481a` |

These are direct `git ls-remote` results, not local remote-tracking assumptions. At query time, the local foundation branch was one planning commit ahead of the direct origin feature ref.

## Toolchain evidence

| Tool | Verified result | Baseline meaning |
|---|---|---|
| Go on normal `PATH` | `ENV-GO-MISSING` | The initial environment fact remains recorded as an environment condition, not a test failure. |
| Goal-local external Go | `go version go1.26.5 windows/amd64` | Provisioned outside the repository for this Goal; no permanent `PATH` change was made. |
| Official Go Windows ZIP | `go1.26.5.windows-amd64.zip`, SHA-256 `97e6b2a833b6d89f9ff17d25419ac0a7e3b482a044e9ab18cdef834bd834fd38` | Rechecked against official go.dev release metadata. The absolute external tool location is intentionally not committed. |
| Node.js | `v24.16.0` | Local baseline. |
| pnpm | `11.12.0` | Local baseline. |
| Python | `3.11.9` | Local validation runtime. |
| Python `jsonschema` | `4.26.0` | Local JSON Schema validator baseline. |
| Git | `2.52.0.windows.1` | Local Git baseline. |
| Git Bash | `5.2.37(1)-release`, `x86_64-pc-msys` | Git for Windows Bash baseline. |

Repository manifests remain authoritative for project requirements: `go.mod` declares Go `1.26.5`, while `web/package.json` and `web/pnpm-lock.yaml` define the frontend dependency graph. This task changed neither manifest nor lockfile.

## Navigation baseline

- `.codegraph/` was absent at task start and remains absent. CodeGraph was therefore skipped under the repository navigation policy; it was not initialized or retried.
- Source inspection used repository files, `rg`, Git, and the project-local planning documents.
- graphify was not requested and was not used. A graphify graph would not substitute for a missing CodeGraph index.

## Baseline test evidence

| Command / check | Result | Evidence and classification |
|---|---|---|
| `pnpm --dir web test -- --reporter=dot` | PASS | Fresh run: 124 test files and 645 tests passed. Existing React `act(...)`, jsdom `Window.scrollTo`, and dialog-description messages were non-failing test-environment warnings. |
| Goal-local Go `test ./...` | FAIL | Fresh Windows run reproduced `TestListAutomationsToolUsesTheUserCatalogAcrossWorkspaces`: synchronization of a temporary `.denova/automations` directory returned `Access is denied`. This is unresolved observed baseline evidence. |
| Targeted reproduction of the named Go test | FAIL | `denova/internal/agent`, `0.02s` test duration; same directory-sync permission signature. |

The Go failure is **not** an allowlisted upstream failure. It has not met the required same-machine, same-command reproduction against a clean `upstream/master@eb5e4ee...` worktree, and this task does not change or waive it.

## Windows upstream failure allowlist

The P0-T01 Windows upstream failure allowlist is explicitly empty:

```json
[]
```

`ENV-GO-MISSING` is an environment record, not an allowlist entry. The observed automation catalog test failure is also not an allowlist entry. Any later entry must include an exact package and test name, stable signature, feature and upstream reproduction SHAs, owner/issue, registration date, and expiry.

## Source and dependency freeze

- `source-path-manifest.json` records 57 real files, using repository-relative forward-slash paths, PowerShell logical line counts, and lowercase SHA-256 over current file bytes.
- `SOURCE_AND_DEPENDENCY_MATRIX.md` separates current source facts, final-solution decisions, and source recommendations.
- Denova is the only engineering base. No dependency, source file, test, configuration, README, or planning document was changed by P0-T01.

## Update rule after upstream synchronization

After every upstream sync, create a new auditable baseline update instead of selectively editing old values:

1. Re-run the exact Git root, branch, HEAD, upstream SHA, merge-base, status, remote URL, and direct `ls-remote` checks, recording a new UTC query time.
2. Re-run toolchain discovery and both frontend and Windows Go baseline commands. Classify results as PASS, FAIL, or environment-blocked; never convert NOT-RUN or an unverified failure into PASS/allowlisted state.
3. Regenerate every manifest entry from the synchronized tree. All paths must exist, line counts and byte hashes must match, and responsibilities/decisions must be reviewed against the current source.
4. Review dependency manifests and source/license provenance. A dependency change requires its own approved task and compatibility/license analysis.
5. Commit the regenerated snapshot with a changelog entry. Do not silently overwrite historical evidence or hand-edit hashes to fit changed files.
