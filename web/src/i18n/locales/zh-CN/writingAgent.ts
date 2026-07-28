const writingAgent = {
  'writingAgent.initPrompt': '请和我一起启动一本新书：先读取 ideas.md 和 CREATOR.md，通过对话梳理灵感、题材、冲突、世界观、人设和写作规则；信息不足时先追问，阶段性结论更新到 ideas.md。暂时不要创建大纲、章节或写入资料库。',
  'writingAgent.fanqieInitPrompt': '我想创作番茄短篇《{{title}}》。目前的想法：{{idea}}。请使用 fanqie-short Skill，从交流想法开始，只追问现在最必要的 1～3 个问题；先给我确认故事方案，再给分章大纲确认，确认前不要写正文。',
  'writingAgent.fanqieIdeaUnset': '还没有成形，请先帮我找到最有压力和反转潜力的切入点',
} as const

export default writingAgent
