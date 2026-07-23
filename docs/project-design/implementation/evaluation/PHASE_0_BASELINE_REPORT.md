# Phase 0 基线报告 / Phase 0 baseline report

> Date / 日期：2026-07-22
>
> Evidence cutoff / 证据截止：`08c0694a8ac26f4b2a0dce815945c46107a0572a`（本报告随后的纯文档提交除外）
>
> Phase 1 engineering / Phase 1 工程开发：may begin / 可以开始
>
> Quality conclusion / 质量结论：`INCONCLUSIVE`
>
> H v1 product use / H v1 产品使用：not promoted / 不推广
>
> Release readiness / 发布就绪：`NOT-READY`
>
> Scope / 范围：P0-T09 的事实基线，不是 P0-T09 完成声明；不创建 `quality-gate-v1.json`，不作质量结论。

## 1. 结论 / Conclusion

工程合同、ADR、隔离评测执行机械和大部分工程门禁已有可复核证据，因此 P1-T01–P1-T07 可以按各自门禁开始。质量结论仍缺少足够的人类数据：`run-4d815afd6f76bbea0926ac55` 的 12 个 regression 盲包没有正式独立人工评审（0/24），所以没有冲突裁决或可计算的人类评审方差。该缺口禁止质量提升声明和 H v1 产品化，但不再阻止 Phase 1 工程开发。

Engineering contracts, ADRs, isolated evaluation mechanics, and most engineering gates have auditable evidence, so P1-T01–P1-T07 may begin under their own gates. The quality conclusion still lacks human data: the 12 regression blind samples in `run-4d815afd6f76bbea0926ac55` have 0 of 24 formal independent reviews, so there are no adjudications or calculable human-review variance. This gap prohibits a quality-improvement claim and H v1 product promotion, but no longer prevents Phase 1 engineering.

`ADR-PROJECTION-001` is `Accepted`; Phase 1 may consume the decision, while the exact dependency addition still requires its own scoped implementation and verification. The Windows upstream failure allowlist is exactly `[]`.

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
| `quality-gate-v1.json` | NOT-RUN | Absent; deferred until preregistered real-product evidence supports defensible rules |
| Legacy blocked run / 历史阻塞运行 | NOT-ENOUGH-DATA | `run-598b2c33eba7f255bd88eaec` remains historical diagnostic only, not quality evidence |
| K/model advisory evidence / K 与模型诊断证据 | DIAGNOSTIC-ONLY | The private non-human panel was position-sensitive; it informed the H v1 no-promotion decision but is not a quality result or human-review substitute |

The bounded run index is `p0-harness-run-index-v1.json`. It contains only safe aggregate reproducibility data. The human-review importer exists at `d357a26`; the current leaf-performance implementation is at the evidence cutoff `08c0694`.

## 3. Engineering readiness / 工程就绪度

Engineering readiness is separate from quality. A PASS below means only that the stated command passed; it does not imply quality PASS or release readiness. The two recorded failures remain explicit debt even though Phase 1 engineering may begin.

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

## 4. Outstanding engineering and quality facts / 尚未关闭的工程与质量事实

- `FAIL`: dependency classification in `go mod tidy -diff`; no dependency-file edit is authorized.
- `FAIL`: reachable `GO-2026-5970`; no `golang.org/x/text` upgrade is authorized.
- `NOT-ENOUGH-DATA`: formal independent human reviews and conflict adjudications are absent, so the current evaluation summary is not `VALID`; this blocks quality claims, not Phase 1 engineering.
- No quality threshold is invented. There is no win rate, final sample size, fact-error tolerance, author-edit tolerance, reviewer/candidate rule, or cost tolerance.
- A successful runner, model output, K, model self-score, legacy blocked run, or old failed run is not quality evidence.

## 5. Remaining work before quality or release claims / 质量或发布声明前的剩余工作

1. Build and validate P1-T01–P1-T07 under their own scoped engineering gates.
2. Do not productize H v1 or invent quality thresholds while its benefit is unproven.
3. Design new preregistered quality evidence after a real writing vertical slice exists; no fixed P0 reviewer quota is carried forward automatically.
4. Obtain explicit approval to correct the `go.mod` direct/indirect classification and to upgrade `golang.org/x/text` to the reviewed fixed version.
5. Rerun the affected Windows/Linux engineering and security gates before any release-ready declaration.

Phase 1 engineering may begin now. Until the remaining quality and release work completes, no Harness quality improvement or release readiness may be claimed. No product/runtime/API/UI change, private review data, identity, credential, or prose is included in this report.
