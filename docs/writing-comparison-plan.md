# 多模型章节比较与合成：开发计划

## 目标

让作者针对一个明确章节任务，使用 2–4 个已配置的 Writing Profile 生成独立候选稿，在同一份冻结写作上下文上比较、评分、选段和备注，再由作者指定的评审/整理模型生成可编辑的综合提示词，交给另一个可独立选择的最终书写模型生成合成稿。正式章节只有在作者确认并通过现有 revision/Change Review 后才改变。

用户完成后能做的事：

1. 从章节编辑器或章节任务卡创建一个比较任务，明确目标路径、任务要求、候选模型、模板/Skill 和候选专属要求。
2. 离开页面后回来继续任务；查看每个候选的模型快照、提示词快照、运行状态、正文和评分证据。
3. 从已完成候选中选择 2–4 份进入比较界面，阅读、查看差异、选取正文片段并写备注。
4. 让评审/整理模型把选择内容组织成可编辑的结构化结果和综合提示词，再让最终书写模型生成合成稿。
5. 通过 Change Review 审阅并应用合成稿；如果目标章节已被外部修改，系统阻止覆盖并要求重新读取或重新生成。

## 首版边界

### 做什么

- 单次只处理一个目标章节；目标文件可以不存在，但创建任务时必须明确路径。
- 2–4 个候选；候选默认串行，环境与用户都明确允许时才并行。
- 候选使用当前可用的 Writing Profile；不兼容只读候选执行的 Profile 在创建时明确排除，不静默降级到别的模型。
- 每个候选独立选择已迁入的写作模板/Skill，并附加候选专属要求；保存完整配置与版本快照。
- 复用现有 Writing Agent 上下文解析，冻结实际被选中的文件、Lore 条目、章节游标、修订号和内容快照。
- 候选、评审、综合提示词、合成稿和选段都归同一个比较任务；普通章节和普通写作会话不保存候选副本。
- 评审/整理模型是独立角色：用户级默认 Profile，任务创建时可临时改选并保存快照。
- 至少一个候选完成后即可评分；后续候选完成时追加新的评分记录，不覆盖已保存评分。
- 默认评分维度：剧情推进、人物动机、连续性、节奏、文风、对白、章节钩子/回收。任务创建时可修改，评分必须返回依据。
- 选段保存候选修订摘要、UTF-8 字节范围、原文快照和作者备注；候选正文不可原地修改，重试生成新修订。
- 综合提示词可编辑；最终书写模型独立选择，生成只读合成稿。
- 任务级暂停/取消、候选级重试；取消保留已完成结果。后端继续执行，前端离开不影响；重启后未完成请求标记 `incomplete`，不自动重放。
- 任务状态支持配置中、生成中、部分完成、已完成、合成中、待应用、已应用、已归档；执行状态（排队、运行、暂停、取消、不完整）与业务阶段分开保存，避免状态组合失控。
- 已应用或不再需要的任务先归档；归档可恢复；永久删除需要明确确认并清理任务资产，不影响正式章节。
- 砚舟的写作提示词只作为迁移参考：审计后迁入 Denova 自有 Skill/提示词资源，运行时不读取 `D:\vibe\yanzhou`。

### 不做什么

- 首版不支持一次比较连续多章。
- 不让候选或评审阶段直接写章节、进度、角色状态或 Lore。
- 不自动以评分或排名替作者选择，不宣称评分等于文学质量。
- 不覆盖正式章节，不绕过 revision/Change Review，不在冲突时静默合并。
- 不把每个候选创建成普通写作会话，不污染普通会话列表。
- 不在首版实现无限候选、无限并发、实时协同编辑、计费金额预测或跨用户共享。
- 不在运行时依赖砚舟源码，也不把砚舟的业务数据格式直接搬进 Denova。
- 不为比较任务建立第二套可写 Goal/Todo 系统；执行调度使用现有运行时能力，产品只持久化已确认的任务与状态快照。

## 推荐领域模型

### ComparisonTask

任务的稳定身份和生命周期。至少包含：

