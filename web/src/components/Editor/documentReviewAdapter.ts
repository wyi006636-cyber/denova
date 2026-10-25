import type { Editor } from '@tiptap/react'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import type { DocumentReviewAnchor } from '@/features/document-review/types'
import {
  captureDocumentReviewSelection,
  commentWidgetPosition,
  createDocumentReviewAnchor,
  createRawDocumentReviewAnchor,
  resolveRawDocumentReviewAnchor,
  type DocumentReviewSelectionSnapshot,
  type DocumentReviewSnapshot,
  type EditorReviewRange,
} from './documentReviewAnchors'
import { readMarkdownSourceDocument } from './markdownSourceDocument'

type FrozenReviewSelection =
  | { mode: 'rich'; selection: DocumentReviewSelectionSnapshot }
  | {
      mode: 'source'
      content: string
      start: number
      end: number
      range: EditorReviewRange
    }

/** Maps one TipTap document representation onto canonical Markdown review anchors. */
export interface DocumentReviewAnchorAdapter {
  coordinateSpace: 'rich-markdown-v1' | 'source-markdown-v1'
  captureSelection: (editor: Editor, range: EditorReviewRange) => FrozenReviewSelection
  createAnchor: (
    editor: Editor,
    snapshot: DocumentReviewSnapshot,
    selection: FrozenReviewSelection,
  ) => DocumentReviewAnchor
  resolveRange: (editor: Editor, anchor: DocumentReviewAnchor) => EditorReviewRange | null
}

export const richMarkdownReviewAdapter: DocumentReviewAnchorAdapter = {
  coordinateSpace: 'rich-markdown-v1',
  captureSelection: (editor, range) => ({
    mode: 'rich',
    selection: captureDocumentReviewSelection(editor, range),
  }),
  createAnchor: (editor, snapshot, frozen) => {
    if (frozen.mode !== 'rich') throw new Error('The review selection belongs to a different editor mode')
    return createDocumentReviewAnchor(editor, snapshot, frozen.selection)
  },
  resolveRange: (editor, anchor) => {
    const from = anchor.editor_from ?? 0
    const to = anchor.editor_to ?? 0
    const displayQuote = anchor.display_quote || anchor.quote
    if (from > 0 && to > from && to <= editor.state.doc.content.size
      && editor.state.doc.textBetween(from, to, '\n').trim() === displayQuote.trim()) {
      return reviewRange(editor, anchor, from, to)
    }
    return resolveRichTextRange(editor, anchor, displayQuote)
  },
}

export const sourceMarkdownReviewAdapter: DocumentReviewAnchorAdapter = {
  coordinateSpace: 'source-markdown-v1',
  captureSelection: (editor, range) => {
    const source = readMarkdownSourceDocument(editor)
    if (!source) throw new Error('Markdown source document is unavailable')
    const start = range.from - source.contentStart
    const end = range.to - source.contentStart
    if (start < 0 || end <= start || end > source.content.length) {
      throw new Error('The selected source range is invalid')
    }
    return { mode: 'source', content: source.content, start, end, range }
  },
  createAnchor: (_editor, snapshot, frozen) => {
    if (frozen.mode !== 'source') throw new Error('The review selection belongs to a different editor mode')
    const anchor = createRawDocumentReviewAnchor(snapshot, frozen)
    return {
      ...anchor,
      kind: frozen.range.kind,
      editor_from: frozen.range.from,
      editor_to: frozen.range.to,
    }
  },
  resolveRange: (editor, anchor) => {
    const source = readMarkdownSourceDocument(editor)
    if (!source) return null
    const resolved = resolveRawDocumentReviewAnchor(source.content, anchor)
    if (!resolved) return null
    const from = source.contentStart + resolved.start
    const to = source.contentStart + resolved.end
    if (from <= 0 || to <= from || to > editor.state.doc.content.size) return null
    return reviewRange(editor, anchor, from, to)
  },
}

