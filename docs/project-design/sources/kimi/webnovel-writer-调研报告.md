# webnovel-writer 调研报告（方法论提炼）

- 调研对象：https://github.com/lingfengQAQ/webnovel-writer （v6.2.0，GPL-3.0，Python，约 5.4k stars / 950 forks，v7 重构 RFC 公示中）
- 调研方式：GitHub API + 浅克隆仓库通读 SKILL.md / agents / references / templates / docs / 部分源码
- 信息确定性标注：【高】=直接读自源码/文档；【中】=由多处材料综合推断；【低】=未验证的推测

---

## 1. 项目定位与目标用户

**定位【高】**：跑在 Claude Code 上的长篇网文创作插件（Claude Code Plugin，8 个斜杠命令 + 3-4 个子代理 + Python 数据层）。官方自述："一套面向长篇连载的一致性系统，不是写完就忘的一次性生成器"，目标是"支持 200 万字量级连载创作"。

**核心命题【高】**：解决 AI 写长篇的三大问题——遗忘（角色/设定漂移）、幻觉（编造与设定冲突的事实）、失控（伏笔埋了不收、节奏崩）。一句话："让 AI 写到几百章，依然记得住设定、接得住伏笔、守得住大纲。"

**目标用户【高】**：中文网文作者（人机协作，不是全自动）。作者参与关键裁决：初始化分阶段问答、创意方案选择、总纲/设定冲突裁决、blocking 审查问题裁决。系统刻意"少打扰"：只在创作方向、事实一致性、文件覆盖风险、blocking issue 时才提问。

**设计哲学【高】**："把'怎么写'和'写了什么'分开：文笔和节奏可以放开发挥，但发生过的事实必须登记、过审、存档。" 以及防幻觉三定律：
- 大纲即法律（Context Agent 强制加载章纲）
- 设定即物理（Reviewer 内置一致性审查，能力 ≤ 已有记录）
- 发明需识别（Data Agent 提取新实体并消歧入库）

---

## 2. 核心方法论 / 总体工作流

### 2.1 命令级流水线【高】

| 阶段 | 命令 | 产物 |
|---|---|---|
| 初始化 | `/webnovel-init` | 设定集（世界观/力量体系/主角卡/反派设计等）、大纲/总纲.md、idea_bank.json、.story-system/MASTER_SETTING.json、state.json |
| 卷纲规划 | `/webnovel-plan {卷号}` | 卷节拍表、卷时间线、卷详细大纲（章纲）、总纲写回.json、章节合同 |
| 写章 | `/webnovel-write {章号}` | 正文 + 审查报告 + CHAPTER_COMMIT + state/index/summary/memory/vector 五路投影 + git 备份 |
| 审查 | `/webnovel-review {范围}` | 结构化问题清单 + 审查报告 + metrics 落库 |
| 查询 | `/webnovel-query` | 角色/伏笔/节奏/实体关系查询 |
| 经验沉淀 | `/webnovel-learn` | project_memory.json（写作模式记忆） |
| 面板/体检 | `/webnovel-dashboard` / `/webnovel-doctor` | 只读可视化 + 健康检查 |

### 2.2 初始化（init）：分阶段交互 + 充分性闸门【高】

不是"一键生成"，而是 7 步分波次问答，"先收集，再生成"：
- Step 1 预检；Step 1.5 灵感来源（可选拆书，由 deconstruction-agent 拆参考书，只提取可迁移模式，严禁污染新书 canon）；
- Step 2 故事核与商业定位：书名、题材（支持 A+B 复合）、目标规模、一句话故事、核心冲突、目标读者/平台；
- Step 3 角色骨架：主角姓名/**欲望**/**缺陷（会害他付代价的）**/单多主角/感情线/反派分层（小中大）与镜像对抗一句话；
- Step 4 金手指与兑现机制：类型、风格、可见度、**不可逆代价（必须有代价或明确"无+理由"）**、成长节奏；条件必收（系统流→系统性格+升级节奏；重生→时间点+记忆完整度）；
- Step 5 世界观：世界规模、力量体系类型、势力格局、阶层与资源分配；
- Step 6 创意约束包（差异化核心）：生成 2-3 套创意包，每套含 一句话卖点 + 反套路规则1条 + 硬约束2-3条 + 主角缺陷驱动 + 反派镜像 + 开篇钩子；三问筛选（为什么这题材必须这么写？换常规主角会不会塌？卖点能否一句话讲清？）+ 五维评分辅助决策；
- Step 7 一致性复述与最终确认：用户未确认不执行生成。

