# NovelForge-Agent 调研报告

> 调研对象：https://github.com/LvPengfei1/novelforge-agent
> 调研方式：GitHub API + 浅克隆仓库后直读 README、docs/novel-agent-plan.md、AGENTS.md、SKILL.md、NOVEL_REVIEW_PROTOCOL.md、novelforge_agent/core.py（约 1900 行）与全部模板文件。
> 仓库快照：main 分支，最新提交 2026-07-10 "Add executable isolated chapter workflow"；18 stars / 3 forks；Apache-2.0；语言 Python；创建于 2026-05-27，版本 1.0.3+。

## 1. 项目定位与核心功能（确定性：高）

一句话定位：**它不是"自动写小说"的端到端程序，而是一套"长篇小说创作的项目结构 + 智能体行为规则（prompt 正本）+ 确定性章节封包 CLI"三合一框架**。

- 自我声明："本项目不是具体小说正文，而是一套可复用的小说写作项目结构、智能体行为规则和确定性章节封包工具。它不会替模型判断故事好坏"。
- 代码的职责边界非常克制：拒绝未批准/被改动的正本、拒绝越界路径、防止写作包与审稿包混用、只向任务封包提供显式声明的材料。**文学判断完全留给模型整章读稿**。
- 核心功能：三层资料模型（raw/ novel/ llm-wiki/）、四份全局正本 + 哈希批准闸门、每章输入白名单封包、写作/审稿隔离、章节龙骨防矛盾机制、章后同步检查清单。

## 2. Agent 架构设计（确定性：高）

### 角色分工（3 个逻辑角色，不是 3 个 agent 文件）

| 角色 | 职责 | 上下文状态 |
|---|---|---|
| 主智能体（orchestrator） | 维护架构与正本、建立本章输入白名单、生成封包、取舍整合审稿意见、最小润色、章后同步与最终验收 | 长期存活，掌握全局 |
| 写作子智能体（writer） | 只写本章正文 | **每章全新实例**，不继承主对话、不复用上章实例、禁止全库搜索；只接收 writer-task.md；资料不足只能返回 `NOVELFORGE_MISSING_CONTEXT:` 缺口报告 |
| 审查子智能体（reviewer） | 整章独立审稿 | **另一个全新实例**，与写作者隔离：接收同一版本全局正本+本章包+待审正文+最多 3 份近章材料+审稿协议，**不接收写作者的推理、自评、辩解** |

注意：仓库明确**拒绝过早拆分为多 agent**——docs/novel-agent-plan.md 末尾说"当前阶段建议先使用一套项目级原则文件……避免过早拆分导致维护成本上升"，outline-agent / character-agent / continuity-agent / style-agent 仅作为"后续可扩展"占位列出，并未实现。

### 编排协作方式

**串行管线 + 受控反馈回路，不是多轮对话式协作**：

```
主智能体（建白名单/封包）→ 全新 writer（写）→ 主智能体（收稿）→ 全新 reviewer（审）
     ↑                        |（缺料时）                |（不合格）
     └── 补充白名单后重新 prepare ←┘   重构章骨/场景后重新审稿 ←┘
```

- writer 与 reviewer 之间**零通信**；reviewer 不知晓 writer 意图，防止"按作者意图辩护"。
- 写作包**不含**审稿原则与近章全文（防止照着清单应试写作）；审稿包**不含**写作者推理（防止污染判断）——这是刻意设计的信息不对称。
- 环境不支持子智能体时，规则要求"用相互隔离的写作轮与审查轮执行，**不能声称调用了子智能体**"（诚实性条款）。

## 3. Agent 框架与工作流定义（确定性：高）

- **完全自研，不用 LangChain/LangGraph/CrewAI/AutoGen**。`novelforge_agent/core.py` 只 import 标准库（hashlib/json/os/re/shutil/subprocess/tempfile/dataclasses/pathlib），零三方依赖；配套 29KB unittest。
- **没有显式状态机库，但 run 目录里的 manifest.json 实现了显式状态机**：`prepared → (missing_context | drafted) → review_prepared → reviewed`。`invoke` 每次执行前校验状态转换合法性（如非 review_prepared 状态不能审）、重新派生任务文本并比对哈希，过期/伪造封包直接拒绝。
- 编排落在三个宿主层：
  1. **Codex**：`SKILL.md`（novelforge-writing skill）+ `agents/openai.yaml`，要求以 `fork_context=false` 启动两个不同子智能体；
  2. **Claude**：`Claude.md`（与 AGENTS.md 语义等价的规则正本）；
  3. **任意宿主**：`python scripts/novelforge.py invoke --stage writer|reviewer -- <command>`，以 `shell=False` 新进程、最小环境变量、stdin 传任务文本、stdout 收产出。拒绝 .cmd/.bat 和 cmd/powershell/sh/bash 等解释器（但官方明确声明这**不是沙箱**，外部命令仍拥有宿主权限）。

## 4. 自动化写作完整 Pipeline（确定性：高）

