# 风险与架构决策登记册

> 状态：规划阶段开放登记。
> 规则：最终融合方案已经冻结的产品方向不在实现 PR 中重新投票；本登记册只决策物理 Schema、类型合同、事务语义、依赖和迁移等实现细节。
> 风险 owner 是职责，不是具体姓名；排期时必须落实到人。

## 1. 已冻结、不得被来源资料覆盖的决定

| Decision ID | 已冻结决定 | 影响 |
|---|---|---|
| FD-001 | Denova 是唯一工程底座，采用模块化单体演进 | 禁止重写后端/前端/Agent Runtime |
| FD-002 | 文件是唯一创作事实真源，SQLite/FTS 只做可重建投影 | 禁止第二真源和 DB 反向写正式事实 |
| FD-003 | Harness 目标是提高优秀网文产出概率 | 禁止用流程/Agent/自动化指标代替质量 |
| FD-004 | 三种创作形态通过 Profile 隔离，共用引擎 | 禁止三套产品或用长篇规则硬套短篇 |
| FD-005 | Author Finalization 是任何自动化都不能绕过的边界 | AI/Automation/Skill 只能产出待审内容 |
| FD-006 | 继续使用 SSE、React、TipTap、shadcn 和现有双语体系 | 禁止 WebSocket/自制编辑器/平行 UI 栈 |
| FD-007 | 虾评是第一优先级 Skill 来源，Harness 依赖 Capability | 禁止具体 Skill 接管状态机或全量提示词堆叠 |
| FD-008 | Tauri 是 Phase 4/v1 发行形态 | 禁止提前阻塞 Phase 0–3 质量验证 |
| FD-009 | 向量检索、独立 Worker、强沙箱、云同步/协作需证据再立项 | 不进入当前 MVP 关键路径 |
| FD-010 | P0-T09 可实现版本化、评测专用离线 Harness runner 取得真实 S/H 配对证据 | 禁止产品运行时/API/SSE/UI/Automation 集成、正式工作区写入、Author Finalization、自动发布、生产 CandidateSet/ReviewIssue/PreferenceMemory/Capability Router/Skill 执行、第三方脚本执行或产品状态机；不是新 Phase/里程碑 |

## 2. 必需 ADR 待办

所有 ADR 必须使用 `Status / Context / Decision / Alternatives / Consequences / Migration / Validation / Supersedes` 结构；`Accepted` 前不得实现其阻塞接口。

| ADR ID | 对象 | 状态 | 决定期限 | 阻塞 Task | 建议方案 |
|---|---|---|---|---|---|
| ADR-WS-001 | Workspace Schema v1 | Proposed | P0-T03 完成前 | P1-T02、P1-T03、P1-T04、P2-T07、P5-T01 | 保留现有 `ideas.md`、`setting/`、`chapters/`、`.denova/lore/items.json`；新增 `.denova/quality/` 文件合同；run/cache/index 精确排除版本；迁移预览+备份+原子切换+回滚，不强制重命名旧路径 |
| ADR-QS-001 | QualitySpec | Proposed | P0-T04 完成前 | P1-T01、P1-T06、P2-T01、P2-T03、P2-T05 | 作品级与任务级分层，Profile default → project → task → 作者本次确认；每条目标有来源/用途/范围/证据/版本；模型修改仅为候选 |
| ADR-PROFILE-001 | Profile | Proposed | P0-T04 完成前 | P1-T01、P1-T06、P2-T01、P3-T01、P3-T02 | 穷尽 `long_serial`、`fanqie_short`、`zhihu_salt_short`；平台易变规则带来源/日期且作者可覆盖；共享能力合同，差异用数据表达 |
| ADR-CS-001 | CandidateSet | Proposed | P0-T05 完成前 | P1-T04、P2-T04、P2-T06、P2-T08、P3-T03 | 候选绑定 source/model/Skill/Profile/QualitySpec/hash；关键节点多候选、普通任务单候选；支持选择/混合/拒绝且保留父来源 |
| ADR-RI-001 | ReviewIssue | Proposed | P0-T05 完成前 | P1-T04、P2-T05、P2-T06、P2-T08、P3-T06 | issue 必须有位置、读感证据、原因、严重度、修订层级、最小影响范围和复核；分数不能代替证据；状态可关闭/重开 |
| ADR-PM-001 | PreferenceMemory | Proposed | P0-T06 完成前 | P1-T04、P2-T06、P2-T07、P2-T08、P5-T04 | 只接收作者明确选择/否决/改写/规则确认；追加式、范围化、带证据/强度/版本，可查看、编辑、撤销；模型自评和无操作无效 |
| ADR-AF-001 | Author Finalization | Proposed | P0-T06 完成前 | P1-T02、P2-T03、P2-T07、P2-T08、P3-T07、P5-T02 | 请求绑定 workspace、全部 base revision、candidate/artifact hash、一次性 nonce；复用 workspacechange durable batch 和 go-git 版本；跨介质失败用明确 roll-forward/补偿+receipt，不虚假宣称 ACID |

