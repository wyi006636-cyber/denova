# 虾评小说 Skill 来源与接入矩阵 / Xiaping Novel Skill Source and Integration Matrix

## 结论 / Conclusions

- 当前核验 / Current verification：2026-07-21T08:10:59.229713Z 至 2026-07-21T08:13:30.2000268Z，仅使用公开、无需认证的 HTTPS GET。
- Current verification used public, unauthenticated HTTPS GET only, from 2026-07-21T08:10:59.229713Z through 2026-07-21T08:13:30.2000268Z.
- 搜索结果 / Search result：2 页，50 + 22，共 72 个去重 Skill。相对仓库历史快照：新增 0、消失 0、无法确认 0、版本变化 1；该变化不是固定九项之一。
- The current search returned 72 unique Skills across two pages (50 + 22): zero additions, zero removals, zero unconfirmed records, and one version change outside the fixed nine versus the repository snapshot.
- 固定候选 / Fixed candidates：9/9 详情、agent.md 和 ZIP 均已核验并计算 SHA-256；9/9 无明确包级许可证，全部禁止预置和再分发；5 个候选含 8 个脚本，均未执行。
- All nine fixed details, agent documents, and ZIPs were verified and hashed. All nine lack an explicit package license, so redistribution and preinstallation are prohibited; eight scripts across five candidates were inspected but never executed.
- 安全边界 / Security boundary：本地静态检查发现路径逃逸 0、符号链接 0、二进制 0、凭据样值 0。平台 security_status 仅作为来源元数据，不能证明运行时安全或强沙箱。
- Local static review found zero path escapes, symlinks, binaries, or credential-like values. Platform security_status remains advisory source metadata, not proof of runtime safety or strong sandboxing.
- 评测边界 / Evaluation boundary：54 条 tuning 静态适用性记录仅表示能力相关性；36 RELEVANT、4 IRRELEVANT、14 UNCERTAIN。9/9 仍为 PENDING-BLIND-REVIEW，不构成质量胜率、盲评或推荐结论。
- The 54 tuning observations express static applicability only: 36 RELEVANT, 4 IRRELEVANT, and 14 UNCERTAIN. All nine remain PENDING-BLIND-REVIEW; no win rate, blind-review result, or quality recommendation is claimed.

机器可复核的完整字段位于 xiaping-catalog-v1.json；6 个 tuning 任务与 54 条判断位于 fixtures/xiaping-static-applicability-v1.json。

## P0-T08A 全目录发现结果 / Full-catalog discovery result

- 完整公开快照 `snapshot-18d24eb4d408a116`：由 manifest 中 46 条 catalog 回执派生为 46 页（末两页分别为 49 和 1 项）、来源报告总数 2,251、去重后 2,237 条；确定性分类得到 1,072 个写作候选、4 个不可路由能力提案和 1 个重复簇。短名单为 65 项（DATA-RICH 34，EXPLORATION 31），覆盖全部 16 个核心 Capability；`character.build-dialogue-voice` 和 `outline.simulate-multiline` 分别仅有 1/5 与 2/5 个可信候选缺口。
- COMPLETE public snapshot `snapshot-18d24eb4d408a116`: 46 pages derived from the manifest's 46 catalog receipts (the final two contain 49 and 1 items), source-reported total 2,251, and 2,237 de-duplicated records; deterministic classification produced 1,072 writing candidates, 4 non-routable capability proposals, and 1 duplicate cluster. The shortlist has 65 entries (34 DATA-RICH and 31 EXPLORATION) across all 16 core Capabilities; `character.build-dialogue-voice` and `outline.simulate-multiline` remain short at 1/5 and 2/5 credible candidates.
- 本地受限缓存已有约 1,594 个评论检查点及 1,072 个详情记录，运行未出现 live 429 或 retry。10 个候选的公开评论证据无法闭合：4 个详情 `comment_count` 与首页 `total` 不一致；6 个在 limit=50 的空第 21 页前仍声明总数 1,159–4,628。它们被诚实记录为证据采集失败，不降低总数/分页协议，也不使用部分评论；本轮短名单未把这 10 项作为平台数据充足结果。
- The restricted local cache has about 1,594 comment checkpoints and 1,072 detail records, with no live 429 or retry in this run. Public review evidence could not close for 10 candidates: 4 detail `comment_count` values disagreed with the first-page `total`, and 6 declared totals of 1,159–4,628 before an empty page 21 at limit 50. They remain honest collection failures: no total or pagination rule was weakened, no partial comments were used, and none is presented as DATA-RICH in this shortlist.
- 该发现是元数据和聚合平台证据，不是写作质量结论；未下载、执行或评审第三方包，也未修改 P0-T08 深度审计 catalog。
- This discovery is metadata and aggregate platform evidence, not a writing-quality result; no third-party package was downloaded, executed, or reviewed, and the P0-T08 deep-audit catalog was not changed.

