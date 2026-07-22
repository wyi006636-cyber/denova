# Phase 0 详细开发计划

> 目标：用 2–3 周冻结工程基线、质量基线、Skills 来源与核心架构合同。
> 本文中的“现有”路径已在 `91c6e509a6beea98e8d025777c97b34b2bc6ac9f` 核实；“计划新增”路径必须由对应 Task 创建。
> Phase 0 不实现 Quality Harness 产品业务闭环，不新增产品依赖，不改变正式工作区数据。唯一受限例外是 P0-T09 的版本化、评测专用离线 Harness runner：它只生成隔离的评测证据，绝不接入产品运行时、用户工作流、API/SSE、UI/页面/菜单/设置、Automation、生产工作区/正式 Markdown、Author Finalization、自动发布、生产 CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill 执行或 Phase 1 实现，也不执行第三方脚本。

## 1. 执行顺序

1. P0-T01 先固定机器真相、上游基线和复用接入点。
2. P0-T02 与 P0-T03 可并行；P0-T02 是推荐的第一个编码任务。
3. P0-T04 在 Workspace Schema 边界明确后开始；P0-T05、P0-T07、P0-T08 可在其后并行。
4. P0-T06 依赖 CandidateSet/ReviewIssue 的状态与定稿写入边界。
5. P0-T09 汇总全部证据；只有真实 regression 人工评审方差足够时才冻结 Phase 1/2 使用的 Gate Manifest。

每项任务独立提交。进入下一项前，前一提交必须通过该项的目标测试；不得把多个 ADR、测试修复和功能原型混成一个提交。

## 2. P0-T01：冻结 Denova 工程、上游与来源基线

**目标**

建立可机器复核的仓库、工具链、上游差异、现有模块接入点和第三方来源基线，避免后续任务在不同前提上开发。

**业务理由**

Harness 会长期跟随 Denova 上游。没有固定基线就无法区分 Harness 回归、上游变化和本机环境问题，也无法证明规划中的复用路径真实存在。

**输入**

- `AGENTS.md`、`README.md`、`README.en.md`、`DESIGN.md`、`CONTRIBUTING.md`。
- `go.mod`、`web/package.json`、`web/pnpm-lock.yaml`。
- `docs/project-design/final/小说写作工具-最终融合最优方案.md`。
- Git 当前分支、HEAD、`origin`、`upstream` 和 `upstream/master`。

**目标 package / 文件**

- `docs/project-design/implementation/baseline/ENGINEERING_BASELINE.md`（计划新增）。
- `docs/project-design/implementation/baseline/SOURCE_AND_DEPENDENCY_MATRIX.md`（计划新增）。
- `docs/project-design/implementation/baseline/source-path-manifest.json`（计划新增）。
- 只读核验 `cmd/denova/main.go`、`internal/api/routes.go`、`internal/app/runtime_builder.go`、`internal/agent/builder.go`、`internal/skills/`、`internal/automation/`、`internal/book/versions/`、`internal/workspacechange/`、`web/src/components/workbench/`。

**产物**

- 包含 branch/HEAD/upstream SHA、remote URL、工具版本和工作区状态的基线记录。
- 至少 30 个真实源码入口的路径、职责、复用判断和内容哈希清单。
- 最终方案、Kimi、WorkBuddy、虾评资料的优先级与许可/来源矩阵。
- Windows 测试失败 allowlist 初始状态；没有复现证据时必须为空。

**依赖**

无。开始条件是分支为 `feat/quality-harness-foundation`、工作区干净、merge-base 为 `eb5e4ee53ad158fe88dfb7148408edc6558e481a`。

**实施步骤**

1. 记录 `git rev-parse`、`git status --short`、`git remote -v`、`git ls-remote` 和 merge-base 结果。
2. 记录 `go version`、`node --version`、`pnpm --version`、`git --version`、Git Bash 版本；工具缺失登记为 `ENV-*`，不得登记为测试失败。
3. 用 `rg --files` 和符号搜索生成路径清单；每个路径必须存在并标注主要职责、行数和是否高风险。
4. 对照最终方案记录“复用、扩展、新增、明确不改”；明确来源资料不能覆盖最终决策。
5. 计算清单文件 SHA-256，使后续基线变更可复核。
6. 在文档中记录更新规则：每次同步 upstream 后重跑，不手工覆盖旧快照。

**测试 / 验证**

```powershell
git rev-parse --show-toplevel
git branch --show-current
git rev-parse HEAD
git rev-parse upstream/master
git merge-base HEAD upstream/master
git status --short --untracked-files=all
git diff --check
```

路径清单用脚本逐项 `Test-Path -LiteralPath`；0 个缺失才通过。工具缺失允许使环境检查返回非零，但必须产生明确 `ENV-*` 记录。

**验收**

- SHA、remote、工作区状态和 30+ 路径可复核。
- 所有结论区分“源码事实”“最终方案决定”“来源建议”。
- 当前已知事实记录本机 `go` 不在 PATH；它是 `ENV-GO-MISSING`，不是 Windows 上游失败。
- 基线文件不含 API Key、完整用户目录内容或模型输出正文。

**风险**

