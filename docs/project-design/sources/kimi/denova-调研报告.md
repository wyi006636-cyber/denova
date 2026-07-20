# Denova 二次开发调研报告

> 调研对象：https://github.com/alfredxw/denova
> 调研时间：2026-07-20 ｜ 数据来源：GitHub REST API、raw.githubusercontent.com 源码直读、README/DESIGN/AGENTS/CHANGELOG 文档、README 界面截图
> 标注约定：✅ = 已核实（有源码/文档/API 证据）；⚠️ = 部分核实或推断；❓ = 未查到/不确定

---

## 0. 一句话结论

Denova 是一个**仅 2 个月历史但完成度很高的 Go + React 单体 AI 创作平台**（v0.3.0 Beta，503 stars），双模式（小说写作 IDE + 互动文字冒险），**无数据库、纯文件系统工作区**，基于 CloudWeGo Eino ADK 的 Deep Agent 做 LLM 编排，上下文工程和工作区变更账本是其最突出的架构资产，代码规范严格、测试覆盖率高、迭代极快，**适合作为二次开发基础**，主要风险是 Beta 期不兼容变更频繁、作者个人项目（bus factor = 1）。

---

## 1. 项目定位与核心功能 ✅

**定位**（README 原文）："面向小说创作与 AI 角色扮演游戏的 AI 创作平台，把写作 IDE、互动故事、结构化资料库、Agent 工具调用、图像生成、自动化和本地版本管理放在同一个项目工作区里，让创作过程可以反复迭代、回溯和沉淀。"

- **双并列工作台**：
  - **写作模式**：小说生产线 —— 构思、设定、大纲、章节组细纲、正文、进度追踪；
  - **游戏模式**：可游玩的互动文字冒险 —— 玩家输入、剧情分支、回合历史、Actor State、故事线切换、内置"故事导演"（全屏导演台：目标/压力/代价/事件卡包/TRPG d20 检定）。
- **单机本地应用形态**：本地跑 Go 后端（:8080），浏览器访问；支持局域网/远程访问（带账号密码）、PWA 手机使用、Caddy/Nginx 反代；Windows/macOS/Linux 全平台，单二进制 Release 内嵌前端。
- **非 SaaS、无账号体系、无云依赖**：模型走用户自配的 OpenAI 兼容接口（DeepSeek 为默认示例）。
- 可从零创作，也可**导入已有小说**做同人/改编/续写，或**导入 AI 酒馆（SillyTavern 系）角色卡**快速开互动冒险。
- 商业化：纯开源（Apache-2.0）+ 赞助（"给项目冲点 token"）+ Discord 社区；无付费功能。

---

## 2. 技术栈 ✅

### 后端（Go，513 个 .go 文件）
| 关注点 | 选型 |
|---|---|
| 语言/运行时 | Go 1.26.5，单二进制 |
| HTTP 框架 | CloudWeGo **Hertz** v0.10.5 |
| LLM 编排 | CloudWeGo **Eino** v0.9.9 + eino-ext（`adk` Deep Agent、`adk/backend/local`、`components/model/openai`、`middlewares/filesystem`、`middlewares/skill`、DuckDuckGo 搜索工具）|
| LLM 协议 | **OpenAI 兼容接口**（openai-go/v3 + eino openai 组件），`OPENAI_API_KEY / OPENAI_BASE_URL / OPENAI_MODEL` 环境变量可覆盖 |
| 图像生成 | OpenAI 兼容图像模型（`OPENAI_IMAGE_*`，示例 gpt-image-1）|
| 版本控制 | **go-git v5**（纯 Go 本地 Git，无外部 git 依赖）|
| 配置 | pelletier/go-toml（config.toml，四层优先级：内置默认 < 全局 < 用户级 < 工作区级 < 环境变量）|
| Diff | sergi/go-diff |
| 小说导入 | goquery（HTML 抓取）+ cavaliergopher/grab（下载）+ 自实现编码探测 text_decode |
| 搜索 | 外部依赖 **ripgrep**（运行要求）+ lithammer/fuzzysearch（资料库索引模糊匹配）|
| **数据库** | **无**。全部为文件系统：Markdown 正文 + JSON 状态 + TOML 配置 |