## 当前原始来源 / Current Original Sources

| 来源 / Source | 当前结果 / Current result | SHA-256 / Evidence |
|---|---|---|
| https://xiaping.coze.com/ | GET 200；194,960 bytes；2026-07-21T08:13:26.7593814Z | f619a296ac26bf984f3da03dec22ef0fbc0d69893b004477176cb39b5c95895f |
| https://xiaping.coze.com/skill.md | GET 200；35,422 bytes；2026-07-21T08:13:30.2000268Z | 80672e853798bd865259b7842ce6e8b074be516461880b457ceeb7e6cc112751 |
| GET /api/skills?search=小说&limit=50&page=1 | 200；50 项；hasMore=true | 6d5e70953e5a2663655d51507cbdb15a032a73a2c2017f4a5474520e601beb78 |
| GET /api/skills?search=小说&limit=50&page=2 | 200；22 项；hasMore=false | f765cf9bca761d9e24642ef76de5d7fa13ff206afa48e5dd95071883154c6f4b |
| GET /api/skills/{skill_id} | 固定 9 项全部 200 JSON | 每项响应 hash 见 catalog |
| GET /skill/{skill_id}/agent.md | 固定 9 项全部 200 Markdown | 响应含时效链接，hash 标为 volatile；链接本身不入库 |
| GET /api/skills/{skill_id}/download | 固定 9 项匿名请求均 401 | 未发送 Authorization、Cookie 或 API Key |

agent.md 中公开的时效下载链仅在本 Goal 自有临时目录内解析和使用；catalog 只保留稳定 agent.md URL、抓取时间和内容 hash。

### 与历史快照差异 / Difference from Historical Snapshot

仓库内 Kimi 报告和四个证据文件只能作为历史快照。旧快照记录 72 项；本轮重新分页得到 72 项，ID 新增 0、消失 0。唯一版本变化为非固定候选 e7923dd4-3e07-4d15-8065-1b7a4a19b1b9，从 1.1.1 变为 1.1.2。固定九项的名称、作者和版本变化为 0。

## 固定九项来源矩阵 / Fixed Nine Candidate Matrix

