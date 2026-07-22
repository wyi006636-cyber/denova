# P0-T07 三 Profile 质量评测协议 / Three-profile quality evaluation protocol

> Contract: `denova.quality-evaluation-protocol/v1`
> Status: P0-T07 offline tooling complete; the committed legacy S arm is `ENVIRONMENT-BLOCKED` and H arm is `NOT-READY`. P0-T09's evaluation-only offline H runner has successful bounded smoke, tuning, and regression cohorts (see §12), but real regression reviews are 0/24 and adjudications are not yet applicable. The quality summary, P0-T09, and Phase 0 remain `NOT-ENOUGH-DATA / BLOCKED`; no `quality-gate-v1.json` or quality PASS exists.
> Date: 2026-07-22
> Scope: baseline and evaluation infrastructure plus the approved P0-T09 runner boundary; no product Harness workflow, runtime integration, P0-T09 success claim, Phase 1 implementation, or quality-gate PASS.

## 1. 目的与非结论 / Purpose and non-conclusion

本协议建立普通单轮生成与未来 Quality Harness 的公平对照。它把任务、输入、模型配置、模板、随机化、评审、冲突裁决、统计和成本记录冻结为可复算合同。P0-T07 的完成只证明评测基础设施和任务集可用，不证明 Harness 已实现，也不证明 Harness 比普通单轮更好。

This protocol pre-registers a fair comparison between ordinary single-turn generation and a future Quality Harness arm. It freezes tasks, allowed inputs, model configuration, template, randomization, review, adjudication, statistics, and cost records. P0-T07 establishes evaluation infrastructure only; it makes no claim that Harness exists or wins.

禁止把以下内容写成质量证据：确定性测试 fixture、模型自评分、流程完成率、Agent 数量、自动化率、总体平均掩盖单 Profile 失败，或当前 `NOT-READY`/`ENVIRONMENT-BLOCKED` 运行。

Deterministic fixtures, model self-scores, process completion, Agent counts, automation rates, aggregate-only results, and the current blocked/not-ready run are not quality evidence.

## 2. Corpus v1

规范清单为 `corpus-manifest-v1.json`。清单严格拒绝未知字段、重复任务 ID、未知 Profile、缺失分层、错误文件 hash、敏感字段和越界路径。所有输入均为本项目自有的合成写作任务简报，不包含私密作品或受版权限制正文。

The normative corpus is `corpus-manifest-v1.json`. Strict validation rejects unknown fields, duplicate task IDs, unknown Profiles, missing strata, incorrect file hashes, sensitive fields, and path traversal. Every committed input is a project-owned synthetic writing brief; no private or copyright-restricted prose is included.

| Profile | Tasks | Genre strata | Split |
|---|---:|---|---|
| `long_serial` | 12 | 都市悬疑、仙侠冒险、职场现实 | tuning 6 / regression 4 / release_holdout 2 |
| `fanqie_short` | 12 | 都市情感、民俗悬疑、现实逆袭 | tuning 6 / regression 4 / release_holdout 2 |
| `zhihu_salt_short` | 12 | 现实婚恋、职场悬疑、家庭伦理 | tuning 6 / regression 4 / release_holdout 2 |

每个 Profile 都覆盖 `opening`、`character_choice` 或 `dialogue`、`structure_turn`、`ending` 或 `continuity`，并包含 `scene`、`chapter` 或 `short_story` 篇幅分层。任务只引用 manifest 声明的 `task_brief`、`quality_spec` 和必要的 `story_state`。

Each Profile covers opening, character choice or dialogue, structural turn, and ending or continuity, with explicit scene/chapter/short-story length strata. A task can use only the input classes declared by its manifest record.

### 2.1 Profile 专项目标 / Profile-specific intent

- `long_serial`：章节功能、前后衔接、人物选择与代价、关系或局势变化、伏笔/连续性和章末追读压力。
- `fanqie_short`：开篇清晰与信息密度、卖点进入速度、冲突进入现场、升级改变处境和结局兑现；不套长篇卷章规则。
- `zhihu_salt_short`：稳定叙事声音、可信因果、持续信息压力、反转前置证据、人物动机和情绪/主题闭环；不把反转当无依据惊吓。