## 3. 补充 ADR 待办

| ADR ID | 对象 | 状态 | 决定期限 | 阻塞 Task | 建议方案 |
|---|---|---|---|---|---|
| ADR-PROJECTION-001 | SQLite/FTS driver 与投影 schema | Accepted | 2026-07-22 | P1-T03、P1-T07、P5-T03 | 选定 `modernc.org/sqlite` + `database/sql` 的纯 Go family；DB 仅为可重建投影，schema/rebuild/损坏恢复不得阻塞 open/edit；P1 仍须精确版本与匹配 `modernc.org/libc`、许可证/`govulncheck`、`CGO_ENABLED=0` 五平台（含 Windows amd64/未来 Tauri triples），并在 fresh activation/rebuild/损坏或外部内容不一致检查执行 `INSERT INTO <fts_table>(<fts_table>, rank) VALUES ('integrity-check', 1)` 的运行时 FTS5 一致性门禁；不引入 vector 或 Tauri 实现 |
| ADR-SSE-001 | Quality event envelope、持久状态与重连 | Proposed | P1-T05 开始前 | P1-T05、P2-T03、P2-T08、P5-T02 | domain event 保持 transport-neutral，App 转 `agent.Event`；SSE 只发稳定 ID/摘要；Run repository 为状态真源，Task snapshot/live 只恢复显示 |
| ADR-CAP-001 | Capability 与 Skill Manifest | Proposed | P2-T02 开始前 | P2-T02、P2-T04–P2-T06、P3-T04–P3-T06 | workflow 只依赖 Capability ID；manifest 记录来源/hash/许可/权限/schema/model/cost/eval；router 选最少必要实现并保存选择证据 |
| ADR-AUTO-001 | Automation/Batch 与 Harness 边界 | Proposed | P2-T03 开始前 | P2-T03、P2-T07、P3-T07 | Automation 只创建/继续 Job 或写待审 Artifact；`auto_write` 不对正式区生效；批次确认可上移粒度但不能取消 hash/revision 校验 |
| ADR-TAURI-001 | Tauri shell/sidecar/更新边界 | Deferred | P4-T03 开始前，且 P3-T08 必须 PASS | P4-T03–P4-T06、P5-T02、P5-T06 | React/Go Web 核心保持可独立运行；Tauri 只做 shell、sidecar 生命周期和 OS 集成；端口/健康/退出/更新/签名有独立状态机 |
| ADR-EVAL-001 | Quality Gate Manifest 与统计方法 | Proposed | P0-T09 完成前 | P2-T09、P3-T08、P5-T04 | 分层配对盲评；CI 下界 >0.50；样本量、事实/改稿/成本非劣容差由 Phase 0 实测冻结；发布 holdout 独立 |

## 4. 技术风险

