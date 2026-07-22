# 验证、质量评测与发布门禁

> 原则：代码绿灯只能证明实现没有已知回归；Harness 是否成功必须由三 Profile 的真实作品与人工盲评证明。
> 禁止通过 skip、删除测试、降低断言、扩大通配 allowlist 或把 NOT-RUN 写成 PASS 制造绿灯。
> 所有模型运行不设置写死超时；允许作者取消，成本/安全/空转限制使用明确配置。

## 1. 当前可复核基线（2026-07-21）

| 检查 | 当前结果 | 说明 |
|---|---|---|
| Git branch | PASS | `feat/quality-harness-foundation` |
| HEAD / merge-base | PASS | HEAD `91c6e509a6beea98e8d025777c97b34b2bc6ac9f`，merge-base 与 `upstream/master` 均为 `eb5e4ee53ad158fe88dfb7148408edc6558e481a` |
| 工作区 | PASS | 开始时 `git status --short --untracked-files=all` 无输出 |
| 前端双语 key | PASS | 2987 keys，zh-CN/en-US 对齐 |
| 前端 Vitest | PASS | 124 test files、645 tests 全通过；jsdom 输出 `Window.scrollTo` 未实现提示，不是失败 |
| 前端 production build | PASS | TypeScript + Vite 完成；存在单个 500 kB 以上 chunk warning，登记为性能观察项而非失败 |
| Go 测试 | NOT-RUN | 本机 PATH 没有 `go`；`go.mod` 要求 Go 1.26.5。分类为 `ENV-GO-MISSING`，不是 Windows 上游失败 |
| 本机工具 | INFO | Node v24.16.0、pnpm 11.12.0、Git 2.52.0.windows.1、Git Bash 5.2.37；正式 CI 仍以 `.github/workflows/ci.yml` 的 Node 22/pnpm 10/go.mod 为准 |

Phase 0 完成前必须在具备 `go.mod` 指定 Go 的 Windows 环境补跑所有 Go 门禁。当前 NOT-RUN 不阻止编写计划，但阻止 P0-T09 宣布完成。

## 2. 通用门禁层级

| Gate | 内容 | 失败结果 |
|---|---|---|
| G0 Scope/Hygiene | 分支、基线、预期文件、format、`git diff --check`、无秘密 | 当前 Task 停止，不进入评审 |
| G1 Targeted | 被改 package/组件的快速测试，单元测试单项 <1 秒 | Task 不可提交 |
| G2 Repository | 全量 Go、vet、frontend test/i18n/build、官方 CI | Phase 不可退出 |
| G3 Integration/UI | 真实 workspace、SSE、恢复、写作/游戏和页面矩阵 | 用户可见 Task 不可验收 |
| G4 Quality | 三 Profile 盲评、过程指标、审稿准确、成本/错误非劣 | 不得宣称 Quality Harness/MVP/v1 |
| G5 Release | 迁移/恢复/安装/版本/tag/Release notes/checksum | 不得发布 Beta/Release |

所有 Task 都运行 G0/G1；每个 Phase 退出运行 G2；涉及 API/状态/文件的 Phase 运行 G3；Phase 0、2、3、5 运行对应 G4；Phase 4/5 运行 G5 子集或全量。

## 3. 通用命令

### 3.1 Scope 与代码卫生

```powershell
git branch --show-current
git rev-parse HEAD
git rev-parse upstream/master
git merge-base HEAD upstream/master
git status --short --untracked-files=all
git diff --check
git diff --name-only
```

实现阶段还应确认变更文件均属于当前 Task；出现无关 Go/TS/配置/锁文件时立即停止并分离，不得覆盖用户修改。

### 3.2 后端全量门禁

```powershell
go mod tidy -diff
go test ./...
go vet ./...
go test ./internal/workspacechange -run '^TestNewServiceInitializesWorkspaceChangeStoreOnWindows$' -count=1
```