### 前端（web/，552 个 .ts/.tsx 文件）
| 关注点 | 选型 |
|---|---|
| 框架 | **React 19 + TypeScript 6 + Vite 8** |
| 编辑器 | **TipTap 3**（富文本 Markdown 写作画布，@tiptap/markdown 序列化 + character-count/image/placeholder/table 扩展）+ **Monaco Editor**（@monaco-editor/react 在依赖中，具体用途未逐一核实 ⚠️，疑似用于 Skill/prompt 源码编辑）|
| UI 组件 | **shadcn/ui + Radix UI + Tailwind CSS 4**（tailwindcss/vite 插件），lucide 图标，cmdk 命令面板，sonner toast |
| 状态管理 | **zustand**（仅本地 UI 状态：`web/src/stores/workspace-store.ts` 注释明确"仅保存本地界面状态，不存放服务端数据"）+ **@tanstack/react-query** |
| AI 对话流 | **Vercel AI SDK**（`ai` v7 + `@ai-sdk/react` useChat），自定义 `AgentChatTransport` 对接后端 SSE |
| 其他 | i18next/react-i18next（中英双语）、next-themes（深浅主题）、react-virtuoso（虚拟列表）、dnd-kit（拖拽）、react-resizable-panels、motion（动效）、shiki、react-markdown、diff（JS diff）、react-zoom-pan-pinch |
| 测试 | Vitest + Testing Library + msw（124 个前端测试文件）|

### 分发
- GitHub Release 平台压缩包（前端已构建内嵌）；
- **npm 包** `@alfredxw/denova`（bin 入口 + vendor 后端，npx 可用）✅；
- 构建：`scripts/bootstrap.sh` / `build.sh` / `build-github-release.sh`。

---

## 3. 整体架构 ✅

### 3.1 顶层目录
```
cmd/denova, cmd/denova-updater   # 入口（含自更新器）
config/                          # 配置加载 + Agent 定义/注册/工具/提示词装配（Go 包）
internal/
  api/         # Hertz 路由(routes.go)、handlers、SSE(agentui)、中间件、webfs 静态托管
  app/         # 应用服务层（75 个文件：book/chat/interactive/automation/lore/image/update...）
  agent/       # Agent 核心（115 个文件：builder、chat stream、context、compaction、
               #   checkpoint、tools、middleware、token usage、run ledger/trace...）
  book/        # 作品工作区领域模型（文件树、章节统计、资料库 lore、导入导出、版本 versions/）
  interactive/ # 游戏模式（director、actor state、state schema、回合协议、历史检索，71 个文件）
  session/     # 会话持久化（journal、compaction、display/有效上下文分离）
  automation/  # 定时/触发器任务
  workspacechange/ # 工作区变更账本（Undo/Redo、审阅评论、原子持久化、崩溃恢复）
  prompts/ skills/ imagegen/ illustration/ bookcover/ i18n/ observability/ update/ ...
web/src/
  components/  # Chat、Editor、Sidebar、Versions、workbench、layout、ui(shadcn)、common
  features/    # agents、automations、changes(Change Review)、chapters、document-review、
               #   interactive、messages、onboarding、settings、skills、versions
  hooks/ stores/ lib/(api-client、agent-ui) i18n/
skills/        # 18 个内置 SKILL.md（outline、group-plan、continue、rewrite、lore、
               #   novel-lite/standard/heavy、chapter-illustration、各 config 类...）
```
### 3.2 分层与通信
- **前后端分离 SPA**：`internal/api/routes.go` 注册约 **150+ 个 REST 端点**（`/api/workspace/*`、`/api/books/*`、`/api/lore/*`、`/api/chat`、`/api/interactive/*`、`/api/skills`、`/api/automations/*`、`/api/versions/*`、`/api/settings` 等），生产模式下同一 Hertz 服务静态托管前端并做 SPA fallback；开发时前端 Vite :5173。
- **流式协议**：`/api/chat/stream`（及 interactive/automation 对应端点）走 **SSE**，后端 `internal/api/agentui` 包负责把 Eino ADK 事件流转换为 UI 消息；前端用 Vercel AI SDK 的 `useChat` + 自定义 transport 消费。
- **分层**：api（传输）→ app（应用服务/编排）→ 领域包（book/agent/interactive/session/workspacechange，均私有、只暴露必要 API）。