| Risk ID | 风险 | 概率/影响 | 当前证据或触发条件 | 应对与退出条件 | Owner / 阻塞 |
|---|---|---|---|---|---|
| TECH-001 | 新逻辑继续堆入大文件，导致上游冲突和职责失控 | 高/高 | `ModeRouter.tsx` 1184、`WorkbenchShell.tsx` 976、`automation_app_service.go` 944、`App.tsx` 937 行 | 新增 `internal/quality/*` 与 `features/quality/*`；大文件只接薄 adapter/route；PR 检查行数和职责 | Tech Lead；P1-T05/P1-T06/P2-T08/P4-T01 |
| TECH-002 | Author Finalization 缺少公开多路径 API，循环写入会半提交 | 高/灾难 | `workspacechange.commitGroupOperationLocked` 当前私有，只服务 review/undo/redo | ADR-AF-001；新增具名 durable batch，故障点测试；无 `os.WriteFile` 直写正式区 | Backend；P2-T07 阻塞 |
| TECH-003 | SSE Task 为内存快照且慢订阅者可丢事件，误当状态真源会丢恢复信息 | 中/高 | `internal/app/task.go` 事件内存缓冲、慢订阅者丢弃 | Run repository 持久化状态/sequence；SSE 只通知；重连先 query 状态再订阅 | Backend；P1-T05/P2-T03 |
| TECH-004 | Checkpoint/receipt 的非原子落盘或跨介质提交造成状态不确定 | 中/高 | Agent checkpoint 与 Git/文件为不同持久介质 | 复用 atomic file/durable intent；receipt 明确 terminal/recovery 状态；故障注入 | Backend；P2-T03/P2-T07/P5-T02 |
| TECH-005 | SQLite driver 带来 CGO、Windows/Tauri 打包或许可问题 | 中/高 | 当前 `go.mod` 无 SQLite driver | ADR-PROJECTION-001 对 pure Go/CGO/许可/FTS 能力做决策；五平台 CI；不提前加依赖 | Backend/Release；P1-T03 |
| TECH-006 | Context Pack 过大、无限历史或全量 Skills 导致成本/质量下降 | 高/高 | 现有已有多个上下文 limit，Harness 会增加来源 | 每片段 source/purpose/hash/limit，总预算 >128KB 但明确可配；精准读取；真实消息测试 | Agent Lead；P2-T01/P5-T03 |
| TECH-007 | 状态/事件新增后 default 吞错，恢复到错误阶段 | 中/高 | Go/TS 状态面扩大 | 状态穷尽 switch、未知值硬错误、迁移测试、event schema version | Backend/Frontend；P2-T03/P2-T08 |
| TECH-008 | 单元测试通过 sleep/真实模型变慢或 flaky，违反 <1 秒 | 中/中 | 并发/SSE/恢复易写时间测试 | channel/barrier/假时钟；模型测试分离为 integration/eval；报告单项耗时 | QA；P0-T02 onward |
| TECH-009 | Tauri sidecar 端口、进程、升级或退出处理破坏现有 Web 运行 | 中/高 | Phase 4 新运行时边界 | P3 PASS 后再做；独立 ADR/矩阵；Web core 可单独运行；不可见 daemon 禁止 | Desktop Lead；P4-T03–P5-T02 |

## 5. 产品风险

| Risk ID | 风险 | 概率/影响 | 触发信号 | 应对与退出条件 | Owner / 阻塞 |
|---|---|---|---|---|---|
| PROD-001 | 流程表演替代正文质量 | 高/灾难 | Stage/Agent 增加但盲评、作者保留率不升 | 每 Stage 声明质量假设；无收益即删除/跳过；GOAL-001 Gate | Product/Eval；P2-T09/P3-T08 |
| PROD-002 | 三 Profile 被同一爽文模板扁平化 | 高/高 | 共享字段/硬规则覆盖专项目标 | Profile 独立合同与分层评测；总体平均不能掩盖单 Profile 失败 | Product/Editor；P3-T01/P3-T02/P5-T04 |
| PROD-003 | 工作流过重、成本高、作者等待长 | 高/高 | 普通段落也多候选/多 reviewer；收益递减 | CandidatePolicy 默认单候选；按需 reviewer；成本档位和 max_cost_ratio | Product/Agent；P2-T04–P3-T08 |
| PROD-004 | UI 暴露 DAG/Agent/技术术语，普通作者不会用 | 中/高 | 可用性测试需工程解释才能完成 | 人话状态、渐进披露；高级证据在运行中心；任务型可用性 Gate | Frontend/Product；P4-T01/P4-T06 |
| PROD-005 | 平台规则变化导致 Profile 快速过期 | 高/中 | 字数、审核、偏好来源日期变化 | 规则版本/来源/日期可见；作者覆盖；更新提示，不写死代码 | Product；P1-T01/P3-T01/P3-T02 |
| PROD-006 | Tauri 吸走质量闭环资源 | 中/高 | P3 未通过就开始桌面 UI/签名 | P3-T08 是 ADR-TAURI-001 前置硬门槛；桌面工作不得前移 | Product/Release；P4-T03 |

## 6. 质量评测风险

