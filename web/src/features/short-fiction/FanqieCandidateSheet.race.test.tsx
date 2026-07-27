import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ComponentProps } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18next, { setConfiguredLocale } from '@/i18n'
import type { ShortFictionCandidate, ShortFictionGenerateRequest } from '@/lib/api-client/short-fiction'
import { server } from '@/test/msw/server'
import { FanqieCandidateSheet } from './FanqieCandidateSheet'

describe('FanqieCandidateSheet request authority', () => {
  afterEach(async () => {
    await act(async () => {
      setConfiguredLocale('zh-CN')
      await i18next.changeLanguage('zh-CN')
    })
  })

  it('discards a deferred generate after close/reopen and selected-file, workspace, and locale changes', async () => {
    const user = userEvent.setup()
    const response = deferred<Response>()
    const generateRequests: ShortFictionGenerateRequest[] = []
    const generateLocales: string[] = []
    let confirmRequestCount = 0
    const staleCandidate = candidateFixture({
      candidate_id: 'sha256:stale-context',
      workspace: '/canonical/a',
      brief: '旧上下文要求',
      preview_markdown: '# 旧上下文候选',
    })

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({
          workspace: '/canonical/a',
          path: 'chapters/short.md',
          content: '# A',
          revision: staleCandidate.base_revision,
        })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocales.push(request.headers.get('X-Denova-Locale') || '')
        generateRequests.push(await request.json() as ShortFictionGenerateRequest)
        return response.promise
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        expect(await request.json()).toEqual({ candidate: staleCandidate })
        return HttpResponse.json(writtenResult(staleCandidate))
      }),
    )

    const initialProps = sheetProps({ workspace: '/workspace/a', selectedFile: 'chapters/a.md' })
    const { rerender } = render(<FanqieCandidateSheet {...initialProps} />)
    await user.type(screen.getByLabelText('创作要求'), '旧上下文要求')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    await waitFor(() => expect(generateRequests).toHaveLength(1))

    rerender(<FanqieCandidateSheet {...initialProps} open={false} workspace="/workspace/b" selectedFile="chapters/b.md" />)
    await act(async () => {
      setConfiguredLocale('en-US')
      await i18next.changeLanguage('en-US')
    })
    rerender(<FanqieCandidateSheet {...initialProps} workspace="/workspace/b" selectedFile="chapters/b.md" />)

    await act(async () => {
      response.resolve(HttpResponse.json(staleCandidate))
      await response.promise
    })

    expect(await screen.findByTestId('fanqie-save-path')).toHaveTextContent('chapters/short.md')
    expect(screen.getByRole('button', { name: 'Generate complete story preview' })).toBeEnabled()
    expect(screen.queryByText('旧上下文候选')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Confirm manuscript write' })).not.toBeInTheDocument()
    expect(generateRequests).toEqual([{
      workspace: '/canonical/a',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: staleCandidate.base_revision,
      brief: '旧上下文要求',
    }])
    expect(generateLocales).toEqual(['zh-CN'])
    expect(confirmRequestCount).toBe(0)
  })

  it('keeps the newer preview when an older deferred generate resolves last', async () => {
    const user = userEvent.setup()
    const firstResponse = deferred<Response>()
    const secondResponse = deferred<Response>()
    const generateRequests: ShortFictionGenerateRequest[] = []
    const generateLocales: string[] = []
    let confirmRequestCount = 0
    const firstCandidate = candidateFixture({ candidate_id: 'sha256:first', brief: '第一版要求', preview_markdown: '# 第一版候选' })
    const secondCandidate = candidateFixture({ candidate_id: 'sha256:second', brief: '第二版要求', preview_markdown: '# 第二版候选' })

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({
          workspace: '/workspace',
          path: 'chapters/short.md',
          content: '# Existing',
          revision: firstCandidate.base_revision,
        })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocales.push(request.headers.get('X-Denova-Locale') || '')
        generateRequests.push(await request.json() as ShortFictionGenerateRequest)
        return generateRequests.length === 1 ? firstResponse.promise : secondResponse.promise
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        expect(await request.json()).toEqual({ candidate: secondCandidate })
        return HttpResponse.json(writtenResult(secondCandidate))
      }),
    )

    const props = sheetProps()
    const { rerender } = render(<FanqieCandidateSheet {...props} />)
    await user.type(screen.getByLabelText('创作要求'), '第一版要求')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    await waitFor(() => expect(generateRequests).toHaveLength(1))

    rerender(<FanqieCandidateSheet {...props} open={false} />)
    rerender(<FanqieCandidateSheet {...props} />)
    await user.clear(await screen.findByLabelText('创作要求'))
    await user.type(screen.getByLabelText('创作要求'), '第二版要求')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    await waitFor(() => expect(generateRequests).toHaveLength(2))

    await act(async () => {
      secondResponse.resolve(HttpResponse.json(secondCandidate))
      await secondResponse.promise
    })
    expect(await screen.findByText('第二版候选')).toBeInTheDocument()

    await act(async () => {
      firstResponse.resolve(HttpResponse.json(firstCandidate))
      await firstResponse.promise
    })
    expect(screen.getByText('第二版候选')).toBeInTheDocument()
    expect(screen.queryByText('第一版候选')).not.toBeInTheDocument()
    expect(generateRequests).toEqual([
      {
        workspace: '/workspace',
        profile_id: 'fanqie_short',
        target_path: 'chapters/short.md',
        base_revision: firstCandidate.base_revision,
        brief: '第一版要求',
      },
      {
        workspace: '/workspace',
        profile_id: 'fanqie_short',
        target_path: 'chapters/short.md',
        base_revision: secondCandidate.base_revision,
        brief: '第二版要求',
      },
    ])
    expect(generateLocales).toEqual(['zh-CN', 'zh-CN'])
    expect(confirmRequestCount).toBe(0)
  })

  it('guards duplicate confirmation and keeps the committed candidate target terminal across close and context change', async () => {
    const user = userEvent.setup()
    const firstConfirmResponse = deferred<Response>()
    const secondConfirmResponse = deferred<Response>()
    const onWorkspaceChanged = vi.fn()
    const confirmCandidates: ShortFictionCandidate[] = []
    const confirmLocales: string[] = []
    const candidate = candidateFixture({
      candidate_id: 'sha256:committed',
      brief: '确认并保留精确路径',
      preview_markdown: '# 已确认候选',
    })
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({
          workspace: '/workspace',
          path: 'chapters/short.md',
          content: '# Existing',
          revision: candidate.base_revision,
        })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(candidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmLocales.push(request.headers.get('X-Denova-Locale') || '')
        const body = await request.json() as { candidate: ShortFictionCandidate }
        confirmCandidates.push(body.candidate)
        return confirmCandidates.length === 1 ? firstConfirmResponse.promise : secondConfirmResponse.promise
      }),
    )

    const props = sheetProps({ selectedFile: 'chapters/committed.md', onWorkspaceChanged })
    const { rerender } = render(<FanqieCandidateSheet {...props} />)
    await user.type(screen.getByLabelText('创作要求'), '确认并保留精确路径')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    const confirmButton = await screen.findByRole('button', { name: '确认写入正文' })

    act(() => {
      fireEvent.click(confirmButton)
      fireEvent.click(confirmButton)
    })
    await waitFor(() => expect(confirmCandidates.length).toBeGreaterThan(0))

    rerender(<FanqieCandidateSheet {...props} open={false} selectedFile="chapters/elsewhere.md" />)
    rerender(<FanqieCandidateSheet {...props} selectedFile="chapters/elsewhere.md" />)
    await act(async () => {
      firstConfirmResponse.resolve(HttpResponse.json({
        status: 'written_checkpoint_failed',
        candidate_id: candidate.candidate_id,
        write_revision: `sha256:${'2'.repeat(64)}`,
        change_group_id: 'group-partial',
        change_set_id: 'change-partial',
        workspace_mutated: true,
        checkpoint_status: 'failed',
        retryable: false,
      }))
      await firstConfirmResponse.promise
    })
    expect(await screen.findByText('正文已写入，版本检查点保存失败')).toBeInTheDocument()

    if (confirmCandidates.length > 1) {
      await act(async () => {
        secondConfirmResponse.resolve(HttpResponse.json({
          error: '目标正文版本已变化',
          code: 'revision_conflict',
          details: { workspace_mutated: false },
        }, { status: 409 }))
        await secondConfirmResponse.promise
      })
    }

    expect(screen.getByText('正文已写入，版本检查点保存失败')).toBeInTheDocument()
    expect(screen.getByText('Markdown 正文已写入 chapters/short.md，但版本检查点保存失败。请先检查该正文，再手动保存一个版本。')).toBeInTheDocument()
    expect(screen.getByText('chapters/short.md')).toBeInTheDocument()
    expect(screen.queryByText('chapters/elsewhere.md')).not.toBeInTheDocument()
    expect(onWorkspaceChanged).toHaveBeenCalledTimes(1)
    expect(onWorkspaceChanged).toHaveBeenCalledWith(['chapters/short.md'])
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: candidate.base_revision,
      brief: '确认并保留精确路径',
    })
    expect(confirmLocales).toEqual(['zh-CN'])
    expect(confirmCandidates).toEqual([candidate])
  })
})

