import { act, renderHook, waitFor } from '@testing-library/react'
import { Editor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Markdown } from '@tiptap/markdown'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import i18n from '@/i18n'
import { WritingSelectionProvider, useWritingSelection } from './WritingSelectionContext'
import { useWritingSelectionEditor } from './use-writing-selection-editor'
import { selectionActionOrigin, writingSelectionPrompt } from './selection-actions'

const api = vi.hoisted(() => ({ readProjectFile: vi.fn() }))
vi.mock('@/lib/api', () => ({ readProjectFile: api.readProjectFile }))

describe('manuscript selection actions and adoption', () => {
  let editor: Editor
  const flush = vi.fn(async () => true)
  const onQuoteSelection = vi.fn()
  beforeEach(async () => {
    await i18n.changeLanguage('zh-CN')
    editor = new Editor({ extensions: [StarterKit, Markdown], content: 'Old prose.\n\nUntouched.', contentType: 'markdown' })
    flush.mockResolvedValue(true)
    onQuoteSelection.mockClear()
    api.readProjectFile.mockResolvedValue({ project_id: 'book', revision: 'r1', content: 'Old prose.\n\nUntouched.' })
  })
  afterEach(() => editor.destroy())

  function mount() {
    const readDraft = () => editor.getMarkdown()
    const wrapper = ({ children }: { children: ReactNode }) => <WritingSelectionProvider enabled projectId="book" fileName="one.md">{children}</WritingSelectionProvider>
    return renderHook(({ fileName = 'one.md', projectId = 'book', enabled = true }) => {
      const run = useWritingSelectionEditor({ editor, projectId, fileName, enabled, flush, readDraft, onQuoteSelection })
      return { run, adapter: useWritingSelection()?.editor }
    }, { wrapper, initialProps: { fileName: 'one.md', projectId: 'book', enabled: true } })
  }

  it('fills the localized instruction with the existing selection attachment', async () => {
    const { result } = mount()
    editor.commands.setTextSelection({ from: 1, to: 11 })
    const listener = vi.fn()
    window.addEventListener('nova:writing-agent-init', listener)
    try {
      await act(() => result.current.run('polish'))
      const selection = onQuoteSelection.mock.calls[0][0]
      expect(selection).toEqual({ fileName: 'one.md', startLine: 1, endLine: 1, content: 'Old prose.' })
      expect(selectionActionOrigin([selection])).toMatchObject({ projectId: 'book', fileName: 'one.md', from: 1, to: 11, revision: 'r1' })
      expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ prompt: writingSelectionPrompt(i18n.t.bind(i18n), 'polish'), autoSend: false })
      expect((listener.mock.calls[0][0] as CustomEvent).detail.prompt).toContain('范围仅限所附选区')
    } finally { window.removeEventListener('nova:writing-agent-init', listener) }
  })

  it('replaces only the captured selection and offers a separate undo step', async () => {
    const { result } = mount()
    editor.commands.setTextSelection({ from: 1, to: 11 })
    const target = await result.current.adapter!.capture('replace')
    expect(await result.current.adapter!.apply(target, 'New prose.', 'replace')).toBeNull()
    expect(editor.getText()).toBe('New prose.\n\nUntouched.')
    expect(editor.commands.undo()).toBe(true)
    expect(editor.getText()).toBe('Old prose.\n\nUntouched.')
  })

  it('inserts at the captured cursor without deleting an existing selection', async () => {
    const { result } = mount()
    editor.commands.setTextSelection({ from: 1, to: 11 })
    const target = await result.current.adapter!.capture('insert')
    expect(target.from).toBe(target.to)
    expect(await result.current.adapter!.apply(target, 'Added.', 'insert')).toBeNull()
    expect(editor.getText()).toContain('Old prose.\n\nAdded.')
    expect(editor.commands.undo()).toBe(true)
    expect(editor.getText()).toBe('Old prose.\n\nUntouched.')
  })

  it('blocks external revision changes and local edits instead of guessing a new range', async () => {
    const { result } = mount()
    editor.commands.setTextSelection({ from: 1, to: 11 })
    const target = await result.current.adapter!.capture('replace')
    api.readProjectFile.mockResolvedValue({ project_id: 'book', revision: 'r2' })
    expect(await result.current.adapter!.apply(target, 'Wrong.', 'replace')).toBe('changed')
    expect(editor.getText()).toContain('Old prose.')
    api.readProjectFile.mockResolvedValue({ project_id: 'book', revision: 'r1' })
    editor.commands.insertContent('Author edit.')
    expect(await result.current.adapter!.apply(target, 'Wrong.', 'replace')).toBe('changed')
    expect(editor.getText()).not.toContain('Wrong.')
  })

  it.each([{ fileName: 'two.md' }, { projectId: 'other-book' }, { enabled: false }])('invalidates coordinates after navigation: %j', async change => {
    const { result, rerender } = mount()
    const adapter = result.current.adapter!
    const target = await adapter.capture('replace')
    rerender({ fileName: 'one.md', projectId: 'book', enabled: true, ...change })
    await waitFor(() => expect(result.current.adapter).toBeNull())
    expect(await adapter.apply(target, 'Wrong.', 'replace')).toBe('unavailable')
    expect(editor.getText()).not.toContain('Wrong.')
  })
})