| ID / Skill | 版本 / 状态 | Archive SHA-256 | 文件 / 脚本 | 许可 / 权限与安全 | 评测 / 接入 |
|---|---|---|---|---|---|
| a3504d81-736d-49b3-9a98-2a4a3cd3aaf1 小说助手 | 1.0.3 official | a31a3241f3d1df34fa5034654c8719213d18f781012663ff4bca98611ddbbeb3 | 3 files；2 scripts | LICENSE-UNKNOWN；脚本可读写、重命名、覆盖记忆文件；文档建议可选 Git pull/push；需逐次授权 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| 12c7fc40-3ae0-474a-a21f-a9b8aff4b572 多米的长篇小说创作 | 1.0.0 official | 40b83c962955c50034de85000a38424fc50db6a3468a4e9e1e0344474207cccf | 23 files；1 script | LICENSE-UNKNOWN；文件写入、可选 Bash/pandoc；v5-check.sh 为 0 字节 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| 5a7d600a-1ff2-4d11-818c-e080f405deb9 深度小说写作法 | 1.0.1 official | 96459babf584b3a248885583cdbe236e5f8d47ae8b906d3f6cbfc7650815bb18 | 7 files；0 scripts | LICENSE-UNKNOWN；纯静态文本；manifest 声称源于具名作品，禁止复制分发 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| 7e465336-639f-469e-a268-9633cfd7d448 黄金开篇大师 | 2.2.0 official | 444220a2994074a6e193fca30922185ae34c96a4aa8b0b36f37d1f2a15902738 | 3 files；1 script | LICENSE-UNKNOWN；可选 Python 写标题文件；文档称 3 个脚本但包内仅 1 个 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| 8643f73c-7eeb-4409-9136-09c9a125c2ff 人味写作引擎 | 1.0.1 official | 2d5f8aac4c968dec341f8741531b044b72a5b7cd82d98f78122933d77422fa5d | 6 files；0 scripts | LICENSE-UNKNOWN；无需脚本/网络；长篇仅限有界片段修订 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| 95687260-d5ad-43d3-b083-008d00a9dfa3 人物塑造大师 | 2.2.0 official | acc80c09366b13511e8d10eefce4cc2edce17665f430eac1d0a2d62e22a49f8c | 7 files；2 scripts | LICENSE-UNKNOWN；可选 Python 读取正文/人物卡并写 Markdown/JSON | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| 56ed7d78-5636-4c78-83cc-0aee4802b0b7 小说爽点架构生成器 | 5.3.0 official | 7cff5ac590ae47c19f3fc4a666dcc435c4b478aede034f89af21590b6b66d512 | 96 files；0 scripts | LICENSE-UNKNOWN；引用缺失 dispatcher.py；一种研究模式声明联网；SKILL.md 含 2,256 个 Unicode Cf 隐藏字符 | STATIC-REVIEWED；独立 findings 已记录；PENDING-BLIND-REVIEW；HOLD |
| f47e299c-3861-421d-9f1d-971de45bf622 小说审校员 | 1.48.0 official | ab96990fcaf65dcea8d2da293f8ca9d449e6ece3a5313845b80609a963e5112c | 1 file；0 scripts | LICENSE-UNKNOWN；批量模式要求显式文件/目录读取授权 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |
| d4f61b34-aa84-4d75-aa43-fa2ae81823c2 小说框架维度化器 | 2.19.0 official | dd2b50c2868bdb86c3c65cbea87d02773de9a3b33864de8661087c58c886e1fb | 3 files；2 scripts | LICENSE-UNKNOWN；可选 Python 递归读 Markdown、建目录并写报告 | STATIC-REVIEWED；PENDING-BLIND-REVIEW；HOLD |

每项 manifest SHA-256、规范化静态文件清单 SHA-256、脚本逐文件 hash、平台元数据和完整权限记录均在 catalog 中；catalog 的 hashing 字段冻结了 raw archive、精确 SKILL.md member 和排序 JSON 文件清单的复算算法。公开可下载不等于允许本地使用、允许再分发或允许预置；本轮九项在后三者上均未取得明确许可证证据。

## Capability 初映射 / Initial Capability Mapping

任务说明中的“15”是计数笔误；显式清单与已批准方案第 11 节均为以下 16 项，用户已确认按 16 项继续。GAP 数为 0，但所有映射只是静态候选。