- 远端在任务执行中变化：同时记录查询时间和 direct remote SHA。
- 路径清单快速过期：清单绑定 HEAD，并在 upstream 同步任务中强制更新。

**回滚方式**

删除本任务新增的三个基线文件；它不修改源码、配置或工作区内容。

**预计人日**

0.75 人日。

**Commit 边界**

单一提交：`docs: freeze Denova quality harness engineering baseline`

## 3. P0-T02：Denova 核心安全边界特征测试（推荐首个编码任务）

**目标**

用快速、确定性的特征测试冻结 Harness 将依赖的当前正确行为：工作区租约与原子写入、版本快照、SSE 重放、会话展示/模型上下文分离、共享菜单不切模式、写作与游戏共通能力不回归。

**业务理由**

这是后续改造的安全网。它先证明“什么已经正确”，再允许扩展 Quality Harness，可显著降低误改 Denova 核心流程和跨模式回归的风险。

**输入**

- P0-T01 的 SHA 与路径清单。
- 现有实现：`internal/app/task.go`、`internal/api/sse/task.go`、`internal/session/`、`internal/workspacechange/`、`internal/book/versions/`、`web/src/stores/workspace-store.ts`、`web/src/components/workbench/WorkbenchShell.tsx`。
- 现有测试风格和 fixture helper。

**目标 package / 文件**

- `internal/app/quality_harness_regression_test.go`（计划新增）。
- `internal/api/sse/quality_harness_regression_test.go`（计划新增）。
- `internal/session/store_test.go`（现有，仅在既有测试无法表达边界时增补）。
- `internal/workspacechange/service_test.go`（现有）。
- `internal/book/versions/service_test.go`（现有）。
- `web/src/components/workbench/quality-harness-navigation.test.tsx`（计划新增）。
- `web/src/stores/workspace-store.test.ts`（现有）。
- `.github/workflows/ci.yml`（现有；只有新增精确 Windows 回归入口确有必要时修改）。

**产物**

- 只描述当前行为的测试，不引入 `QualitySpec` 等未来生产类型。
- 最小跨模块 fixture：临时 workspace、章节文件、Agent 变更、版本、Task/SSE 重连和模式状态。
- Windows 目录持久化回归保持现有 `internal/workspacechange/service_windows_test.go` 精确入口。

**依赖**

P0-T01；本机或 CI 必须有 `go.mod` 指定版本的 Go。当前机器先解决 `ENV-GO-MISSING`，不得因此跳过 Go 测试。

**实施步骤**

1. 先写 RED：验证 `Task.Subscribe()` 的快照 + live 事件不重复执行任务，完成事件可重放。
2. 先写 RED：验证 `session.DisplayEvent` 不进入有效模型上下文，`ContextMessage` 进入上下文但不进入 UI 历史。
3. 先写 RED：验证同一 workspace 的 editor save、Agent change、review、version snapshot 共享写租约；错误 base revision 被拒绝。
4. 先写 RED：验证版本排除 `.denova/runs`/changes/reviews/interactive，且正式文件、Quality Harness 将复用的非运行文件仍可版本化。
5. 先写 RED：点击 `skills`、`agents`、`automations`、`versions` 等共享一级菜单不会改写 `nova:content-mode`；任一时刻仅一个一级菜单 active。
6. 先写 RED：写作/游戏切换必须由显式 mode action 触发，刷新后分别恢复可见 mode 与内容 mode。
7. 只在测试暴露当前代码真实缺陷时单独报告；本任务默认不修业务代码。若修复不可避免，应拆出独立 bugfix Task，不污染特征测试提交。
8. 确保每个单元测试单项小于 1 秒；涉及真实文件的集成测试使用 `t.TempDir()`，不启动前后端。

**测试 / 验证**

```powershell
go test ./internal/app ./internal/api/sse ./internal/session ./internal/workspacechange ./internal/book/versions -count=1
go test ./internal/workspacechange -run '^TestNewServiceInitializesWorkspaceChangeStoreOnWindows$' -count=1
pnpm --dir web exec vitest run src/stores/workspace-store.test.ts src/components/workbench/quality-harness-navigation.test.tsx src/components/workbench/WorkbenchShell.test.ts src/components/workbench/WorkbenchShell.responsive.test.tsx
go test ./...
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
git diff --check
```

**验收**

- RED 断言能在故意破坏对应边界时失败，恢复后 GREEN。
- 目标包、全量 Go、全量前端、i18n 和 build 均通过。
- 没有 sleep 型竞态测试、固定 LLM 超时或单项超过 1 秒的单元测试。
- 不启动或终止用户前后端进程；不修改生产 API。

**风险**

- 测试把实现细节冻结过死：只断言外部行为、完整结果和安全不变量。
- 并发测试不稳定：用 channel/barrier 控制顺序，不用任意 sleep。
- 当前环境缺 Go：先安装与 `go.mod` 一致的工具链或在 CI 执行，不能用前端绿灯代替。

**回滚方式**

删除两个计划新增测试文件，并回退对现有测试的本任务增量；生产代码应无变更。

**预计人日**

2.5 人日。

**Commit 边界**

单一提交：`test: characterize Denova quality harness safety boundaries`

## 4. P0-T03：冻结 Workspace Schema v1 ADR

**目标**