- `id`、`project_id`、`target_path`、`target_exists`、目标文件基础 revision。
- 章节任务正文、创建者输入、创建/更新时间、业务阶段、执行状态。
- `input_snapshot_id` 与摘要哈希。
- 候选配置列表、评审/整理模型快照、评分维度快照、最终书写模型快照。
- 当前选中的候选修订、选段引用、综合提示词修订、合成稿修订、Change Review 引用。

### WritingInputSnapshot

任务创建时由现有 Writing Agent 上下文解析产生。保存实际纳入上下文的：

- 文件路径、项目相对路径、读取 revision、内容快照、来源和用途。
- Lore 条目 ID/版本与内容快照。
- 章节游标、Lore 范围、IDE 当前章节引用、章节任务与 Skill/模板版本。
- 解析时的容量、截断和省略记录，以及稳定摘要哈希。

快照是候选公平性的依据。候选执行不能重新读取 live project 来替换快照；需要新上下文时创建新比较任务或明确创建新快照版本。

### CandidateDraft 与 CandidateRevision

`CandidateDraft` 是候选的稳定身份；`CandidateRevision` 是一次生成结果。候选配置包括 model profile、runtime、prompt/template、Skill、附加要求和调度顺序。修订包括正文、正文摘要哈希、运行引用、token/usage（服务提供时）、完成时间和错误分类。重试只追加新修订，不覆盖旧正文；选段必须绑定修订哈希。

### ReviewRecord

评审/整理模型一次不可变输出。评分项包括 `dimension`、分数、依据引用、模型快照、输入候选修订列表和 rubric 版本。新候选完成后追加新记录，历史记录保持不变。作者的手工改分或忽略标记是独立的用户修订，不能伪装成模型评分。

### SelectionInput

作者对候选的选择：候选修订 ID、UTF-8 字节起止位置、原文快照、候选摘要哈希、作者备注和可选改写意见。候选正文改变或哈希不符时，选择标为失效并要求重新选择，不按模糊文本搜索自动迁移。

### SynthesisRevision 与 CompositionRevision

评审/整理模型根据冻结上下文、评审记录和作者选择生成结构化综合结果；作者可编辑并保存多个修订。最终书写模型使用指定的综合提示词修订和同一上下文快照生成合成稿；合成稿在应用前始终是任务资产，不是正式章节。

## 持久化与恢复

在 Project Store 增加独立比较域，例如：

```text
stores/<store_dir>/comparisons/<comparison_id>/
  journal.jsonl             # 唯一事实源：任务、状态、配置、事件、引用
  snapshots/<id>.json       # 写作输入快照及内容引用
  candidates/<id>/<rev>.md  # 不可变候选修订正文
  reviews/<id>.json         # 不可变评分/证据输出
  selections/<id>.json      # 选段与作者备注
  synthesis/<rev>.json      # 综合结果及作者编辑修订
  compositions/<rev>.md     # 合成稿修订
```

`journal.jsonl` 负责恢复任务阶段、候选修订、评审、选段、合成稿和 Change Review 引用；目录扫描只用于重建索引，不作为恢复事实源。写入采用原子追加/替换和 revision 检查。所有 Project 内引用使用规范相对路径，不保存宿主绝对路径；模型密钥只从现有设置解析，绝不写入任务快照或日志。

需要在 `internal/project.Layout` 增加比较域路径方法，并在 Project Store 初始化/备份/迁移检查中纳入该目录。首版只做当前 Release 的小而明确迁移：旧项目没有比较域时创建空目录即可，不需要双写或兼容未发布格式。

## 后端模块拆分

建议新增 `internal/app/writingcomparison`（名称可在实现时调整，但职责保持独立），不要把编排下沉到 `agent/`：

