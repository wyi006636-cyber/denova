# 小说写作工具 — 项目设计方案执行摘要

> **交付物**：完整项目设计方案（PRD + 架构设计 + 任务分解）
> **交付日期**：2026-07-20
> **团队**：齐活林（主理人）+ 许清楚（产品经理）+ 高见远（架构师）

---

## TL;DR

基于 denova 二次开发，融合 webnovel-writer 方法论、novelforge-agent Agent 隔离架构、虾评 11 个写作 skills，打造**以 Agent 编排为核心的 AI 长篇小说创作工作站**。完整设计方案已产出，包含 PRD（1189 行）+ 架构设计（约 2000 行），覆盖架构设计、模块划分、技术选型、实施路径，Phase 1 MVP 任务已细化到可执行粒度。

---

## 一、核心价值主张

**三大不可替代性**：

1. **唯一的「Agent 编排 + 方法论 + 架构安全」三位一体平台** —— denova 有架构无方法论，webnovel-writer 有方法论无架构，novelforge-agent 有安全机制无界面，本产品将三者融合
2. **可插拔的 Harness Agent 技能生态** —— 虾评 11 个 skills 抽象为标准 Agent 能力插槽，按需启用/组合/自定义
3. **可视化全流程创作操作系统** —— 构思→设定→大纲→章节→审校→发布，一个界面完成

---

## 二、架构设计要点

### 技术栈
- **后端**：保留 denova Go + 引入 SQLite（modernc.org/sqlite 纯 Go 无 CGO）+ chi 路由 + gorilla/websocket + go-git
- **前端**：保留 Vite + TypeScript，重构为 Shadcn UI + Radix + Tailwind + Zustand + Tiptap + Recharts/D3
- **AI**：OpenAI 兼容接口 + 多模型管理 + Embedding/Rerank/BM25 三层回退
- **部署**：Tauri 桌面应用（主要）+ Docker（团队）

### 核心创新：Harness Agent 系统
- **5 个 Agent**：Orchestrator（主控）+ Writer/Reviewer/Data（隔离子进程）+ Context（主进程协程）
- **三层隔离**：上下文隔离 + 进程隔离（exec.Command 启动独立 agent-worker 二进制）+ 封包隔离（SHA-256 验证）
- **9 步流水线**：预检 → 刷新契约 → 生成任务书 → 起草正文 → 多维审查 → 润色终检 → 事实提取 → 章节提交 → 章节备份
- **断点续跑**：每步写 checkpoint.json，失败后从断点恢复

### 关键技术决策
| 决策 | 方案 | 理由 |
|------|------|------|
| Agent 隔离 | exec.Command 启动独立 Go 子进程 | goroutine 共享内存无法物理隔离，子进程启动开销 10-50ms 相比 AI 调用(10-60s)可忽略 |
| SQLite 选型 | modernc.org/sqlite（纯 Go） | 无需 CGO，Tauri 跨平台编译零配置 |
| Git 操作 | go-git/go-git（纯 Go） | 不依赖系统 git 二进制 |
| 不需要 Python | Skills 提示词以 YAML 存储 | Go 后端 SkillExecutor 加载执行 |

---

## 三、模块划分

| 模块 | 职责 | 来源 |
|------|------|------|
| 创作工作台 | Tiptap 编辑器、章节树、上下文面板、进度追踪 | denova + 重构 |
| 资料库系统 | 8 类资产（角色/世界观/地点/势力/规则/物品/伏笔/时间线） | denova + webnovel-writer |
| Harness Agent 系统 | 5 Agent + 三层隔离 + 9 步流水线 + 断点续跑 | novelforge-agent + 新增 |
| 一致性保障系统 | 6 层防护（Story System + 龙骨 + 封包 + 事实追踪 + RAG + 证据分级） | 三项目融合 |
| 审查与质量系统 | 11 维审查 + Blocking/Warning 机制 | webnovel-writer + 虾评 |
| 版本管理系统 | 5 层版本（实时/章节/里程碑/Agent 快照/备份） | denova 增强 |
| 可视化 Dashboard | 8 个面板（实体图谱/追读力/质量雷达/伏笔时间线等） | webnovel-writer + 新增 |
| 题材模板系统 | 37 网文题材 + 自定义 | webnovel-writer |

---

## 四、实施路径