确定现有 Denova 物理目录到“正式区、待审产物区、运行区、投影区”的可迁移映射，明确版本保护、备份、可重建和人工修改语义。

**业务理由**

文件是真源；如果先写模型再决定文件位置，会产生第二真源、迁移破坏或无法原子定稿。

**输入**

- `internal/book/state.go` 的现有 `ideas.md`、`setting/`、`chapters/`、`.denova/lore/`。
- `internal/workspacepath/workspacepath.go` 的 `.denova`/`.nova` 兼容选择。
- `internal/book/versions/files.go` 的版本包含/排除规则。
- 最终方案 7.1–7.3、14.1、ADR-002。

**目标 package / 文件**

- `docs/project-design/implementation/adr/ADR-WS-001-workspace-schema-v1.md`（计划新增）。
- `docs/project-design/implementation/adr/workspace-schema-v1.example.json`（计划新增）。
- 只读验证 `internal/book/state.go`、`internal/book/files.go`、`internal/book/versions/files.go`、`internal/workspacepath/workspacepath.go`。

**产物**

- 版本化 Schema，包含路径、数据类别、真源、是否可手改、是否可重建、是否进入 Git 版本、迁移策略。
- 推荐决定：保留现有正式路径；新增 Harness 文件仍位于当前 workspace 的 `.denova` 下；`.denova/index.db`/cache/运行检查点可重建且不进入版本，QualitySpec/PreferenceMemory/待审 Artifact 是文件记录并按 ADR 进入版本；不一次性重命名现有目录。
- 每次迁移必须预览、备份、原子切换和可回滚。

**依赖**

P0-T01。

**实施步骤**

1. 列出现有所有创作/运行/会话/版本路径及当前版本保护规则。
2. 为逻辑目录逐项选择现有物理路径或计划新增路径，不复制同一正式事实。
3. 定义 schema version、最小 reader/writer version、迁移状态和失败恢复记录。
4. 定义手改检测：哈希变化只使投影/下游产物失效，不阻止作者打开项目。
5. 定义 `.nova` 遗留工作区读取边界，不在 Harness 首次启用时强制搬迁。
6. 进行 ADR 评审，结论必须是 Accepted 或明确拒绝进入 P1-T02。

**测试 / 验证**

```powershell
go test ./internal/workspacepath ./internal/book ./internal/book/versions -count=1
git diff --check
```

另以至少 3 个 fixture 走查：新项目、已有 `.denova` 项目、仅有 `.nova` 数据的旧项目。

**验收**

- 每类数据恰好一个真源；投影与缓存明确可重建。
- 现有正式文件不被强制重命名。
- 中文、空格和长路径行为明确。
- P1-T02/P1-T03/P2-T07 的目标路径均可由该 ADR 推导。

**风险**

- 把运行状态纳入版本造成噪音；必须逐目录定义排除规则。
- 排除过宽导致 QualitySpec/偏好丢失；需要版本包含测试。

**回滚方式**

回退两个 ADR 文件；未实现迁移前没有用户数据变化。

**预计人日**

0.75 人日。

**Commit 边界**

单一提交：`docs: define workspace schema v1 decision`

## 5. P0-T04：冻结 Profile 与 QualitySpec ADR

**目标**

定义三个 Profile 的稳定 ID、版本、质量目标、必需产物、能力需求、CandidatePolicy、ReviewRubric、导出配置，以及作品级/任务级 QualitySpec 的合并和作者确认规则。

**业务理由**

三种网文形态必须共享引擎但不能互相硬套规则；QualitySpec 必须表达当前作品/任务怎样才算好，而非固定评分表。

**输入**

- 最终方案第 6、8、9、15、17 节。
- P0-T03 Workspace Schema 决定。
- 虾评能力域初始清单和现有 `config.Settings` 分层模式。

**目标 package / 文件**

- `docs/project-design/implementation/adr/ADR-PROFILE-001-profile-contract.md`（计划新增）。
- `docs/project-design/implementation/adr/ADR-QS-001-quality-spec-contract.md`（计划新增）。
- `docs/project-design/implementation/contracts/profile-v1.schema.json`（计划新增）。
- `docs/project-design/implementation/contracts/quality-spec-v1.schema.json`（计划新增）。
- `docs/project-design/implementation/contracts/examples/long_serial.json`（计划新增）。
- `docs/project-design/implementation/contracts/examples/fanqie_short.json`（计划新增）。
- `docs/project-design/implementation/contracts/examples/zhihu_salt_short.json`（计划新增）。

**产物**

- 穷尽 ID：`long_serial`、`fanqie_short`、`zhihu_salt_short`；未知值返回显式错误。
- Profile 只保存默认策略和来源日期，不写死平台易变字数/规则；作者可覆盖且保留来源。
- QualitySpec 合并顺序：Profile defaults → project spec → task spec → 本次作者确认，不允许模型建议自动生效。
- 每条质量目标包含 `id`、来源、目的、适用范围、优先级、证据要求和版本。

**依赖**

P0-T03。

**实施步骤**