**充分性闸门【高】**：书名题材确定、规模可计算、主角姓名+欲望+缺陷、世界规模+力量体系、金手指类型、反套路1条+硬约束≥2条——全部满足才允许执行生成脚本。

### 2.3 写章（write）：9 步带关卡流水线【高】

1. 预检（preflight + 占位符扫描）
2. 刷新本章 runtime contract（章级"合同"）
3. **context-agent** 生成"写作任务书"
4. 起草正文（只依据任务书）
5. **reviewer** 五维审查，blocking issue 阻断
6. 润色 → 排版 → Anti-AI 终检
7. **data-agent** 提取事实（三份 JSON artifact）
8. 生成 CHAPTER_COMMIT，驱动 state/index/summary/memory/vector 五路投影
9. 章节级 git 备份

要点：审查只跑一轮，blocking 定点修复或用户裁决；失败只补跑失败步骤（run-ledger 断点续跑，不重写已可信产物）。

---

## 3. 章节大纲生成的具体做法【高】

### 3.1 三级大纲结构
- **总纲**：故事一句话、创意约束（反套路/硬约束/主角缺陷/反派镜像）、核心主线/暗线、反派分层、卷划分表（卷号/卷名/章节范围/核心冲突/卷末高潮）、主角成长线、关键爽点里程碑、**伏笔表（内容/埋设章/回收章/层级）**。
- **卷级**三件套：
  - **卷节拍表**：开卷承诺（Promise）→ 催化事件（Catalyst，含不可逆变化）→ 升级危机链（Fichtean 危机递增，**至少 3 次**，节点/危机/代价升级/可量化结果）→ **中段反转（必填，无则写"无（理由）"）**→ 卷末最低谷（All Is Lost）→ 卷末大兑现 + 新钩子（必须落到最后一章的章末未闭合问题）。
  - **卷时间线表**：时间基准、本卷跨度、倒计时事件（D-N）；每章 时间锚点/章内跨度/与上章间隔/倒计时状态；硬性时间规则（跨夜需过渡、跨日需过渡段、>3天必须过渡章；禁止时间回跳、倒计时跳跃）。
  - **卷纲骨架**：卷摘要、关键人物与反派层级、Strand 分布、爽点密度规划、伏笔规划、约束触发规划。
- **章纲**（详细大纲）：见 3.2。

### 3.2 章纲字段（粒度）
每章必含：目标、阻力、代价、时间锚点、章内时间跨度、与上章时间差、倒计时状态、爽点、Strand（主线/感情线/世界观线）、反派层级、视角/主角、关键实体、本章变化、章末未闭合问题、钩子，以及结构化节点：
- **CBN**（章节起点，固定1个，承接上章 CEN）
- **CPNs**（推进节点，2-4个，按时间排序）
- **CEN**（章节终点，固定1个，落到章末未闭合问题）
- 节点格式：`主体 | 动作/变化 | 对象/结果`（如 `萧炎 | 展示 | 异火控制力`）
- **必须覆盖节点** ≤4 个（建议 CBN+CEN+1~2核心CPN）——写章后由 fulfillment_result 校验 covered/missed
- **本章禁区** ≤5 条（只写硬禁区，违反即不通过）

### 3.3 连贯性保障机制
- **相邻章承接**：上章 CEN → 本章 CBN 必须逻辑承接（验证项）；上章有钩子本章必须回应（reviewer 查 continuity）。
- **批量拆章**：默认 10 章/批（复杂题材 8，简单升级流 12），失败只重做失败批次。
- **跨卷一致性**（非首卷必查）：读最近 5 章摘要 + 主角实体状态 + 关系状态 + 跨卷未回收伏笔；上卷未回收伏笔必须出现在新卷伏笔规划；角色关系必须延续；能力/境界不回退不跳级。
- **时间线单调递增**是硬约束，时间回跳且未标注闪回 → 阻断当前批次。
- **总纲写回**：规划完生成结构化 `第N卷-总纲写回.json`（下一卷锚点 + 新增伏笔 + 持续开放环，只写显式列出的，禁止从自由文本推断），脚本增量更新总纲。
- **优先级链**：用户明确要求 > 总纲核心冲突与卷末高潮 > 时间线硬约束 > skill 默认流程 > reference 建议。