| Phase | 目标 | 工作量 | 关键交付 |
|-------|------|--------|---------|
| Phase 1 MVP | 跑通核心写作流程 + Harness Agent 基础 | 27 天 | denova 适配 + 工作台 + 资料库 + 9 步流水线 + 5 维审查 + 版本管理 |
| Phase 2 方法论整合 | 写作质量达到"可连载"水平 | 18 天 | Story System + 封包白名单 + 追读力 + 11 维审查 + 6 核心 skills + 去 AI 味 + RAG + 37 模板 |
| Phase 3 前端重构 | 达到一线 SaaS 美观度 | 10 天 | Shadcn 设计系统 + 工作台重构 + Agent 编排台 + Dashboard + 资料库重构 |
| Phase 4 增强功能 | 扩展产品边界 | 15 天 | 游戏模式 + 视频转化 + 多用户协作 + 移动端 + 番茄策略 + Tauri 打包 |
| **合计** | | **约 80 人天**（含缓冲） | |

**Phase 1 任务分解**（已细化到可执行）：
- T01 项目基础设施（3 天）
- T02 后端核心服务层（5 天）
- T03 Harness Agent 系统 + 9 步流水线（8 天）← 核心难点
- T04 前端创作工作台（6 天）← 可与 T02 并行
- T05 版本管理 + WebSocket + Skills + 集成测试（5 天）

---

## 五、待老板确认的关键决策点

### 来自 PRD（8 个）

| # | 问题 | 建议倾向 |
|---|------|---------|
| Q1 | 目标用户优先级：网文连载作者 vs 转型写作者 | 优先连载作者 |
| Q2 | 游戏模式（RPG）是否保留？ | 保留但降为 P2 |
| Q3 | 商业模式：开源 vs 闭源 | 核心开源 + 高级能力闭源 |
| Q4 | AI 模型依赖策略：是否支持本地模型优先？ | 本地草稿 + 云端审查混合 |
| Q5 | 多用户协作优先级 | P2 后期（Phase 4） |
| Q6 | 虾评 skills 获取方式：完整提示词能否获取？ | 需确认，影响 Phase 2 工作量 ±30% |
| Q7 | denova 二开法律合规（Apache-2.0） | 建议法务确认 |
| Q8 | 前端技术栈替换风险（Shadcn UI 重写） | 建议接受 |

### 来自架构设计（4 个）

| # | 问题 | 建议 |
|---|------|------|
| A1 | Agent 隔离子进程方案是否接受？ | 需编译独立 agent-worker 二进制，但这是真正隔离的唯一可靠方案 |
| A2 | Tauri 打包优先级 | 建议 Phase 1 先本地 Web 服务开发，Phase 3/4 再 Tauri 打包 |
| A3 | SQLite 引入是否接受？ | denova 原无数据库，但 50 万字+时文件检索会慢，SQLite 嵌入式零部署成本 |
| A4 | Go 版本：PRD 写 1.26.5+，架构建议 1.22+ 即可 | 采用架构建议，降低用户安装门槛 |

---

## 六、交付文件清单

| # | 文件 | 内容 | 行数 |
|---|------|------|------|
| 1 | `deliverables/software-company/novel-writer-PRD.md` | 完整 PRD（10 章节 + 2 附录） | 1189 |
| 2 | `deliverables/software-company/novel-writer-architecture.md` | 系统架构设计（8 章节） | ~2000 |
| 3 | `deliverables/software-company/docs/sequence-diagram.mermaid` | 9 步流水线时序图等 | — |
| 4 | `deliverables/software-company/docs/class-diagram.mermaid` | 核心类图 | — |
| 5 | `overview.md` | 本执行摘要 | — |

---

## 七、下一步建议

1. **审阅 PRD 和架构设计文档**，重点关注 12 个待确认决策点（PRD 8 个 + 架构 4 个）
2. **优先确认 Q6（虾评 skills 获取方式）**：这直接影响 Phase 2 工作量，如无法获取完整提示词需自写，工作量增加约 30%
3. **确认 Q3（开源/闭源）和 Q7（法律合规）**：影响项目启动和 License 隔离架构
4. **决策后启动 Phase 1 MVP 开发**：调度工程师寇豆码按 T01-T05 任务列表开工
5. **Phase 1 开发期间**：可并行准备虾评 skills 的完整提示词研究，为 Phase 2 做准备