- `long_serial`: chapter function, continuity, character choice and cost, relationship/situation change, setup/payoff, and chapter-end momentum.
- `fanqie_short`: opening clarity, premise velocity, enacted conflict, meaningful escalation, and ending delivery without serial-volume machinery.
- `zhihu_salt_short`: narrative voice, credible causality, information pressure, planted reversal evidence, motive, and emotional/thematic closure.

### 2.2 数据分组纪律 / Split discipline

- `tuning` 可用于开发评测工具和未来调模板。
- `regression` 用于检查已冻结方案的回归。
- `release_holdout` 的全部六个 task 只登记任务和 hash：零模型调用、零输出、零盲包、零评审、零调优，也不在同一批数据上宣称发布通过。
- P0-T09 必须先完成 `tuning` runner/template shakeout，随后只以冻结的 `regression` paired pilot 和人工评审方差冻结未来正式样本量及非劣规则；不宣称 P0-T09、Phase 0 或 Phase 2/3/5 质量 PASS。本文件不创建 Gate PASS。

- `tuning` may support future template or method development.
- `regression` checks a frozen approach for regressions.
- All six `release_holdout` tasks are registered and hashed only: zero model calls, outputs, blind packages, reviews, and tuning; they cannot support a same-cohort release claim.
- P0-T09 must first run a `tuning` runner/template shakeout, then derive future formal sample-size and non-inferiority rules only from a frozen `regression` paired pilot and human-review variance. It does not claim a P0-T09, Phase 0, or Phase 2/3/5 quality PASS.

## 3. 普通单轮 S arm / Ordinary single-turn S arm

冻结模板位于 `runs/templates/single-turn-baseline-v1.md`：

- version: `single-turn-v1`
- SHA-256: `sha256:56a566495b63cea286fd8bb60abeb9a7770422c3c4c4c67f63e86739859c3a54`
- exactly one model call per task
- no tools, reviewer, revision, candidate comparison, Skill output, future H answer, hidden reasoning persistence, or post-result template changes

S arm 获得任务 manifest 允许的输入和该任务 QualitySpec 目标。模板必须在输出生成前保存并校验 hash；结果生成后不得修改旧模板来偏袒 S 或 H。新的模板版本产生新的稳定 Run ID 和独立 cohort。

The S arm receives exactly the manifest-authorized input and task QualitySpec goals. The template is stored and hashed before generation. Post-result edits are forbidden; a new template creates a new stable Run ID and cohort.

### 3.1 冻结模型配置 / Frozen model configuration

当前 corpus 为每项记录相同快照：

- Provider: `deepseek`
- Base URL: `https://api.deepseek.com`
- model profile: `default`
- model: `deepseek-v4-pro`
- temperature: `0.7`
- maximum output boundary: `4096` tokens
- thinking content persistence: disabled
- credential source: runtime only; credentials never enter manifest, run metadata, logs, or blind packages
- model snapshot SHA-256: `sha256:8940c87702e5e928745498105567929bc1a237af0ebbb7dc106a5b70cb249834`

该输出上限是两 arm 的公平参数边界，不是运行超时。CLI 不设置固定模型超时或最大运行时间。

The output limit is a shared comparison boundary, not a runtime timeout. The CLI sets no fixed model timeout or maximum execution duration.

冻结快照中的 `thinking_enabled: false` 是评测传输不变量：对于 DeepSeek V4，S 与 H 的共享评测生成器必须发送且仅发送 `"thinking":{"type":"disabled"}`，不得同时发送 legacy `enable_thinking`。若将来冻结快照明确为 `true`，同一路径发送 `"thinking":{"type":"enabled"}`。DeepSeek V4 默认启用 thinking，且 `max_tokens` 同时包含 reasoning 与最终回答；因此该 4096 边界不增加、不重试、不改变解析或输出上限。非 DeepSeek-V4 provider 保持既有兼容字段行为。

`thinking_enabled: false` in the frozen snapshot is an evaluation transport invariant: for DeepSeek V4, the shared S/H evaluation generator sends only `"thinking":{"type":"disabled"}`, never the legacy `enable_thinking` field. If a future frozen snapshot explicitly sets it to `true`, the same path sends `"thinking":{"type":"enabled"}`. DeepSeek V4 enables thinking by default and includes reasoning plus final answer in `max_tokens`; this does not increase the 4096 boundary, add retries, or alter parsing/output limits. Non-DeepSeek-V4 providers retain their existing compatibility fields.