---

## 4. 角色管理

### 4.1 角色卡字段（主角卡模板）【高】
基本信息（姓名/年龄/身份/起点状态）；核心标签（3关键词/读者第一印象）；性格与底色（核心性格/行为底线/情绪触发点/易激怒点/容易心软点）；动机与目标（短/中/长期目标 + **真正渴望（可能不自知）**）；缺陷与代价（性格缺陷/能力限制/心理阴影/代价承受底线）；关键关系（盟友/对手/情感/债务牵绊）；**镜像对抗**（与反派共享的欲望/缺陷、反派道路）；当前能力（境界/技能/资源）；金手指（类型/代价限制/核心卖点）；行为模式（常用解决方式/失败时本能反应/破局特长）；成长弧线（起点/变化/蜕变三阶段）；**OOC 警戒（绝不该做的事 / 需要提前铺垫的事）**。

另有女主卡、主角组（多主角分工与冲突）、反派设计（小/中/大分层 + 镜像关系）。

### 4.2 角色状态追踪【高】
- **state.json → protagonist_state**：name / power(realm, layer, bottleneck) / location(current, last_chapter) / golden_finger(name, level, cooldown)。
- **index.db → entities/state_changes/appearances/aliases**：每章 data-agent 提取 `state_deltas`（entity_id+field+old+new，如 萧炎 realm 斗者→斗师）、`entity_deltas`（新实体 upsert，类型：角色/组织/地点/物品/势力）、`entities_appeared`（含别名 mentions 与置信度）。
- **消歧**：置信度 >0.8 自动采用，0.5-0.8 采用+warning，<0.5 标记待人工（disambiguation_result.pending）。
- **长期记忆 character_state 桶**：按 subject+field 主键去重，旧值降级 outdated 保留审计。

### 4.3 防 OOC / 防遗忘的手段
- 角色卡的 "OOC 警戒" 字段写进写作任务书（context-agent 第4段：主角卡 OOC 警戒 + anti_patterns）。
- reviewer 的 **character 维度**：对话风格是否符合角色特征、行为是否与性格/动机一致、**角色知识边界（是否用了不应知道的信息）**。
- 写章前 context-agent 为每个出场角色生成一段：状态、驱动力、本章作用、说话倾向（"每人一段"）。
- 配角按需 `query-entity` 深查；核心实体从 index 取 core-entities 与 recent-appearances。

---

## 5. 情节推进与伏笔管理（网文节奏控制）

### 5.1 伏笔系统【高】
- **三层级 + 回收周期 + 权重**：核心（50-300章, 3.0x）/ 支线（30-100章, 2.0x）/ 装饰（10-30章, 1.0x）。
- **紧急度公式**：`紧急度 = (已过章节 / 目标章节) × 层级权重`；🔴 Critical（超目标 or 核心>50章未回收）/ 🟡 Warning（>80%）/ 🟢 Normal。
- 密度建议：同时进行伏笔 ≤5 条；200万字+ 长篇：浅层50+/中层20+/深层5-10条。
- 埋设技巧：顺手埋/对话埋/细节埋/梦境埋；回收：直接揭晓/层层揭开/意外反转。
- 数据面：data-agent 每章必须将摘要中的伏笔同步为 `open_loop_created` / `open_loop_closed` / `promise_created` / `promise_paid_off` 事件（带 urgency 0-100、expected_payoff）；context-agent 写前拿 `urgent_loops`，remaining≤5 或超期的必须处理，可选伏笔最多 5 条。