function resolveRichTextRange(
  editor: Editor,
  anchor: DocumentReviewAnchor,
  displayQuote: string,
): EditorReviewRange | null {
  if (!editor.markdown || !anchor.quote) return null
  const source = normalizeRichMarkdownSource(readMarkdownSourceDocument(editor)?.content ?? editor.getMarkdown())
  const normalizedAnchor: DocumentReviewAnchor = {
    ...anchor,
    quote: normalizeRichMarkdownSource(anchor.quote),
    prefix: normalizeRichMarkdownSource(anchor.prefix || ''),
    suffix: normalizeRichMarkdownSource(anchor.suffix || ''),
    display_quote: normalizeRichMarkdownSource(anchor.display_quote || anchor.quote),
  }
  const sourceRange = resolveRawDocumentReviewAnchor(source, normalizedAnchor)
  const resolvedSourceRange = sourceRange || resolveCollapsedRichTextRange(source, normalizedAnchor)
  if (!resolvedSourceRange) return null

  // Parse the exact source with private-use markers around the resolved range.
  // The Markdown parser then gives us the ProseMirror coordinates even when the
  // quote is repeated, crosses a block boundary, or contains a trailing newline.
  const startMarker = uniqueMarker(source, '\uE000nova-review-resolve-start\uE001')
  const endMarker = uniqueMarker(source + startMarker, '\uE000nova-review-resolve-end\uE001')
  const markedSource = source.slice(0, resolvedSourceRange.start)
    + startMarker
    + source.slice(resolvedSourceRange.start, resolvedSourceRange.end)
    + endMarker
    + source.slice(resolvedSourceRange.end)
  const markedDocument = editor.schema.nodeFromJSON(editor.markdown.parse(markedSource))
  const startPosition = textPosition(markedDocument, startMarker)
  const endPosition = textPosition(markedDocument, endMarker)
  if (startPosition < 0 || endPosition <= startPosition) return null

  const from = startPosition
  const to = endPosition - startMarker.length
  if (from <= 0 || to <= from || to > editor.state.doc.content.size) return null
  const visibleQuote = editor.state.doc.textBetween(from, to, '\n').trim()
  if (visibleQuote !== displayQuote.trim()) return null
  return reviewRange(editor, anchor, from, to)
}

function textPosition(document: ProseMirrorNode, text: string): number {
  let result = -1
  document.descendants((node, position) => {
    if (result >= 0 || !node.isText || !node.text) return
    const offset = node.text.indexOf(text)
    if (offset >= 0) result = position + offset
  })
  return result
}

function uniqueMarker(content: string, base: string): string {
  let marker = base
  while (content.includes(marker)) marker += '\uE002'
  return marker
}

function normalizeLineEndings(value: string): string {
  return value.replace(/\r\n?/g, '\n')
}

function normalizeRichMarkdownSource(value: string): string {
  return normalizeLineEndings(value).replace(/[ \t]+\n/g, '\n')
}

function resolveCollapsedRichTextRange(
  source: string,
  anchor: DocumentReviewAnchor,
): { start: number; end: number } | null {
  const collapsed = collapseRichWhitespace(source)
  const collapsedAnchor: DocumentReviewAnchor = {
    ...anchor,
    quote: collapseRichWhitespace(anchor.quote).text,
    prefix: collapseRichWhitespace(anchor.prefix || '').text,
    suffix: collapseRichWhitespace(anchor.suffix || '').text,
    display_quote: collapseRichWhitespace(anchor.display_quote || anchor.quote).text,
    start: 0,
    end: 0,
  }
  const range = resolveRawDocumentReviewAnchor(collapsed.text, collapsedAnchor)
  if (!range) return null
  return { start: collapsed.boundaries[range.start], end: collapsed.boundaries[range.end] }
}

function collapseRichWhitespace(value: string): { text: string; boundaries: number[] } {
  const text: string[] = []
  const boundaries = [0]
  for (let index = 0; index < value.length; index += 1) {
    if (/\s/.test(value[index])) continue
    text.push(value[index])
    boundaries.push(index + 1)
  }
  return { text: text.join(''), boundaries }
}

function reviewRange(
  editor: Editor,
  anchor: DocumentReviewAnchor,
  from: number,
  to: number,
): EditorReviewRange {
  return {
    from,
    to,
    widgetPos: commentWidgetPosition(editor.state.doc, to),
    kind: anchor.kind,
    displayQuote: anchor.display_quote || anchor.quote,
  }
}