1. 从最终方案抽取共通能力和三 Profile 差异，禁止从来源文档引入冲突硬规则。
2. 定义 Profile/QualitySpec JSON Schema、版本和向前读取策略。
3. 明确平台规则来源日期、作者覆盖、升级提示和不自动覆盖语义。
4. 为三个例子完成完整合同，不保留未完成标记或省略字段。
5. 走查三个真实任务：长篇第 12 章、番茄黄金开篇、盐选反转结尾。
6. ADR 评审通过后冻结 P1-T01 的类型与验证语义，不冻结具体 Go 字段排布。

**测试 / 验证**

- 使用 JSON Schema validator 校验三个示例和故意错误示例。
- 检查未知 Profile、缺少作者确认、任务 spec 试图删除项目红线时均失败。
- `git diff --check`。

**验收**

- 三 Profile 差异通过数据表达，不出现三套引擎。
- QualitySpec 可读、可审查、可版本化，并区分作品级与任务级。
- 模型生成的 spec 修改只能是候选，不能自动写成确认态。

**风险**

- Schema 过度设计：只保留 Phase 1/2 会实际消费的字段。
- 平台规则过期：所有平台特有值带来源与日期，可由作者覆盖。

**回滚方式**

回退 ADR、Schema 和示例；没有生产数据。

**预计人日**

1.0 人日。

**Commit 边界**

单一提交：`docs: define profile and quality spec contracts`

## 6. P0-T05：冻结 CandidateSet 与 ReviewIssue ADR

**目标**

定义候选、比较证据、选择/混合、审稿问题、修订路由和闭环状态，使多候选与审稿可审计且不污染正式事实。

**业务理由**

多候选和多审稿只有在来源、问题证据和修订结果可追溯时才可能提高质量；否则只是增加调用次数。

**输入**

- P0-T04 Profile/QualitySpec 合同。
- 最终方案 6.7–6.9、9.1–9.3、17.3。
- 当前差异/评论实现 `internal/workspacechange/types.go`、`internal/documentreview/`，仅用于复用边界，不复用其领域含义。

**目标 package / 文件**

- `docs/project-design/implementation/adr/ADR-CS-001-candidate-set.md`（计划新增）。
- `docs/project-design/implementation/adr/ADR-RI-001-review-issue.md`（计划新增）。
- `docs/project-design/implementation/contracts/candidate-set-v1.schema.json`（计划新增）。
- `docs/project-design/implementation/contracts/review-issue-v1.schema.json`（计划新增）。

**产物**

- CandidateSet 生命周期：open → compared → author_selected/mixed/rejected → finalized/archived；状态穷尽，无吞错 default。
- 每个候选绑定 Run/Stage/Artifact、输入 Source Manifest、模型/Skill/Profile/QualitySpec 版本和内容哈希。
- ReviewIssue 包含位置、读感证据、原因类别、严重度、修订层级、最小影响范围、来源 reviewer、状态和复核证据；分数不能替代证据。
- CandidatePolicy 由任务价值与 Profile 决定，普通任务默认单候选。

**依赖**

P0-T04。

**实施步骤**

1. 定义标识、状态机和不可变来源字段。
2. 定义候选组合时的片段来源和新内容哈希，不能失去父候选关系。
3. 定义 reviewer 不可读取 Writer thinking/self-review 的隔离要求。
4. 定义 ReviewIssue 到 Capability ID 的修订路由，而非统一“润色”。
5. 定义修订后重新检查的影响维度与 issue closed/reopened 规则。
6. 用开篇多候选、单候选机械修改、跨段混合三个例子审查 Schema。

**测试 / 验证**

- Schema 正反例：缺来源、内容哈希不匹配、非法状态跳转、无证据 issue 必须失败。
- 追踪走查：Requirement → CandidateSet → ReviewIssue → Revision → Author decision 不丢 ID。
- `git diff --check`。

**验收**

- 候选不进入正式 Context Pack，除非被作者选择为本次任务输入。
- 比较结果含正文证据而非只有总分。
- 任一 ReviewIssue 可定位、可路由、可复核、可重开。

**风险**

- 将候选比较变成分数竞赛；主判断保留完整阅读与证据。
- 状态过多；只保留能影响恢复、作者决策和质量评测的状态。

**回滚方式**

回退四个计划新增文件；没有生产数据。

**预计人日**

1.0 人日。

**Commit 边界**

单一提交：`docs: define candidate and review issue contracts`

## 7. P0-T06：冻结 PreferenceMemory 与 Author Finalization ADR

**目标**

定义只有作者确认信号才能进入的偏好记忆，以及唯一可写正式区的、带草稿哈希和版本回滚的定稿事务。

**业务理由**

这是作者控制权和数据安全的核心。若没有该边界，Automation、Agent 或旧写入 API 都可能静默覆盖正文/设定并把模型自评固化为偏好。

**输入**

- P0-T03、P0-T05。
- `internal/workspacechange/operation.go` 的多路径持久意图与 roll-forward 机制。
- `internal/app/workspace_version_mutations.go`、`internal/book/versions/`。
- `internal/automation/types.go` 的 `read_only`/`confirm_write`/`auto_write` 现状。

**目标 package / 文件**

- `docs/project-design/implementation/adr/ADR-PM-001-preference-memory.md`（计划新增）。
- `docs/project-design/implementation/adr/ADR-AF-001-author-finalization.md`（计划新增）。
- `docs/project-design/implementation/contracts/preference-memory-v1.schema.json`（计划新增）。
- `docs/project-design/implementation/contracts/author-finalization-v1.schema.json`（计划新增）。