### 5.2 追读力（Reading Power）体系【高】（v5.3 引入）
- **钩子类型**：危机钩/悬念钩/渴望钩（5个子类型：成长/关系/复仇/真相/收获）/情绪钩/选择钩；强度 strong/medium/weak；章内用悬念+情绪钩，章末用危机/选择/渴望钩。
- **爽点模式**：6 经典（装逼打脸/扮猪吃虎/越级反杀/打脸权威/反派翻车/甜蜜超预期）+ 扩展（迪化误解、身份掉马）；结构参考 铺垫30%/兑现40%/余波30%。
- **微兑现（Micro-Payoff）**：7 类（信息/关系/能力/资源/认可/情绪/线索），让读者"这章没白看"；爽文每章 2-3 个，言情 1-2 个。
- **爽点密度红线**：每10章≥1个B级爽点；理想：每5章1C + 每10章1B + 每30章1A + 每50-100章1S；连续20章无爽点=弃书风险。
- **硬约束（Hard Invariants，违反=必修）**：可读性底线 / 承诺违背（上章钩子下章无回应）/ 节奏灾难（连续N章无任何推进）/ 冲突真空（整章无问题/目标/代价）。
- **软建议 + 债务机制**：违背软建议可申诉，但必须选 rationale_type（7种：铺垫需要/逻辑优先/人物可信度/世界规则/长线节奏/题材惯例/作者意图），并记入 override_contracts + chase_debt（追读力债务，index.db 有专表），作者意图类理由配额更严——"欠读者的爽点要还"。

### 5.3 Strand Weave 三线节奏【高】
- Quest（主线，55-65%）/ Fire（感情线，20-30%）/ Constellation（世界观线，10-20%）。
- 红线：Quest 连续 ≤5 章；Fire 断档 ≤10 章；Constellation 断档 ≤15 章。
- state.json 有 strand_tracker（last_*_chapter / current_dominant / chapters_since_switch / history）自动追踪。
- 有前 30 章织网模板（开局 Quest 密集，第 6-8 章必须安排首次 Fire）。

### 5.4 章节级节奏模板【高】
- 黄金结构（3000字章）：开头钩子300字(10%) / 发展1500字(50%) / 高潮1000字(33%) / 结尾钩子200字(7%)。
- 章节类型配比：爽点章 70% / 过渡章 20% / 刀子章 10%。
- 开头钩子 5 型：悬念式/冲突式/信息差/倒叙式/承接式。
- 自检清单：前300字吸引力、每章≥1情绪高点、不能连续3章无爽点、每章最多引入2个新角色等。

---

## 6. 长篇上下文管理

### 6.1 双真源架构（Story System，v6 核心）【高】
- **写前真源**：`.story-system/` 合同树——MASTER_SETTING.json（调性/禁忌/核心约束）、volumes/（卷级节奏合同）、chapters/（章级合同：chapter_directive.goal/time_anchor/countdown/chapter_end_open_question + CBN/CPN/CEN + forbidden_zones + reasoning 裁决层：style_priority/pacing_strategy）、reviews/（必须节点/禁区审查合同）。
- **写后真源**：accepted 的 CHAPTER_COMMIT（`.story-system/commits/chapter_XXX.commit.json`）+ 事件审计链（events/）。
- **派生只读视图（投影）**：state.json、index.db(SQLite)、summaries/、memory_scratchpad.json、vectors.db——commit 后由 projection writers 写入五路；projection_log.jsonl 记录每路状态，失败可 `projections retry` 单独补跑。
- commit 自动判定：blocking_count>0 或 missed_nodes 非空 或消歧 pending 非空 → rejected。
- 三道写闸门：write-gate prewrite / precommit / postcommit。

### 6.2 记忆三层模型【高】
- **Working Memory**（运行时拼装）：本章章纲 + 最近几章摘要 + state.json 主角状态/情节线程/待消歧项。
- **Episodic Memory**：index.db 近期结构化证据（最近状态变化/关系变化/出场记录）——偏"最近证据"，非全量语义召回。
- **Semantic Memory**：memory_scratchpad.json，7 个分桶（character_state/story_facts/world_rules/timeline/open_loops/reader_promises/relationships），每条 MemoryItem 有 status（active/outdated/contradicted/tentative）+ source_chapter + evidence；按主键去重、旧值降级；超阈值压缩（同key只留最新、清已回收伏笔、旧时间线合并为 story_fact、按新鲜度截断）。
- 读前过滤：subject/field/value 是否出现在本章章纲 + 章节窗口 + 预算截断，不是全量注入。
- 另有旁路：project_memory.json（/webnovel-learn 的写作经验，pattern_type: hook/pacing/dialogue/payoff/emotion/format）会以 author_style_patterns 注入任务书。