| Capability | 候选 / Candidates | Profile | 阶段与 I/O / Stage and I/O | 最小权限与证据 / Minimum permission and evidence |
|---|---|---|---|---|
| ideation.generate-premises | 多米、深度、黄金、爽点 | 三者；短篇需裁剪 | 构思；brief → premise candidates | 纯文本；各包 manifest capability evidence |
| project.bootstrap-genre | 多米、黄金、爽点 | long/fanqie 优先 | 初始化；genre target → scoped project proposal | 纯文本；不得写 Harness 状态 |
| outline.build-long-arc | 多米、深度、爽点、维度化器 | long_serial 优先 | 大纲；premise/state → long arc | 纯文本或显式文件读写 |
| outline.build-short-structure | 多米、深度、黄金、爽点 | fanqie/zhihu 优先 | 短篇结构；brief → bounded structure | 纯文本 |
| outline.simulate-multiline | 多米、深度 | long_serial | 推演；premise/characters → line simulation | 纯文本、长上下文 |
| outline.build-execution-brief | 多米、爽点、维度化器 | long 优先，短篇条件适用 | 施工单；outline → chapter/scene brief | 默认纯文本；脚本路径需文件授权 |
| character.build-profile | 多米、深度、人物塑造 | 三者 | 人物设计；seed → profile/card | 纯文本；可选文件写入需确认 |
| character.build-dialogue-voice | 小说助手、深度、人物塑造、维度化器 | 三者 | 人物/对白；profile/sample → voice constraints | 纯文本或有界文件读取 |
| opening.review-hook | 多米、深度、黄金、爽点、维度化器 | fanqie 优先，其他可用 | 开篇审查；opening → hook findings | 纯文本 |
| engagement.review-reading-drive | 多米、深度、黄金、爽点、审校员、维度化器 | long/fanqie 优先 | 阅读动力审查；draft/outline → findings | 纯文本；爽点联网模式默认禁用 |
| plot.manage-foreshadowing | 小说助手、多米、深度、爽点、审校员、维度化器 | long 优先 | 规划/审查；state/draft → foreshadow ledger/findings | 有界文本；文件写入另行授权 |
| continuity.review-facts | 小说助手、多米、爽点、人物塑造、审校员、维度化器 | 三者 | 事实审；bounded context/draft → issue list | 有界文件读取；禁止无限历史注入 |
| editor.review-story | 多米、深度、爽点、审校员、维度化器 | 三者 | 独立审稿；draft + QualitySpec → issues | 纯文本或有界文件读取 |
| editor.review-profile-rubric | 多米、人物塑造、审校员 | 三者 | Profile 审查；draft + rubric → findings | 纯文本；Profile 闭集校验 |
| style.revise-naturalness | 人味、爽点、维度化器 | fanqie/zhihu 优先 | 修订；selection → revised selection | 纯文本；必须保留作者定稿边界 |
| style.revise-prose | 多米、深度、人味、爽点、审校员 | 三者 | 修订；selection/issues → prose candidate | 纯文本；不得直接覆盖正式正文 |

所有全流程 Skill 都按 Capability 拆分。它们不得决定候选状态、Quality Gate、Profile 切换、版本写入、Author Finalization 或 Harness 下一步。

## 三 Profile 适用矩阵 / Three-Profile Applicability

R = RELEVANT，U = UNCERTAIN。R 仅为静态能力相关，不代表质量通过。

| Skill | long_serial | fanqie_short | zhihu_salt_short |
|---|---:|---:|---:|
| 小说助手 | R | U | U |
| 多米的长篇小说创作 | R | U | U |
| 深度小说写作法 | R | R | R |
| 黄金开篇大师 | R | R | R |
| 人味写作引擎 | U | R | R |
| 人物塑造大师 | R | R | R |
| 小说爽点架构生成器 | R | R | U |
| 小说审校员 | R | R | R |
| 小说框架维度化器 | R | U | U |

## P0-T07 tuning 静态初评 / Static Tuning Review

P0-T07 manifest 共 36 项：tuning 18、regression 12、release_holdout 6。仅选择每个 Profile 中 task_id 字典序前两项，共 6 项；regression 和 release_holdout 输入读取数均为 0。

| Profile | 选中任务 / Selected tasks |
|---|---|
| long_serial | ls-mystery-dialogue-03；ls-mystery-ending-07 |
| fanqie_short | fq-revenge-dialogue-09；fq-urban-dialogue-03 |
| zhihu_salt_short | zh-family-dialogue-09；zh-marriage-dialogue-03 |