**产物**

- PreferenceSignal 只接受明确选择、否决、作者改写、规则确认；模型评分、流程完成或无操作不能进入。
- 偏好带作用域、证据、强度、来源决策、版本，可查看、编辑、撤销；撤销追加记录，不物理抹除审计历史。
- FinalizationRequest 必须含 workspace identity、目标路径、每个 base revision、candidate/artifact hash、同步候选选择、作者确认 nonce、Profile/QualitySpec version。
- FinalizationReceipt 记录写入路径、before/after revision、版本备份/提交 ID、偏好信号和失败/回滚状态。
- Automation 的 `auto_write` 对 Harness 正式区无效；只能写待审 artifact。

**依赖**

P0-T03、P0-T05。

**实施步骤**

1. 枚举所有当前正式写入入口，区分 editor user save、Agent review change、unmanaged mutation 和版本恢复。
2. 推荐扩展 `workspacechange.Service` 的具名公开批量操作，复用 durable operation；禁止在 Harness 中循环调用 `os.WriteFile`。
3. 定义定稿前版本检查点、全部 base revision 复验、写入、同步候选、版本提交和失败补偿顺序。
4. 定义一次确认只能消费一次，workspace/candidate/hash 任一变化使确认失效。
5. 定义 editor 有未保存 draft、外部手改、SSE 重连重复请求和崩溃中断的结果。
6. 用故障注入表审查：首文件前、路径之间、路径可见后、Git 提交前后、receipt 落盘前后。
7. ADR 必须由工程与产品共同确认后进入 Accepted。

**测试 / 验证**

- 设计级模型检查：无确认、旧 hash、旧 revision、错误 workspace、重复 nonce 均拒绝。
- 原子性走查：任何故障点都只能恢复到全部旧或全部新，不能“正文已换、事实未换”。
- 现有安全网：`go test ./internal/workspacechange ./internal/book/versions ./internal/app -count=1`。
- `git diff --check`。

**验收**

- 所有正式写入都能回答“谁确认、确认了哪个 hash、写了哪些路径、如何恢复”。
- PreferenceMemory 不接收模型自评或隐式行为。
- 批量和 Automation 明确停在待审边界。

**风险**

- 现有 `workspacechange` 多路径操作为内部 review/undo/redo 设计，公开扩展需保持依赖方向和恢复语义。
- Git 提交失败和文件事务不是同一原子介质；ADR 必须定义补偿与 receipt 状态，不能声称数据库式原子性。

**回滚方式**

回退四个 ADR/Schema 文件；未实现时不触碰用户文件。

**预计人日**

1.0 人日。

**Commit 边界**

单一提交：`docs: define preference memory and author finalization`

## 8. P0-T07：建立三 Profile 质量任务集与普通单轮基线

**目标**

建立可重复、去来源标签、分层采样的真实写作任务集和普通单轮生成基线，为后续 Harness 是否真的提升质量提供对照。

**业务理由**

没有先验对照，后续只能证明“流程能跑”，不能证明正文更好。

**输入**

- P0-T04 三 Profile 与 QualitySpec 合同。
- 最终方案 17.3 的人工盲评维度。
- 当前模型/Profile 配置入口 `config/model_profiles.go`、`config/settings.go`。
- Context 来源边界 `internal/agent/context/context.go`。

**目标 package / 文件**

- `internal/quality/evaluation/`（计划新增，纯评测工具 package）。
- `internal/quality/evaluation/testdata/long_serial/`（计划新增）。
- `internal/quality/evaluation/testdata/fanqie_short/`（计划新增）。
- `internal/quality/evaluation/testdata/zhihu_salt_short/`（计划新增）。
- `cmd/quality-eval/main.go`（计划新增，本地评测命令；不进入产品 API）。
- `docs/project-design/implementation/evaluation/EVALUATION_PROTOCOL.md`（计划新增）。
- `docs/project-design/implementation/evaluation/corpus-manifest-v1.json`（计划新增）。
- `docs/project-design/implementation/evaluation/runs/`（计划新增、生成结果不提交原始敏感正文时只提交去标识清单和摘要）。

**产物**

- 每 Profile 至少 12 个 Phase 0 试运行任务，覆盖开篇、人物选择/对白、结构转折、结尾或连续性；Phase 5 扩展样本量由 P0-T09 的功效分析冻结。
- 每个任务记录输入文件、用途、哈希、许可/匿名化状态、Profile、task-level QualitySpec、模型和成本预算。
- 普通单轮 arm 只获得同一允许输入与质量目标，不使用 Harness reviewer/revision 结果。
- 随机化 A/B 打包、盲评表、冲突裁决和结果计算工具。

**依赖**

P0-T04；真实模型运行需要用户已配置 Provider，但工具本身可离线验证 manifest。

**实施步骤**

