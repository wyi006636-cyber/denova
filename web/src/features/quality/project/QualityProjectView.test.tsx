import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { setConfiguredLocale } from '@/i18n'
import { server } from '@/test/msw/server'
import {
  qualityProfileDetailFixture,
  qualityProfileSummariesFixture,
  qualityProjectFixture,
} from '@/test/msw/quality-fixtures'
import { QualityProjectView } from './QualityProjectView'

describe('QualityProjectView states', () => {
  beforeEach(() => {
    setConfiguredLocale('zh-CN')
    useQualityHandlers()
  })

  it('renders a bounded loading state instead of a blank page', () => {
    server.use(
      http.get('/api/quality/profiles', () => new Promise<never>(() => {})),
      http.get('/api/quality/project', () => new Promise<never>(() => {})),
    )

    renderQualityView()

    expect(screen.getByRole('status')).toHaveTextContent('正在整理作品质量信息')
    expect(screen.getByTestId('quality-loading-layout')).toBeInTheDocument()
  })

  it('shows overview, read-only Profile catalog, and author-readable QualitySpec content', async () => {
    renderQualityView()

    expect(await screen.findByRole('heading', { name: '作品质量' })).toBeInTheDocument()
    expect(await screen.findAllByText('只读参考目录')).not.toHaveLength(0)
    expect(screen.getByRole('button', { name: /长篇连载/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /番茄短篇/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /盐选短篇/ })).toBeInTheDocument()
    expect(await screen.findByText('与既有角色状态、设定和伏笔保持连续。')).toBeVisible()
    expect(screen.getByText('避免人物、因果与作品方向在创作过程中无依据漂移。')).toBeVisible()
    expect(screen.getByText('必须做到')).toBeVisible()
    expect(screen.getByText('来源依据')).toBeVisible()
    expect(screen.getByText('适用范围')).toBeVisible()
    expect(screen.getByTestId('quality-technical-metadata')).toHaveTextContent('v1')
  })

  it('shows an explicit empty catalog while retaining read-only authority', async () => {
    server.use(http.get('/api/quality/profiles', () => HttpResponse.json([])))

    renderQualityView()

    expect(await screen.findByText('暂无可显示的 Profile 参考')).toBeVisible()
    expect(screen.getByText('只读参考目录')).toBeVisible()
  })

  it('shows a bounded 404 detail state without exposing backend payload', async () => {
    server.use(http.get('/api/quality/profiles/:profileID', () => HttpResponse.json({
      code: 'quality_profile_not_found',
      message: '/Users/private/workspace/profile not found',
    }, { status: 404 })))

    renderQualityView()

    expect(await screen.findByText('这份 Profile 参考暂时不可用')).toBeVisible()
    expect(screen.queryByText(/\/Users\/private/)).not.toBeInTheDocument()
  })

  it('keeps the Profile catalog usable when project inspection returns 409 no workspace', async () => {
    server.use(http.get('/api/quality/project', () => HttpResponse.json({
      code: 'quality_no_workspace',
      message: 'No workspace is currently open.',
    }, { status: 409 })))

    renderQualityView()

    expect(await screen.findByText('先打开一个作品')).toBeVisible()
    expect(screen.getByRole('button', { name: /长篇连载/ })).toBeInTheDocument()
  })

  it('shows a bounded server error state for a 500 response', async () => {
    server.use(http.get('/api/quality/profiles', () => HttpResponse.json({
      code: 'quality_assets_unavailable',
      message: 'embedded asset /private/path failed',
    }, { status: 500 })))

    renderQualityView()

    expect(await screen.findByText('质量参考暂时不可用')).toBeVisible()
    expect(screen.queryByText(/private\/path/)).not.toBeInTheDocument()
  })

  it('shows a bounded network failure state and supports retry', async () => {
    let failed = true
    server.use(http.get('/api/quality/project', () => {
      if (failed) return HttpResponse.error()
      return HttpResponse.json(qualityProjectFixture())
    }))

    renderQualityView()

    expect(await screen.findByText('无法连接质量服务')).toBeVisible()
    failed = false
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('可安全规划')).toBeVisible()
  })

  it('renders unknown/newer contracts as unsupported instead of valid Profile data', async () => {
    const detail = qualityProfileDetailFixture()
    detail.profile.contract.version = 'v2'
    server.use(http.get('/api/quality/profiles/:profileID', () => HttpResponse.json(detail)))

    renderQualityView()

    expect(await screen.findByText('合同版本暂不支持')).toBeVisible()
    expect(screen.queryByText('与既有角色状态、设定和伏笔保持连续。')).not.toBeInTheDocument()
  })
})

function renderQualityView() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <QualityProjectView />
    </QueryClientProvider>,
  )
}

function useQualityHandlers() {
  server.use(
    http.get('/api/quality/profiles', () => HttpResponse.json(qualityProfileSummariesFixture())),
    http.get('/api/quality/profiles/:profileID', ({ params }) => HttpResponse.json(qualityProfileDetailFixture(String(params.profileID)))),
    http.get('/api/quality/project', () => HttpResponse.json(qualityProjectFixture())),
  )
}
