import { Schema } from '@tiptap/pm/model'
import { describe, expect, it } from 'vitest'
import type { Editor } from '@tiptap/react'
import type { DocumentReviewAnchor } from '@/features/document-review/types'
import { richMarkdownReviewAdapter } from './documentReviewAdapter'

const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: { content: 'inline*', group: 'block' },
    text: { group: 'inline' },
  },
})

function editorFor(source: string): Editor {
  const markdown = {
    parse: (value: string) => ({
      type: 'doc',
      content: [{ type: 'paragraph', content: value ? [{ type: 'text', text: value }] : undefined }],
    }),
  }
  const doc = schema.nodeFromJSON(markdown.parse(source))
  return {
    state: { doc },
    schema,
    markdown,
    getMarkdown: () => source,
  } as unknown as Editor
}

function anchor(overrides: Partial<DocumentReviewAnchor>): DocumentReviewAnchor {
  return {
    kind: 'text-range',
    encoding: 'utf8-bytes-v1',
    revision: 'sha256:test',
    start: 0,
    end: 0,
    quote: '他说',
    prefix: '',
    suffix: '',
    display_quote: '他说',
    ...overrides,
  }
}

describe('richMarkdownReviewAdapter', () => {
  it('maps a repeated quote by its prefix instead of requiring document-wide uniqueness', () => {
    const source = '开头\n他说\n中间\n他说\n结尾'
    const result = richMarkdownReviewAdapter.resolveRange(editorFor(source), anchor({
      prefix: '中间\n',
      suffix: '\n结尾',
    }))

    expect(result).not.toBeNull()
    expect(result && result.displayQuote).toBe('他说')
    expect(result && result.from).toBeGreaterThan(1)
  })

  it('accepts an anchor whose quote includes a trailing newline while display_quote is trimmed', () => {
    const source = '开头\n目标\n尾部'
    const result = richMarkdownReviewAdapter.resolveRange(editorFor(source), anchor({
      quote: '目标\n',
      display_quote: '目标',
      prefix: '开头\n',
      suffix: '尾部',
    }))

    expect(result).not.toBeNull()
    expect(result && result.displayQuote).toBe('目标')
  })

  it('normalizes CRLF anchors before resolving the rich document', () => {
    const result = richMarkdownReviewAdapter.resolveRange(editorFor('开头\n目标\n尾部'), anchor({
      quote: '目标\r\n',
      display_quote: '目标',
      prefix: '开头\r\n',
      suffix: '尾部',
    }))

    expect(result).not.toBeNull()
    expect(result && result.displayQuote).toBe('目标')
  })
})