1. `types.go`：任务、候选、修订、评分、选段、合成稿和状态枚举。
2. `store.go`：canonical JSONL、不可变正文/JSON 资产、索引重建、归档/删除和恢复。
3. `snapshot.go`：调用现有 Writing context 解析，生成文件/Lore/revision/内容快照和摘要哈希。
4. `scheduler.go`：任务级运行控制、候选串行队列、可选并发上限、取消、重试和崩溃后的 incomplete 标记。
5. `candidate_runner.go`：把候选配置和快照交给现有 Agent/runtime 适配层，使用只读工具权限；禁止直接复用当前前台 Writing session 的可写循环。
6. `reviewer.go`：评审模型结构化评分与证据校验；输入只来自快照、候选修订、评分维度和作者选择。
7. `synthesis.go`：生成、校验和保存综合结果；支持作者编辑后的新修订。
8. `composition.go`：按最终书写模型生成只读合成稿。
9. `apply.go`：以目标基础 revision 创建现有 workspace Change Review，处理新文件与已有文件，冲突时返回稳定错误并保留任务资产。
10. `events.go`：复用现有项目事件/任务投影向前端报告候选和任务变化，不另造一套无边界的实时总线。

应用层通过 `internal/agents/runtime` 或现有统一执行接口选择 Native/Codex/Claude 等兼容 Profile；`agent/` 只负责 Native runtime 的执行，不接收比较任务概念。评分与综合提示词属于产品层工作，不能通过给 `agent.Definition` 注入外部 runtime 或第二套 Goal/Todo 实现。

## API 草案

所有接口均为 Project-scoped，返回 `project_id`、任务 ID 和 revision/状态摘要；写操作带任务 revision，冲突不覆盖。

```text
POST   /api/projects/:project_id/writing-comparisons
GET    /api/projects/:project_id/writing-comparisons
GET    /api/projects/:project_id/writing-comparisons/:id
PATCH  /api/projects/:project_id/writing-comparisons/:id
DELETE /api/projects/:project_id/writing-comparisons/:id          # 仅永久删除，需明确确认

POST   /api/projects/:project_id/writing-comparisons/:id/run
POST   /api/projects/:project_id/writing-comparisons/:id/pause
POST   /api/projects/:project_id/writing-comparisons/:id/resume
POST   /api/projects/:project_id/writing-comparisons/:id/cancel
POST   /api/projects/:project_id/writing-comparisons/:id/candidates/:candidate_id/retry

GET    /api/projects/:project_id/writing-comparisons/:id/candidates/:candidate_id/content
POST   /api/projects/:project_id/writing-comparisons/:id/reviews
POST   /api/projects/:project_id/writing-comparisons/:id/synthesis
PATCH  /api/projects/:project_id/writing-comparisons/:id/synthesis/:revision
POST   /api/projects/:project_id/writing-comparisons/:id/compositions
POST   /api/projects/:project_id/writing-comparisons/:id/apply
POST   /api/projects/:project_id/writing-comparisons/:id/archive
POST   /api/projects/:project_id/writing-comparisons/:id/restore
GET    /api/projects/:project_id/writing-comparisons/:id/events
```

候选状态和任务状态必须在 API 中显式返回；不要让前端从“文件是否存在”猜测。用户可见错误使用现有 message/error key，本地化中英文同时提供；内部日志用英文并带 task/candidate/revision/run 标识。

## 前端落点

建议新增 `web/src/features/writing-comparison/`，复用现有设置、Skill、上下文收据、差异和 Change Review 组件：

- `CreateComparisonDialog`：目标路径、章节任务、候选配置、评分维度、评审 Profile、最终书写 Profile、调用数量预览。
- `ComparisonTaskList`：写作工作台入口，显示阶段、执行状态、完成候选数和最近更新时间。
- `ComparisonTaskWorkspace`：候选生成状态、候选卡、评分、重试/暂停/取消、恢复入口。
- `CandidateCompareView`：从完成候选中选择 2–4 个，支持全文阅读、同步滚动、片段选择和差异查看；超过 4 个分批进入比较。
- `SelectionInputPanel`：保存候选片段、UTF-8 anchor、原文快照和作者备注；失效 anchor 明确提示重新选择。
- `SynthesisEditor`：展示评分证据、整合说明、最终书写约束和综合提示词，支持作者编辑后保存修订。
- `CompositionReview`：显示合成稿、目标章节基础 revision、模型快照和 Change Review 入口。

