import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { afterEach, describe, expect, it } from 'vitest'
import { setConfiguredLocale } from '@/i18n'
import { server } from '@/test/msw/server'
import { qualityProfileDetailFixture, qualityProfileSummariesFixture, qualityProjectFixture } from '@/test/msw/quality-fixtures'
import { QualityProjectView } from './QualityProjectView'

describe('QualityProjectView responsive, locale, and theme boundary', () => {
  afterEach(() => {
    document.documentElement.removeAttribute('data-theme')
    setConfiguredLocale('zh-CN')
  })

  it.each([
    ['zh-CN', 'light', '作品质量', '只读参考目录'],
    ['en-US', 'dark', 'Project Quality', 'Read-only reference catalog'],
  ] as const)('renders complete %s copy on the %s theme', async (locale, theme, title, readOnlyLabel) => {
    setConfiguredLocale(locale)
    document.documentElement.setAttribute('data-theme', theme)
    useQualityHandlers()

    renderQualityView()

    expect(await screen.findByRole('heading', { name: title })).toBeVisible()
    expect(await screen.findAllByText(readOnlyLabel)).not.toHaveLength(0)
    expect(document.querySelector('[data-i18n-key]')).toBeNull()
  })

  it('uses adaptive, overflow-safe containers for a long Profile and QualitySpec', async () => {
    const longText = '一段需要被完整换行而不能撑破窄屏的作者质量目标。'.repeat(40)
    server.use(
      http.get('/api/quality/profiles', () => HttpResponse.json(qualityProfileSummariesFixture())),
      http.get('/api/quality/profiles/:profileID', () => HttpResponse.json(qualityProfileDetailFixture('long_serial', longText))),
      http.get('/api/quality/project', () => HttpResponse.json(qualityProjectFixture())),
    )

    renderQualityView()

    const page = await screen.findByTestId('quality-project-view')
    const text = await screen.findByText(longText)
    expect(page).toHaveClass('min-w-0', 'overflow-x-hidden')
    expect(text).toHaveClass('break-words')
    expect(page.querySelector('[class*="w-[1440px]"]')).toBeNull()
  })
})

function renderQualityView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><QualityProjectView /></QueryClientProvider>)
}

function useQualityHandlers() {
  server.use(
    http.get('/api/quality/profiles', () => HttpResponse.json(qualityProfileSummariesFixture())),
    http.get('/api/quality/profiles/:profileID', ({ params }) => HttpResponse.json(qualityProfileDetailFixture(String(params.profileID)))),
    http.get('/api/quality/project', () => HttpResponse.json(qualityProjectFixture())),
  )
}