1. 先为 corpus manifest、去标识导出和随机化写纯函数单元测试。
2. 为三 Profile 分层选择任务；不得用同一爽文标准覆盖盐选或长篇连续性。
3. 固定输入 Source Manifest 和模型配置快照；不保存 API Key。
4. 定义普通单轮 prompt 的最小公平模板，禁止在结果出炉后调模板偏袒任一 arm。
5. 生成 baseline 时保存模型/Provider、参数、token/cost、输入/输出 hash 和失败类型。
6. 盲评包移除 arm、Skill、模型推理、文件名等来源线索；评审先完整阅读再选择并写证据。
7. 计算配对胜/负/平、bootstrap 95% CI、事实错误、作者首轮可用/改稿量和成本；模型自动分数只作预警。
8. 将原始有版权或私密文本保留在本地不提交；提交可审计 manifest、hash 和聚合结果。

**测试 / 验证**

```powershell
go test ./internal/quality/evaluation -count=1
go run ./cmd/quality-eval validate --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json
$runId = (go run ./cmd/quality-eval create-run --manifest docs/project-design/implementation/evaluation/corpus-manifest-v1.json).Trim()
go run ./cmd/quality-eval package-blind --run $runId
go run ./cmd/quality-eval summarize --run $runId
git diff --check
```

`create-run` 必须把稳定 Run ID 单独写到标准输出，供后两条命令原样传入；命令不得设置固定模型超时。

**验收**

- 三 Profile 均有分层任务、单轮输出、成本记录和盲评包。
- 同一任务两 arm 使用同模型、同输入许可和同配置成本上限。
- 任何评审结果都可追到匿名样本 hash，但不能泄漏 arm。
- 评测工具可离线复算聚合结果。

**风险**

- 数据泄漏/版权：原文不默认提交，manifest 记录许可与匿名化方式。
- reviewer 偏差：至少两人独立评审，分歧交第三人；同一人不评自己的输出来源信息。
- 小样本波动：Phase 0 只用于建立方差和冻结正式门槛，不提前宣称固定胜率。

**回滚方式**

删除计划新增评测工具和无敏感数据的 manifest；本地未提交原始样本按数据所有者决定保留或删除，不能由回滚脚本自动清除。

**预计人日**

3.0 人日，不含真人评审等待时间。

**Commit 边界**

单一提交：`test: establish three-profile quality evaluation baseline`

## 9. P0-T08：盘点虾评 Skills 与接入红线

**目标**

基于原始来源发现、下载并静态盘点小说 Skills，记录来源、版本/日期、内容哈希、许可、权限、Capability 初映射和初步评测，不把第三方包复制进核心代码。

**业务理由**

虾评是第一优先级专业能力源，但未经来源、安全和质量治理直接安装会带来许可、行为漂移和提示词冲突。

**输入**

- `docs/project-design/sources/kimi/xiaping_research_report.md` 及 `xiaping-evidence/`。
- 现有 `internal/skills/remote.go`、`github.go`、`install.go`、`types.go`。
- 最终方案第 11 节能力域和首批映射。

**目标 package / 文件**

- `docs/project-design/implementation/skills/xiaping-catalog-v1.json`（计划新增）。
- `docs/project-design/implementation/skills/XIAPING_SOURCE_MATRIX.md`（计划新增）。
- `docs/project-design/implementation/skills/SKILL_INTEGRATION_RED_LINES.md`（计划新增）。
- `docs/project-design/implementation/skills/fixtures/`（计划新增，只保存公开 manifest/哈希/元数据；第三方正文按许可决定，不默认复制）。
- 只读验证 `internal/skills/` 和 `web/src/features/skills/`。

**产物**

- 搜索/详情/下载原址、抓取时间、文件 hash、作者/许可、Skill 结构、输入输出、文件/网络/工具权限。
- 映射至稳定 Capability ID；全流程型 Skill 必须拆成能力，不得接管 Harness 状态机。
- 接入红线：禁止无来源安装、禁止所有 Skill 全量注入、禁止未经提示扩大权限、禁止 hash 漂移静默更新、禁止不可回滚覆盖。
- 评测优先级：连续性、综合审稿、自然度、施工单、多线推演、对白等首批能力。

**依赖**

P0-T04；可与 P0-T05/P0-T07 并行。

**实施步骤**

1. 从虾评原始页面重新确认公开下载地址和当前内容，不以 Kimi 转述代替原址。
2. 下载到临时目录，计算 SHA-256，解析 `SKILL.md`/manifest，静态列出脚本和权限需求。
3. 不执行未知脚本；当前 Phase 仅用既有安全解压与文本检查。
4. 记录 license 缺失或冲突；不能确认再分发时只记录来源和原址安装策略。
5. 建立 Capability 初映射和三个 Profile 适用性，标明“已验证”“待盲评”“拒绝”。
6. 用 P0-T07 的小样本对首批能力做离线/人工初评，不能用模型自评分直接升级推荐等级。
7. 走查现有 Skills 安装链可复用项与缺口：source record、content hash、permission、evaluation、update、rollback。

**测试 / 验证**

```powershell
go test ./internal/skills -count=1
go test ./internal/skills -run 'Test.*(Remote|Install|Security|GitHub)' -count=1
git diff --check
```

另外对 catalog 做 JSON Schema/唯一 ID/hash 格式校验，并确认未提交未经许可的第三方正文。

**验收**

- 每项都有原始 URL、抓取时间和 hash；来源不明项不能标记为可安装。
- 每个首批 Capability 至少有一个候选实现或明确缺口。
- 接入矩阵明确复用当前安装器，缺失能力有后续 Task 承接。