| Risk ID | 风险 | 概率/影响 | 触发信号 | 应对与退出条件 | Owner / 阻塞 |
|---|---|---|---|---|---|
| QUAL-001 | 调参集泄漏，Harness 对评测题过拟合 | 高/高 | 同一批样本反复调 prompt/Skill 后宣称胜出 | train/regression/holdout 分离；发布使用新 holdout | Eval Lead；P0-T07/P5-T04 |
| QUAL-002 | 样本太少或 reviewer 偏好造成假胜率 | 高/高 | 大 CI、评审冲突率高、题材单一 | P0 方差/功效分析；双盲双评审+第三人裁决；Profile/题材分层 | Eval Lead；P0-T09/P3-T08 |
| QUAL-003 | Reviewer 奖励投机，文本迎合 Rubric 而不像小说 | 中/高 | 自动分数升、人工阅读下降 | Reviewer 先复述故事；证据要求；人工主指标；跨题材 holdout | Editor/Eval；P2-T05/P5-T04 |
| QUAL-004 | 普通单轮基线被刻意削弱，比较不公平 | 中/灾难 | S arm 缺关键输入/模型更弱/模板事后改变 | 模板预注册；同模型/输入许可/预算；配置和 hash 锁定 | Eval Lead；P0-T07/P0-T09 |
| QUAL-005 | 多候选成本上升但没有选择收益 | 高/高 | min_candidate_gain 未达标 | 关键节点分层评测；无收益任务退回单候选 | Product/Eval；P2-T04/P3-T08 |
| QUAL-006 | 审稿 issue 空泛、误报或把所有问题路由到润色 | 高/高 | 定位/可执行率低，作者拒绝率高 | ReviewIssue 合同、分类 precision/actionability、Revision Router 穷尽 | Review Lead；P2-T05/P2-T06 |
| QUAL-007 | Provider/model 漂移使历史回归不可比 | 高/中 | 同 model name 行为/价格改变 | 记录 Provider/model 版本/日期/参数；新 cohort，不混合旧结果 | Eval/Agent；P5-T04 |
| QUAL-008 | PreferenceMemory 把一次选择固化成僵硬风格 | 中/高 | 新任务质量下降、偏好互相冲突 | 作用域/强度/证据、弱信号累计、作者查看/撤销、holdout 回归 | Product/Eval；P2-T07/P5-T04 |
| QUAL-009 | 把 P0-T09 离线 pilot 误写成产品 Harness 或 Phase 2/3/5 质量 PASS | 高/灾难 | runner 出现产品 API/SSE/UI/workspace 写入，或报告把 tuning/holdout/fixture 当质量结论 | 仅版本化离线 runner；tuning shakeout 后冻结 regression paired pilot；`release_holdout` 只保留 metadata/hash 且零调用/输出/盲包/评审/调优；P0-T09 只冻结未来样本量和非劣规则 | Eval/Tech Lead；P0-T09 |

## 7. Skills 风险

| Risk ID | 风险 | 概率/影响 | 触发信号 | 应对与退出条件 | Owner / 阻塞 |
|---|---|---|---|---|---|
| SKILL-001 | 虾评来源页面/下载地址变化或下线 | 高/中 | URL 失效、内容 hash 变化 | 原址、抓取时间、hash 和 source record；失败不静默换源 | Skill Lead；P0-T08/P3-T04 |
| SKILL-002 | 许可不明或与产品分发方式冲突 | 高/高 | 包内无 license、GPL/专有内容 | 默认原址安装、不复制进核心；发布前许可审计；不明确则不预置 | Legal/Release；P0-T08/P5-T05 |
| SKILL-003 | Skill 包含脚本/网络行为，普通子进程不能提供安全隔离 | 中/灾难 | manifest 需要 executable/network/file 权限 | 安装前权限提示；默认不执行未知代码；需执行时单独强沙箱 ADR | Security；P3-T04/P5-T05 |
| SKILL-004 | 更新时行为/hash 漂移导致质量或安全回归 | 高/高 | 同版本内容变化、权限扩大、评测下降 | hash 锁、版本记录、diff、权限重新确认、回归评测、一键回滚 | Skill Lead；P3-T05/P5-T01 |
| SKILL-005 | Capability 映射错误或输入输出不兼容 | 中/高 | router 输出无法验证/审稿错类 | 稳定 schema、adapter contract test、人工映射覆盖和评测 | Agent/Skill；P2-T02/P3-T05 |
| SKILL-006 | 全量 Skill 堆叠造成 prompt 冲突和成本失控 | 高/高 | 单次调用注入多个不相关全文 | Router 选择最少必要能力；记录选择原因；组合必须单独评测 | Agent Lead；P2-T02/P3-T06 |

## 8. 数据安全风险