function sheetProps(overrides: Partial<ComponentProps<typeof FanqieCandidateSheet>> = {}): ComponentProps<typeof FanqieCandidateSheet> {
  return {
    open: true,
    onOpenChange: vi.fn(),
    workspace: '/workspace',
    selectedFile: null,
    fileSuggestions: [],
    disabled: false,
    onWorkspaceChanged: vi.fn(),
    ...overrides,
  }
}

function candidateFixture(overrides: Partial<ShortFictionCandidate> = {}): ShortFictionCandidate {
  return {
    profile_id: 'fanqie_short',
    profile_version: 'fanqie-short-v1',
    candidate_id: 'sha256:candidate',
    workspace: '/workspace',
    target_path: 'chapters/short.md',
    base_revision: `sha256:${'1'.repeat(64)}`,
    brief: '完整短篇要求',
    source: '# Existing',
    locale: 'zh-CN',
    preview_markdown: '# 明日订单',
    model_profile_id: 'ide',
    model: 'gpt-5.6',
    ...overrides,
  }
}

function writtenResult(candidate: ShortFictionCandidate) {
  return {
    status: 'written' as const,
    candidate_id: candidate.candidate_id,
    write_revision: `sha256:${'2'.repeat(64)}`,
    change_group_id: 'group-1',
    change_set_id: 'change-1',
    workspace_mutated: true as const,
    checkpoint_status: 'created' as const,
    checkpoint: {
      version_id: 'version-1',
      source: 'manual',
      path: candidate.target_path,
      revision: `sha256:${'2'.repeat(64)}`,
    },
    retryable: false as const,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((settle) => {
    resolve = settle
  })
  return { promise, resolve }
}