### 3.3 工作区数据模型（每本书 = 一个目录）✅
```
<workspace>/                  # 一本书
  ideas.md                    # 创作灵感/构思结论（注入上限 2000 字符）
  CREATOR.md                  # 创作者定调文件（模板自动生成）
  chapters/                   # 章节正文 .md（支持卷子目录；文件名正则识别
                              #   ch1/0001-/第X章/序章楔子/Chapter N 等命名）
  setting/
    outline.md                # 长期大纲（卷/章结构）
    progress.md               # 当前进度 + 已完成章节摘要 + 短期衔接提示
    character-states.md       # 角色"当前状态"（位置/伤势/心理/目标/持有物/伏笔）
    chapter-groups/           # 章节组细纲 group01-*.md（按当前进度分批生成，非一次性）
  .denova/                    # 内部数据（旧版 .nova 兼容迁移）
    lore/                     # 结构化资料库（JSON：角色/世界观/地点/势力/规则/物品条目）
    sessions/  backups/  config.toml
```
- **章节元数据不靠数据库**：遍历 chapters/ 目录 + 文件名正则解析序号/标题，统计字数（CJK 感知）、状态（初稿/已确认）、更新时间。
- **资料库 LoreItem** 是唯一的结构化实体：固定字段（type/importance/tags/keywords/brief_description/**load_mode**）负责索引与注入策略，正文仍是 Markdown；load_mode 三档：`resident`（常驻上下文，建议 ≤32KB，硬上限 1MB）/ `auto`（关键词命中按需注入，索引默认上限 64KB）/ `manual`。

---

## 4. 核心架构优势（二次开发价值）✅

1. **Agent 基座扎实**：不是手写 ReAct 循环，而是基于 Eino ADK 的 **Deep Agent 预建件**（`deep.New`，含文件系统中间件、Skill 中间件、TODO 规划、子 Agent `task` 委派）。五类 Agent 统一 builder 构造：`ide`（写作）、`interactive_story`（游戏叙事）、`interactive_director`（后台导演）、`config_manager`（配置管理）、`automation`（后台任务）。无迭代次数/超时硬限制（可配置），符合长写作任务特性。
2. **上下文工程是第一公民**（最值得复用的部分）：
   - `internal/agent/context` 定义统一 **Source 抽象**：每个注入片段必须有来源、用途、大小上限、放置位置（leading_message / final_user_prefix / audit_only）；
   - **Context Ledger** 全量审计：前端有"上下文分析"对话框可视化每轮实际注入了什么、多少字节、是否截断；
   - **展示历史与模型上下文分离**（thinking/工具卡片不默认回注）；`/clear` 用过滤标记而非物理删除；
   - **StableContextParts 前置低变动内容**（资料库/大纲）以命中 prompt cache；
   - context_compaction 压缩、checkpoint 带回合来源的历史检查点、工具结果结构化筛选（有界回填）。
3. **工作区变更账本（workspacechange）**：Agent 每次修改生成 change group，支持累计/单轮 Unified/Split Diff、行内评论、**跨重启 Undo/Redo**、原子写入、崩溃恢复、工作区租约防并发写；配合 go-git 版本快照（versions/ 子包：create/diff/restore-plan/restore + 自动保存）形成双保险。
4. **Skills 声明式工作流**：SKILL.md（YAML frontmatter：`name/description/agent`）即可定义写作流程。例：`novel-standard` 用自然语言规定"主 Agent 初稿 → `task` 委派 reviewer 子 Agent 严格审稿 → 主 Agent 修订 → 更新状态文件"，重/中/轻三档（heavy/standard/lite）。用户可在 UI 创建、zip/URL/GitHub 安装 Skill。**改写作流程不用改代码**。
5. **职责清晰的数据契约**："已提交 Turn 是历史真源、Actor State 是可计算事实、资料库是稳定设定、director.md 是未来意图"——长期上下文不维护第二套可写真源，检索找回早期事实走有界 history_search。
6. **工程纪律罕见地严格**（AGENTS.md 明文）：单文件 ~500 行预警 / 800 行封顶、goroutine 必须 recover、前后端充分日志（带文件行号）、所有用户可见文案中英双语、修复 bug 先写复现测试、Commit/PR 必须英文、CHANGELOG 每次提交更新。测试占比：Go 175/513（34%）、前端 124/552（22%）。

---

## 5. 小说写作功能清单

| 功能 | 状态 | 实现位置/方式 |
|---|---|---|
| 大纲生成 | ✅ | skill `outline` → `setting/outline.md`（卷/章/一句话摘要，含起承转合/伏笔规范）|
| 章节组细纲 | ✅ | skill `group-plan` → `setting/chapter-groups/groupNN-*.md`，按进度分批生成 |
| 章节写作/续写 | ✅ | skills `continue` / `novel-lite/standard/heavy`；Agent 用 write_file/edit_file 工具直写 chapters/ |
| AI 审稿 | ✅ | reviewer 子 Agent（task 委派），检查连续性/设定一致性/节奏/文风，只审不改 |
| 角色管理 | ✅ | 资料库角色条目（长期设定）+ `character-states.md`（当前状态抖动）双层分离；支持导入 AI 酒馆角色卡（含分类/清洗/兼容性处理 5 个文件）|
| 世界观/设定 | ✅ | setting/ 目录 + 资料库六类条目（角色/世界观/地点/势力/规则/物品）|
| 长篇上下文管理 | ✅ | 见 §4.2：分层注入 + 上限 + 压缩 + 检查点 + 缓存优化 |
| 进度追踪 | ✅ | progress.md + 章节字数/状态统计 API（/workspace/summary）|
| 正文评论 | ✅ | document-review（锚定/装饰/hover 全套前端 + 后端 handler）|
| Change Review | ✅ | 累计 Diff 审阅 + 评论 + Undo/Redo（v0.3.0 新增）|
| 全局搜索 | ✅ | ripgrep；编辑器内查找替换 + 正则（2026-07-20 刚加）|
| 现有小说导入 | ✅ | novel_import（含网页抓取 goquery、编码探测、流式预览）|
| 导出 | ⚠️ | **仅纯文本 txt 合并导出**（/books/export，重组卷/章标题）。**未见 epub/docx/pdf 导出** |
| 章节插画/封面 | ✅ | imagegen + bookcover + illustration（OpenAI 兼容图像模型）|
| 自动化 | ✅ | 定时任务、自动 Review、自动续写、自定义 Prompt 工作流 + 收件箱确认机制 |
| 版本管理 | ✅ | 本地 Git 快照/Diff/恢复 + 定时与大量输出后自动保存 |
| 多书籍管理 | ✅ | 书籍列表/切换/排序/封面/元信息（/api/books/*）|
| 协作/多人 | ❓ | 未见任何协作功能（单机定位）|

---

## 6. 前端 UI 现状 ✅（依据 README 截图 + 源码 + DESIGN.md）

**页面结构**：VSCode/Cursor 式工作台 —
- 最左 Activity Bar：写作 / 资料库 / 方案预设 / 书籍管理 / Skills / Agents / 自动化 / 版本管理 / 设置（共享一级菜单，点击不隐式切换模式；写作模式⇄游戏模式只能显式切换）；
- 左侧栏：作品目录（分卷章节树 + 字数/状态）/ 项目文件 / 全局搜索；
- 中央：多 Tab 编辑器（TipTap 渲染 Markdown、**对话高亮**、760px 最大行宽、中文衬线阅读字体 18px/1.9、查找替换栏）；
- 右侧：Agent 面板（对话、思考过程折叠、工具调用卡片、TODO 列表、Token 用量、模型切换、上下文分析）；
- 游戏模式：叙事流 + 内嵌生成插图 + 底部"你要做什么？"输入 + 右侧 Actor State 状态栏 + 全屏导演台。
- 设计系统成熟：DESIGN.md 509 行规范（`--nova-*` 双层 token、纯黑/纯白底色纪律、4px 间距栅格、WCAG AA、深浅主题、Adaptive 布局 + 移动端 PWA）。

**明显不足**：
- ⚠️ **性能问题**：存在未关闭 issue「使用过程当中严重卡顿」（2026-07-19 仍 open）；
- ⚠️ App.tsx 主组件臃肿（一个文件里数十个 useState，角色卡导入等逻辑堆在根组件）；
- ⚠️ Beta 期破坏性变更明确存在（v0.3.0 CHANGELOG："审阅反馈、Agent 文件编辑和游戏回合提交协议均有调整；后台 Shell 暂不再支持"）；
- 导出格式单一（仅 txt）；编辑器是富文本抽象而非源码级 Markdown 编辑器（对习惯纯文本 diff 的开发者可能不顺手）；
- 截图版本为 v0.1.17，v0.3.0 UI 可能已有差异。

---

## 7. 活跃度与代码质量 ✅

| 指标 | 数值（2026-07-20 查） |
|---|---|
| 创建时间 | 2026-05-19（**仅 2 个月**）|
| 最近 push | **2026-07-20**（调研当天，几乎每日提交）|
| Stars / Forks | **503 / 87** |
| Open / Closed issues | 4 / 10+（含 PR），作者响应快（bug 当日或次日修复合入）|
| 贡献者 | 10 人，**alfredxw 一人 306/328+ commits（93%）**——个人项目，bus factor 高 ⚠️ |
| 版本节奏 | 约 15 个 release（v0.1.12 → v0.3.0），CHANGELOG 规范（Keep a Changelog + 中英双语）|
| 代码规模 | Go 513 文件（测试 175，34%）；TS/TSX 552 文件（测试 124，22%）；仓库 ~23MB |
| License | **Apache-2.0**（二次开发/商用友好）|
| 文档 | README 详尽（中英双份）；DESIGN.md（前端视觉规范）；AGENTS.md（工程规范）；CONTRIBUTING.md；❓**无后端架构文档、无 docs/ 目录** |

---

## 8. 未查到/不确定项汇总 ❓

1. Monaco Editor 的确切使用场景（依赖存在，未逐一核实用在哪些视图，疑似 Skill/提示词源码编辑）；
2. 后端 SSE 事件协议的确切字段格式（`internal/api/agentui` 未逐行读，二次开发对接流式协议时需注意它并非原生 Vercel AI SDK 协议，而是自定义 agentui 消息 + transport 适配）⚠️；
3. epub/docx 等富格式导出是否存在（路由与 export.go 中均未见，基本可判定无）；
4. 游戏模式 state schema 的完整语义（71 个文件仅抽样阅读）；
5. npm 包实际发布状态（package.json 存在，未查 npm registry 验证是否已 publish）；
6. 多端数据同步能力：无（单工作区路径由 `DENOVA_WORKSPACE` 指定，远程访问只是暴露同一后端）。

## 9. 对二次开发的建议（简要）

- **值得直接复用**：context Source/Ledger 体系、workspacechange 变更账本、Skills 加载机制、go-git 版本层、Eino ADK 多 Agent builder 模式。
- **二次开发切入点**：写作模式相关功能集中在 `internal/book` + `internal/agent` + `web/src/features|components/Editor|Chat`，改动半径清晰；新增写作流程优先写 SKILL.md 而非改 Go 代码。
- **主要风险**：Beta 期协议/存储格式可能继续破坏性变更（作者明示"不考虑兼容性"）；Go 生态的 Eino/Hertz 相对小众，资料少于主流栈；无数据库设计意味着复杂查询（如全文检索升级、关系图谱）需要自建或引入 SQLite。
