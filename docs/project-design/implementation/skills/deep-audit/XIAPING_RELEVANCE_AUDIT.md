# Xiaping Novel Skill Relevance Audit / 虾评小说 Skill 相关性审计

## Scope / 范围

This is a frozen human-curated content-audit batch, not an approval, installation, or writing-quality result. It joins the completed public snapshot (`snapshot-18d24eb4d408a116`), its 65-entry automated shortlist (50 unique Skills), 1,072 writing candidates, and the original nine-candidate catalog. All platform counts below are source metadata only.

这是冻结的人为筛选内容审计批次，不是批准、安装或写作质量结论。它关联完整公开快照（`snapshot-18d24eb4d408a116`）、65 项自动短名单（50 个唯一 Skill）、1,072 个写作候选和原始九候选目录。下列平台数据仅是来源元数据。

- Snapshot: 46 catalog pages; reported 2,251 records; 2,237 de-duplicated records.
- 快照：46 个目录页；来源报告 2,251 条；去重后 2,237 条。
- Automated lanes: 34 DATA-RICH and 31 EXPLORATION entries; 10 evidence-collection failures remain limitations, not quality scores.
- 自动通道：34 项 DATA-RICH、31 项 EXPLORATION；10 项证据采集失败仍是限制，不是质量分数。
- Closed profiles: `long_serial`, `fanqie_short`, `zhihu_salt_short`. Closed capability set is the 16 IDs recorded in [the batch JSON](xiaping-deep-audit-batch-v1.json).
- 封闭画像：`long_serial`、`fanqie_short`、`zhihu_salt_short`。封闭能力集为[批次 JSON](xiaping-deep-audit-batch-v1.json) 中记录的 16 个 ID。

## Frozen batch / 冻结批次

Wave A contains six directly novel-writing, higher-evidence entries. Wave B intentionally fills profile and capability gaps, including two explicitly labelled EXPLORATION candidates. The full IDs, selection basis, closed targets, and exact platform fields are in the JSON; no package contents, review bodies, reviewer identities, or signed URLs are committed.

Wave A 包含六个直接小说写作、证据较多的条目。Wave B 有意补齐画像和能力缺口，其中包括两个明确标为 EXPLORATION 的候选。完整 ID、选择依据、封闭目标和精确平台字段在 JSON 中；不提交包内容、评论正文、评审身份或签名 URL。

| Wave | Skill / Skill | Selection basis / 选择依据 |
| --- | --- | --- |
| A | 小说助手 | DATA-RICH；原始目录；长篇连续性、伏笔 |
| A | 深度小说写作法 | DATA-RICH；原始目录；小说技法、人物、伏笔 |
| A | 小说爽点架构生成器 | DATA-RICH；网文爽点与阅读驱动 |
| A | 小说审校员 | DATA-RICH；原始目录；故事与连续性审校 |
| A | 人味写作引擎 | DATA-RICH；受限的自然化/润色切片 |
| A | 小说创作大神·100+位作家风格引擎 | DATA-RICH；仅审计可拆分的小说切片 |
| B | AI文本人性化 | DATA-RICH；文风对照组 |
| B | 小说框架维度化器 | 原始目录；长篇框架与一致性 |
| B | 多米的长篇小说创作 | 原始目录；长篇七阶段流程 |
| B | 黄金开篇大师 | 原始目录；开篇钩子缺口 |
| B | 人物塑造大师 | 原始目录；人物画像缺口 |
| B | 番茄短故事创作法 | 原始目录；番茄短篇画像 |
| B | 盐言小说创作 | 原始目录；盐言短篇画像 |
| B | 情节架构大师 | EXPLORATION；多线大纲仅有两项可信自动候选之一 |
| B | 网文写作前必检八项 | EXPLORATION；网文写前检查/故事审校 |

## Rejected automated-shortlist false positives / 被拒绝的自动短名单误报

Each row below records every automated-shortlist Skill not retained in the 15-candidate batch. Reasons are deliberately narrow: a capability-keyword match is insufficient without a direct novel-writing connection.

下表记录了 15 候选批次未保留的每一个自动短名单 Skill。理由刻意收窄：仅命中能力关键词、但未直接关联小说写作，不足以进入批次。

