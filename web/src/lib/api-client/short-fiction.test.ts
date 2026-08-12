import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw/server'
import {
  confirmShortFictionCandidate,
  generateShortFictionCandidate,
  type ShortFictionCandidate,
} from './short-fiction'

const candidate: ShortFictionCandidate = {
  profile_id: 'fanqie_short',
  profile_version: 'fanqie-short-v1',
  candidate_id: 'sha256:candidate',
  workspace: '/workspace',
  target_path: 'chapters/short.md',
  base_revision: 'missing',
  brief: '完整短篇要求',
  source: '',
  locale: 'zh-CN',
  preview_markdown: '# 完整短篇\n\n正文。',
  model_profile_id: 'ide',
  model: 'gpt-5.6',
}

describe('short-fiction API client', () => {
  it('posts the exact generation authority and locale header', async () => {
    server.use(
      http.post('/api/short-fiction/candidates', async ({ request }) => {
        expect(request.headers.get('X-Denova-Locale')).toBe('zh-CN')
        expect(await request.json()).toEqual({
          workspace: '/workspace',
          profile_id: 'fanqie_short',
          target_path: 'chapters/short.md',
          base_revision: 'missing',
          brief: '完整短篇要求',
        })
        return HttpResponse.json(candidate)
      }),
    )

    await expect(generateShortFictionCandidate({
      workspace: '/workspace',
      profile_id: 'fanqie_short',
      target_path: 'chapters/short.md',
      base_revision: 'missing',
      brief: '完整短篇要求',
    }, 'zh-CN')).resolves.toEqual(candidate)
  })

  it('posts the complete client-held candidate for confirmation', async () => {
    const confirmation = {
      status: 'written' as const,
      candidate_id: candidate.candidate_id,
      write_revision: 'sha256:written',
      change_group_id: 'group-1',
      change_set_id: 'change-1',
      workspace_mutated: true as const,
      checkpoint_status: 'created' as const,
      checkpoint: {
        version_id: 'version-1',
        source: 'manual',
        path: candidate.target_path,
        revision: 'sha256:written',
      },
      retryable: false as const,
    }
    server.use(
      http.post('/api/short-fiction/candidates/confirm', async ({ request }) => {
        expect(request.headers.get('X-Denova-Locale')).toBe('en-US')
        expect(await request.json()).toEqual({ candidate })
        return HttpResponse.json(confirmation)
      }),
    )

    await expect(confirmShortFictionCandidate({ candidate }, 'en-US')).resolves.toEqual(confirmation)
  })
})
