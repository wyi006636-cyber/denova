import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ComponentProps } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import i18next, { setConfiguredLocale } from '@/i18n'
import type { ShortFictionCandidate, ShortFictionGenerateRequest } from '@/lib/api-client/short-fiction'
import { server } from '@/test/msw/server'
import { FanqieCandidateSheet } from './FanqieCandidateSheet'

describe('FanqieCandidateSheet', () => {
  afterEach(async () => {
    await act(async () => {
      setConfiguredLocale('zh-CN')
      await i18next.changeLanguage('zh-CN')
    })
  })

  it('previews without writing and confirms only after the explicit author action', async () => {
    const user = userEvent.setup()
    const onWorkspaceChanged = vi.fn()
    let confirmRequestCount = 0
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmedCandidate: ShortFictionCandidate | undefined

    const candidate: ShortFictionCandidate = {
      profile_id: 'fanqie_short',
      profile_version: 'fanqie-short-v1',
      candidate_id: 'sha256:candidate-existing',
      workspace: '/canonical/workspace',
      target_path: 'chapters/short.md',
      base_revision: `sha256:${'1'.repeat(64)}`,
      brief: '写一篇完整的都市反转短篇',
      source: '# 旧正文',
      locale: 'zh-CN',
      preview_markdown: '# 明日订单\n\n她接到了一份来自明天的外卖订单。',
      model_profile_id: 'ide',
      model: 'gpt-5.6',
    }

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({
          workspace: '/canonical/workspace',
          path: 'chapters/short.md',
          content: '# 旧正文',
          revision: `sha256:${'1'.repeat(64)}`,
        })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(candidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        const body = await request.json() as { candidate: ShortFictionCandidate }
        confirmedCandidate = body.candidate
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        return HttpResponse.json({
          status: 'written',
          candidate_id: candidate.candidate_id,
          write_revision: `sha256:${'2'.repeat(64)}`,
          change_group_id: 'group-1',
          change_set_id: 'change-1',
          workspace_mutated: true,
          checkpoint_status: 'created',
          checkpoint: {
            version_id: 'version-1',
            source: 'manual',
            path: candidate.target_path,
            revision: `sha256:${'2'.repeat(64)}`,
          },
          retryable: false,
        })
      }),
    )

    render(
      <FanqieCandidateSheet
        open
        onOpenChange={vi.fn()}
        workspace="/workspace"
        selectedFile="chapters/short.md"
        fileSuggestions={['chapters/short.md']}
        disabled={false}
        onWorkspaceChanged={onWorkspaceChanged}
      />,
    )

    expect(screen.getByLabelText('目标 Markdown')).toHaveValue('chapters/short.md')
    await user.type(screen.getByLabelText('创作要求'), '写一篇完整的都市反转短篇')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(await screen.findByText('明日订单')).toBeInTheDocument()
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/canonical/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: `sha256:${'1'.repeat(64)}`,
      brief: '写一篇完整的都市反转短篇',
    })
    expect(confirmRequestCount).toBe(0)

    await user.click(screen.getByRole('button', { name: '确认写入正文' }))

    await waitFor(() => expect(onWorkspaceChanged).toHaveBeenCalledWith(['chapters/short.md']))
    expect(onWorkspaceChanged).toHaveBeenCalledTimes(1)
    expect(confirmRequestCount).toBe(1)
    expect(confirmedCandidate).toEqual(candidate)
  })

  it.each([
    ['absent', null],
    ['non-Markdown', 'notes/idea.txt'],
  ])('requires an explicit Markdown path when selection is %s and binds a missing target to literal missing', async (_case, selectedFile) => {
    const user = userEvent.setup()
    let readRequestCount = 0
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmRequestCount = 0
    const expectedCandidate = candidateFixture({
      target_path: 'chapters/new-short.md',
      base_revision: 'missing',
      brief: '一篇悬疑反转短篇',
      source: '',
      candidate_id: 'sha256:missing-target',
    })

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        readRequestCount += 1
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/new-short.md')
        return HttpResponse.json({
          error: '文件不存在',
          code: 'not_found',
          details: { workspace_mutated: false },
        }, { status: 404 })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(expectedCandidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(await request.json()).toEqual({ candidate: expectedCandidate })
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        return HttpResponse.json(writtenResult(expectedCandidate))
      }),
    )

    renderSheet({ selectedFile, fileSuggestions: ['notes/idea.txt'] })

    expect(screen.getByLabelText('目标 Markdown')).toHaveValue('')
    await user.type(screen.getByLabelText('创作要求'), '一篇悬疑反转短篇')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    expect(screen.getByRole('alert')).toHaveTextContent('请输入工作区内可见的相对 .md 路径')
    expect(readRequestCount).toBe(0)

    await user.type(screen.getByLabelText('目标 Markdown'), 'chapters/new-short.md')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(await screen.findByText('明日订单')).toBeInTheDocument()
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/new-short.md',
      base_revision: 'missing',
      brief: '一篇悬疑反转短篇',
    })
    expect(confirmRequestCount).toBe(0)
  })

  it.each([
    '/abs.md',
    '../x.md',
    '.hidden/x.md',
    'chapters/.draft/x.md',
    'C:/x.md',
    'chapters/x.txt',
  ])('rejects explicit invalid target %s before any API request', async (invalidTarget) => {
    const user = userEvent.setup()
    const readPaths: string[] = []
    const generateBodies: ShortFictionGenerateRequest[] = []
    const generateLocales: string[] = []
    const confirmCandidates: ShortFictionCandidate[] = []

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        readPaths.push(new URL(request.url).searchParams.get('path') || '')
        return HttpResponse.json({ workspace: '/workspace', path: invalidTarget, content: '', revision: `sha256:${'1'.repeat(64)}` })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocales.push(request.headers.get('X-Denova-Locale') || '')
        generateBodies.push(await request.json() as ShortFictionGenerateRequest)
        return HttpResponse.json(candidateFixture({ target_path: invalidTarget }))
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        const body = await request.json() as { candidate: ShortFictionCandidate }
        confirmCandidates.push(body.candidate)
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        return HttpResponse.json(writtenResult(body.candidate))
      }),
    )

    renderSheet({ selectedFile: null, fileSuggestions: [] })
    await user.type(screen.getByLabelText('目标 Markdown'), invalidTarget)
    await user.type(screen.getByLabelText('创作要求'), '非法目标不得调用 API')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(screen.getByRole('alert')).toHaveTextContent('请输入工作区内可见的相对 .md 路径')
    expect(readPaths).toEqual([])
    expect(generateBodies).toEqual([])
    expect(generateLocales).toEqual([])
    expect(confirmCandidates).toEqual([])
  })

  it('defaults to the Markdown file selected before the first Sheet open', () => {
    const baseProps: ComponentProps<typeof FanqieCandidateSheet> = {
      open: false,
      onOpenChange: vi.fn(),
      workspace: '/workspace',
      selectedFile: null,
      fileSuggestions: [],
      disabled: false,
      onWorkspaceChanged: vi.fn(),
    }
    const { rerender } = render(<FanqieCandidateSheet {...baseProps} />)

    rerender(<FanqieCandidateSheet {...baseProps} open selectedFile="chapters/late-selection.md" />)

    expect(screen.getByLabelText('目标 Markdown')).toHaveValue('chapters/late-selection.md')
  })

  it('allows an explicit target override, normalizes it, and binds the fetched workspace and revision', async () => {
    const user = userEvent.setup()
    let readPath = ''
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmRequestCount = 0
    const revision = `sha256:${'3'.repeat(64)}`
    const expectedCandidate = candidateFixture({
      workspace: '/canonical/override',
      target_path: 'chapters/override.md',
      base_revision: revision,
      brief: '覆盖当前目标',
      source: '# 原正文',
      candidate_id: 'sha256:override',
    })

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        readPath = new URL(request.url).searchParams.get('path') || ''
        return HttpResponse.json({
          workspace: '/canonical/override',
          path: 'chapters/override.md',
          content: '# 原正文',
          revision,
        })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(expectedCandidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(await request.json()).toEqual({ candidate: expectedCandidate })
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        return HttpResponse.json(writtenResult(expectedCandidate))
      }),
    )

    renderSheet({ selectedFile: 'chapters/short.md' })
    await user.clear(screen.getByLabelText('目标 Markdown'))
    await user.type(screen.getByLabelText('目标 Markdown'), ' chapters//override.md ')
    await user.type(screen.getByLabelText('创作要求'), '覆盖当前目标')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(await screen.findByText('明日订单')).toBeInTheDocument()
    expect(readPath).toBe('chapters/override.md')
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/canonical/override',
      profile_id: 'fanqie_short',
      target_path: 'chapters/override.md',
      base_revision: revision,
      brief: '覆盖当前目标',
    })
    expect(confirmRequestCount).toBe(0)
  })

  it('does not treat a non-404 read failure as a missing target and preserves author input', async () => {
    const user = userEvent.setup()
    let generateRequestCount = 0
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/locked.md')
        return HttpResponse.json({
          error: '无权读取目标文件',
          code: 'forbidden',
          details: { workspace_mutated: false },
        }, { status: 403 })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateRequestCount += 1
        const body = await request.json() as ShortFictionGenerateRequest
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        expect(body).toEqual({
          workspace: '/workspace',
          profile_id: 'fanqie_short',
          target_path: 'chapters/locked.md',
          base_revision: 'missing',
          brief: '保留我的创作要求',
        })
        return HttpResponse.json(candidateFixture())
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(await request.json()).toEqual({ candidate: candidateFixture() })
        return HttpResponse.json(writtenResult(candidateFixture()))
      }),
    )

    renderSheet({ selectedFile: 'chapters/locked.md' })
    await user.type(screen.getByLabelText('创作要求'), '保留我的创作要求')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('无法生成预览')
    expect(screen.getByRole('alert')).toHaveTextContent('无权读取目标文件')
    expect(screen.getByLabelText('目标 Markdown')).toHaveValue('chapters/locked.md')
    expect(screen.getByLabelText('创作要求')).toHaveValue('保留我的创作要求')
    expect(generateRequestCount).toBe(0)
    expect(confirmRequestCount).toBe(0)
  })

  it('renders a long target and preview without a horizontal overflow class', async () => {
    const user = userEvent.setup()
    const longTarget = `chapters/${'very-long-story-segment-'.repeat(18)}end.md`
    const longMarkdown = `# 明日订单\n\n${'没有空格的超长正文'.repeat(140)}`
    const revision = `sha256:${'4'.repeat(64)}`
    const expectedCandidate = candidateFixture({
      target_path: longTarget,
      base_revision: revision,
      brief: '长文本验证',
      source: '# 旧正文',
      preview_markdown: longMarkdown,
      candidate_id: 'sha256:long-preview',
    })
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe(longTarget)
        return HttpResponse.json({ workspace: '/workspace', path: longTarget, content: '# 旧正文', revision })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(expectedCandidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(await request.json()).toEqual({ candidate: expectedCandidate })
        return HttpResponse.json(writtenResult(expectedCandidate))
      }),
    )

    renderSheet({ selectedFile: longTarget })
    await user.type(screen.getByLabelText('创作要求'), '长文本验证')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    const preview = await screen.findByTestId('fanqie-preview')
    const targetMetadata = screen.getByText(longTarget)
    expect(preview).toHaveClass('overflow-x-hidden')
    expect(preview).not.toHaveClass('overflow-x-auto')
    expect(preview.querySelector('.overflow-x-auto')).toBeNull()
    expect(targetMetadata).toHaveClass('break-all')
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: longTarget,
      base_revision: revision,
      brief: '长文本验证',
    })
    expect(confirmRequestCount).toBe(0)
  })

  it('reports written_checkpoint_failed as committed content without unsafe recovery language', async () => {
    const user = userEvent.setup()
    const onWorkspaceChanged = vi.fn()
    const revision = `sha256:${'5'.repeat(64)}`
    const expectedCandidate = candidateFixture({
      base_revision: revision,
      brief: '检查点失败场景',
      source: '# 旧正文',
      candidate_id: 'sha256:partial',
    })
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmedCandidate: ShortFictionCandidate | undefined
    let confirmLocale = ''

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({ workspace: '/workspace', path: 'chapters/short.md', content: '# 旧正文', revision })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(expectedCandidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmLocale = request.headers.get('X-Denova-Locale') || ''
        const body = await request.json() as { candidate: ShortFictionCandidate }
        confirmedCandidate = body.candidate
        return HttpResponse.json({
          status: 'written_checkpoint_failed',
          candidate_id: expectedCandidate.candidate_id,
          write_revision: `sha256:${'6'.repeat(64)}`,
          change_group_id: 'group-partial',
          change_set_id: 'change-partial',
          workspace_mutated: true,
          checkpoint_status: 'failed',
          retryable: false,
        })
      }),
    )

    renderSheet({ selectedFile: 'chapters/short.md', onWorkspaceChanged })
    await user.type(screen.getByLabelText('创作要求'), '检查点失败场景')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    expect(await screen.findByText('明日订单')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '确认写入正文' }))

    expect(await screen.findByText('正文已写入，版本检查点保存失败')).toBeInTheDocument()
    expect(screen.getByText('Markdown 正文已写入 chapters/short.md，但版本检查点保存失败。请先检查该正文，再手动保存一个版本。')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).not.toHaveTextContent(/retry|rollback|receipt|重试|回滚|收据/i)
    expect(onWorkspaceChanged).toHaveBeenCalledTimes(1)
    expect(onWorkspaceChanged).toHaveBeenCalledWith(['chapters/short.md'])
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: revision,
      brief: '检查点失败场景',
    })
    expect(confirmLocale).toBe('zh-CN')
    expect(confirmedCandidate).toEqual(expectedCandidate)
  })

  it.each([
    [
      'zh-CN',
      '正文可能已经写入 chapters/durability.md，但写入耐久性或日志恢复仍待完成。请先检查这个目标，不要重试本次确认。',
      '确认写入正文',
    ],
    [
      'en-US',
      'The manuscript may already be visible at chapters/durability.md, but write durability or journal recovery is still pending. Inspect that target first and do not retry this confirmation.',
      'Confirm manuscript write',
    ],
  ] as const)('treats durability_pending as a non-retryable possibly visible write in %s', async (locale, message, confirmLabel) => {
    await act(async () => {
      setConfiguredLocale(locale)
      await i18next.changeLanguage(locale)
    })
    const user = userEvent.setup()
    const onWorkspaceChanged = vi.fn()
    const revision = `sha256:${'d'.repeat(64)}`
    const expectedCandidate = candidateFixture({
      target_path: 'chapters/durability.md',
      base_revision: revision,
      brief: 'durability pending',
      candidate_id: 'sha256:durability-pending',
      locale,
    })
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', () => HttpResponse.json({
        workspace: '/workspace',
        path: expectedCandidate.target_path,
        content: expectedCandidate.source,
        revision,
      })),
      http.post('/api/short-fiction/candidates', () => HttpResponse.json(expectedCandidate)),
      http.post('/api/short-fiction/candidates/confirm', () => {
        confirmRequestCount += 1
        return HttpResponse.json({
          error: 'generic server text must not imply the workspace stayed unchanged',
          code: 'durability_pending',
          details: {
            workspace_mutated: true,
            recovery_pending: true,
            retryable: false,
            target_path: 'chapters/untrusted-response-target.md',
            write_revision: `sha256:${'e'.repeat(64)}`,
          },
        }, { status: 500 })
      }),
    )

    renderSheet({ selectedFile: expectedCandidate.target_path, onWorkspaceChanged })
    await user.type(screen.getByLabelText(locale === 'zh-CN' ? '创作要求' : 'Writing brief'), 'durability pending')
    await user.click(screen.getByRole('button', { name: locale === 'zh-CN' ? '生成完整短篇预览' : 'Generate complete story preview' }))
    const confirmButton = await screen.findByRole('button', { name: confirmLabel })
    await user.click(confirmButton)

    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(screen.getByRole('button', { name: confirmLabel })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: confirmLabel }))
    await waitFor(() => expect(onWorkspaceChanged).toHaveBeenCalledTimes(1))
    expect(onWorkspaceChanged).toHaveBeenCalledWith(['chapters/durability.md'])
    expect(confirmRequestCount).toBe(1)
  })

  it.each([
    [
      'zh-CN',
      '工作区之前的变更可能已经在 recovery/prior.md 可见，但恢复仍待完成；当前候选尚未确认写入。请先检查该路径，在恢复完成前不要重试。',
      '创作要求',
      '生成完整短篇预览',
      '确认写入正文',
    ],
    [
      'en-US',
      'A previous workspace change may already be visible at recovery/prior.md, but recovery is still pending; the current candidate was not confirmed. Inspect that path first and do not retry until recovery is resolved.',
      'Writing brief',
      'Generate complete story preview',
      'Confirm manuscript write',
    ],
  ] as const)('keeps prior durability recovery separate from the current candidate in %s', async (locale, message, briefLabel, generateLabel, confirmLabel) => {
    await act(async () => {
      setConfiguredLocale(locale)
      await i18next.changeLanguage(locale)
    })
    const user = userEvent.setup()
    const onWorkspaceChanged = vi.fn()
    const revision = `sha256:${'a'.repeat(64)}`
    const currentTarget = 'chapters/current.md'
    const expectedCandidate = candidateFixture({
      target_path: currentTarget,
      base_revision: revision,
      brief: 'prior recovery pending',
      candidate_id: 'sha256:prior-recovery',
      locale,
    })
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', () => HttpResponse.json({
        workspace: '/workspace',
        path: currentTarget,
        content: expectedCandidate.source,
        revision,
      })),
      http.post('/api/short-fiction/candidates', () => HttpResponse.json(expectedCandidate)),
      http.post('/api/short-fiction/candidates/confirm', () => {
        confirmRequestCount += 1
        return HttpResponse.json({
          error: 'prior recovery pending',
          code: 'durability_pending',
          details: {
            workspace_mutated: false,
            recovery_pending: true,
            retryable: false,
            recovery_target_path: 'recovery/prior.md',
          },
        }, { status: 500 })
      }),
    )

    renderSheet({ selectedFile: currentTarget, onWorkspaceChanged })
    await user.type(screen.getByLabelText(briefLabel), 'prior recovery pending')
    await user.click(screen.getByRole('button', { name: generateLabel }))
    await user.click(await screen.findByRole('button', { name: confirmLabel }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(message)
    expect(alert).not.toHaveTextContent(currentTarget)
    expect(screen.getByRole('button', { name: confirmLabel })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: confirmLabel }))
    expect(onWorkspaceChanged).not.toHaveBeenCalled()
    expect(confirmRequestCount).toBe(1)
  })

  it('keeps the brief and target after a generation error', async () => {
    const user = userEvent.setup()
    const revision = `sha256:${'7'.repeat(64)}`
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/error.md')
        return HttpResponse.json({ workspace: '/workspace', path: 'chapters/error.md', content: '# 旧正文', revision })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json({
          error: '上游模型暂不可用',
          code: 'generation_failed',
          details: { workspace_mutated: false },
        }, { status: 502 })
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(await request.json()).toEqual({ candidate: candidateFixture() })
        return HttpResponse.json(writtenResult(candidateFixture()))
      }),
    )

    renderSheet({ selectedFile: 'chapters/error.md' })
    await user.type(screen.getByLabelText('创作要求'), '错误后仍要保留的要求')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('无法生成预览')
    expect(screen.getByRole('alert')).toHaveTextContent('上游模型暂不可用')
    expect(screen.getByLabelText('目标 Markdown')).toHaveValue('chapters/error.md')
    expect(screen.getByLabelText('创作要求')).toHaveValue('错误后仍要保留的要求')
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/error.md',
      base_revision: revision,
      brief: '错误后仍要保留的要求',
    })
    expect(confirmRequestCount).toBe(0)
  })

  it('keeps the complete preview and gives actionable guidance on a confirmation revision conflict', async () => {
    const user = userEvent.setup()
    const onWorkspaceChanged = vi.fn()
    const revision = `sha256:${'8'.repeat(64)}`
    const expectedCandidate = candidateFixture({
      base_revision: revision,
      brief: '确认冲突场景',
      source: '# 旧正文',
      candidate_id: 'sha256:conflict',
    })
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmedCandidate: ShortFictionCandidate | undefined
    let confirmLocale = ''

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({ workspace: '/workspace', path: 'chapters/short.md', content: '# 旧正文', revision })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(expectedCandidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmLocale = request.headers.get('X-Denova-Locale') || ''
        const body = await request.json() as { candidate: ShortFictionCandidate }
        confirmedCandidate = body.candidate
        return HttpResponse.json({
          error: '目标正文版本已变化',
          code: 'revision_conflict',
          details: { workspace_mutated: false },
        }, { status: 409 })
      }),
    )

    renderSheet({ selectedFile: 'chapters/short.md', onWorkspaceChanged })
    await user.type(screen.getByLabelText('创作要求'), '确认冲突场景')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))
    await user.click(await screen.findByRole('button', { name: '确认写入正文' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('无法确认写入')
    expect(screen.getByRole('alert')).toHaveTextContent('重新生成预览，以绑定最新版本')
    expect(screen.getByText('明日订单')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '返回创作要求' })).toBeInTheDocument()
    expect(onWorkspaceChanged).not.toHaveBeenCalled()
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: revision,
      brief: '确认冲突场景',
    })
    expect(confirmLocale).toBe('zh-CN')
    expect(confirmedCandidate).toEqual(expectedCandidate)
  })

  it.each([
    ['empty candidate', HttpResponse.json(candidateFixture({ preview_markdown: '' })), '模型返回了空内容'],
    ['oversized candidate', HttpResponse.json({ error: '候选太长', code: 'candidate_too_large', details: { workspace_mutated: false } }, { status: 502 }), '生成内容超过候选大小上限'],
  ])('distinguishes %s before confirmation', async (_case, generationResponse, expectedMessage) => {
    const user = userEvent.setup()
    const revision = `sha256:${'9'.repeat(64)}`
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({ workspace: '/workspace', path: 'chapters/short.md', content: '# 旧正文', revision })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return generationResponse
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(await request.json()).toEqual({ candidate: candidateFixture({ preview_markdown: '' }) })
        return HttpResponse.json(writtenResult(candidateFixture()))
      }),
    )

    renderSheet({ selectedFile: 'chapters/short.md' })
    await user.type(screen.getByLabelText('创作要求'), '空内容与超限检查')
    await user.click(screen.getByRole('button', { name: '生成完整短篇预览' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(expectedMessage)
    expect(screen.queryByRole('button', { name: '确认写入正文' })).not.toBeInTheDocument()
    expect(generateLocale).toBe('zh-CN')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: revision,
      brief: '空内容与超限检查',
    })
    expect(confirmRequestCount).toBe(0)
  })

  it('renders paired English copy and sends the English locale', async () => {
    await act(async () => {
      setConfiguredLocale('en-US')
      await i18next.changeLanguage('en-US')
    })
    const user = userEvent.setup()
    const revision = `sha256:${'a'.repeat(64)}`
    const expectedCandidate = candidateFixture({
      base_revision: revision,
      brief: 'Write a complete reversal story',
      source: '# Existing copy',
      locale: 'en-US',
      candidate_id: 'sha256:english',
      preview_markdown: '# Tomorrow Order\n\nThe order arrived one day early.',
    })
    let generateRequest: ShortFictionGenerateRequest | undefined
    let generateLocale = ''
    let confirmRequestCount = 0

    server.use(
      http.get('/api/workspace/file', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('chapters/short.md')
        return HttpResponse.json({ workspace: '/workspace', path: 'chapters/short.md', content: '# Existing copy', revision })
      }),
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        generateLocale = request.headers.get('X-Denova-Locale') || ''
        generateRequest = await request.json() as ShortFictionGenerateRequest
        return HttpResponse.json(expectedCandidate)
      }),
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        confirmRequestCount += 1
        expect(request.headers.get('X-Denova-Locale')).toBe('en-US')
        expect(await request.json()).toEqual({ candidate: expectedCandidate })
        return HttpResponse.json(writtenResult(expectedCandidate))
      }),
    )

    renderSheet({ selectedFile: 'chapters/short.md' })
    expect(screen.getByRole('dialog')).not.toHaveTextContent(/chat\.fanqie\./)
    expect(screen.getByLabelText('Target Markdown')).toHaveValue('chapters/short.md')
    await user.type(screen.getByLabelText('Writing brief'), 'Write a complete reversal story')
    await user.click(screen.getByRole('button', { name: 'Generate complete story preview' }))

    expect(await screen.findByText('Tomorrow Order')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Confirm manuscript write' })).toBeInTheDocument()
    expect(generateLocale).toBe('en-US')
    expect(generateRequest).toEqual({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: revision,
      brief: 'Write a complete reversal story',
    })
    expect(confirmRequestCount).toBe(0)
  })
})

function renderSheet(overrides: Partial<ComponentProps<typeof FanqieCandidateSheet>> = {}) {
  return render(
    <FanqieCandidateSheet
      open
      onOpenChange={vi.fn()}
      workspace="/workspace"
      selectedFile="chapters/short.md"
      fileSuggestions={['chapters/short.md']}
      disabled={false}
      onWorkspaceChanged={vi.fn()}
      {...overrides}
    />,
  )
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
    source: '# 旧正文',
    locale: 'zh-CN',
    preview_markdown: '# 明日订单\n\n她接到了一份来自明天的外卖订单。',
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
