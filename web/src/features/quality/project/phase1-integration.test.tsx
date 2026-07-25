import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { setConfiguredLocale } from '@/i18n'
import { server } from '@/test/msw/server'
import { QualityProjectView } from './QualityProjectView'

describe('Phase 1 Quality project integration', () => {
  beforeEach(() => {
    setConfiguredLocale('zh-CN')
  })

  it('leaves initial loading and renders bounded errors when both overview reads fail with production retry semantics', async () => {
    server.use(
      http.get('/api/quality/profiles', () => HttpResponse.json({
        code: 'quality_assets_unavailable',
        message: 'P1T07_INTERNAL_SECRET /private/real-workspace',
      }, { status: 500 })),
      http.get('/api/quality/project', () => HttpResponse.json({
        code: 'quality_workspace_inspection_failed',
        message: 'P1T07_INTERNAL_SECRET /private/real-workspace',
      }, { status: 500 })),
    )

    const client = new QueryClient({
      defaultOptions: {
        queries: { staleTime: 30_000, retry: 1, retryDelay: 0, refetchOnWindowFocus: false },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <QualityProjectView />
      </QueryClientProvider>,
    )

    expect(screen.getByRole('status')).toHaveTextContent('正在整理作品质量信息')
    expect(await screen.findByText('质量参考暂时不可用')).toBeVisible()
    expect(await screen.findByText('无法连接质量服务')).toBeVisible()
    expect(screen.queryByText(/P1T07_INTERNAL_SECRET|private\/real-workspace/)).not.toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
