# P0-T07 三 Profile 质量评测协议 / Three-profile quality evaluation protocol

> Contract: `denova.quality-evaluation-protocol/v1`
> Status: P0-T07 offline tooling complete; real S arm is `ENVIRONMENT-BLOCKED`; H arm is `NOT-READY`
> Date: 2026-07-21
> Scope: baseline and evaluation infrastructure only; no Harness workflow, P0-T08, P0-T09, Phase 1, or quality-gate manifest

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
- `release_holdout` 只登记任务和 hash；当前阶段不根据其结果调模板，也不在同一批数据上宣称发布通过。
- P0-T09 才能根据真实方差和最小检测效应冻结正式样本量及非劣容差。本文件不创建 `quality-gate-v1.json`。

- `tuning` may support future template or method development.
- `regression` checks a frozen approach for regressions.
- `release_holdout` is registered and hashed but is not used to tune the template or claim success on the same cohort.
- P0-T09, not P0-T07, must derive formal sample sizes and non-inferiority tolerances from real variance. This protocol does not create a gate manifest.

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

### 3.2 用量、成本与失败 / Usage, cost, and failure

每个 S 结果记录 Provider、模型、参数、输入/输出 SHA-256、一次调用的 prompt/completion/reasoning/total token 用量、成本状态和失败类型。没有经过核验的价格表时，成本金额明确为 `NOT-AVAILABLE`，不能由 token 数伪造货币成本。失败不生成输出 hash 或正文文件。

Each S result records provider, model, parameters, input/output SHA-256, single-call token usage, cost status, and failure classification. Without verified pricing, monetary cost is explicitly `NOT-AVAILABLE`; token counts are not converted using invented prices. Failed calls do not receive fabricated output hashes or prose files.

## 4. 未来 H arm 与公平性 / Future H arm and fairness

H arm 未来必须使用同一任务、相同允许输入事实、相同模型族、相同参数边界和同一任务 QualitySpec。Harness 多次调用、候选、审稿和修订的实际 token 与成本必须全部计入。禁止通过削弱 S prompt、删除关键事实、改用更弱模型或隐藏失败重试制造优势。

The future H arm must use the same task, factual input permissions, model family, parameter boundary, and task QualitySpec. All Harness candidate/review/revision calls and costs count. Weakening S, withholding facts, using a weaker model, or hiding retries is prohibited.

P0-T07 不实现 H arm。当前运行中所有 H 项均为 `NOT-READY`，工具不会创建 H 正文、胜率或 PASS。

P0-T07 does not implement H. Every H record in the current run is `NOT-READY`; no H prose, win rate, or PASS is fabricated.

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

## 8. 指标与分层 / Metrics and strata

配对主指标把 H 胜记为 `1`、平记为 `0.5`、H 负记为 `0`，用稳定 seed 的 2,000 次 task-level bootstrap 计算 95% CI。报告必须同时按以下维度汇总：

- Profile
- genre
- task type
- length bucket
- overall paired result

同时记录事实错误、作者改稿比例、token 用量、货币成本（若可核验）及 H/S 成本比。总体平均不能替代任一 Profile 的独立结果。

The paired score records H win as 1, tie as 0.5, and H loss as 0. A deterministic 2,000-resample task bootstrap produces the 95% CI. Reports include Profile, genre, task type, length bucket, overall pairs, fact errors, author edit ratio, token usage, verified monetary cost when available, and H/S cost ratio.

P0-T07 不提前写死 60%、70% 等胜率，也不把 CI 下界规则当作当前 PASS。正式门槛属于 P0-T09 的真实方差分析。

P0-T07 pre-registers no arbitrary 60% or 70% win rate and does not treat a CI rule as a current PASS. Formal thresholds belong to P0-T09 variance and power analysis.

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

## 12. 当前可复算运行 / Current reproducible run

- Run ID: `run-598b2c33eba7f255bd88eaec`
- S arm: 36 × `ENVIRONMENT-BLOCKED` (`provider_credentials_missing`)
- H arm: 36 × `NOT-READY` (`harness_arm_not_available`)
- Blind package: 36 × `NOT-READY`, no A/B prose files
- Summary: `NOT-READY`, paired samples `0`, missing arms `36`
- Quality claim: none

当前配置存在 DeepSeek Provider、`default` model profile 和 `deepseek-v4-pro` 标识，但当前有效配置与进程环境没有 API Key。此分类是环境事实，不是模型或项目质量失败。配置凭据后应产生新的真实 S 输出记录；H arm 仍须等后续 Harness 实现，不能在 P0-T07 伪造。

The Provider/model identifiers exist, but no API key is available in the effective configuration or process environment. This is an environment fact, not a model or product quality result. Supplying runtime credentials can produce real S outputs; H must still wait for an actual Harness implementation.

## 13. 命令 / Commands

```powershell
go run ./cmd/quality-eval validate --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json
$runId = (go run ./cmd/quality-eval create-run --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json).Trim()
go run ./cmd/quality-eval package-blind --run $runId
go run ./cmd/quality-eval summarize --run $runId
```

`create-run` 只把稳定 Run ID 写 stdout；诊断日志写 stderr，因此 PowerShell 可原样捕获。命令不设置固定模型超时。

`create-run` writes only the stable Run ID to stdout and diagnostics to stderr, so PowerShell captures it exactly. No command sets a fixed model timeout.