fixture 覆盖 9 × 6 = 54 条：RELEVANT 36、IRRELEVANT 4、UNCERTAIN 14。任务上的 Capability 是 P0-T08 静态推断，corpus manifest 本身没有 Capability 字段。没有调用模型、运行脚本、生成小说正文、打分或读取受限 split 输入。

## 现有安装链可复用能力与缺口 / Reusable Installer Capabilities and Gaps

| 能力 | 状态 | 源码证据 |
|---|---|---|
| Remote/GitHub 预览与安装 | 可复用 | internal/skills/remote.go:126-163；internal/skills/github.go:31-52,55-101 |
| HTTPS 与重定向约束 | 可复用 | internal/skills/remote.go:32-58 |
| DNS/SSRF 私网与保留地址拒绝 | 可复用 | internal/skills/remote.go:61-114；internal/skills/remote_security_test.go:11-47 |
| ZIP 32 MiB、4,096 文件、128 MiB 解压限制 | 可复用 | internal/skills/install.go:19-25,225-283；internal/skills/remote.go:267-283 |
| 路径穿越与 symlink 防护 | 可复用 | internal/skills/install.go:247-251,550-568 |
| 选择性安装与 staging | 可复用 | internal/skills/install.go:132-222,342-457；web/src/features/skills/SkillInstallPanel.tsx:38-112,291-332 |
| source record | 缺失 | RemoteArchiveSource/GitHubSource 仅有 url/ref/subdir：internal/skills/remote.go:120-124；internal/skills/github.go:16-21 |
| archive/content hash | 缺失 | internal/skills/install.go:578-581 的 SHA 仅用于截断 candidate ID，不是内容 hash |
| license | 缺失 | internal/skills/types.go:28-69；internal/skills/install.go:45-66 无许可字段 |
| requested/granted permission | 缺失 | internal/skills/types.go:28-69 无安装权限模型 |
| evaluation linkage | 缺失 | App 层仅透传安装并记录 scope/count：internal/app/skills_app_service.go:43-65,114-150；API handler 只映射 source/scope/candidate_ids：internal/api/handlers/handler_skills.go:40-46,192-249；前端候选仅展示名称、冲突、路径和描述：web/src/features/skills/SkillInstallPanel.tsx:307-316 |
| update comparison | 缺失 | internal/skills/install.go:442-457 只有目标冲突，无 source/ref/hash diff |
| provenance rollback | 缺失 | web/src/features/skills/SkillsView.tsx:219-257,296-355 只有通用删除/恢复 builtin |
| 前端 provenance/hash/license/permission/evaluation/update/rollback 展示 | 缺失 | web/src/features/skills/SkillInstallPanel.tsx:307-316 只显示名称、冲突、路径和描述 |

P0-T08 只记录缺口，不实现上述产品功能。

## 计数 / Counts

| 指标 | 数量 |
|---|---:|
| SOURCE-UNAVAILABLE | 0 |
| SOURCE-VERIFIED | 9 |
| LICENSE-UNKNOWN | 9 |
| PENDING-BLIND-REVIEW | 9 |
| 含脚本候选 / Candidates with scripts | 5 |
| 脚本总数 / Script files | 8 |
| Capability GAP | 0 |
| 隐藏格式控制字符发现 / Hidden format-control finding | 1 candidate / 2,256 code points |

## 事实、推断、待验证 / Facts, Inferences, Pending Validation

| 类别 | 内容 |
|---|---|
| 事实 / Facts | 当前 GET 状态、72 项分页结果、9 个 ZIP/hash、文件和脚本清单、许可证文件缺失、静态权限发现、隐藏字符、installer 源码能力与缺口。 |
| 推断 / Inferences | Capability 映射、Profile 适用性、6 个 tuning 任务相关性、最小权限建议。它们均可撤销，并受后续合同和评测约束。 |
| 待验证 / Pending | 真实作者使用许可、商业再分发/预置权、运行时行为、强沙箱、模型与成本适配、盲评质量、更新后回归和可逆回滚。 |

本矩阵不是法律意见，也不是质量推荐榜。任何接入必须满足 SKILL_INTEGRATION_RED_LINES.md。
