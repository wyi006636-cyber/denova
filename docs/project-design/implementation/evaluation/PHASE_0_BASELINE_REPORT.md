# Phase 0 基线报告 / Phase 0 baseline report

> Date / 日期：2026-07-22
>
> Evidence cutoff / 证据截止：`08c0694a8ac26f4b2a0dce815945c46107a0572a`（本报告随后的纯文档提交除外）
>
> Overall / 总体：`NOT-ENOUGH-DATA / BLOCKED`
>
> Scope / 范围：P0-T09 的事实基线，不是 P0-T09 完成声明；不创建 `quality-gate-v1.json`，不作质量结论。

## 1. 结论 / Conclusion

工程与评测执行机械已有可复核证据，但质量结论没有足够的人类数据。`run-4d815afd6f76bbea0926ac55` 的 12 个 regression 盲包尚无真实独立人工评审（需要 24 份），所以没有冲突、没有第三人裁决、没有可计算的真实评审方差。P0-T09、Phase 0 和 Phase 1 均为 `NOT-ENOUGH-DATA / BLOCKED`。

Engineering and evaluation execution mechanics have auditable evidence, but the quality conclusion lacks human data. The 12 regression blind samples in `run-4d815afd6f76bbea0926ac55` have 0 of 24 required real independent reviews; there are therefore no conflicts, third-human adjudications, or calculable review variance. P0-T09, Phase 0, and Phase 1 are `NOT-ENOUGH-DATA / BLOCKED`.

`ADR-PROJECTION-001` is `Accepted`; it is a Phase 1 decision freeze, not authorization to install a dependency or begin Phase 1. The Windows upstream failure allowlist is exactly `[]`.

## 2. Evidence status / 证据状态

| Area / 项目 | Status / 状态 | Evidence / 证据 |
|---|---|---|
| Smoke cohort / 冒烟批次 | PASS | `run-2ac80556cb00c5aa3ff52f42`: S/H READY 1/1; calls S/H 1/4; blind samples 1 |
| Tuning cohort / 调优批次 | PASS | `run-2f9cce8a71c485df0881cdbb`: S/H READY 18/18; calls S/H 18/72; blind samples 18 |
| Regression cohort / 回归批次 | PASS | `run-4d815afd6f76bbea0926ac55`: S/H READY 12/12; calls S/H 12/48; blind samples 12 |
| Cohort failures and reasoning / 失败与推理 | PASS | All three bounded cohorts: failures 0; reasoning-token activity 0 |
| Release holdout / 发布留出集 | PASS | Calls, outputs, intermediates, and blind-review activity all 0 by split discipline |
| Human independent reviews / 人工独立评审 | NOT-ENOUGH-DATA | Regression: 0 of required 24 real human reviews |
| Third-human adjudications / 第三人裁决 | NOT-ENOUGH-DATA | Unknown until real independent reviews produce conflicts; none exists now |
| Evaluation summary / 评测汇总 | NOT-ENOUGH-DATA | No quality conclusion, threshold, win rate, sample-size rule, fact-error tolerance, edit tolerance, reviewer/candidate rule, or cost tolerance is frozen |
| `quality-gate-v1.json` | NOT-RUN | Absent by design until real review variance is available |
| Legacy blocked run / 历史阻塞运行 | NOT-ENOUGH-DATA | `run-598b2c33eba7f255bd88eaec` remains historical diagnostic only, not quality evidence |
| K/model self-score / K 与模型自评 | NOT-ENOUGH-DATA | Capability experiment and model self-scores are not substitutes for blinded human evidence |

The bounded run index is `p0-harness-run-index-v1.json`. It contains only safe aggregate reproducibility data. The human-review importer exists at `d357a26`; the current leaf-performance implementation is at the evidence cutoff `08c0694`.

## 3. Engineering readiness / 工程就绪度

Engineering readiness is separate from quality. A PASS below means the stated command passed; it does not imply quality PASS or Phase 0 exit.

| Gate / 门禁 | Status / 状态 | Result / 结果 |
|---|---|---|
| Windows repo-local Go 1.26.5 `go test ./... -count=1` | PASS | Full suite passed |
| Windows `go vet ./...` | PASS | Passed |
| Affected eval + CLI JSON audit | PASS | 124 leaves; zero over 1s; maximum 0.82s |
| Web tests | PASS | 125 files / 651 tests |
| Web i18n | PASS | 2987 keys |
| Web production build | PASS | Passed; existing chunk warning only |
| Git Bash `scripts/build.sh` | PASS | Passed |
| Git diff check | PASS | Passed at controller execution |
| Tracked privacy/secret scan | PASS | Passed at controller execution |
| Windows `go mod tidy -diff` | FAIL | Only classification diff: existing P0-T09 Windows ACL import requires `golang.org/x/sys v0.46.0` to move indirect → direct; no version or `go.sum` change. Stop-if prohibits unapproved dependency-file edits. |
| Linux native filesystem archive at exact HEAD: diff, full Go, web test/i18n/build/distribution build | PASS | Docker native filesystem, Go 1.26.5 / Node 22 / pnpm 10 |
| Linux `govulncheck@latest ./...` | FAIL | Reachable `GO-2026-5970` in `golang.org/x/text v0.38.0`: `internal/interactive/actor_state.go:165` `normalizeActorStateFieldName -> norm.Form.String`; fixed in v0.39.0. Upgrade needs explicit authorization. |
| Windows bind-mount Linux-container Vitest attempt | FAIL | Ran on the Windows bind-mounted filesystem: 27 test files / 317 tests passed, then 98 worker-start errors/timeouts prevented the remaining files. This is an environment diagnostic, not a code-assertion failure, and it is not the retained native-Linux gate. |

## 4. Blockers and non-conclusions / 阻塞项与非结论

- `FAIL`: dependency classification in `go mod tidy -diff`; no dependency-file edit is authorized.
- `FAIL`: reachable `GO-2026-5970`; no `golang.org/x/text` upgrade is authorized.
- `NOT-ENOUGH-DATA`: 24 independent human reviews and conflict adjudications are absent, so summary is not `VALID`.
- No quality threshold is invented. There is no win rate, final sample size, fact-error tolerance, author-edit tolerance, reviewer/candidate rule, or cost tolerance.
- A successful runner, model output, K, model self-score, legacy blocked run, or old failed run is not quality evidence.

## 5. Exact unblock sequence / 精确解锁顺序

1. Obtain 24 independent real-human blind reviews through private `quality-eval record-review` for the 12 regression samples.
2. Obtain third real-human adjudications for every conflicting pair.
3. Recompute the regression summary and require `VALID` before deriving review variance.
4. Freeze a pilot-backed `quality-gate-v1.json` and its future sample-size/non-inferiority rules; only then reevaluate Phase 0 exit and Phase 1 entry.
5. Obtain explicit approval to correct the `go.mod` direct/indirect classification and to upgrade `golang.org/x/text` to the reviewed fixed version.
6. Rerun `go mod tidy -diff`, `govulncheck`, and the final Windows/Linux gates after those approved changes.

Until this sequence completes, Phase 1 remains blocked. No product/runtime/API/UI change, live call, private review data, identity, credential, or prose is included in this report.
