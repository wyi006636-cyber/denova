# Skill 接入红线 / Skill Integration Red Lines

## 冻结结论 / Frozen Decision

这些红线适用于写作与游戏两种模式。P0-T08 只冻结治理边界，不实现安装、Capability Registry/Router、更新、回滚或评测产品功能。

These red lines apply to both Writing and Game modes. P0-T08 freezes governance boundaries only; it does not implement installation, capability routing, updates, rollback, or evaluation product features.

## 权利状态必须分开 / Rights States Must Remain Separate

以下四种状态不得互相推导：

1. 公开可下载 / Publicly downloadable：无需登录即可取得当前包。
2. 允许本地使用 / Allowed for local use：许可证或权利人明确允许用户运行或使用。
3. 允许再分发 / Redistribution allowed：允许 Harness、Denova 或其他主体复制并再次提供包。
4. 允许预置 / Preinstallation allowed：允许将包随产品、镜像或安装器一同发布。

公开可下载只证明获取路径存在，不证明后三项。许可证未知时固定采用 LICENSE-UNKNOWN：只保留稳定原址、版本和 hash；默认不复制、不再分发、不预置，原址安装也必须等待用户明确确认和产品接入能力。

Public download proves availability only. When the license is unknown, retain stable provenance and hashes, prohibit copying, redistribution, and preinstallation, and require explicit user consent before any future origin-referenced install.

## 十项接入红线 / Ten Integration Red Lines

### 1. 禁止无原始来源安装 / No Installation Without an Original Source

- 必须有稳定 HTTPS 详情或 agent 文档地址、抓取时间、当前版本和可复算内容 hash。
- SOURCE-UNAVAILABLE、来源被替换、要求登录或凭据时，不得用相似名称、镜像或历史快照静默替代。
- 时效签名 URL 只能在当次下载内存或 Agent 自有临时目录中使用，不得写入日志、catalog、fixture、Git 或遥测。

An install candidate must have a stable HTTPS origin, capture time, current version, and reproducible hash. Unavailable or authenticated sources may not be silently replaced, and ephemeral signed URLs must never enter logs, catalog data, fixtures, Git, or telemetry.

### 2. 禁止全量 Skill 注入 / No Whole-Skill Prompt Injection

- 每次只注入被选 Capability 所需的摘要、选区或有界片段。
- 禁止把多个完整 SKILL.md、references、scripts、assets 或历史运行日志一次性注入模型。
- 注入片段必须记录来源、用途和高于 128 KiB 的可配置硬上限；展示历史不得自动进入模型上下文。

Inject only the bounded excerpt required by the selected Capability, with provenance, purpose, and a configurable hard limit above 128 KiB. Complete Skills, references, scripts, assets, display history, and logs must not be bulk-injected.

### 3. 禁止未经提示扩大权限 / No Silent Permission Expansion

- 文件读取、目录遍历、文件写入/覆盖/重命名、网络、子进程和工具权限必须分项展示并逐项确认。
- 默认最小权限：网络 DENY、进程 DENY、写入 DENY；只对当前 Capability 和当前用户选择临时放行。
- 包声明与静态发现不一致、引用缺失工具或隐藏内容时，必须进入 HOLD。

File reads, traversal, writes, overwrites, renames, network, subprocesses, and tools require separate disclosure and consent. Network, process, and write access default to DENY; declaration drift, missing tools, or hidden content forces HOLD.

### 4. 禁止静默接受内容、hash 或权限漂移 / No Silent Content, Hash, or Permission Drift

- 每次更新必须重新抓取稳定原址，计算 archive、manifest 和规范化文件清单 SHA-256。
- hash、文件、脚本、Unicode 不可见字符、依赖或权限任一变化都必须展示 diff。
- 用户未重新确认前，不得自动升级、自动执行或继续沿用旧授权。

Every update must refetch the stable origin and show archive, manifest, normalized file-list, script, Unicode-control, dependency, and permission diffs. No upgrade, execution, or inherited authorization is allowed before renewed consent.

### 5. 禁止不可回滚覆盖 / No Irreversible Overwrite

- 写入作者内容、Skill 安装目录或版本状态前，必须保存可验证的旧 hash、版本、来源和恢复点。
- 回滚必须恢复内容与权限记录，不能只删除新文件或声称“可重新下载”。
- 涉及 rename、overwrite、批量目录输出或 Git 操作的 Skill 必须先显示精确目标；失败不能留下半安装或部分覆盖。

Before any author-content or installation write, preserve the prior version, hashes, provenance, permissions, and a tested restore point. Rename, overwrite, directory output, and Git targets must be shown exactly, and failures must not leave partial state.

### 6. 禁止第三方全流程接管 Harness 状态机 / No Third-Party Workflow May Own Harness State

