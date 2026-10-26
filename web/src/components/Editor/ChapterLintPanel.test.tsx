import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChapterSummary } from '@/lib/api-client/types'
import { ChapterLintPanel } from './ChapterLintPanel'

const projectBookMock = vi.hoisted(() => ({
  getProjectBookSummary: vi.fn(),
  getProjectLoreItems: vi.fn(),
}))
const projectFilesMock = vi.hoisted(() => ({
  readOptionalProjectFile: vi.fn(),
  readProjectFile: vi.fn(),
}))
const styleFingerprintMock = vi.hoisted(() => ({ throwOnMultiChapter: false }))

vi.mock('@/lib/api-client/project-book', () => projectBookMock)
vi.mock('@/lib/api-client/project-files', () => projectFilesMock)
vi.mock('@/features/writing/style-fingerprint', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/writing/style-fingerprint')>()
  return {
    ...actual,
    computeStyleFingerprint: (chapters: Parameters<typeof actual.computeStyleFingerprint>[0], options?: Parameters<typeof actual.computeStyleFingerprint>[1]) => {
      if (styleFingerprintMock.throwOnMultiChapter && options?.includeRepeatedPhrases !== false) throw new Error('statistics failed')
      return actual.computeStyleFingerprint(chapters, options)
    },
  }
})

const chapters: ChapterSummary[] = [
  { path: 'chapters/001.md', file_name: '001.md', display_title: '第一章', index: 1, words: 3, status: 'draft', confirmed: false, updated_at: '', volume: '', volume_path: '' },
  { path: 'chapters/002.md', file_name: '002.md', display_title: '第二章', index: 2, words: 3, status: 'draft', confirmed: false, updated_at: '', volume: '', volume_path: '' },
]

function renderPanel() {
  return render(
    <ChapterLintPanel
      projectId="project-voice-stats"
      content="第一章正文。"
      currentChapterPath="chapters/001.md"
      editor={null}
      onClose={vi.fn()}
      onIssueClick={vi.fn()}
    />,
  )
}

async function openStats() {
  const statsTab = await screen.findByRole('tab', { name: '统计' })
  await userEvent.click(statsTab)
}

beforeEach(() => {
  styleFingerprintMock.throwOnMultiChapter = false
  projectBookMock.getProjectBookSummary.mockResolvedValue({ chapters })
  projectBookMock.getProjectLoreItems.mockResolvedValue([])
  projectFilesMock.readOptionalProjectFile.mockResolvedValue(null)
  projectFilesMock.readProjectFile.mockResolvedValue({ content: '第二章正文。' })
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  })
})

describe('ChapterLintPanel statistics errors and cancellation', () => {
  it('renders the translated statistics error instead of its internal key', async () => {
    styleFingerprintMock.throwOnMultiChapter = true
    renderPanel()
    await openStats()
    await userEvent.click(screen.getByRole('button', { name: '统计多章' }))

    expect(await screen.findByText('读取章节失败，请重试。')).toBeInTheDocument()
    expect(screen.queryByText('editor.lint.statsFailed')).not.toBeInTheDocument()
  })

  it('renders the translated copy error instead of its internal key', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('clipboard unavailable'))
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    renderPanel()
    await openStats()
    await userEvent.click(screen.getByRole('button', { name: '复制为 Markdown' }))

    expect(await screen.findByText('复制失败，请手动选择统计结果。')).toBeInTheDocument()
    expect(screen.queryByText('editor.lint.copyFailed')).not.toBeInTheDocument()
  })

  it('drops loading and ignores a result after selection changes invalidate the request', async () => {
    let resolveRead: ((value: { content: string }) => void) | undefined
    projectFilesMock.readProjectFile.mockImplementationOnce(() => new Promise(resolve => { resolveRead = resolve }))
    renderPanel()
    await openStats()
    const secondChapter = screen.getByText('第二章').closest('label')
    expect(secondChapter).not.toBeNull()
    const secondCheckbox = within(secondChapter!).getByRole('checkbox')
    await userEvent.click(secondCheckbox)
    const calculate = screen.getByRole('button', { name: '统计多章' })
    await userEvent.click(calculate)
    expect(screen.getByRole('button', { name: '统计中…' })).toBeDisabled()

    await userEvent.click(secondCheckbox)
    expect(screen.getByRole('button', { name: '统计多章' })).toBeEnabled()

    resolveRead?.({ content: '第二章正文。' })
    await waitFor(() => {
      const chapterCard = screen.getByText('章数').parentElement
      expect(chapterCard).not.toBeNull()
      expect(within(chapterCard!).getByText('1')).toBeInTheDocument()
    })
  })
})
