const writingAgent = {
  'writingAgent.initPrompt': 'Help me start a new book: read ideas.md and CREATOR.md, then use conversation to shape the spark, genre, conflict, world, cast, and writing rules. Ask first when details are missing, and update interim conclusions in ideas.md. Do not create outlines, chapters, or lore yet.',
  'writingAgent.fanqieInitPrompt': 'I want to create the Fanqie short story “{{title}}”. Current idea: {{idea}}. Use the fanqie-short Skill and begin by discussing the idea. Ask only the 1–3 questions that matter now, then show me a story proposal for confirmation and a chapter outline for a second confirmation. Do not write prose before both confirmations.',
  'writingAgent.fanqieIdeaUnset': 'It is not formed yet; help me find the angle with the strongest pressure and reversal potential',
} as const

export default writingAgent
