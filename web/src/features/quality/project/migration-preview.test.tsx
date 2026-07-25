import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { setConfiguredLocale } from '@/i18n'
import { server } from '@/test/msw/server'
import {
  qualityPreviewFixture,
  qualityProfileDetailFixture,
  qualityProfileSummariesFixture,
  qualityProjectFixture,
} from '@/test/msw/quality-fixtures'
import { QualityProjectView } from './QualityProjectView'

describe('Quality migration preview', () => {
  beforeEach(() => {
    setConfiguredLocale('zh-CN')
  })

  it('posts an empty body once and presents digest, compatibility, issues, and page summary as preview only', async () => {
    const previewBodies: unknown[] = []
    const workspaceTree = { files: ['ideas.md', 'chapters/0001.md'], revision: 'tree-before' }
    const before = structuredClone(workspaceTree)
    useQualityHandlers(async ({ request }) => {
      previewBodies.push(await request.json())
      return HttpResponse.json(qualityPreviewFixture())
    })

    renderQualityView()
    fireEvent.click(await screen.findByRole('button', { name: '生成零写入预览' }))

    expect(await screen.findByText('仅预览，未写入')).toBeVisible()
    expect(screen.getByText('cccccccccccc…')).toBeVisible()
    expect(screen.getByText('安全读取，暂不可管理')).toBeVisible()
    expect(screen.getByText('1 个文件 · 2 项操作 · 1 个冲突')).toBeVisible()
    expect(screen.getByText('workspace_schema_missing')).toBeVisible()
    expect(previewBodies).toEqual([{}])
    expect(workspaceTree).toEqual(before)

    const forbidden = screen.queryAllByRole('button', { name: /应用|确认迁移|开始运行|定稿|apply|confirm|start|finalize/i })
    expect(forbidden).toHaveLength(0)
  })

  it('shows a bounded preview error without constructing a follow-up action', async () => {
    useQualityHandlers(() => HttpResponse.json({
      code: 'quality_workspace_inspection_failed',
      message: 'failed at /Users/private/work',
    }, { status: 500 }))

    renderQualityView()
    fireEvent.click(await screen.findByRole('button', { name: '生成零写入预览' }))

    expect(await screen.findByText('无法生成迁移预览')).toBeVisible()
    expect(screen.queryByText(/Users\/private/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /应用|确认迁移/ })).not.toBeInTheDocument()
  })
})

function renderQualityView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><QualityProjectView /></QueryClientProvider>)
}

function useQualityHandlers(preview: Parameters<typeof http.post>[1]) {
  server.use(
    http.get('/api/quality/profiles', () => HttpResponse.json(qualityProfileSummariesFixture())),
    http.get('/api/quality/profiles/:profileID', ({ params }) => HttpResponse.json(qualityProfileDetailFixture(String(params.profileID)))),
    http.get('/api/quality/project', () => HttpResponse.json(qualityProjectFixture())),
    http.post('/api/quality/project/migration-preview', preview),
  )
}
