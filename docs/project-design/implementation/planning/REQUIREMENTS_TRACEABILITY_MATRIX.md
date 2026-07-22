# 需求追踪矩阵

> 基线：`docs/project-design/final/小说写作工具-最终融合最优方案.md` v1.1。
> 规则：最终方案是需求真源；Kimi、WorkBuddy、竞品和虾评资料只提供论据、候选方法或来源证据。
> 本矩阵追踪到 `MASTER_DEVELOPMENT_PLAN.md` 与 `PHASE_0_DETAILED_PLAN.md` 的稳定 Task ID。

## 1. 优先级和状态定义

| 优先级 | 含义 | 进入阶段的规则 |
|---|---|---|
| P0 | 宪法级、安全级或产品目标级，不满足即不能宣称 Harness 成立 | 必须在 Phase 0 有决策/测试承接，并在首次实现阶段有具体 Task |
| P1 | Quality Harness MVP 或 v1 发布必需 | 必须有明确 Phase、Task、验收证据；不得以“后续优化”悬空 |
| P2 | 有证据才立项的延期候选 | 可以只有触发条件，不得混入 MVP 关键路径 |

状态均表示“计划覆盖”，不表示代码已经完成。

## 2. 正向追踪：需求 → Phase / Task

| Requirement ID | P | 需求与验收含义 | 最终方案来源 | 承接 Phase / Task | 主要验证证据 |
|---|---:|---|---|---|---|
| GOAL-001 | P0 | 产品目标是提高优秀网文产出概率；不得以自动化、Agent 数或流程完成率替代；P0-T09 可用评测专用离线 H 取得真实配对证据，但不形成产品状态机 | 1、3、6.4、17.3 | P0-T07、P0-T09、P2-T09、P3-T08、P5-T04 | 同任务同模型盲评、作者保留/改稿、成本与错误率 |
| BASE-001 | P0 | 当前项目是唯一仓库，固定 Denova 上游基线并可重复核验 | 2、15.1、16 Phase 0 | P0-T01、P0-T09、P5-T05 | branch/HEAD/remotes/merge-base、差异清单 |
| ARCH-001 | P0 | Denova 是唯一底座；模块化单体演进，不重写前后端和 Agent Runtime | 2、5、19 ADR-001 | P0-T01、P0-T02、P1-T05、P2-T02、P5-T05 | 依赖图、源码 diff、核心回归、上游同步冲突量 |
| MODE-001 | P0 | 写作/游戏共通能力保持边界；共享一级菜单不自动切换模式，且只有一个 active | 项目约定、5、12 | P0-T02、P1-T06、P2-T08、P3-T03、P4-T01、P4-T06 | store/navigation 组件测试、双模式人工矩阵 |
| DATA-001 | P0 | 正式正文、大纲、人物和设定只有文件真源；数据库/会话/模型记忆不是正式事实 | 3、7.2、19 ADR-002 | P0-T03、P0-T06、P1-T02、P1-T04、P2-T07、P5-T01 | Schema、写入路径审计、删除 DB 后事实不丢 |
| DATA-002 | P1 | SQLite/FTS 是可删除、可全量重建的投影；向量检索需证据触发 | 4.3、7.2、20 | P0-T09、P1-T03、P1-T07、P5-T02、P5-T03 | rebuild 等价性、失效检测、索引损坏演练 |
| DATA-003 | P0 | 作者手改 Markdown 合法；系统标记下游失效而非拒绝打开或静默覆盖 | 7.3、14.1、17.1 | P0-T03、P1-T02、P1-T03、P1-T07、P2-T03 | hash 变化、invalidated 事件、手改恢复集成测试 |
| PROFILE-001 | P0 | `long_serial` 有独立目标、产物、能力、候选和审稿策略 | 8.2 | P0-T04、P0-T07、P1-T01、P2-T01–P2-T09、P5-T04 | 合同样例、连续多章闭环、长篇盲评 |
| PROFILE-002 | P0 | `fanqie_short` 不套长篇规则，强调开篇清晰、卖点、升级和结局兑现 | 8.3 | P0-T04、P0-T07、P1-T01、P3-T01、P3-T03、P3-T08、P5-T04 | Profile 合同、多题材端到端、专项盲评 |
| PROFILE-003 | P0 | `zhihu_salt_short` 不套长篇规则，强调叙事声音、因果、信息压力、反转证据和闭环 | 8.4 | P0-T04、P0-T07、P1-T01、P3-T02、P3-T03、P3-T08、P5-T04 | Profile 合同、多题材端到端、专项盲评 |
| QS-001 | P0 | QualitySpec 是作者可读、可确认、可版本化的作品/任务质量合同，不是固定评分表 | 6.3、17.1/17.2 | P0-T04、P1-T01、P1-T06、P2-T01、P2-T03、P3-T01、P3-T02 | Schema、合并规则、确认 UI、版本与来源 |
| HARNESS-001 | P0 | Harness 围绕质量闭环；Go 管确定状态，模型只处理语义任务 | 6.4、9、19 ADR-003 | P0-T05、P0-T06、P2-T03–P2-T07、P3-T07 | 状态转换测试、结构化产物校验、无 Agent 自定稿 |
| HARNESS-002 | P1 | CandidateSet 保存一个或多个候选、来源、比较、选择/混合和 hash；普通任务默认单候选 | 6.7、9.3 | P0-T05、P1-T04、P2-T04、P2-T06、P2-T08、P3-T03 | 生命周期测试、来源链、关键节点收益评测 |
| HARNESS-003 | P1 | Stage 可检查点、取消、恢复、安全重跑；输入变化使下游失效但不静默删除 | 9.4/9.5、14.2 | P1-T05、P2-T03、P2-T09、P3-T07、P5-T02 | crash/reconnect/idempotency/invalidation 集成测试 |
| REVIEW-001 | P1 | ReviewIssue 必须可定位、有读感证据、原因、修订层级和最小影响范围 | 6.8、9.3、17.2/17.3 | P0-T05、P1-T04、P2-T05、P2-T06、P2-T08、P3-T06 | Schema、定位 UI、误报/漏报/采纳率 |
| REVIEW-002 | P0 | Writer、Reviewer、比较者使用全新上下文；Reviewer 不看 Writer thinking/self-review | 6.6、10.1、17.1 | P0-T02、P2-T01、P2-T04、P2-T05、P2-T09 | 真实消息装配断言、Source Manifest、上下文账本 |
| REVISION-001 | P1 | Revision Router 按 issue 类型选能力，修订后复审，不用统一润色处理全部问题 | 6.8、9.3、17.1 | P0-T05、P2-T02、P2-T05、P2-T06、P3-T06 | 路由穷尽测试、issue closed/reopened、修订前后盲评 |
| AUTH-001 | P0 | Author Finalization 是不可绕过的唯一正式定稿边界，确认绑定 workspace/revision/candidate hash | 3、6.10、9.2、19 ADR-004 | P0-T06、P1-T02、P2-T03、P2-T07、P2-T08、P3-T07、P5-T02 | 缺确认/旧 hash/重复 nonce 拒绝、原子写入故障注入 |
| AUTH-002 | P0 | AI 不自动覆盖正文/设定，不自动提交 Git；事实同步只能是待审候选 | 3、9.2/9.4、20 | P0-T02、P0-T06、P2-T05、P2-T07、P3-T07、P5-T01 | 全写入路径审计、Automation/Agent 负向测试 |
| PREF-001 | P1 | PreferenceMemory 只接收作者确认的选择、否决、改写和规则；可解释、可撤销、不过拟合 | 6.9、17.1/17.2、19 ADR-009 | P0-T06、P1-T04、P2-T06、P2-T07、P2-T08、P5-T04 | 信号来源测试、撤销 journal、盲评/过拟合回归 |
| CTX-001 | P0 | 每个模型片段有来源、用途、hash 和高但明确的硬上限；不得注入无限历史/日志/全文 | 项目上下文约定、6.5、14.3 | P0-T02、P1-T01、P2-T01、P2-T04、P2-T05、P5-T03 | Context Ledger、真实消息大小、截断/拒绝策略 |
| CTX-002 | P0 | 展示历史与模型上下文分离；thinking、工具卡片和日志预览不默认回填 | 项目上下文约定、10.1 | P0-T02、P2-T01、P2-T09、P5-T02 | session/display/context 特征测试、恢复消息装配 |
| SSE-001 | P0 | 继续使用现有 SSE；事件载荷只含稳定 ID/摘要，重连不重复执行或丢完成事件 | 5、13.2、17.1 | P0-T02、P1-T05、P2-T03、P2-T08、P5-T02 | snapshot/live/reconnect/idempotency 测试 |
| SKILL-001 | P0 | 虾评是第一优先级来源；支持发现、详情、原址下载、安装和登记 | 11.3、19 ADR-010 | P0-T08、P0-T08A、P2-T02、P3-T04 | 原始 URL/hash/许可、现有 installer 复用测试 |
| SKILL-002 | P0 | Harness 依赖 Capability ID，不依赖具体 Skill 名；按 QualitySpec 选择最少必要能力 | 6.6、11.1、19 ADR-007 | P0-T08、P0-T08A、P2-T02、P2-T04–P2-T06、P3-T05、P3-T06 | registry/router 契约、无全量注入、能力替换测试 |
| SKILL-003 | P1 | Skill Manifest 记录版本、来源、许可、hash、权限、模型要求、成本和评测 | 11.2/11.3 | P0-T08、P0-T08A、P3-T04、P3-T05、P5-T05 | manifest 校验、权限 UI、许可审计 |
| SKILL-004 | P1 | Skills 可评测、更新比较、hash 锁定和回滚；更新后不得静默改变行为 | 11.3、18 | P0-T08、P0-T08A、P3-T05、P3-T08、P5-T01、P5-T04 | A/B、update diff、rollback、回归 cohort |
| AUTO-001 | P0 | Automation/批量运行最多产出待审内容；`auto_write` 不得对 Harness 正式区生效 | 9.4、12、19 ADR-004 | P0-T02、P0-T06、P2-T03、P2-T07、P3-T07、P5-T02 | 权限负向测试、批次待审、逐项确认 |
| FE-001 | P1 | 前端继续 React、TipTap、shadcn 和既有 API client/query 模式，不另建 UI 技术栈 | 5、12 | P0-T01、P1-T05、P1-T06、P2-T08、P3-T03、P4-T01 | package diff、组件复用、构建 |
| FE-002 | P1 | 全部用户可见交互中英双语，亮/暗主题，宽/窄屏、长文本和空数据适配 | 项目前端规范、12、15 | P0-T02、P1-T06、P2-T08、P3-T03、P4-T01、P4-T06、P5-T06 | i18n key check、组件测试、页面验证矩阵 |
| FE-003 | P1 | 作者能理解 QualitySpec、候选、ReviewIssue、来源、差异、定稿与恢复，无需理解 DAG/Git/Agent | 12、17.2 | P1-T06、P2-T08、P3-T03、P4-T01、P4-T02、P4-T06 | 可用性脚本、任务完成观察、空/错误状态 |
| TAURI-001 | P1 | Tauri 是 v1 后期发行形态，不能阻塞前期质量验证 | 12.4、15.2、19 ADR-011 | P0-T01、P3-T08、P4-T03–P4-T06、P5-T02、P5-T06 | P3 前无 Tauri 依赖、sidecar/安装/退出矩阵 |
| EVAL-001 | P0 | 同任务、同模型、可比成本下做普通单轮 vs Harness 人工盲评；P0-T09 的 H 仅为评测专用离线 runner，模型分数不是发布结论 | 3、17.3 | P0-T07、P0-T08A、P0-T09、P2-T09、P3-T08、P5-T04 | 随机盲包、双评审、CI、证据文本 |
| EVAL-002 | P1 | 三 Profile 分层评测，记录首轮可用、保留/改稿、候选收益、审稿准确、事实错误和成本；P0-T09 只为后续规则提供隔离的真实 pilot 证据 | 17.3 | P0-T07、P0-T08A、P0-T09、P2-T09、P3-T08、P4-T02、P5-T03、P5-T04 | corpus manifest、指标聚合、质量 Gate Manifest |
| SAFE-001 | P0 | 迁移、覆盖、定稿前有预览/版本/备份/回滚；崩溃不能留下半提交 | 14.1、17.1 | P0-T02、P0-T03、P0-T06、P1-T02、P2-T07、P5-T01、P5-T02 | 故障注入、restore receipt、备份恢复演练 |
| SAFE-002 | P0 | API Key 不进作品目录/日志；第三方来源和权限可见；未知代码不被称作沙箱 | 10.2、11.2、14.1 | P0-T01、P0-T08、P0-T08A、P2-T02、P3-T04、P4-T05、P5-T05 | secret scan、权限 manifest、静态安全审计 |
| REL-001 | P1 | goroutine recover、错误日志有上下文、LLM 无写死超时、状态可恢复 | 项目代码约定、14.2/14.5 | P0-T02、P1-T05、P2-T03、P2-T09、P5-T02、P5-T03 | panic/recovery、日志字段、取消/重试分类测试 |
| UPSTREAM-001 | P0 | 保持增量模块和最小侵入，定期同步 upstream，并区分上游失败与新增回归 | 18、19 ADR-001 | P0-T01、P0-T02、P0-T09、P5-T05 | 双 SHA 复现、allowlist、同步演练、冲突统计 |
| RELEASE-001 | P1 | 发布同步前端版本、CHANGELOG、README/README.en、tag 和双语 Release notes | 项目约定、Phase 5 | P4-T06、P5-T05、P5-T06、P5-T07 | release script、tag/version 文本一致、安装/回滚包 |
| DEFER-001 | P2 | 向量检索只有 FTS/精确读取在真实评测中不足才立项 | 4.3、15.3、20 | P1-T03、P5-T03 | FTS 失败案例、收益/复杂度 ADR；否则不实施 |
| DEFER-002 | P2 | 独立 Worker、强沙箱、云同步、协作和游戏扩展均需独立证据与新 Goal | 15.3、20 | P3-T08、P5-T05 | MVP/v1 scope audit；本计划不含实现 Task |

