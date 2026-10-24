import { describe, expect, it } from 'vitest'
import { createAgentTextMessage } from '@/lib/agent-ui-message'
import { extractReplyProse, replySelectionTargets, selectionActionOrigin, type WritingSelectionTarget } from './selection-actions'

describe('reply prose extraction and turn ownership', () => {
  it('excludes chat commentary and requires an unambiguous marked prose block', () => {
    expect(extractReplyProse('Here is the revision:\n\n```manuscript\nOnly the story.\n```\n\nI improved the rhythm.')).toBe('Only the story.')
    expect(extractReplyProse('Here is my analysis and a possible revision.')).toBe('')
    expect(extractReplyProse('```js\nconst unsafe = true\n```')).toBe('')
    expect(extractReplyProse('```prose\nFirst version.\n```\n```prose\nSecond version.\n```')).toBe('')
  })

  it('shows replacement only after the action turn and stops at the next user message', () => {
    const target = {
      projectId: 'book-1', fileName: 'chapters/one.md',
      quote: { fileName: 'chapters/one.md', startLine: 1, endLine: 1, content: 'Original' },
    } as WritingSelectionTarget
    const messages = [
      createAgentTextMessage({ id: 'old', role: 'user', text: 'Older question' }),
      createAgentTextMessage({ id: 'old-reply', role: 'assistant', text: 'Older answer' }),
      createAgentTextMessage({ id: 'command', role: 'user', text: 'Polish' }),
      createAgentTextMessage({ id: 'reply', role: 'assistant', text: 'Prose' }),
      createAgentTextMessage({ id: 'child', role: 'assistant', text: 'Child prose', metadata: { subagent: true } }),
      createAgentTextMessage({ id: 'other', role: 'user', text: 'Another question' }),
      createAgentTextMessage({ id: 'other-reply', role: 'assistant', text: 'Other answer' }),
    ]
    expect([...replySelectionTargets(messages, new Map([['command', target]]))]).toEqual([['reply', target]])
  })

  it('does not guess a target for an unsent action or a replaced attachment', () => {
    const target = {
      projectId: 'book-1', fileName: 'chapters/one.md',
      quote: { fileName: 'chapters/one.md', startLine: 1, endLine: 1, content: 'Original' },
    } as WritingSelectionTarget
    const messages = [
      createAgentTextMessage({ id: 'old', role: 'user', text: 'Older question' }),
      createAgentTextMessage({ id: 'reply', role: 'assistant', text: 'Older answer' }),
    ]
    expect(replySelectionTargets(messages, new Map([['optimistic-command', target]]))).toEqual(new Map())
    expect(selectionActionOrigin([], target)).toBeUndefined()
    expect(selectionActionOrigin([{ fileName: 'chapters/other.md', startLine: 1, endLine: 1, content: 'Other' }], target)).toBeUndefined()
  })
})