**风险**

- 来源页面变化或下线：保存元数据与 hash，不伪造版本；更新需要重新评测。
- Skill 包含脚本：Phase 0 不执行；真正执行前走权限与沙箱决策。
- 许可不明确：默认原址安装、不再分发。

**回滚方式**

回退新增 catalog/矩阵/fixture；下载临时目录不进入仓库，人工确认后清理。

**预计人日**

2.0 人日，不含外部站点不可用等待时间。

**Commit 边界**

单一提交：`docs: inventory Xiaping novel skills and integration rules`

## 10. P0-T08A：全目录发现与证据短名单增量

**目标**

在不改写 P0-T08 已接受静态盘点历史的前提下，落实用户批准的全目录公开元数据发现、完整 Capability 召回、重复与评论证据治理、数据强/探索双通道短名单合同。

**依赖与边界**

- 依赖 P0-T08，并承接 `SKILL-001`、`SKILL-002`、`SKILL-003`、`SKILL-004`、`EVAL-001`、`EVAL-002` 与 `SAFE-002`。
- 阻塞 P0-T09 中的 Skill 证据闭环；不得回写、替换或重新解释 P0-T08 的历史 catalog、静态审计或结论。
- 仅采集可公开访问的元数据和合成测试 fixture；不下载、执行、复制或持久化第三方包、原始评论、签名 URL、令牌或密钥。

**产物与验证**

- 版本化快照 manifest、标准化 SkillRecord、Capability 候选/提案、重复簇、评论证据向量与短名单 JSON 合同。
- 严格 JSON 解码、稳定 SHA-256、唯一 Skill ID、部分/完整快照一致性和敏感数据拒绝测试；所有 fixture 使用 `example.test` 与虚构评论。
- 后续采集、证据评分和短名单任务必须复用这些合同，P0-T09 只在该证据链完成后冻结 Gate Manifest。
- 已发布的 COMPLETE 快照为 `snapshot-18d24eb4d408a116`（由 manifest 的 46 条 catalog 回执派生为 46 页；报告总数 2,251、去重 2,237）；分类结果为 1,072 个写作候选、4 个提案、1 个重复簇，短名单 65 项（DATA-RICH 34、EXPLORATION 31），16 个核心 Capability 均有条目，但 `character.build-dialogue-voice` 为 1/5、`outline.simulate-multiline` 为 2/5。
- 公开评论证据有 10 个不可闭合来源限制（4 个详情/首页总数不一致，6 个在 limit=50 的空第 21 页前仍声明 1,159–4,628 总数）；这些候选保持失败记录，不放宽分页/总数校验、不使用部分评论，且不得声明为质量结论或 DATA-RICH。约 1,594 个评论检查点和 1,072 个详情均保留在仓库外的受限本地缓存；本轮无 live 429/retry。

**Commit 边界**

首个合同提交：`test: define Xiaping discovery contracts`

## 11. P0-T09：Phase 0 集成门禁与质量阈值冻结

**目标**

汇总工程、ADR、质量和 Skills 证据，执行 Phase 0 全门禁，并基于真实 regression paired pilot 与人工评审方差冻结未来 MVP/v1 的质量、成本和安全阈值。为取得该 pilot 的真实 H 证据，P0-T09 允许实现版本化、评测专用、离线 Harness runner；该 runner 是本 Task 的内部执行机械，不是新 Phase、里程碑或独立产品目标。

**当前状态（2026-07-22）**：受限 smoke、tuning 和 regression S/H cohort 已成功，但 regression 的真实人工独立评审为 0/24，尚无冲突可裁决；因此 P0-T09 和 Phase 0 为 `NOT-ENOUGH-DATA / BLOCKED`。`quality-gate-v1.json` 仍不存在，且不得因现有工程证据或模型输出创建阈值。详见 `../evaluation/PHASE_0_BASELINE_REPORT.md`。

**业务理由**

最终方案明确禁止在基线前伪造固定胜率。P0-T09 把“没有预设数字”转化为可审计的计算和批准流程，而非留下永久未决项。

**输入**

P0-T01–P0-T08、P0-T08A 全部产物和当前 CI/release 命令，以及 P0-T07 的历史 run `run-598b2c33eba7f255bd88eaec`（S 为 `ENVIRONMENT-BLOCKED`、H 为 `NOT-READY`、有效配对为 0；该记录保持原状）。

**目标 package / 文件**

- `docs/project-design/implementation/evaluation/quality-gate-v1.json`（计划新增）。
- `docs/project-design/implementation/evaluation/PHASE_0_BASELINE_REPORT.md`（计划新增）。
- `docs/project-design/implementation/baseline/windows-upstream-failure-allowlist.json`（已建立，初始值为空数组）。
- `docs/project-design/implementation/adr/ADR-PROJECTION-001-sqlite-fts-driver.md`（计划新增，决定 Phase 1 使用的成熟驱动和 CGO/Tauri 约束）。

**产物**

