# Xiaping capability-reference evaluation status / 虾评能力参考评测状态

## Current state / 当前状态

The six preregistered comparisons are `MODEL-EVAL-BLOCKED`: the runtime has no configured provider credential (`DEEPSEEK_API_KEY`, `OPENAI_API_KEY`, or `ANTHROPIC_API_KEY`). Therefore this record contains zero paired outputs, and anonymous blind review and human review are both `NOT-READY`. No Codex or subagent output has been substituted, and this is not a quality claim.

六组预注册比较目前为 `MODEL-EVAL-BLOCKED`：运行时未配置提供方凭证（`DEEPSEEK_API_KEY`、`OPENAI_API_KEY` 或 `ANTHROPIC_API_KEY`）。因此本记录中的配对输出为零，匿名盲审与人工评审均为 `NOT-READY`。没有用 Codex 或子代理输出替代，也不构成质量结论。

## Preregistered fair comparison / 预注册公平比较

`S` is the frozen P0-T07 single-turn baseline. `K` is **capability-reference isolation**, explicitly not an official future Harness arm (`H`): it differs from `S` only by the canonical UTF-8 JSON serialization of `summary`, `inputs`, `outputs`, and `constraints` from exactly one reference in [xiaping-capability-reference-v1.json](xiaping-capability-reference-v1.json). Each slice is bounded below 48 KiB, and the matrix preserves its hash, byte count, source-Skill/version/archive/line-span composite identity, frozen task hash/profile/goals, and model-configuration hash.

`S` 是冻结的 P0-T07 单轮基线。`K` 是**能力参考隔离**组，明确不是未来官方 Harness 组（`H`）：与 `S` 的唯一区别，是来自 [xiaping-capability-reference-v1.json](xiaping-capability-reference-v1.json) 中恰好一个参考单元的 `summary`、`inputs`、`outputs`、`constraints` 的规范 UTF-8 JSON 序列化。每个切片均小于 48 KiB；矩阵保留其哈希、字节数、源 Skill/版本/归档/行段的复合身份，以及冻结任务哈希、画像/目标和模型配置哈希。

Both arms must keep provider, model, model profile, temperature, output-token ceiling, task facts, and QualitySpec identical. Full Skills, raw third-party text, scripts, extra references, prior outputs, and reviewer feedback are excluded. Only P0-T07 `tuning` inputs may be used; `regression` and `release_holdout` are neither read nor represented here.

两组必须保持提供方、模型、模型画像、温度、输出 token 上限、任务事实和 QualitySpec 完全一致。完整 Skill、第三方原文、脚本、额外参考、既有输出和评审反馈均被排除。仅可使用 P0-T07 的 `tuning` 输入；`regression` 与 `release_holdout` 均未读取、也未出现在此处。

## Matrix / 矩阵

| Tuning task | Profile | K capability reference | Slice bytes | State |
| --- | --- | --- | ---: | --- |
| `ls-mystery-dialogue-03` | `long_serial` | `editor.review-profile-rubric` | 272 | `MODEL-EVAL-BLOCKED` |
| `ls-mystery-turn-05` | `long_serial` | `outline.build-long-arc` | 262 | `MODEL-EVAL-BLOCKED` |
| `fq-urban-opening-01` | `fanqie_short` | `engagement.review-reading-drive` | 271 | `MODEL-EVAL-BLOCKED` |
| `fq-urban-dialogue-03` | `fanqie_short` | `style.revise-naturalness` | 294 | `MODEL-EVAL-BLOCKED` |
| `zh-marriage-opening-01` | `zhihu_salt_short` | `engagement.review-reading-drive` | 271 | `MODEL-EVAL-BLOCKED` |
| `zh-workplace-turn-05` | `zhihu_salt_short` | `editor.review-story` | 275 | `MODEL-EVAL-BLOCKED` |

Machine-readable preregistration: [xiaping-evaluation-matrix-v1.json](xiaping-evaluation-matrix-v1.json).