### 3.2 用量、成本与失败 / Usage, cost, and failure

每个 S 结果记录 Provider、模型、参数、输入/输出 SHA-256、一次调用的 prompt/completion/reasoning/total token 用量、成本状态和失败类型。没有经过核验的价格表时，成本金额明确为 `NOT-AVAILABLE`，不能由 token 数伪造货币成本。失败不生成输出 hash 或正文文件。

Each S result records provider, model, parameters, input/output SHA-256, single-call token usage, cost status, and failure classification. Without verified pricing, monetary cost is explicitly `NOT-AVAILABLE`; token counts are not converted using invented prices. Failed calls do not receive fabricated output hashes or prose files.

对 H 而言，一旦 `BuildHarnessRequest` 成功，任何后续失败记录都必须保留该精确请求的 input SHA-256，以及冻结的模型配置、Harness policy 和阶段模板哈希；这包括既有输出读取、Provider、空/超限输出、结构化审稿和输出持久化失败。非空但拒绝的响应只进入私有 failure 文件及 `FailureOutputSHA256`，而不会成为接受的 `OutputSHA256`；空响应和 Provider 错误不伪造输出哈希。请求构建失败没有 request input hash，但可以仅在来源明确时保留已知的其他冻结哈希。续跑保留完整 attempts，并且五次 Provider 调用绝不转为四调用 READY。

For H, once `BuildHarnessRequest` succeeds, every later failure record must retain that exact request input SHA-256 plus the frozen model-config, Harness-policy, and stage-template hashes; this includes prior-output reads, provider failures, empty/oversize output, structured-review rejection, and output-persistence failures. A non-empty rejected response remains only in a private failure file with `FailureOutputSHA256`, never accepted `OutputSHA256`; empty responses and provider errors do not fabricate an output hash. A request-build failure has no request input hash, though it may retain other known frozen hashes only when their source is unambiguous. Resume preserves the full attempts list, and five provider calls never become a four-call READY result.

## 4. P0-T09 评测专用 H arm 与公平性 / P0-T09 evaluation-only H arm and fairness

P0-T09 可实现一个版本化、评测专用、离线 H runner。H 必须使用同一任务、相同允许输入事实、相同模型族、相同参数边界和同一任务 QualitySpec；其精确流程为两个独立候选、一次结构化审稿、一次最终修订，共四次模型调用。所有调用的实际 token 与成本必须全部计入。禁止通过削弱 S prompt、删除关键事实、改用更弱模型或隐藏失败重试制造优势。

P0-T09 may implement a versioned, evaluation-only, offline H runner. H uses the same task, factual input permissions, model family, parameter boundary, and task QualitySpec; its exact flow is two independent candidates, one structured review, and one final revision, for four model calls total. All call costs and tokens count. Weakening S, withholding facts, using a weaker model, or hiding retries is prohibited.

该 runner 是 P0-T09 的内部执行机械，不是新 Phase、里程碑、独立产品目标或产品 Harness 状态机。它绝不添加产品运行时集成、用户可见 Harness workflow、正式工作区写入、自动发布或第三方脚本执行；也不接入产品 API/SSE/UI/页面/菜单/设置、Automation、正式 Markdown、Author Finalization、生产 CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill 执行或 Phase 1。

This runner is internal P0-T09 execution machinery, not a new Phase, milestone, independent product goal, or product Harness state machine. It adds no product runtime integration, user-facing Harness workflow, formal workspace write, automatic publication, or third-party-script execution; it also excludes product API/SSE/UI/pages/menus/settings, Automation, formal Markdown, Author Finalization, production CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill execution, and Phase 1.

S remains exactly one model call. K remains a separate capability-reference isolation experiment and must never be renamed or substituted as H. P0-T07 does not implement H: every H record in the committed legacy run remains `NOT-READY`, and no H prose, win rate, or PASS is fabricated from that run.

## 5. 稳定 ID 与 hash / Stable IDs and hashes