阶段流程（文档原文）：**项目简报 → 资料摄入 → 章节龙骨 → 正文初稿 → 编辑检查 → 修改复查 → 章后同步 → 健康检查**。机器化部分：

1. `init projects/<slug>`：复制空模板（三层目录 + 全部卡片模板）。
2. 人工填充四份全局正本：项目简报 brief.md（故事承诺/主角欲望/麻烦发动机/外部动力/主要关系/阶段不可逆变化——六项机器校验非空）、全书龙骨 book-spine.md、剧情总纲 outline.md、文风正本 style-canon.md。
3. 用户显式批准后 `approve --confirmation "<依据>"`：校验正本实质字段，把四份正本的**版本 + SHA-256** 写入 architecture-approval.json。此后任何正本变化自动关闭写作闸门。
4. 每章：复制 `_chapter-spine-template.md` 和 `_chapter-job-template.json`，显式填写 chapter_sources（本章资料路径白名单）、previous_excerpt（前章衔接的**精确行号范围**）、recent_chapters（≤3 份仅给审稿者的比较材料）。
5. `prepare --job chXXX-job.json`：校验架构闸门+白名单+角色边界（控制文件和审稿专用材料不得漏入写作包），生成 `writer-task.md` + `manifest.json`（记录每个来源的相对路径、用途、行号、SHA-256），run 目录名含指纹。
6. 启动全新 writer（Codex 子智能体或 invoke 外部进程）→ 产出正文到 `novel/05_manuscript/chXXX.md`（正文文件只许含标题+正文，工程信息禁止混入）。
7. `prepare-review --run <dir>`：**锁定待审正文哈希**（正文再变旧审稿包即失效），生成 `reviewer-task.md`。
8. 全新 reviewer 按 NOVEL_REVIEW_PROTOCOL.md 审稿，意见写入 run 目录 review.md。
9. 主智能体整合：故事层失败→重构章骨/场景（禁止同义词替换式修补）；语言层问题→最小修改。
10. 章后同步：按章节龙骨模板的同步检查表更新 book-spine/outline/timeline/character-states/relationship-states/foreshadowing-ledger/contradiction-flags/受影响节点页/index/log，并做"节点缺口检查"（人物出场>2章、推动关键剧情等达到复用门槛必须建独立节点页，只写总账摘要不算同步完成）。
11. 一段连续创作后，把 llm-wiki 结论镜像回 novel/ 各摘要文件。

## 5. 质量控制机制（确定性：高）

- **架构批准闸门（机器强制）**：四份正本的"批准状态=已批准、可写标志=是、架构版本一致"由代码逐字段校验；版本不一致或哈希变化即拒绝 prepare。
- **独立审稿协议（NOVEL_REVIEW_PROTOCOL.md）三遍法**：① 整章连续读完，复述人物目的/阻力/选择/代价/不可逆变化；② 与最近 3 章比较叙事功能同构（解法、信息传播路径、关系变化载体、情绪释放、结尾压力）；③ 最后才审语言节奏。
- **明确反"AI 检测器"路线**：关键词、词表、句长统计、物件计数、检测器分数**禁止**作为分析入口或结论；只有项目正本明确规定的禁用词/篇幅/格式可做确定性校验。任何"像 AI"的判断必须落到具体场面、读感证据和因果修法。
- **重写循环**：故事层失败必须重构章骨/场景并重新完整审稿，无自动次数上限（靠主智能体判断）。
- **证据四级**：已确立 / 有来源依据 / 推断待确认（不得进正文）/ 已废弃（保留追溯不得回流）。
- **防连续章节同构**：写前必须回看近三章的"读者记忆点"，若只换地点人物而结构相同，先改章骨。
- **正文纯净检查 + 工程词转译**：阶段/节点/同步/检测等工程词进正文前必须转译为作品世界内表达。
- **章节字数口径**：不内置全局字数规则，以用户/平台口径为准并在龙骨中验收。

## 6. 记忆与上下文管理方案（确定性：高）

**核心哲学："所有设定必须落文件，不依赖对话记忆"、"不相关设定不进上下文"。**

三层资料模型：
- `raw/`：原始资料，只读不改写（证据层）。
- `novel/`：工程层（正文、章节龙骨、过程文件、镜像摘要），不是日常检索入口。
- `llm-wiki/`：设定检索层，wiki 图谱（`[[wikilinks]]` 互链）。"图谱总账 + 节点页 + 章节龙骨"三层：index.md 只做扫描入口；人物/地点/组织/规则/物品/场景/伏笔达到复用门槛必须建独立节点页；正文原文不进 wiki。

关键记忆构件：
- **全书龙骨 book-spine.md**：主线高密度压缩版（一句话主线、人物发动机、核心矛盾、阶段表、不可违背事实）。
- **剧情总纲 outline.md**：串联章节组功能、支线咬合、阶段推进，链接到各章龙骨。
- **章节龙骨 chXXX-spine.md**（300–800 字，复杂章≤1200 字）：只记防矛盾骨架——章节功能/开章状态/本章目标/核心推进(1-3事件)/结尾变化/后续约束/待回收问题，外加架构闸门、白名单、同步检查表。
- **状态账本群**：character-states、relationship-states、timeline、foreshadowing-ledger、contradiction-flags。
- **log.md**：只追加的时间日志（ingest/query/audit 固定前缀），让智能体知道最近发生了什么。