| Risk ID | 风险 | 概率/影响 | 触发信号 | 应对与退出条件 | Owner / 阻塞 |
|---|---|---|---|---|---|
| DATA-001 | 出现 Markdown 与 DB/运行 JSON 两个正式真源 | 中/灾难 | index/Run 能覆盖正式文件或独立修改事实 | Schema 真源矩阵；projection 单向；静态写入路径审计 | Architect；P1-T02/P1-T03 |
| DATA-002 | 定稿半提交：正文已换、事实/版本未换 | 中/灾难 | 多路径/版本任一点失败 | ADR-AF-001、durable batch、preflight、compensation、receipt、故障注入 | Backend；P2-T07/P5-T02 |
| DATA-003 | 外部手改导致静默覆盖或下游继续使用旧输入 | 高/高 | base revision/hash 不匹配 | revision 拒绝；标记 invalidated；保留旧 artifact；作者选择重跑 | Backend; P1-T07/P2-T03 |
| DATA-004 | SQLite 损坏或 schema 漂移阻止项目打开 | 中/高 | DB open/migrate 失败 | 打开项目不依赖 DB；隔离损坏文件并全量 rebuild；一致性测试 | Backend；P1-T03/P5-T02 |
| DATA-005 | API Key、prompt、thinking 或用户正文进入日志/评测提交 | 中/灾难 | secret scan/大对象日志命中 | structured bounded logs、redaction、原始评测本地、提交 hash/聚合 | Security/Eval；P0-T07/P5-T05 |
| DATA-006 | Workspace/Profile/Skill 迁移覆盖用户内容 | 中/灾难 | in-place 改名/写入无预览备份 | preview、backup、临时路径、atomic switch、rollback receipt | Backend；P1-T02/P5-T01 |
| DATA-007 | 版本排除过宽丢 QualitySpec/Preference，过窄提交 run/index 噪音 | 高/高 | restore 后合同丢失或 repo 膨胀 | ADR-WS-001 精确矩阵；`versions/files.go` 正反测试 | Backend；P1-T02/P1-T07 |
| DATA-008 | Preference 撤销物理删除历史，审计不可解释 | 中/中 | 直接改写/删 JSON | 追加 revoke event；有效投影可重建；UI 显示当前/历史 | Backend/Product；P1-T04/P2-T08 |

## 9. 上游同步与发布风险

| Risk ID | 风险 | 概率/影响 | 触发信号 | 应对与退出条件 | Owner / 阻塞 |
|---|---|---|---|---|---|
| UP-001 | Denova 上游持续变化与质量模块冲突 | 高/高 | merge conflicts、接口漂移、回归 | quality package 增量、现有 package 不反向依赖；每 Phase 同步演练和核心特征测试 | Tech Lead；P5-T05 |
| UP-002 | 把本机/当前分支问题误标为 Windows 上游失败 | 中/高 | 无 upstream 双复现就 allowlist | 精确 test/signature/SHA/owner/expiry；环境缺失为 NOT-RUN | QA；P0-T09 onward |
| UP-003 | 当前机器缺 Go，后端门禁长期未运行 | 当前/高 | `go` 不在 PATH | 安装 `go.mod` 指定版本或用标准 CI；P0-T09 前必须在 Windows 实跑 | Environment Owner；P0-T02/P0-T09 |
| UP-004 | 新依赖破坏五平台或 release `CGO_ENABLED=0` | 中/高 | SQLite/Tauri/Skill 依赖引入 | ADR dependency matrix；五平台 build；必要时调整发行设计而非跳过平台 | Release；P1-T03/P4-T03/P5-T06 |
| UP-005 | README/CHANGELOG/package/tag/notes 版本不一致 | 中/高 | release script metadata validation 失败 | 使用现有 `build-github-release.sh`；单 release commit；tag 指向同 commit | Release；P5-T06 |
| UP-006 | 业务提交夹带规划、依赖、重构或无关用户修改 | 中/高 | diff 跨 Task/dirty baseline | 一 Task 一 commit、G0 scope audit、保留用户改动、必要时独立 worktree | Tech Lead；全部 Task |

## 10. 决策与风险治理流程

1. Task 开始前读取其阻塞 ADR；非 Accepted 则停止该 Task，不自行假设字段或接口。
2. 风险触发时记录实际证据、影响 Requirement/Task、临时保护和决定期限；“可能有问题”不能替代复现。
3. 风险降低只能在验证证据附上后从 Open 改为 Monitoring/Closed；代码合并本身不是关闭证据。
4. ADR 改变已发布文件/API/配置时，必须包含迁移、备份、回滚和兼容说明；Beta 可不兼容，但必须明确说明。
5. 任何试图改变 FD-001–FD-009 的提案都需要先修订最终融合方案并由用户审核，不能在普通实现 Goal 中完成。
6. 每个 Phase 退出由负责人复查本登记册：高/灾难风险没有 owner、缓解或验证证据时不得退出。