- 输入 hash：对原始输入文件字节计算 SHA-256。
- 配置 hash：对 provider、base URL、model profile、model、credential policy 和参数的稳定 JSON 计算 SHA-256。
- 输出 hash：对模型返回正文原始字节计算 SHA-256。
- Task hash：对完整任务快照稳定计算，包括任务 ID、Profile、题材、类型、用途、篇幅、数据分组、允许输入、输入/来源/许可、QualitySpec、模型配置和成本记录位置。
- Run ID：由 corpus contract/version、完整冻结基线协议，以及按 task ID 排序后的完整 Task hash 稳定计算。

Formatting-only changes to the manifest do not alter task identity. Any semantic change to a task, allowed input, QualitySpec goal/contract, template, baseline rule, or model configuration produces a different stable Run ID.

## 6. 匿名盲包 / Blind package

`package-blind` 使用稳定 task hash 决定 `S/H -> A/B` 顺序。真实映射只保存在 run 的 `private/blind-map.json`；`blind/package.json` 和 `blind/samples/**` 不包含 arm、Provider、base URL、model profile、模型名、Skill、运行目录或原文件名。导出器还会从输出正文中移除显式来源标签。

`package-blind` deterministically maps S/H to A/B from the stable task hash. The mapping remains under `private/blind-map.json`; the blind package excludes arm, provider, base URL, model profile/name, Skill, run directory, and source filenames. Explicit source labels in output text are redacted during export.

若 S 或 H 任一缺失，该样本为 `NOT-READY`，不创建 A/B 正文文件。缺 arm 绝不能通过复制、交换或 fixture 补齐。

If either arm is unavailable, the sample is `NOT-READY` and no A/B prose files are created. Missing arms cannot be filled by copying, swapping, or substituting deterministic fixtures.

## 7. 双人评审与第三人裁决 / Two reviewers and adjudication

每个 ready 样本由两名不同评审者独立完整阅读。评审者必须先复述：

1. 人物目标 / character goal
2. 阻力 / obstacle
3. 选择 / choice
4. 代价 / cost
5. 文本造成的关系、信息或局势变化 / resulting text change

随后选择 `A`、`B` 或 `tie`，引用正文并说明证据。系统拒绝同一评审者重复提交、第三份“独立评审”、空复述、无引用证据、非法决定和越界指标。

If the two independent decisions differ, a third reviewer adjudicates while still blind to source. The adjudication record must reference the two conflicting review IDs. Agreement does not permit an unnecessary adjudication record.

### 7.1 私有人工提交 / Private human submission

只有真实的人类评审者可以提交或裁决盲评；Codex、其他模型和自动化 agent 不能充当评审者。评审者在仓库外的私有运行根中保存一份 JSON，且该文件的规范真实路径必须位于目标 run 的 `private/review-inbox/` 内。随后由本机操作者显式导入：

Only real human reviewers may submit or adjudicate blind reviews; Codex, other models, and automated agents cannot act as reviewers. A reviewer stores one JSON file in the out-of-repository private run root, whose canonical real path must be inside the target run's `private/review-inbox/`, then a local operator explicitly imports it:

```powershell
quality-eval record-review --run <run-id> --run-root <absolute-private-root> --input <absolute-private-review-json>
```

三个参数均为必填。`--run-root` 与 `--input` 必须为绝对路径；导入器拒绝仓库内文件、inbox 外文件、符号链接/junction/reparse 逃逸、非普通文件、未知字段、尾随 JSON 和无效评审合同。成功只输出稳定的 run、sample、kind、`RECORDED` 状态；不会输出评审者、选择、引用、备注或任何私有路径。输入文件不会被自动删除或改写。通过后，记录只以 owner-only 权限原子保存到私有 review evidence；盲包、索引和摘要仍仅保留既有匿名聚合。

All three arguments are required. `--run-root` and `--input` must be absolute; the importer validates the run ID before deriving a run path, opens the final input handle without following its reparse point, verifies the handle's canonical regular file remains in the inbox, and rejects repository files, files outside the inbox, symlink/junction/reparse escapes, non-regular files, unknown fields, trailing JSON, and invalid review contracts. Failures return only stable safe reason codes and never echo private paths, identities, decisions, evidence, notes, or prose. Success prints only stable run, sample, kind, and `RECORDED` status. The input is never automatically deleted or rewritten. On acceptance, an OS-released cross-process lock serializes validation and atomic owner-only private persistence; blind packages, indexes, and summaries retain only their existing anonymous aggregates.