### 6.3 章节摘要格式【高】
每章 summaries/ch0001.md：YAML 头（chapter/time/location/characters/state_changes/hook_type/hook_strength）+ 剧情摘要（100-150字）+ 伏笔（埋设/回收）+ 承接点（30字）；场景切片 scenes（50-100字/场景，含 start_line/end_line/location/characters）供检索。

### 6.4 RAG【高】
- 流程：查询 → QueryRouter(auto) → vector / bm25 / hybrid / graph_hybrid（叠加实体图谱）→ RRF 融合 + Rerank → Top-K。
- 默认 Embedding Qwen3-Embedding-8B（ModelScope）+ Rerank jina-reranker-v3；任何 OpenAI 兼容接口可换；无 Key 自动回退 BM25。
- context-agent 一键拿基础包：memory-contract load-context --chapter N（含 story_contracts/recent_summaries/urgent_loops/active_rules/protagonist/memory_pack/genre_profile_excerpt/author_style_patterns/style_contract）。
- 另有本地 CSV 知识库检索（reference_search.py）：爽点与节奏/桥段套路/命名规则/场景写法/写作技法/人设与关系/金手指与设定/裁决规则 8 张表 + 37 个题材模板。

### 6.5 任务书（Prompt 组装）思路【高】
context-agent 输出五段式"写作任务书"（自然语言，不暴露系统术语）：
1. 开篇委托（书名/章号/标题/一句话目标）
2. 这章的故事（前文摘要、目标/阻力、CBN/CPNs/CEN、必须覆盖/禁区、跨章约束、RAG 线索）
3. 这章的人物（每人一段：状态/驱动力/本章作用/说话倾向）
4. 怎么写更顺（把裁决层 style_priority/pacing_strategy 翻成具体指导；题材基调；anti_patterns 翻成自然提醒；审查得分趋势）
5. 收在哪里（结尾停在什么感觉，留什么未完感）

数据权重：用户要求 > 章纲原文 > MASTER_SETTING > reasoning 裁决 > CHAPTER_COMMIT > CSV 检索。组装后有红线校验清单（事实无冲突/时空承接/能力有来源/动机不断裂/时间正确/伏笔按紧急度输出…），fail 则重组。

### 6.6 Anti-AI 与润色【高】
起草后单独润色阶段：修非 blocking issue → 风格适配 → 排版 → Anti-AI 终检（fail 则不提交）。Anti-AI 指南列了 LLM 八大癖好（每段闭环/副词修饰一切/全员同反应/对话像辩论赛/情绪贴标签/信息均匀分布/安全着陆/展示后解释）+ 每段 5 个即时检查 + 替代方案速查表。原则："写的时候就写对，比写完再改好"；润色"只改表达不改事实"。

---

## 7. 技术栈与架构【高】

- **形态**：Claude Code Plugin（marketplace 安装）；8 个 Skill（Markdown 指令）+ 3-4 个 Subagent（context/reviewer/data/deconstruction，也是 Markdown）+ Python 数据层（scripts/data_modules，约 60 个模块）。
- **存储**：文件系统（Markdown 设定/大纲/正文/摘要 + JSON 状态/记忆/合同）+ SQLite（index.db 实体/别名/场景/出场/状态变化/关系/审查指标/追读力债务；vectors.db 向量）+ git（章节备份）。
- **RAG**：OpenAI 兼容 Embedding/Rerank API，BM25 兜底，RRF 融合，graph_hybrid 实体图谱。
- **Dashboard**：Python 后端（server.py + watcher）+ React/Vite 前端（预打包 dist），只读。
- **工程化**：pytest 测试、行为 eval（evals.json）、hooks（session_start / guard_runtime_write）、doctor/preflight 体检、run-ledger 断点续跑、user-report 统一作者报告。
- 关键架构决策：**Markdown SKILL 做编排逻辑（靠 LLM 执行），Python 只做确定性数据操作与校验**；"合同-提交-投影"事件溯源式架构，把 LLM 的不确定性关进闸门里。

---

## 8. 值得借鉴的经验与短板

