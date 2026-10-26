import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { APIError } from '@/lib/api-client/client'
import { CasebookView } from './CasebookView'
import { getCasebook, toggleCasebookCase } from '@/lib/api-client/casebook'

vi.mock('@/lib/api-client/casebook', () => ({
  getCasebook: vi.fn(),
  toggleCasebookCase: vi.fn(),
}))

vi.mock('@/lib/api-client/project-files', () => ({
  applyProjectFileOperations: vi.fn(),
}))

const mockedGetCasebook = vi.mocked(getCasebook)
const mockedToggle = vi.mocked(toggleCasebookCase)

const snapshot = {
  project_id: 'book',
  index: { content: '# cases', revision: 'sha256:index' },
  entries: [],
  cases: [
    {
      id: 'c-collected', title: 'Collected', status: '已收藏' as const, direction: '开篇', scenario: '章首', method: '先写异常', avoid: '不要复制原作', source: 'references/sources/source.txt', location: '第 2 行', quote: '灯亮了。', path: 'setting/cases/c-collected.md', revision: 'sha256:case', source_exists: true, registered: true,
    },
    {
      id: 'c-pending', title: 'Pending', status: '待确认' as const, direction: '对白', scenario: '对话', method: '用动作打断', avoid: '不要复制原作', source: 'references/sources/source.txt', location: '未知', quote: '一句话', path: 'setting/cases/c-pending.md', revision: 'sha256:pending', source_exists: true, registered: true,
    },
    {
      id: 'c-missing', title: '', status: '待确认' as const, direction: '', scenario: '', method: '', avoid: '', source: '', location: '', quote: '', path: 'setting/cases/c-missing.md', revision: '', issue: 'case_file_missing', registered: true,
    },
  ],
}

function renderView() {
  return render(<CasebookView projectId="book" path="setting/case-index.md" onSelectFile={vi.fn()} rawEditor={null} />)
}

describe('CasebookView', () => {
  beforeEach(() => {
    mockedGetCasebook.mockReset()
    mockedToggle.mockReset()
    mockedGetCasebook.mockResolvedValue(snapshot)
  })

  it('shows the empty state and import action after the initial load', async () => {
    mockedGetCasebook.mockResolvedValue({ ...snapshot, cases: [] })
    renderView()
    expect(await screen.findByText('还没有写法案例')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '导入参考原文' }).length).toBeGreaterThan(0)
  })

  it('filters by direction and disables actions for a broken file', async () => {
    renderView()
    await screen.findByText('c-pending')
    fireEvent.click(screen.getByRole('button', { name: '对白' }))
    expect(screen.getByText('c-pending')).toBeInTheDocument()
    expect(screen.queryByText('c-collected')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '全部方向' }))
    fireEvent.click(screen.getByText('c-missing'))
    expect(screen.getAllByText('案例文件不存在').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: '收藏' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '带入对话' })).toBeDisabled()
  })

  it('keeps a localized action error beside the case after a background refresh', async () => {
    mockedToggle.mockRejectedValueOnce(new APIError('missing', { status: 400, code: 'case_quote_missing' }))
    mockedGetCasebook.mockResolvedValueOnce(snapshot).mockResolvedValueOnce(snapshot)
    renderView()
    fireEvent.click(await screen.findByText('c-pending'))
    fireEvent.click(screen.getByRole('button', { name: '收藏' }))
    expect(await screen.findByText('原文摘录在来源中找不到，未收藏。')).toBeInTheDocument()
    expect(screen.queryByText('正在读取案例库…')).not.toBeInTheDocument()
  })
})
