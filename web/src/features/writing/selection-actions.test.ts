import { describe, expect, it } from 'vitest'
import { createAgentTextMessage } from '@/lib/agent-ui-message'
import { extractReplyProse, replySelectionTargets, type WritingSelectionTarget } from './selection-actions'

describe('reply prose extraction and turn ownership', () => {
  it('excludes chat commentary and requires an unambiguous marked prose block', () => {
    expect(extractReplyProse('Here is the revision:\n\n```manuscript\nOnly the story.\n```\n\nI improved the rhythm.')).toBe('Only the story.')
    expect(extractReplyProse('Here is my analysis and a possible revision.')).toBe('')
    expect(extractReplyProse('```js\nconst unsafe = true\n```')).toBe('')
    expect(extractReplyProse('```prose\nFirst version.\n```\n```prose\nSecond version.\n```')).toBe('')
  })

  it('does not let a later unrelated turn or a subagent replace the originating selection', () => {
    const target = { projectId: 'book-1', fileName: 'chapters/one.md' } as WritingSelectionTarget
    const messages = [
      createAgentTextMessage({ id: 'command', role: 'user', text: 'Polish' }),
      createAgentTextMessage({ id: 'reply', role: 'assistant', text: 'Prose' }),
      createAgentTextMessage({ id: 'child', role: 'assistant', text: 'Child prose', metadata: { subagent: true } }),
      createAgentTextMessage({ id: 'other', role: 'user', text: 'Another question' }),
      createAgentTextMessage({ id: 'other-reply', role: 'assistant', text: 'Other answer' }),
    ]
    expect([...replySelectionTargets(messages, new Map([['command', target]]))]).toEqual([['reply', target]])
  })

  it('keeps the first selection when transcript hydration replaced the optimistic command id', () => {
    const target = { projectId: 'book-1', fileName: 'chapters/one.md' } as WritingSelectionTarget
    const messages = [
      createAgentTextMessage({ id: 'canonical-user', role: 'user', text: 'Polish' }),
      createAgentTextMessage({ id: 'reply', role: 'assistant', text: 'Prose' }),
      createAgentTextMessage({ id: 'later-user', role: 'user', text: 'Other question' }),
      createAgentTextMessage({ id: 'later-reply', role: 'assistant', text: 'Other answer' }),
    ]
    expect([...replySelectionTargets(messages, new Map([['optimistic-command', target]]))]).toEqual([['reply', target]])
  })
})
