import { describe, expect, it } from 'vitest'
import type { AgentMessageView } from '@/lib/agent-message-view'
import type { AgentMessageMetadata } from '@/lib/agent-ui'
import { buildContextReceipt } from './context-receipt'

function view(partial: Partial<AgentMessageView> & Pick<AgentMessageView, 'kind'>): AgentMessageView {
  return {
    key: partial.key || `${partial.kind}-key`, messageId: partial.messageId || `${partial.kind}-message`,
    partId: partial.partId || `${partial.kind}-part`, partIndex: 0,
    ref: { messageId: partial.messageId || `${partial.kind}-message`, partId: partial.partId || `${partial.kind}-part`, partIndex: 0, type: partial.kind },
    part: {} as AgentMessageView['part'], metadata: partial.metadata || {}, data: partial.data || {},
    content: partial.content || '', streaming: false, status: partial.status,
    toolName: partial.toolName, input: partial.input, output: partial.output, errorText: partial.errorText,
    ...partial,
  }
}

const readEnvelope = (path: string, status = 'success', kind = 'local_text') => JSON.stringify({
  schema: 'resource.read.v1', status, source: { kind, path }, limits: { truncated: false },
}) + (status === 'not_found' ? '\nmissing' : '\n1\tcontent')

describe('context receipt projection', () => {
  it('deduplicates reads, excludes failed/not-found reads, and separates chapters from files', () => {
    const receipt = buildContextReceipt({ runViews: [
      view({ kind: 'tool', toolName: 'read', status: 'success', input: { path: 'chapters/001.md' }, output: readEnvelope('chapters/001.md') }),
      view({ kind: 'tool', toolName: 'read', status: 'success', input: { path: 'chapters/001.md' }, output: readEnvelope('chapters/001.md') }),
      view({ kind: 'tool', toolName: 'read', status: 'success', input: { path: 'setting/progress.md' }, output: readEnvelope('setting/progress.md') }),
      view({ kind: 'tool', toolName: 'read', status: 'success', input: { path: 'missing.md' }, output: readEnvelope('missing.md', 'not_found') }),
      view({ kind: 'tool', toolName: 'read', status: 'error', input: { path: 'private.md' }, errorText: 'permission denied' }),
    ] })

    expect(receipt.sources).toHaveLength(1)
    expect(receipt.sources[0].chapters.map(item => item.path)).toEqual(['chapters/001.md'])
    expect(receipt.sources[0].files.map(item => item.path)).toEqual(['setting/progress.md'])
    expect(receipt.sources[0].failures.map(item => item.path)).toEqual(['missing.md', 'private.md'])
  })

  it('counts only full lore bodies and marks index-only results as browse', () => {
    const full = '# Lore Items\n\n## Hero (character / major / resident)\nID: hero\nBrief: A hero\n\n```markdown\nHero body\n```'
    const index = '# Lore Items\n\n## Villain (character / minor / auto)\nID: villain\nBrief: A villain'
    const receipt = buildContextReceipt({ runViews: [
      view({ kind: 'tool', toolName: 'query_lore_items', status: 'success', output: full }),
      view({ kind: 'tool', toolName: 'query_lore_items', status: 'success', output: index }),
    ] })

    expect(receipt.sources[0].lore.map(item => item.label)).toEqual(['Hero'])
    expect(receipt.sources[0].browse.map(item => item.label)).toEqual(['Villain'])
  })

  it('keeps child-agent reads in their own source and preserves author references', () => {
    const metadata: AgentMessageMetadata = { subagent: true, subagent_session_id: 'child-1', agent_name: 'Researcher' }
    const author = view({ kind: 'user', metadata: {
      user_references: [
        { kind: 'selection', label: 'chapters/002.md', start_line: 8, end_line: 12 },
        { kind: 'lore', id: 'hero', label: 'Hero' },
      ],
      attachments: [{ id: 'att-1', name: 'notes.md', size: 10 }],
    } })
    const receipt = buildContextReceipt({
      runViews: [view({ kind: 'tool', metadata, toolName: 'read', status: 'success', input: { path: 'chapters/003.md' }, output: readEnvelope('chapters/003.md') })],
      authorView: author,
    })

    expect(receipt.sources[0].label).toBe('Researcher')
    expect(receipt.sources[0].chapters[0].path).toBe('chapters/003.md')
    expect(receipt.author.map(item => item.label)).toEqual(['chapters/002.md', 'Hero', 'notes.md'])
  })

  it('bounds the receipt and reports an unrecognized provider result', () => {
    const runViews = Array.from({ length: 4 }, (_, index) => view({
      kind: 'tool', toolName: 'read', status: 'success', input: { path: `f${index}.md` }, output: 'provider-specific result',
    }))
    const receipt = buildContextReceipt({ runViews, maxItems: 2 })
    expect(receipt.sources[0].files).toHaveLength(2)
    expect(receipt.sources[0].omittedCount).toBe(2)
    expect(receipt.hasDetails).toBe(true)
  })

  it('applies the item limit across the main Agent and child sources', () => {
    const childMetadata: AgentMessageMetadata = { subagent: true, subagent_session_id: 'child-limit' }
    const receipt = buildContextReceipt({ runViews: [
      view({ kind: 'tool', toolName: 'read', status: 'success', input: { path: 'main.md' }, output: readEnvelope('main.md') }),
      view({ kind: 'tool', metadata: childMetadata, toolName: 'read', status: 'success', input: { path: 'child.md' }, output: readEnvelope('child.md') }),
    ], maxItems: 1 })
    const accepted = receipt.sources.reduce((total, source) => total + source.files.length + source.chapters.length + source.lore.length + source.browse.length + source.failures.length, 0)
    expect(accepted).toBe(1)
    expect(receipt.sources.reduce((total, source) => total + source.omittedCount, 0)).toBe(1)
  })

  it('degrades explicitly when an external runtime uses an unknown read event', () => {
    const receipt = buildContextReceipt({ runViews: [
      view({ kind: 'tool', toolName: 'filesystem_read_text', status: 'success', input: { path: 'chapters/004.md' }, output: 'provider payload' }),
    ] })
    expect(receipt.sources[0].unavailable).toBe(true)
    expect(receipt.sources[0].chapters).toHaveLength(0)
  })

  it('does not project absolute host paths as workspace files', () => {
    const receipt = buildContextReceipt({ runViews: [
      view({ kind: 'tool', toolName: 'read', status: 'success', input: { path: 'C:/outside/secret.md' }, output: readEnvelope('C:/outside/secret.md') }),
    ] })
    expect(receipt.sources[0].files).toHaveLength(0)
    expect(receipt.sources[0].chapters).toHaveLength(0)
  })
})