候选文本选择需要把浏览器 UTF-16 selection 转成 UTF-8 byte anchor；可复用现有 review comment anchor 的编码约定，但候选选段必须绑定 candidate revision hash。所有用户可见状态、空状态、错误和按钮提供 zh-CN/en-US 独立资源。

## 砚舟提示词迁移

先在 `D:\vibe\yanzhou\src\main\services\promptComponents\` 建立迁移清单，不直接把整个目录接入 Denova。首批只迁移能支撑本功能的四类：

1. 整章写作/续写：对应候选生成和最终书写。
2. 开篇/章节任务写作：支持目标章节尚不存在的场景。
3. 一致性/伏笔审校：作为评审维度的证据来源。
4. 组合/改写指令：转化为综合提示词的约束结构。

每个模板转成 Denova 自有资源，至少带稳定 ID、版本、适用任务、上下文引用范围、输出格式和质量检查；候选任务保存模板快照。砚舟的 `contextScope`、slot 和 prompt preview 思路可以借鉴，但 Denova 的事实源仍是自己的 Skills、上下文快照和任务 journal。迁移工作必须逐条核对与 `novel-lite`/`novel-standard` 的重复和冲突，不能同时注入两套等价写作规则。

## 分阶段实施

### Phase 0：基线与提示词迁移清单（1–2 个开发日）

**用户价值**：明确可选写作模板，避免先做 UI 再发现提示词和上下文不兼容。

- 核对当前 Writing Profile、Skill、Agent runtime 和只读工具能力。
- 读取并审计砚舟候选模板，映射到 Denova 现有 `novel-lite`/`novel-standard`。
- 选择首批模板并写入版本化 Denova 资源。
- 为模板写 schema/装配纯函数测试，确保输出格式稳定。

### Phase 1：任务域、快照和恢复（3–5 个开发日）

**用户价值**：创建任务后可以安全离开、重启和恢复，正式章节不会被候选污染。

- 增加 Project Store 比较域路径和初始化。
- 实现 canonical JSONL journal、不可变资产、索引重建、revision/CAS、归档/删除。
- 实现 Writing context snapshot，保存实际输入来源、内容、revision、截断记录和哈希。
- 实现任务/候选/修订状态机，区分业务阶段与执行状态。
- 集成用户级默认评审 Profile；任务内保存模型/模板/Skill 快照。

### Phase 2：候选生成与调度（4–7 个开发日）

**用户价值**：多个模型可以在同一章输入上独立产出候选，某个模型失败不会丢掉其他结果。

- 新增只读候选执行入口，复用应用层 runtime/profile 选择，不复用前台可写 Writing session。
- 默认串行队列；实现显式并发上限、暂停、取消、单候选重试和 incomplete 恢复。
- 持久化每次模型快照、运行引用、usage、输出正文和错误分类。
- 通过事件投影向前端报告状态，避免轮询和第二套任务状态。

### Phase 3：评分、选段和综合提示词（4–6 个开发日）

**用户价值**：作者能看到有依据的模型差异，并把多个候选的优点交给后续创作。

- 实现专门评审/整理模型的结构化 JSON schema 和默认评分维度。
- 完成至少一个候选即可评分，追加式保存后续评审记录。
- 实现候选正文的不可变修订读取和 UTF-8 选段 anchor。
- 实现作者备注、评分改动、忽略标记和候选选择。
- 实现综合结果：评分、证据、整合说明、书写约束、可编辑综合提示词。

### Phase 4：合成稿与正式应用（4–6 个开发日）

**用户价值**：作者能指定一个最终书写模型生成合成稿，并安全地应用到正式章节。

- 按综合提示词和冻结上下文生成只读合成稿。
- 以目标基础 revision 创建 Change Review；支持已有文件替换和新文件创建。
- 目标 revision 冲突时阻止应用，保留任务、选段和合成稿。
- 作者确认后接入正常写作状态同步：更新 progress/character-states，必要 Lore 变更必须遵守现有确认规则。
- 记录已应用的 Change Review 引用和最终任务状态。

### Phase 5：写作工作台与比较界面（5–8 个开发日）

**用户价值**：作者不需要命令或开发脚本，就能创建、恢复、比较、合成和应用任务。

- 从章节编辑器/章节任务卡创建入口。
- 新增比较任务列表和详情路由。
- 候选配置、调用数量预览、状态卡、失败/重试、评分和多候选比较界面。
- 选段和备注、综合提示词编辑、最终书写模型选择、合成稿 Change Review。
- zh-CN/en-US 文案、窄屏/宽屏、长章节/空候选/部分失败状态。

### Phase 6：验证与交付（3–5 个开发日）

**用户价值**：确认功能在重启、冲突、失败和真实模型配置下仍然可恢复，不把测试通过误当成创作质量通过。

- 运行 Go 受影响 package 的单测、`go vet`；跨 package 持久化改动后扩大到两个 Go module 的相关 `go test ./...`/`go vet ./...`。
- 运行前端 unit、browser、e2e、i18n 检查，覆盖 Writing 主链路。
- 使用确定性模型 fixture 做完整端到端：创建任务 → 两个候选 → 部分失败/重试 → 评分 → 选段 → 综合提示词 → 合成稿 → Change Review → 应用 → 状态同步 → 重启恢复。
- 使用真实配置做一次人工验证：只确认调用、状态、候选正文和应用护栏；不把评分结果写成文学质量或用户验收结论。
- 更新用户文档和 CHANGELOG，仅在功能真正可用并完成发布时记录重大用户能力。

## 关键验收标准

1. 候选生成、评分、综合提示词和合成稿阶段，正式章节及连续性文件字节不变。
2. 同一任务所有候选显示相同的输入快照哈希；每个候选显示自己的 model/template/Skill/要求快照。
3. 候选正文修订不可变；重试追加修订，旧选段不会静默指向新正文。
4. 评分有维度、分数、证据和模型快照；增量评分不会重写旧记录，作者手工改动有独立记录。
5. 任务支持离开、重启、暂停、取消、单候选重试；未完成付费调用不自动重放。
6. 目标章节 revision 冲突时应用被阻止，正式章节内容保持原样。
7. 应用成功后可追溯到 Change Review；归档可恢复，永久删除不影响正式章节。
8. 所有 UI 状态和错误在中英文资源中成对存在，比较任务不出现在普通写作会话列表。

## 主要风险与处理

| 风险 | 处理 |
| --- | --- |
| 完整上下文快照过大 | 复用现有上下文预算和分段记录；保存实际纳入内容及省略证据，不扫描整个作品。 |
| 外部 runtime 无法保证候选只读 | 在 Profile 能力检查中明确排除或使用只读 adapter；禁止静默切换到别的模型。 |
| 评分模型把偏好当事实 | 要求每个分数附候选/上下文证据，UI 明示评分是辅助判断，作者选择优先。 |
| 候选选段因重试漂移 | 候选修订不可变，选段绑定内容哈希和 UTF-8 byte range。 |
| 正式章节被新内容覆盖 | 应用使用基础 revision 和 Change Review；冲突直接拒绝。 |
| 任务状态组合爆炸 | 业务阶段与执行状态分离，所有转换写入 journal 并穷举校验。 |
| 迁移砚舟提示词造成双重规则 | 逐条审计，首批只迁移明确需要的模板；Denova Skill 是唯一运行时来源。 |

## 估算与交付顺序

按单人熟悉代码库的开发估算，首个可用垂直切片约 8–12 个开发日：Phase 1 → Phase 2（两个确定性候选）→ Phase 5 的最小比较面板 → Phase 4 的安全应用。完整首版约 23–39 个开发日，主要波动来自只读 runtime 适配、上下文快照复用和 Change Review/状态同步接缝。

建议先交付最小闭环：**两个候选、串行、无自动评分、只读全文比较、选中整稿、Change Review 应用**，用它验证任务存储和安全写入；再加入选段、评分、综合提示词、最终书写模型和可选并发。这样仍保留最终设计，但不会在上下文快照、调度、评分和编辑器交互尚未验证时一次性铺开所有复杂度。