### 8.1 值得借鉴（按价值排序）
1. **"合同-提交-投影"三段式**【高】：写前把约束固化为章级合同（目标/节点/禁区，违反即不通过），写后把事实固化为 commit（事件+delta），再投影成各种只读视图。写前写后分离，事实必须"入账"。这是对抗长篇遗忘最系统的设计。
2. **防幻觉三定律**【高】：大纲即法律、设定即物理、发明需识别——简单、可执行、贯穿所有 agent。
3. **结构化章纲节点（CBN/CPNs/CEN）+ 必须覆盖节点 + 本章禁区**【高】：让"章纲完成度"可机器校验（covered/missed_nodes），而不只是提示。
4. **追读力债务机制**【高】：软节奏规则可违背，但必须登记理由并欠债——比硬阻断更符合创作实际，又防止长期摆烂。
5. **伏笔紧急度公式与 ≤5 条并发上限**【高】：把"该收伏笔了"变成可计算的提醒。
6. **充分性闸门 + 分波次提问**【高】：初始化不急着生成，每轮只问"当前缺失且会阻塞下一步"的信息。
7. **创意约束包（反套路+硬约束+缺陷驱动）**【高】：用"约束"制造差异化，比"生成一个好创意"更可操作。
8. **时间线作为硬约束单列一表**（时间锚点/跨度/间隔/倒计时）【高】：时间一致性是 AI 长篇最容易崩的点之一。
9. **三级记忆（working/episodic/semantic）+ 状态降级（active/outdated/contradicted）**【高】：旧值不删只降级，保留审计与回溯。
10. **拆书 agent 的"只提取模式、严禁污染 canon"**【高】：参考借鉴与抄袭的边界工程化（do_not_copy、canon_contamination_warnings）。
11. **少打扰 + 作者友好报告契约**【高】：固定三段式报告（产物/异常分级：已自动处理·建议确认·必须处理/下一步命令），值得任何写作工具借鉴。
12. **失败只补跑失败步骤 + run-ledger 断点续跑**【高】。

### 8.2 短板与风险
1. **强绑定 Claude Code**【高】：整套方法论以 Claude Code 的 Skill/Subagent 机制为载体，迁移到其他模型/平台需要重写编排层（但数据层和方法论本身可移植）。
2. **链路重、章节成本高**【高】：一章要过 3 个 subagent + 多个脚本闸门 + 五路投影，token 与时间开销大；--fast/--minimal 是官方承认的降级出口。
3. **Episodic memory 偏近期**【高】（官方自述）：是"最近结构化证据层"，不是全书语义回忆；超远期细节的召回依赖 RAG 与人工 query。
4. **Semantic memory 仍是 JSON scratchpad**【高】（官方自述）：无独立长期语义向量层/图数据库；冲突裁决是轻量规则（主键去重+旧值降级），跨章语义矛盾仍需 reviewer 兜底。
5. **事实提取质量依赖 LLM 自觉**【中】：data-agent 的事件/delta 提取没有独立的二次核验（只有 schema 校验与 commit 闸门），提取漏了就会"账实不符"且不易发现。
6. **Reviewer 只查 5 个事实维度**【高】：爽点/节奏维度从 reviewer 剥离到 taxonomy 与 metrics，"事实审查"与"好看与否"的闭环不如一致性闭环严密。
7. **v7 重构公示中**【高】：作者自己认为 v6 有需要重构之处（Discussions #118），说明当前架构并非终态。
8. **中文目录名路径**【中】：设定集/大纲/正文等中文目录对跨平台脚本不友好（项目自己也在修 Windows 编码问题）。

---

## 附：调研方法与确定性说明
- 本报告基于 2026-07 时间点浅克隆的 master 分支（v6.2.x）：通读 8 个 SKILL.md、4 个 agents/*.md、关键 references（chapter-planning / foreshadowing / reading-power-taxonomy / strand-weave-pattern / cool-points-guide / anti-ai-guide）、输出模板（总纲/卷节拍表/卷时间线/主角卡/state-schema/index-schema）、docs（architecture/overview、memory 架构、rag-and-config）及部分 Python 源码文件名与关键函数。
- 【高】= 直接读自上述文件；【中】= 综合推断；【低】= 未验证（本报告基本未使用低确定性内容）。
- 仓库元数据（stars 5419 / forks 950 / GPL-3.0 / Python）来自 GitHub API。