| Skill ID | Skill / 名称 | Class | Reason / 原因 |
| --- | --- | --- | --- |
| d99f52ed-dd28-4c35-ac3d-0dd84ec37a28 | Agent配置卫士 | GENERIC_TOOLING | Agent configuration security, not fictional character design / Agent 配置安全，非人物创作 |
| ef98e945-6e0d-4a58-8c9c-c6bbe3dab4ea | Agent成长追踪 | GENERIC_TOOLING | Agent growth telemetry, not prose revision / Agent 成长追踪，非文稿润色 |
| 806e3dbc-dc8a-431f-843d-a91383deff35 | AI不说谎 | GENERIC_TOOLING | Agent rules and safety framework / Agent 规则与安全框架 |
| 6e1d94d0-d03a-4583-9aaf-99e9f04b5520 | AI做市商日记 | NON_WRITING_DOMAIN | Securities-market diary / 证券市场日记 |
| f705a76c-c9c5-4e41-b934-a8a47996992e | A股实时行情助手 Pro | NON_WRITING_DOMAIN | Stock analysis / 股票分析 |
| b42663ba-5409-4843-89c6-3b51746b5c8e | InStreet虾评 社区互动助手 | SOCIAL_CONTENT_ONLY | Platform operation and engagement / 平台运营互动 |
| 8bf2a1d3-8ad1-45d7-8708-766c7a6b1124 | OpenClaw 心智矩阵自进化系统 | GENERIC_TOOLING | Agent self-improvement system / Agent 自进化系统 |
| e3ccb282-5c81-42ab-a811-d88791eb8178 | PUA万能激励引擎 | GENERIC_TOOLING | General Agent prompting/debugging / 通用 Agent 提示与调试 |
| 74bb44aa-022c-4690-95dd-51b81fd222b3 | Red·爆款钩子 | SOCIAL_CONTENT_ONLY | Xiaohongshu hooks, not fiction openings / 小红书钩子，非小说开篇 |
| 23051889-089c-42a0-819b-fe894de45963 | Skill 安全扫描 | GENERIC_TOOLING | Skill security scanning / Skill 安全扫描 |
| 45cfc35c-95c4-4377-ab15-899d476c03e5 | skill-vetter | GENERIC_TOOLING | Skill security vetting / Skill 安全审查 |
| 6356c7fe-12ec-4036-9e8f-f349c0a0aed4 | SVG架构图生成器 | NON_WRITING_DOMAIN | Architecture diagram generation / 架构图生成 |
| aee3dcad-7080-4a00-82ed-4f7291ce32c8 | ToolCallEval · Agent工具调用能力评测 | GENERIC_TOOLING | Agent tool-call evaluation / Agent 工具调用评测 |
| 3a433283-b8a5-41c9-80d0-4d38a6ca10df | 互动知识图谱搭建技能 | GENERIC_TOOLING | Knowledge-graph visualization / 知识图谱可视化 |
| 568be137-29fa-485c-8763-41f1f4a0d1c7 | 人文社科稿件初审技能 | INSUFFICIENT_NOVEL_LINK | Academic-paper review, no novel scope / 学术论文初审，无小说范围 |
| 1924944f-6e45-4f4c-ad06-6c7ac2b20717 | 从忙碌到高效 - Agent 精准工作法 | GENERIC_TOOLING | Agent productivity and memory management / Agent 效率与记忆管理 |
| 4a5aaf84-fca5-483c-8a52-da61318db8f6 | 内容诊断与选题推荐 | SOCIAL_CONTENT_ONLY | Self-media diagnosis and topics / 自媒体诊断与选题 |
| ea80da9e-7196-4e7a-9e95-d1c0a8d6353a | 写作风格技能 | INSUFFICIENT_NOVEL_LINK | Literary-style claim lacks capability-specific novel link / 文风主张缺少能力级小说关联 |
| b708f427-6503-4a6b-a338-6e46cd1a4bda | 力学计算 | NON_WRITING_DOMAIN | Engineering mechanics / 工程力学 |
| fb86cc77-eae1-41cf-8332-352649626db1 | 双子辩论写小说 · 扣子 ×Kimi 7 轮仲裁法 | GENERIC_TOOLING | Multi-model workflow, not a bounded reference slice / 多模型流程，非可边界化参考切片 |
| 3ea7d696-1806-4021-92e0-3439860d0855 | 口播稿5遍质检法 | SOCIAL_CONTENT_ONLY | Spoken-script QA, not novel prose / 口播稿质检，非小说文稿 |
| 245a4eec-21c1-48de-bd83-be3f97161076 | 家具培训课件智造官 | NON_WRITING_DOMAIN | Furniture-sales training materials / 家具销售培训材料 |
| e03452b0-aa43-48d2-bc39-6ef6d386864f | 小红书爆款标题生成器 | SOCIAL_CONTENT_ONLY | Xiaohongshu title formulas / 小红书标题公式 |
| 976a4a25-74b2-4852-8db7-e615ff098fc9 | 平面设计创意布局大师 | NON_WRITING_DOMAIN | Architectural layout planning / 建筑平面布局 |
| 572b33b4-828f-4d5e-abc5-788f015af538 | 幽默剧本增强 | INSUFFICIENT_NOVEL_LINK | Screenplay-only enhancement, no novel evidence / 剧本增强，无小说证据 |
| 3742382e-0a12-4ecc-a4f1-fad439e462f8 | 强结构---支线事件驱动观察池 | NON_WRITING_DOMAIN | Investment event watchlist / 投资事件观察池 |
| 638f81e5-e4fb-4090-a59c-06c81c341db5 | 影拾（朋友圈神器） | MEDIA_ONLY | Film still retrieval for social posts / 影视截图检索用于社交发布 |
| 8e9fbce1-c688-4c83-8fff-4f1ee7418611 | 律师协作框架 | NON_WRITING_DOMAIN | Legal-agent collaboration / 法律 Agent 协作 |
| f323f0d2-2a93-40da-bda2-f7a9f771c265 | 报废资产投标全流程助手 | NON_WRITING_DOMAIN | Asset-tender workflow / 资产投标流程 |
| c3046817-cbed-4c63-a7bc-88c95066eb7e | 正则表达式助手 | GENERIC_TOOLING | Regex development aid / 正则开发工具 |
| 8d24564b-58c3-4bdc-ab0b-2423fd6dd2ad | 每日简报 | GENERIC_TOOLING | Sales briefing / 销售简报 |
| f0abe330-2b79-4898-a48e-a45fdf3c15b6 | 毒点检测器 | INSUFFICIENT_NOVEL_LINK | Novel-related claim retained for later exploration, but this batch prioritizes direct auditable methods / 有小说声明但本批优先直接可审计方法 |
| 07480ade-0087-4fe0-8b50-02e20b94d9ac | 牛股起爆点跟踪系统 | NON_WRITING_DOMAIN | Stock tracking / 股票跟踪 |
| e40a47b2-178d-4f23-be06-0497523cc86e | 理喀写法 | INSUFFICIENT_NOVEL_LINK | Explanatory article framework, not novel writing / 解释型文章框架，非小说写作 |
| 7fee05ce-81e0-4046-a62d-d2fa10d32908 | 穿搭搭配助手 | NON_WRITING_DOMAIN | Fashion advice / 穿搭建议 |
| bca59879-292c-45bf-8b0b-c6eb5a77d86c | 网络聊天助手 | INSUFFICIENT_NOVEL_LINK | Everyday chat scripts, not character voice / 日常聊天脚本，非角色口吻 |
| 4df2d104-49c7-4e40-a82b-8b845943982f | 股票筹码分析助手 | NON_WRITING_DOMAIN | Stock-chip analysis / 股票筹码分析 |
| 76143503-d81c-46cc-8cef-1ea2129bd4da | 行业资讯智能推送 | GENERIC_TOOLING | News aggregation / 新闻聚合 |
| b9460eb7-1a0d-4e21-8545-fdc591092783 | 设计评审 | NON_WRITING_DOMAIN | Product/design review / 产品设计评审 |
| 4dac3dfc-b8bd-4bde-8578-938b2f9cf3a0 | 针织电商图片设计 | MEDIA_ONLY | E-commerce image workflow / 电商图片工作流 |
| 52196eb5-d68c-4d74-aee4-79d125bcaa59 | 飞书多维表格-官方 | GENERIC_TOOLING | Feishu Bitable operations / 飞书多维表格操作 |

No rejected row has been converted into a positive quality claim. A retained candidate still requires content retrieval, static safety inspection, capability-scoped assessment, and later paired evaluation before it can be used as a bounded reference.

任何拒绝记录均未被转化为正向质量结论。保留候选仍须经过内容获取、静态安全检查、能力边界评估和后续配对评测，才能作为受限参考使用。
