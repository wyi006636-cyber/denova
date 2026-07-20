# 小说写作工具设计资料索引

本目录保存基于 Denova 演进小说写作工具所需的产品、架构、评审与调研资料。

## 当前开发基线

- 上游项目：`alfredxw/denova`
- 上游分支：`master`
- 基线提交：`eb5e4ee53ad158fe88dfb7148408edc6558e481a`
- 开发分支：`feat/quality-harness-foundation`
- 仓库策略：从最新 Denova 独立克隆；旧项目仅作为既有成果和迁移参考，不直接合并其工作区状态
- 远端策略：官方仓库登记为 `upstream`；个人 Fork 建立后再登记为 `origin`

## 文档优先级

### 1. 最终实施基线

- [`final/小说写作工具-最终融合最优方案.md`](final/小说写作工具-最终融合最优方案.md)

该文档是当前产品与架构决策的唯一实施基线。发生冲突时，来源方案只作为论据和备选，不覆盖最终方案。

### 2. Kimi 方案与调研

- [`sources/kimi/小说写作工具-项目设计方案.md`](sources/kimi/小说写作工具-项目设计方案.md)
- [`sources/kimi/denova-调研报告.md`](sources/kimi/denova-调研报告.md)
- [`sources/kimi/DESIGN_denova.md`](sources/kimi/DESIGN_denova.md)
- [`sources/kimi/webnovel-writer-调研报告.md`](sources/kimi/webnovel-writer-调研报告.md)
- [`sources/kimi/novelforge-agent-调研报告.md`](sources/kimi/novelforge-agent-调研报告.md)
- [`sources/kimi/xiaping_research_report.md`](sources/kimi/xiaping_research_report.md)
- `sources/kimi/xiaping-evidence/`：虾评检索与详情页原始证据
- `sources/kimi/screenshots/`：Denova 界面参考截图

### 3. WorkBuddy 方案与评审

- [`sources/workbuddy/deliverables/software-company/novel-writer-PRD.md`](sources/workbuddy/deliverables/software-company/novel-writer-PRD.md)
- [`sources/workbuddy/deliverables/software-company/novel-writer-architecture.md`](sources/workbuddy/deliverables/software-company/novel-writer-architecture.md)
- [`sources/workbuddy/deliverables/software-company/codex-review-feedback.md`](sources/workbuddy/deliverables/software-company/codex-review-feedback.md)
- `sources/workbuddy/deliverables/software-company/docs/`：类图与时序图

## 开发约束

1. Harness 的目标是提高优秀网文的产出概率，自动化只是实现手段。
2. 同时支持长篇、番茄短篇和知乎盐选短篇，以 Profile 表达平台差异。
3. 本地文件是作品事实真源；索引、缓存和运行状态必须可重建。
4. AI 生成、审稿和修订结果必须经过作者确认，不能自动覆盖正式正文或设定。
5. 保留 Denova 的 Go/Hertz、Eino、Agent、Skill、Automation、SSE、工作区和版本能力，按模块化单体演进。
6. 旧 `denova` 项目的本地代码不自动迁入；任何迁移都需要逐项审计、测试和单独提交。

## 下一步

开发从最终方案的 Phase 0 / 基础设施阶段开始：先建立 QualitySpec、Profile、Capability Registry 和质量评测基线，再进入具体写作流程与前端重构。