- 先以 `tuning` 执行 runner/template shakeout，再以冻结的 `regression` cohort 进行 paired pilot；所有六个 `release_holdout` task 始终仅保留元数据/hash，零模型调用、零输出、零盲包、零评审、零调优。
- S arm 精确为每 task 一次模型调用；H arm 精确为两个独立候选、一次结构化审稿、一次最终修订，共四次模型调用。K 是独立的 capability-reference 隔离实验，不得改名或替代 H。
- 每 Profile 的真实 pilot 方差、最小检测效应，以及未来 Phase 2/3/5 的最小样本量和非劣规则；P0-T09 不宣称任何 Phase 2/3/5 质量 PASS。
- Gate Manifest 只可记录由真实 regression pilot 冻结的统计规则；若方差/人工评审数据不足，保持 `NOT-ENOUGH-DATA`，不得将 Phase 0 或质量 Gate 写为 PASS。
- 本 runner 不创建产品 CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill 执行、正式工作区写入或 Author Finalization；这些产品合同仍留给后续 Phase。
- 精确 Windows upstream failure allowlist；每项有测试名、失败签名、upstream 复现 SHA、issue/owner、到期日。当前无法用 Go 运行不构成 allowlist 条目。

**依赖**

P0-T02、P0-T06、P0-T07、P0-T08、P0-T08A。

**实施步骤**

1. 在具备 Go 工具链的 Windows 环境执行全量 Go 和精确 Windows 回归；在 Linux CI 执行官方门禁。
2. 执行全量前端/i18n/build，并记录非失败 warning。
3. 先运行 tuning runner/template shakeout，冻结其后模板/配置，再由双人完成 regression paired pilot 的盲评、第三人裁决冲突；评测工具计算 CI 和人工评审方差。
4. 仅据真实 regression pilot 冻结未来 `quality-gate-v1.json` 的样本量与非劣规则，记录计算方法、批准者、适用 Profile/版本和修改流程；不得宣称 Phase 2/3/5 PASS。
5. 对现有失败同时在特性分支和 `upstream/master@eb5e4ee53ad158fe88dfb7148408edc6558e481a` 复现；不能复现为上游的均视为新增回归。
6. 审核所有核心 ADR 为 Accepted；任一未决则明确阻塞 Phase 1 对应 Task。
7. 生成 Phase 0 报告，逐项给出 PASS/FAIL/NOT-RUN；NOT-RUN 不能算 PASS。

**测试 / 验证**

```powershell
go mod tidy -diff
go test ./...
go vet ./...
go test ./internal/workspacechange -run '^TestNewServiceInitializesWorkspaceChangeStoreOnWindows$' -count=1
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
git diff --check
git status --short
```

在 Linux CI 另外运行 `.github/workflows/ci.yml` 的 `govulncheck` 和 `./scripts/build.sh`。当前 Windows 机器在 Go 缺失时必须显示 `NOT-RUN / ENV-GO-MISSING`，修复前 P0-T09 不得完成。

**验收**

- 所有硬门禁结果均有明确记录；本 P0-T09 边界性工作包不得据此宣称 P0-T09、Phase 0 或任何未来质量 Gate 为 PASS，且没有未说明 NOT-RUN。
- 质量 Gate Manifest 的未来规则有真实 regression pilot、人工评审方差、样本依据和版本，不含随意拍定的胜率；P0-T09 本身及任何未来质量 Gate 均不得因本 Task 宣称 PASS。
- 离线 runner 仍与产品运行时完全隔离：无产品 API/SSE/UI/Automation、无正式 Markdown/workspace 写入、无 Author Finalization/自动发布、无生产 CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill 执行，且无第三方脚本执行。
- allowlist 默认空；若非空，每项满足双基线复现和到期规则。
- `git status --short` 仅含本 Task 预期文档/门禁文件，且提交后工作区干净。

**风险**

- 试运行样本不足：扩大样本，不降低置信要求。
- 基线模型/Provider 漂移：锁定配置和日期；新模型另建 cohort。
- 为赶进度扩大 allowlist：要求 upstream 精确复现、owner 和到期日，禁止通配测试名。

**回滚方式**

回退本 Task 的四个文件；不得删除原始评测记录来掩盖失败。若 Gate Manifest 已被后续版本引用，必须新增 superseding 版本而非历史重写。

**预计人日**

1.5 人日，不含人工评审排队。

**Commit 边界**

单一提交：`test: freeze phase zero quality and release gates`

## 11. Phase 0 总退出检查

- [ ] P0-T01–P0-T09（含 P0-T08A）每项一个英文 Commit，工作区干净。
- [ ] 七个必需 ADR 与 Projection ADR 均有明确状态、批准者和日期。
- [ ] 三 Profile 任务集、普通单轮结果、盲评包和 Gate Manifest 可复算。
- [ ] 写作/游戏、菜单、SSE、会话、Workspace Change、版本与 Skills 安装链无新增回归。
- [ ] Windows Go 门禁实际执行；环境缺失不再存在。
- [ ] 没有新依赖、业务页面、产品 Harness 状态机或正式数据迁移混入 Phase 0；P0-T09 唯一允许的是版本化、评测专用离线 runner。
- [ ] 没有 skip、删测试、降低断言或通配 allowlist。

Phase 0 通过后，下一步从 P1-T01/P1-T02 开始；不得直接跳到 Agent 编排或 Tauri。