Review JSON is exactly one object with no unknown or trailing values. Its required fields are: `contract` (`denova.quality-evaluation-review`), `version` (`v1`), safe stable `review_id`, `sample_id`, and `reviewer_id`; `kind` (`independent` or `adjudication`); complete `restatement` (`character_goal`, `obstacle`, `choice`, `cost`, `text_change`); `decision` (`A`, `B`, or `tie`); non-empty `evidence` items with `option` (`A` or `B`), `quote`, and `reason`; `fact_errors.A/B` (non-negative); and `author_edit_ratio.A/B` (0 through 1). `notes` is optional. An independent review has no `conflict_review_ids`; an adjudication names exactly two different existing independent review IDs for the same sample, and is accepted only when those two decisions conflict. `SaveReview` is the final authority for ready-sample membership, reviewer uniqueness, the two-independent-review cap, and all semantic validation.

当前 regression cohort 的真实人工评审仍为 0；导入能力不改变其 `NOT-ENOUGH-DATA` 状态，也不构成质量 PASS 或任何优劣结论。

The current regression cohort still has 0 real human reviews; import capability does not change its `NOT-ENOUGH-DATA` status and does not create a quality PASS or comparative conclusion.

## 8. 指标与分层 / Metrics and strata

配对主指标把 H 胜记为 `1`、平记为 `0.5`、H 负记为 `0`，用稳定 seed 的 2,000 次 task-level bootstrap 计算 95% CI。报告必须同时按以下维度汇总：

- Profile
- genre
- task type
- length bucket
- overall paired result

同时记录事实错误、作者改稿比例、token 用量、货币成本（若可核验）及 H/S 成本比。总体平均不能替代任一 Profile 的独立结果。

The paired score records H win as 1, tie as 0.5, and H loss as 0. A deterministic 2,000-resample task bootstrap produces the 95% CI. Reports include Profile, genre, task type, length bucket, overall pairs, fact errors, author edit ratio, token usage, verified monetary cost when available, and H/S cost ratio.

P0-T07 不提前写死 60%、70% 等胜率，也不把 CI 下界规则当作当前 PASS。P0-T09 只从真实 frozen regression pilot 与人工评审方差冻结未来样本量和非劣规则；这不构成 Phase 2/3/5 质量 PASS。

P0-T07 pre-registers no arbitrary 60% or 70% win rate and does not treat a CI rule as a current PASS. P0-T09 freezes future sample-size and non-inferiority rules only from a real frozen regression pilot and human-review variance; it does not establish a Phase 2/3/5 quality PASS.

## 9. 结果状态 / Result states

| Status | Meaning |
|---|---|
| `ENVIRONMENT-BLOCKED` | Provider、凭据或运行环境不足，真实调用未执行；不是 PASS |
| `NOT-READY` | 至少一个配对 arm 不存在；不能计算质量结论 |
| `NOT-ENOUGH-DATA` | 两 arm 存在，但独立评审、冲突裁决或最低可计算样本不足 |
| `VALID` | 配对数据和评审完整，统计可复算；仍不等于质量 Gate PASS |
| `FAILED` | 已尝试的运行或数据合同失败，失败类型已记录 |

No status defaults to PASS. Missing data remains visible.

## 10. 失败分类 / Failure taxonomy

- `provider_credentials_missing`
- `model_configuration_mismatch`
- `model_call_failed`
- `empty_model_output`
- `model_call_limit_exceeded`
- `harness_arm_not_available`
- manifest/contract/hash/path/review validation error

错误记录包含 run/task/Profile 等定位信息，但不保存 API Key、Authorization header、完整 prompt、thinking、正文预览或大对象。

Errors retain enough run/task/Profile context for diagnosis without credentials, authorization headers, full prompts, thinking, prose previews, or large payloads.

## 11. 数据安全 / Data safety