## 3. 反向追踪：核心任务 → 需求

下表确保不存在无法追溯到需求的核心开发任务。

| Task ID | 主要 Requirement ID | 交付证据 |
|---|---|---|
| P0-T01 | BASE-001、ARCH-001、SAFE-002、UPSTREAM-001、TAURI-001 | 工程基线、路径和来源矩阵 |
| P0-T02 | ARCH-001、MODE-001、REVIEW-002、CTX-001、CTX-002、SSE-001、AUTH-002、AUTO-001、SAFE-001、REL-001、UPSTREAM-001 | 核心特征测试 |
| P0-T03 | DATA-001、DATA-003、SAFE-001 | Workspace Schema ADR |
| P0-T04 | PROFILE-001、PROFILE-002、PROFILE-003、QS-001 | Profile/QualitySpec ADR 与 Schema |
| P0-T05 | HARNESS-001、HARNESS-002、REVIEW-001、REVISION-001 | CandidateSet/ReviewIssue ADR |
| P0-T06 | DATA-001、AUTH-001、AUTH-002、PREF-001、AUTO-001、SAFE-001 | PreferenceMemory/Finalization ADR |
| P0-T07 | GOAL-001、PROFILE-001–003、EVAL-001、EVAL-002 | 三 Profile corpus 与单轮基线 |
| P0-T08 | SKILL-001、SKILL-002、SKILL-003、SKILL-004、SAFE-002 | 虾评 catalog、Capability 初映射和红线 |
| P0-T08A | SKILL-001、SKILL-002、SKILL-003、SKILL-004、EVAL-001、EVAL-002、SAFE-002 | 用户批准的公开全目录快照、严格证据合同、合成 fixture 与双通道短名单；依赖 P0-T08 并阻塞 P0-T09 的 Skill 证据闭环，不改写 P0-T08 历史 |
| P0-T09 | GOAL-001、DATA-002、EVAL-001、EVAL-002、UPSTREAM-001 | 仅评测专用离线 H runner 的真实 regression paired pilot、未来 Gate 规则、Phase 0 报告、allowlist；不创建产品 Harness 状态机 |
| P1-T01 | PROFILE-001–003、QS-001、CTX-001 | Profile registry 与 QualitySpec 合同实现 |
| P1-T02 | DATA-001、DATA-003、AUTH-001、SAFE-001 | Schema adapter 与迁移回滚 |
| P1-T03 | DATA-002、DATA-003、DEFER-001 | FTS 投影与重建 |
| P1-T04 | DATA-001、HARNESS-002、REVIEW-001、PREF-001 | 文件仓储与版本迁移 |
| P1-T05 | ARCH-001、HARNESS-003、SSE-001、FE-001、REL-001 | API/SSE 事件合同 |
| P1-T06 | MODE-001、QS-001、FE-001、FE-002、FE-003 | 作品主页/规划中心骨架 |
| P1-T07 | DATA-002、DATA-003、MODE-001、SAFE-001 | Phase 1 集成门禁 |
| P2-T01 | PROFILE-001、QS-001、REVIEW-002、CTX-001、CTX-002 | Context Pack Builder |
| P2-T02 | ARCH-001、SKILL-001、SKILL-002、REVISION-001、SAFE-002 | Capability Registry/Router |
| P2-T03 | HARNESS-001、HARNESS-003、AUTH-001、AUTO-001、SSE-001、REL-001 | 状态机、检查点、恢复和失效 |
| P2-T04 | HARNESS-002、REVIEW-002、CTX-001、SKILL-002 | Writer 与候选生成 |
| P2-T05 | REVIEW-001、REVIEW-002、REVISION-001、AUTH-002、SKILL-002 | 独立审稿与 ReviewIssue |
| P2-T06 | HARNESS-002、REVIEW-001、REVISION-001、PREF-001、SKILL-002 | 修订路由与候选选择 |
| P2-T07 | DATA-001、AUTH-001、AUTH-002、PREF-001、AUTO-001、SAFE-001 | 同步候选与 Author Finalization |
| P2-T08 | MODE-001、HARNESS-002、REVIEW-001、AUTH-001、PREF-001、FE-001–003、SSE-001 | 长篇质量 UI |
| P2-T09 | GOAL-001、PROFILE-001、REVIEW-002、HARNESS-003、EVAL-001、EVAL-002、REL-001 | 长篇真实闭环和盲评 |
| P3-T01 | PROFILE-002、QS-001 | 番茄 Profile 实现 |
| P3-T02 | PROFILE-003、QS-001 | 盐选 Profile 实现 |
| P3-T03 | MODE-001、PROFILE-002、PROFILE-003、HARNESS-002、FE-001–003 | 短篇工作台与导出 |
| P3-T04 | SKILL-001、SKILL-003、SAFE-002 | 虾评发现/下载/登记/权限 |
| P3-T05 | SKILL-002、SKILL-003、SKILL-004 | Capability 映射、评测、更新、回滚 |
| P3-T06 | REVIEW-001、REVISION-001、SKILL-002、SKILL-004 | 网文能力补齐 |
| P3-T07 | HARNESS-001、HARNESS-003、AUTH-001、AUTH-002、AUTO-001 | 批量待审与逐项定稿 |
| P3-T08 | GOAL-001、PROFILE-002、PROFILE-003、EVAL-001、EVAL-002、SKILL-004、DEFER-002 | 三 Profile MVP 门禁 |
| P4-T01 | MODE-001、FE-001、FE-002、FE-003 | 体验重构与职责拆分 |
| P4-T02 | FE-003、EVAL-002、PREF-001、SKILL-004 | 质量洞察/评测/运行中心 |
| P4-T03 | TAURI-001、REL-001 | Tauri shell 与 sidecar |
| P4-T04 | TAURI-001、FE-002、RELEASE-001 | Windows 安装与系统集成 |
| P4-T05 | TAURI-001、SAFE-002、REL-001 | 桌面权限与异常矩阵 |
| P4-T06 | MODE-001、FE-002、FE-003、TAURI-001、RELEASE-001 | v1 RC 可用性门禁 |
| P5-T01 | DATA-001、DATA-003、SKILL-004、SAFE-001 | 迁移/备份/回滚演练 |
| P5-T02 | DATA-002、HARNESS-003、AUTH-001、AUTO-001、SAFE-001、REL-001、TAURI-001 | 故障注入与恢复 |
| P5-T03 | CTX-001、DATA-002、EVAL-002、DEFER-001、REL-001 | 性能/上下文/成本门禁 |
| P5-T04 | GOAL-001、PROFILE-001–003、PREF-001、SKILL-004、EVAL-001、EVAL-002 | 三 Profile 发布盲评 |
| P5-T05 | ARCH-001、SAFE-002、SKILL-003、UPSTREAM-001、RELEASE-001、DEFER-002 | 上游/依赖/许可/安全审计 |
| P5-T06 | FE-002、TAURI-001、RELEASE-001 | 双语版本资料、tag、Beta 包 |
| P5-T07 | GOAL-001、RELEASE-001 | Beta 反馈和发布/回滚决定 |

## 4. P0/P1 覆盖审计

- P0 Requirement：24 项，全部至少有一个 Phase 0 决策或特征测试 Task，并有首次实现/验证 Task。
- P1 Requirement：15 项，全部映射到明确 Phase 与 Task；没有“后续优化”或无 Task 承接项。
- P2 Requirement：2 项，仅记录证据触发和 scope audit；没有提前实现任务。
- 核心 Task：P0-T01 至 P5-T07 共 46 项，全部在反向矩阵中至少关联一个 Requirement ID。

## 5. 变更控制规则

1. 新的 P0/P1 需求必须先取得 Requirement ID、最终方案依据、验收证据和至少一个 Task ID。
2. 新的核心 Task 必须在本矩阵登记反向来源；无法追溯的 Task 不得进入开发。
3. Task 删除或延期时必须同时检查其承接需求；若导致 P0/P1 无承接，变更不通过。
4. Requirement 的验收口径只能由已批准 ADR 或最终融合方案修订，不得在实现 PR 中私自降低。
5. Profile 平台规则变化只更新版本化来源和默认值，不改变 Requirement ID 的稳定产品语义。