Linux CI 另外执行：

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
./scripts/build.sh
```

`go mod tidy -diff` 只检查依赖一致性，不改文件。只有 Task 明确批准依赖变化时才允许修改 `go.mod`/`go.sum`，且必须说明选择理由、许可、跨平台和 Tauri/CGO 影响。

### 3.3 前端全量门禁

```powershell
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
```

依赖安装由 CI 使用：

```powershell
pnpm --dir web install --frozen-lockfile
```

不得为解决测试失败改成非 frozen 安装或更新 lockfile，除非当前 Task 明确批准依赖升级。

## 4. 各 Phase 精确门禁

### Phase 0 Gate

P0-T09 唯一允许的 Harness 执行是版本化、评测专用、离线 runner：它不接入产品 API/SSE、UI/页面/菜单/设置、Automation 或产品工作流，不写正式 workspace/Markdown，不走 Author Finalization 或自动发布，不创建生产 CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill 执行，且不执行第三方脚本。它不是新 Phase、里程碑或产品 Harness 状态机。

**目标命令：**

```powershell
go test ./internal/app ./internal/api ./internal/api/sse ./internal/api/agentui ./internal/agent ./internal/agent/context ./internal/session ./internal/workspacechange ./internal/workspacepath ./internal/book ./internal/book/versions ./internal/skills ./internal/automation -count=1
go test ./internal/workspacechange -run '^TestNewServiceInitializesWorkspaceChangeStoreOnWindows$' -count=1
go test ./internal/quality/evaluation -count=1
pnpm --dir web exec vitest run src/stores/workspace-store.test.ts src/components/workbench/quality-harness-navigation.test.tsx src/components/workbench/WorkbenchShell.test.ts src/components/workbench/WorkbenchShell.responsive.test.tsx
go test ./...
go vet ./...
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
git diff --check
```

`internal/quality/evaluation` 和 `quality-harness-navigation.test.tsx` 是 P0-T07/P0-T02 计划新增入口；创建前不执行，创建后必须纳入。

**验收门槛：**

- 核心请求、session display/context、Task/SSE、workspace revision/lease、版本包含/排除、Skills 安装和共享菜单特征测试全绿。
- tuning runner/template shakeout 后，只对冻结 regression cohort 做 S/H paired pilot：S 是一调用，H 是双独立候选、结构化审稿、最终修订的四调用流程；K 保持独立 capability-reference 隔离实验，不能替代 H。六个 `release_holdout` task 只保留元数据/hash，零模型调用、输出、盲包、评审和调优。
- 七个必需核心 ADR 与 Projection driver ADR 为 Accepted。
- `quality-gate-v1.json` 仅从真实 regression pilot 和人工评审方差冻结未来样本量、非劣容差和成本规则，不含拍脑袋阈值；这不证明 P0-T09、Phase 0 或 Phase 2/3/5 已质量 PASS。
- Windows allowlist 默认空；若非空，满足第 7 节全部证据。

### Phase 1 Gate

**目标命令：**

```powershell
go test ./internal/quality/domain ./internal/quality/profile ./internal/quality/workspace ./internal/quality/projection -count=1
go test ./internal/app ./internal/api ./internal/book ./internal/book/versions ./internal/workspacechange ./internal/workspacepath -count=1
pnpm --dir web exec vitest run src/features/quality/project src/lib/api-client/quality-projects.test.ts src/stores/workspace-store.test.ts
go test ./...
go vet ./...
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
```

上列 `internal/quality/*` 和 `web/src/features/quality/*` 是 Phase 1 计划新增入口。

**必须通过的集成场景：**

1. 新 workspace、现有 `.denova` workspace、仅有 `.nova` 数据的 workspace 均可打开。
2. 迁移先产生 preview 和 backup；任一故障可回到原 schema。
3. 删除 `.denova/index.db` 后，rebuild 产生与文件一致的查询结果。
4. 作者外部修改 `chapters/*.md`：项目仍可打开，索引和依赖 run 被标记失效，不自动改其他正式文件。
5. Candidate/未确认 Preference 不进入正式 Context Pack。
6. Profile/QualitySpec UI 在 zh-CN/en-US、light/dark、窄/宽屏、空数据和长文本下可用。
7. 共享 quality 页面不自动把内容 mode 从 `ide` 改为 `interactive` 或反向改变。

### Phase 2 Gate

**目标命令：**

```powershell
go test ./internal/quality/contextpack ./internal/quality/capability ./internal/quality/review ./internal/quality/harness ./internal/quality/finalization -count=1
go test ./internal/app ./internal/api ./internal/agent ./internal/agent/context ./internal/session ./internal/workspacechange ./internal/book/versions -count=1
pnpm --dir web exec vitest run src/features/quality/harness src/features/quality/review src/lib/api-client/quality-runs.test.ts src/components/Editor src/components/Versions
go test ./...
go vet ./...
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
```

**必须通过的后端场景：**

- 实际模型消息装配证明 Writer/Reviewer 新上下文、来源白名单和大小上限；日志预览不能代替消息断言。
- Run 每个 Stage 后可关闭进程并恢复；重连 SSE 不重复调用模型。
- Source hash 变化使下游 Stage invalidated，旧 Artifact 保留。
- 缺少确认、旧 candidate hash、旧 base revision、错误 workspace、重复 nonce 均不能 Finalize。
- durable batch 在每个故障注入点恢复为全部旧或全部新；失败不产生 PreferenceSignal。
- Automation 与 Agent 直接调用正式写入 API 的负向测试全部被拒绝。
- 定稿成功产生可恢复版本和 receipt；版本提交失败触发明确补偿/恢复状态。

**页面验证：**

不停止或另启用户前端。使用现有热加载页面验证长篇任务、候选比较、ReviewIssue、差异、作者确认、失败恢复；覆盖 light/dark、zh/en、窄屏/宽屏、长正文、无候选、无 issue、网络断开后重连。

**质量门槛：**仅对 `long_serial` 使用第 6 节方法；主指标和非劣指标必须满足 `quality-gate-v1.json`。如果不满足，Phase 2 不退出；不能通过增加流程阶段掩盖失败，应定位无收益阶段并修订或删除。

### Phase 3 Gate

**目标命令：**

```powershell
go test ./internal/quality/... ./internal/skills ./internal/automation ./internal/app ./internal/api -count=1
pnpm --dir web exec vitest run src/features/quality src/features/skills src/features/automations src/stores/workspace-store.test.ts
go test ./...
go vet ./...
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
```

**必须通过的场景：**

- 番茄和盐选分别使用自己的 Profile 产物、Rubric 和工作流，不依赖长篇卷章字段。
- 虾评 Skill 从发现、详情、原址下载、预览、安装、登记、Capability 映射、评测、更新比较到回滚全链可审计。
- hash 或权限变化后 Skill 不能静默升级；旧版本仍可恢复。
- 同一 Capability 可替换实现；Harness 状态机不 import 具体 Skill 名。
- 批量任务完成后全部 Artifact 仍为待审；逐项或批次确认都保留 candidate hash，任何一项冲突可定位恢复。
- Profile 化导出不修改正式事实。

**质量门槛：**三 Profile 分别独立满足 Gate Manifest；不能把三个 Profile 合并后用总体平均掩盖某一个失败。每个 Profile 还必须有至少两个题材层，避免单一题材过拟合。

Phase 3 通过即 M3 Quality Harness MVP；此时才允许进入 Tauri 实现。

### Phase 4 Gate

**目标命令：**

```powershell
go test ./...
go vet ./...
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
```

Tauri package 建立后追加其官方 lint/test/build 命令，并固定在仓库脚本中；ADR-TAURI-001 批准前不在本文臆造不存在的命令或依赖。

**必须通过的桌面矩阵：**

- Windows 中文用户名、中文/空格/长 workspace 路径。
- 首次安装、覆盖升级、降级拒绝或明确迁移、卸载保留用户作品。
- sidecar 正常启动、端口冲突、启动失败、崩溃重启提示、窗口关闭、任务运行中退出、系统重启。
- 单实例、文件选择、通知、快捷键和 update 基础。
- Tauri 不改变 Web 开发主路径；质量评测仍可不经桌面 shell 执行。
- 普通作者可完成三 Profile 核心任务而无需理解 DAG、Git、Agent；高级视图仍可查看来源和 receipt。

### Phase 5 Gate

**目标命令：**

```powershell
go mod tidy -diff
go test ./...
go vet ./...
pnpm --dir web install --frozen-lockfile
pnpm --dir web test
pnpm --dir web check:i18n
pnpm --dir web build
git diff --check
```

在 clean Linux release runner 执行：

```bash
scripts/build-github-release.sh "$(git describe --tags --exact-match)"
```

该脚本已真实校验 `web/package.json`、`npm/package.json`、`CHANGELOG.md`、`README.md`、`README.en.md` 与 tag 一致，执行 Go/frontend 门禁，生成五平台包、双语 notes 和 SHA-256 checksums。

**必须通过：**

- 迁移/备份/回滚、索引删除/损坏、SSE 断线、应用崩溃、sidecar 异常、磁盘/权限错误故障演练。
- 大 workspace 的启动、增量读取、Context Pack、FTS、流式 UI 和版本操作基准；目标值由真实基线冻结，不能写未经测量的绝对数字。
- 新 holdout 样本上的三 Profile 盲评均满足 Gate Manifest；不得复用调参集宣布发布胜利。
- 安全/许可/依赖、API Key/日志、虾评来源与 hash 审计通过。
- 版本号、双语 README、CHANGELOG、tag、Release notes、安装包和回滚说明完全一致。

## 5. 人工页面验证矩阵

每个用户可见 Phase 至少保存一份评审记录，覆盖：

| 维度 | 必测值 |
|---|---|
| 语言 | zh-CN、en-US；没有混用或缺 key |
| 主题 | light、dark；红黄绿状态色均有可读对比 |
| 宽度 | 窄屏移动布局、常规桌面、宽屏；采用 adaptive 而非固定宽度 |
| 数据 | 空 workspace、单项目、长标题/长路径、大量候选/issue、失败和恢复态 |
| 模式 | ide、interactive；打开共享 quality/skills/automation/version 页面不自动切内容 mode |
| 编辑状态 | 未保存 draft、外部修改冲突、SSE streaming、重连、任务完成 |
| 可访问性 | 键盘主路径、可见 focus、按钮 label、错误/空状态可读 |

验证只访问用户已有前端；后端变化需要更新时使用仓库规定的 `scripts/restart-backend.sh`，不另启后台守护进程，不 kill 用户前端。

## 6. 三 Profile 人工盲评协议

### 6.1 比较对象

对同一 `EvaluationTask` 生成：

- S arm：普通单轮。使用同一模型、同一允许输入、同一任务 QualitySpec 与预先冻结的单轮模板；一次模型调用，不使用 Harness reviewer/revision 输出。
- H arm：Harness 候选。使用相同模型族和输入事实，经过当期已声明的 Capability、candidate/review/revision 策略；在作者人工改写前取样。

两 arm 都保存模型、Provider、参数、输入/output hash、实际 token/cost、失败类型。API Key、thinking 和来源标签不进入盲包。

### 6.2 随机化与评审

1. `quality-eval` 用稳定 task hash 派生随机顺序，把 S/H 映射为 A/B；评审者看不到映射。
2. 每个样本由 2 名评审者独立完整阅读，先复述人物目标、阻力、选择、代价和文本变化，再做 A/B/平选择并引用证据。
3. 两人结论冲突时由第 3 人裁决；裁决者仍看不到 arm。
4. 评测按 Profile、题材、任务类型和篇幅分层；报告同时给分层结果和总体结果。
5. 调参集、回归集和发布 holdout 分离；Skill/model 策略不能看到 holdout 结果后再修改并在同一批重测宣称通过。

### 6.3 主指标

每对样本记分：H 胜 1、平 0.5、H 负 0。按 task 分层 bootstrap 计算 95% CI。

- Phase 0：只估计方差并冻结样本量/容差，不要求 Harness 胜出。
- Phase 2：`long_serial` 的 CI 下界必须大于 0.50。
- Phase 3：三个 Profile 各自 CI 下界均大于 0.50。
- Phase 5：新的发布 holdout 上三个 Profile 各自再次满足；不能用总体平均替代单 Profile。

不提前写死 60%、70% 等任意胜率；“CI 下界 > 0.50”表达的是可重复优于随机/基线。实际最小样本量由 P0-T09 根据方差和最小检测效应写入版本化 Gate Manifest；样本未达到 manifest 值时结果为 NOT-ENOUGH-DATA，不是 PASS。

### 6.4 Profile 专项判断

| Profile | 必看维度 |
|---|---|
| `long_serial` | 本章功能、前章衔接、人物选择/代价、关系/局势变化、阅读动力、伏笔/连续性、章末压力 |
| `fanqie_short` | 开篇清晰与信息密度、卖点进入速度、冲突是否进入场面、升级是否改变处境、重复/停滞、结局兑现 |
| `zhihu_salt_short` | 叙事声音、可信因果、持续信息压力、反转前置证据、人物动机、情绪/主题闭环 |

共通维度包括对白身份/关系/利益、场景变化、机械解释/模板腔、事实错误和阅读完整性。Profile 规则可以版本化更新，但必须显示来源日期和作者覆盖。

### 6.5 非劣与成本门槛

以下阈值由 P0-T09 用实际基线写入 `quality-gate-v1.json`，缺任一值即 Gate 配置无效：

- `max_fact_error_delta`：H 相对 S 的事实错误率非劣容差。
- `max_author_edit_delta`：达到可定稿状态的作者改稿量非劣容差。
- `max_cost_ratio`：默认质量档 H/S 实际成本上限；超出只能作为显式高质量档，不得冒充默认胜利。
- `min_review_precision`、`min_review_actionability`：ReviewIssue 具体、可定位、可执行的最低值。
- `min_candidate_gain`：启用多候选的关键任务相对单候选必须达到的收益；无收益节点改回单候选。

两个 arm 使用相同模型和相同配置输入/输出预算边界；Harness 多调用的实际成本完整计入。不能通过不给单轮 arm 关键事实、给 H 更强模型或隐藏失败重试制造优势。

### 6.6 禁止作为发布结论的指标

- AI 文本检测分数、困惑度或固定“爽点/伏笔比例”。
- 自动化率、Agent 数、Stage 完成率、每小时字数。
- Reviewer 对自己产出的自评分。
- 只抽取关键词、不完整阅读正文的比较。
- 只有总体平均、没有 Profile/题材分层的结果。

这些信号可以用于回归预警或诊断，但发布判断必须由人工配对证据确认。

## 7. Windows 上游失败与新增回归分类

### 7.1 当前状态

- 当前没有经双基线复现而批准的 Windows 上游失败。
- `ENV-GO-MISSING` 是本机工具链缺失，不是测试失败，也不能进入 allowlist。
- 前端 `Window.scrollTo` jsdom 提示和 Vite 大 chunk warning 当前不导致测试失败；分别登记为测试环境噪音和性能观察项。

### 7.2 允许登记上游失败的必要证据

每个 allowlist 条目必须同时包含：

1. 精确 test package 和完整 test name，禁止正则通配整个 package。
2. Windows、Go/Node/pnpm 版本和稳定失败签名。
3. 在当前 feature HEAD 复现。
4. 在同一机器、同一命令的 `upstream/master@eb5e4ee53ad158fe88dfb7148408edc6558e481a` clean worktree 复现。
5. 当前 Task 未修改相关代码，或差异证明与失败无关。
6. upstream issue/内部 owner、临时处理方式、登记日期和到期日。

缺任一项即分类为：

- `NEW-REGRESSION`：当前分支新增或无法证明是上游；Phase Gate 失败。
- `ENVIRONMENT-BLOCKED`：工具/权限/磁盘/网络缺失；Gate NOT-RUN，修复环境后重跑。
- `FLAKY-UNCLASSIFIED`：不稳定但未找到根因；仍然失败，不能直接 allowlist。

### 7.3 Allowlist 规则

- 文件：`docs/project-design/implementation/baseline/windows-upstream-failure-allowlist.json`（P0-T09 计划新增）。
- 默认值是 `[]`。
- 只能在 targeted 汇总中显示已知失败；全量命令仍运行并保留原始退出码。
- 到期条目自动使 Gate 失败；上游修复后立即删除。
- 不允许用 build tag、`t.Skip`、Vitest `.skip`、删除 fixture 或降低断言实现 allowlist。

## 8. 失败处理与绿灯纪律

遇到失败时：

1. 保存完整命令、环境、package/test 名和最小失败签名。
2. 先复现，再定位来源；修复 bug 时先保留能失败的测试。
3. 如果与本 Task 无关但阻塞 Gate，单独登记并请求决策；不得顺手扩大 scope。
4. 修复后先跑 targeted，再跑完整 Phase Gate。
5. 最终报告区分 PASS、FAIL、NOT-RUN、NOT-ENOUGH-DATA；只有 PASS 计入退出条件。

以下行为直接使评审失败：

- `t.Skip`、`.skip`、`--passWithNoTests` 或删除测试以获得绿色。
- 把错误改成 warning、降低 expected 值、缩小 fixture 以绕开真实场景。
- 把当前分支失败未经 upstream 双复现写入 allowlist。
- 只跑 targeted 不跑全量，或只跑前端不跑 Go。
- 用 AI 自动评分替代人工盲评发布结论。

## 9. Release Gate

发布前必须同时满足：

- M5 所有数据安全、质量、性能、许可和桌面矩阵 PASS。
- 当前 branch/HEAD 与 release commit 一致，工作区 clean，tag 指向该 commit。
- `web/package.json` 与 `npm/package.json` 版本一致。
- `CHANGELOG.md` 有该 tag 章节；`README.md` 与 `README.en.md` 当前版本同步。
- Release notes 中列出用户感知功能、修复、三 Profile 质量验证、迁移/备份、已知限制和回滚方式。
- 五平台产物和 checksums 由 `scripts/build-github-release.sh` 生成；Windows Tauri 包另按 Phase 4 批准的发行流程签名验证。
- `git status --short` 在构建后的 clean release worktree 无非预期变更。

任一质量 Profile、数据恢复或 Author Finalization 安全门槛失败时，不得以 Beta 标签规避；可以发布明确的内部测试构建，但不能作为对外质量版本。