- 原始私密作品和受版权限制正文保留在仓库外，由数据所有者管理；工具不自动删除。
- 仓库只提交项目自有合成 brief、去标识 manifest、hash、协议、blocked/not-ready 元数据和聚合摘要。
- API Key 只可在运行时配置；manifest 递归拒绝敏感字段和常见凭据值。
- thinking、完整敏感 prompt、Provider authorization 和来源线索不进入 run 或盲包。
- 私密输出路径位于 run 的 `private/`，盲包只复制去标识正文；提交前仍须按数据许可做人工审计。

- Private and copyright-restricted source material stays outside Git and is never auto-deleted.
- Git contains only project-owned synthetic briefs, de-identified manifests/hashes/protocol, blocked/not-ready metadata, and aggregate summaries.
- Credentials are runtime-only; the manifest recursively rejects sensitive fields and common credential values.
- Thinking, sensitive full prompts, provider authorization, and source clues do not enter run metadata or blind packages.
- Private outputs remain under the run's `private/` area and still require a license/privacy audit before any commit.

## 12. 当前可复算运行 / Current reproducible runs

历史快照 `run-598b2c33eba7f255bd88eaec` 是凭据受阻的 legacy 运行：S 为 36 × `ENVIRONMENT-BLOCKED`（`provider_credentials_missing`），H 为 36 × `NOT-READY`，没有可用 A/B 盲评样本。它只保留为历史诊断记录，旧失败运行不能作为成功或质量证据。

The historical `run-598b2c33eba7f255bd88eaec` snapshot is a credential-blocked legacy run: S is 36 × `ENVIRONMENT-BLOCKED` (`provider_credentials_missing`), H is 36 × `NOT-READY`, and it has no usable blind A/B samples. It remains only as a diagnostic record; old failed runs are not success or quality evidence.

当前 P0-T09 live cohort 的已提交可复现索引为 `p0-harness-run-index-v1.json`，仅包含选择、哈希、状态计数、聚合用量/成本、本地证据可用性和盲包哈希。运行时 API Key 从未持久化到 manifest、运行元数据、日志、盲包或提交文件。

The current P0-T09 live cohort is recorded in the committed reproducibility index `p0-harness-run-index-v1.json`, which contains only selection, hashes, status counts, aggregate usage/cost, local-evidence availability, and blind-package hashes. The runtime API key was never persisted to the manifest, run metadata, logs, blind packages, or committed files.

| Cohort / 批次 | Run ID | Safe aggregate facts / 安全聚合事实 |
| --- | --- | --- |
| Smoke / 冒烟 | `run-2ac80556cb00c5aa3ff52f42` | S/H `READY`: 1/1; model calls S/H: 1/4; blind samples: 1 |
| Tuning / 调优 | `run-2f9cce8a71c485df0881cdbb` | S/H `READY`: 18/18; model calls S/H: 18/72; blind samples: 18 |
| Regression / 回归 | `run-4d815afd6f76bbea0926ac55` | S/H `READY`: 12/12; model calls S/H: 12/48; blind samples: 12 |

三个 cohort 的失败数均为 0，reasoning tokens 均为 0；`release_holdout` 的调用、输出、中间产物和盲评样本均为 0。真实独立人工评审仍缺失，因此汇总结论是 `NOT-ENOUGH-DATA`，不产生质量 `PASS` 或任何优劣结论。

All three cohorts have zero failures and zero reasoning tokens; `release_holdout` has zero calls, outputs, intermediates, and blind samples. Real independent human reviews are still missing, so the summary conclusion is `NOT-ENOUGH-DATA`; there is no quality `PASS` and no comparative quality conclusion.

## 13. 命令 / Commands

```powershell
go run ./cmd/quality-eval validate --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json
$runId = (go run ./cmd/quality-eval create-run --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json).Trim()
go run ./cmd/quality-eval package-blind --run $runId
go run ./cmd/quality-eval summarize --run $runId
```

`create-run` 只把稳定 Run ID 写 stdout；诊断日志写 stderr，因此 PowerShell 可原样捕获。命令不设置固定模型超时。

`create-run` writes only the stable Run ID to stdout and diagnostics to stderr, so PowerShell captures it exactly. No command sets a fixed model timeout.