上下文工程：用**显式白名单封包替代 RAG/全库检索**。writer 收到的 = 四份全局正本 + 本章 spine 节选 + 显式列出的本章来源 + 前章精确行号片段，全部内容以 `--- NOVELFORGE SOURCE BEGIN/END ---` 包裹并标注来源与行号；归档、废案、日志、质量报告、未批准方案在 novelforge.json 的 blocked_paths 中被机器级禁止入包。

## 7. 技术栈与模型接入（确定性：高）

- 纯 Python 3 标准库 CLI（`scripts/novelforge.py` 薄壳 + `novelforge_agent` 包），无 Web 服务、无数据库、无向量索引、无任何 LLM SDK。
- **不内置任何 LLM API 调用**，模型接入完全由宿主负责：Codex（AGENTS.md/SKILL.md）、Claude（Claude.md）为一等公民；其他任意模型通过 `invoke` 以无状态外部进程接入（stdin/stdout 协议），因此理论上 GPT/Claude/Gemini/本地模型均可，只要包一层"stdin→模型→stdout"的可执行程序。
- 测试：`python -m unittest discover -s tests -v`（tests/test_novelforge.py 约 29KB，覆盖封包/闸门/状态机）。

## 8. 值得借鉴的架构经验（分析判断，基于上述事实）

1. **把"批准"变成机器可校验状态**：版本字段 + 四份正本哈希 + 用户确认依据绑定，正本一动写作闸门自动关闭——比纯 prompt 约定可靠得多。
2. **显式输入白名单 > 全库 RAG**：每章上下文最小化、可审计（manifest 含路径/行号/SHA-256）、防污染；写作者缺料只能"报缺口"由主控补白名单，形成受控反馈回路而非放任检索。
3. **fresh-per-chapter + 写审双重隔离**：每章全新 writer 防上下文漂移；reviewer 不见 writer 推理防辩护偏差；写作包不含审稿清单、审稿包不含写作自评的**不对称信息设计**非常讲究。
4. **封包过期自动失效**：正文哈希锁定、任务文本每次重新派生比对，杜绝"新正本配旧正文"的静默混用；run 状态机让流程可恢复、可审计。
5. **审查方法论成熟**：三遍法（整章读→近章同构比较→语言）、拒绝检测器分数驱动、故事层失败必须结构重构而非换词——这套质控哲学直接可搬。
6. **章节龙骨作为"防矛盾最小骨架"**：只记状态变化与后续约束，不复述剧情，是长篇一致性管理中成本极低的记忆单元。
7. **诚实性工程**：明确声明 invoke 不是沙箱、子智能体不可用时不得谎称、人工流程不能声称执行了确定性校验。

## 9. 短板与风险（分析判断）

1. **不是端到端自动化**：框架本身不调用任何模型；每章要人工/主控填 job JSON、批准、跑 4 条命令、做同步，自动化程度取决于宿主 agent 的执行力。"写 100 章"仍需 100 轮编排。
2. **章后同步无机器校验**：同步检查表靠主智能体自觉填写，代码不验证 wiki/账本是否真的更新了——长篇跑到几十章后同步遗漏风险大（这正是它想解决的问题，但解决手段是纪律而非机制）。
3. **白名单选择本身无兜底**：不检索全库意味着"该给 writer 什么"完全依赖主智能体判断，选漏了只能靠 writer 报缺口；没有向量检索等召回手段做补充。
4. **质量无量化指标**：无评分、无通过率统计，reviewer 质量=所用模型水平；重写循环无次数上限与终止条件，可能死循环或过早放行。
5. **纯串行、单章粒度**：无多章/多场景并行写作，无 scene 级封包；对网文日更级产能场景吞吐有限。
6. **记忆层是纯手工 Markdown**：无结构化数据库，角色/伏笔一致性检查靠人读和 reviewer 比较近 3 章，超长篇（数百章）的召回精度存疑。
7. **生态极新极小**：2026-05 创建、18 stars、单一作者，未经大规模实践验证；1.0.x 阶段模板字段仍在漂移。
8. **安全边界靠自觉**：invoke 不是沙箱，白名单也不是 OS 级隔离，框架官方建议强隔离需求者自行加容器。

## 附：关键文件索引

- 规则正本：`AGENTS.md` / `Claude.md`（等价）、`SKILL.md`、`NOVEL_REVIEW_PROTOCOL.md`
- 方案文档：`docs/novel-agent-plan.md`、`docs/usage-guide.md`、`docs/aigc-quality-control.md`
- 代码：`novelforge_agent/core.py`（封包/闸门/状态机/invoke）、`cli.py`、`tests/test_novelforge.py`
- 模板：`templates/novel-project/`（novelforge.json、brief、book-spine、outline、style-canon、章节龙骨与 job 模板、全套 wiki 卡片模板）