- 全流程 Skill 必须拆成稳定 Capability；调用结果只能成为候选、审稿证据或修订建议。
- 第三方 Skill 不得决定 Profile 切换、CandidateSet 状态、Quality Gate、下一步路由、版本写入、Author Finalization 或发布。
- Harness 只依赖 Capability 契约，不依赖第三方 Skill 名称、提示词顺序或内部状态机。

End-to-end third-party workflows must be split into stable Capabilities. They may produce candidates or evidence, but may never control Profiles, CandidateSet state, Quality Gates, routing, version writes, Author Finalization, or release.

### 7. 未知许可默认原址候选 / Unknown License Means Origin-Referenced Candidate Only

- LICENSE-UNKNOWN 不得标记为“可安装”“已授权”“可商用”或“可预置”。
- 不把第三方正文、提示词、references、assets、scripts、ZIP 或二进制复制进仓库和产品包。
- 正式商业发布前必须完成独立的权利与素材清单审计；P0-T08 不是法律意见。

LICENSE-UNKNOWN never means installable, commercially cleared, redistributable, or preinstallable. Keep only stable provenance and hashes, do not copy package contents into the product, and require an independent rights review before commercial release.

### 8. 未知脚本默认不执行 / Unknown Scripts Default to No Execution

- 静态审计可以读取脚本文本和计算 hash，但不得执行、导入、安装依赖或调用其中的网络/进程行为。
- 普通子进程、临时目录、容器标签或平台 security_status 都不能称为强沙箱证明。
- 真正执行前必须有独立安全决策：固定内容 hash、显式命令和参数、隔离的文件/网络边界、资源限制、日志和可撤销授权。

Unknown scripts may be read and hashed but not executed, imported, or used to install dependencies. A normal subprocess, temporary directory, container label, or platform status is not a strong sandbox; execution needs a separate, hash-bound security decision.

### 9. 更新必须重验并进入独立回归 cohort / Updates Require Revalidation and an Independent Regression Cohort

- 更新流程必须重新计算 hash、显示内容与权限 diff、重新确认授权并保留旧版本恢复点。
- 更新后的 Skill 必须进入与 tuning 分离的 regression cohort；release_holdout 只能用于冻结发布判断，不能反向调规则。
- 静态相关性不能自动继承为新版本质量结论，PENDING-BLIND-REVIEW 必须重新建立。

An update must be rehashed, permission-reviewed, consented, and evaluated in an independent regression cohort with a reversible prior version. Static applicability and prior quality results do not carry forward automatically.

### 10. 来源、能力、Profile、评测和安装决策必须可审计且可撤销 / Every Decision Must Be Auditable and Revocable

- source、security、license、evaluation、recommendation 必须是独立状态字段。
- 每次映射要记录稳定 Skill ID、Capability ID、Profile、输入/输出、阶段、最小权限、证据 hash、评测 cohort 和决策人/时间。
- 用户必须能撤销安装授权、权限授权、Capability 映射和推荐选择；撤销不能物理删除会话历史，而应通过稳定标记和有效上下文过滤实现。

Source, security, license, evaluation, and recommendation remain independent states. Every source, Capability, Profile, permission, cohort, and install decision must be attributable and revocable without physically deleting conversation history.

## 当前九项的附加 HOLD 条件 / Additional HOLD Conditions for the Current Nine

- 小说助手：脚本会读写、重命名和覆盖记忆文件，文档还建议 Git pull/push；未实现逐路径授权和恢复点前不得执行。
- 多米的长篇小说创作：v5-check.sh 为 0 字节；不得把文档宣称的检查当作现有安全或质量门。
- 黄金开篇大师：文档声称三个脚本，当前包只有一个；缺失部分不得动态生成后自动执行。
- 小说爽点架构生成器：引用缺失 dispatcher.py，且 SKILL.md 含 2,256 个 Unicode Cf 隐藏字符；接入前必须可视化/规范化 diff 并人工复核，一种联网搜索模式默认禁用。
- 深度小说写作法：manifest 表示方法源于具名作品但没有许可；禁止复制分发和预置。
- 其余候选：即使没有脚本，也不能把平台 safe 或 safe_checked 当作运行时证明；文件读取和模型上下文仍需有界。

## 状态推进 / Status Promotion

SOURCE-VERIFIED 只表示原始来源与当前 hash 可核验。STATIC-REVIEWED 只表示完成了不执行代码的静态检查。两者都不能把 evaluation 从 PENDING-BLIND-REVIEW 提升，也不能把 recommendation 从 HOLD 提升。

Promotion requires, in order: explicit rights evidence, permission review, isolated execution design where needed, frozen evaluation inputs, blind review, update/rollback proof, and an auditable product decision. No single platform field or static scan can skip these gates.
