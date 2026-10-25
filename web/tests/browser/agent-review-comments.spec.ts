import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, readProjectFile, saveProjectFile } from '../support/api'
import { openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'

const reviewMarker = 'E2E_AGENT_REVIEW_COMMENTS'
const chapterPath = 'chapters/agent-review-comments.md'

test('shows Agent review comments, promotes one, and sends only that comment next turn', async ({ page, request }) => {
  test.setTimeout(90_000)
  const book = await createAndOpenBook(request, 'Agent review comments')
  const original = '# Agent review comments\n\n第一段原文。\n\n第二段原文。\n他说：“第一个。”\n他说：“第二个。”\n'
  await createProjectFile(request, book.projectId, chapterPath, original)
  const screenshotPath = (name: string) => test.info().outputPath(name)

  const streamRequests: Array<Record<string, unknown>> = []
  page.on('request', (requestEvent) => {
    if (requestEvent.method() !== 'POST' || !requestEvent.url().includes('/agent-chat')) return
    try {
      const body = requestEvent.postDataJSON()
      if (body && typeof body === 'object') streamRequests.push(body as Record<string, unknown>)
    } catch {
      // Non-JSON requests are outside this assertion's scope.
    }
  })

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByRole('button', { name: /^agent review comments\b/i }).click()
  await expect(page.locator('.editor-content .ProseMirror')).toContainText('第一段原文。')
  const composer = await openWritingAgent(page)
  await submitAgentChatMessage(page, composer, `Review the chapter. ${reviewMarker}`)
  await expect(page.getByText('Review comments created.', { exact: true })).toBeVisible()

  const review = async () => {
    const response = await request.get(`/api/projects/${encodeURIComponent(book.projectId)}/book/document-review`)
    expect(response.ok(), await response.text()).toBe(true)
    return response.json() as Promise<{ review_thread: { id: string; comments: Array<{ id: string; source?: string; body: string }> } }>
  }
  await expect.poll(async () => (await review()).review_thread.comments.length).toBe(2)
  await expect(page.locator('.nova-document-review-highlight')).toHaveCount(2)
  await expect(page.getByText('原文位置已变化', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: screenshotPath('wide-dark.png'), fullPage: true, animations: 'disabled' })

  const firstHighlight = page.locator('.nova-document-review-highlight').first()
  await firstHighlight.click()
  const firstThread = page.locator('.nova-review-comment-thread').filter({ hasText: 'AI 建议：补充这一段的具体动机。' })
  await expect(firstThread).toBeVisible()
  await expect(firstThread.getByText('AI 审稿', { exact: true })).toBeVisible()

  const beforePromotion = await review()
  const agentComment = beforePromotion.review_thread.comments.find((comment) => comment.body.includes('补充这一段'))
  const untouchedAgentComment = beforePromotion.review_thread.comments.find((comment) => comment.body.includes('转折更明确'))
  expect(agentComment?.source).toBe('agent')
  expect(untouchedAgentComment?.source).toBe('agent')
  await firstThread.getByRole('button', { name: '交给 Agent 处理', exact: true }).click()
  await expect(firstThread.getByRole('button', { name: '交给 Agent 处理', exact: true })).toHaveCount(0)
  await page.locator('.nova-document-review-highlight').nth(1).click()
  await expect(page.getByText('AI 审稿', { exact: true })).toHaveCount(1)
  await page.screenshot({ path: screenshotPath('author-and-agent.png'), fullPage: true, animations: 'disabled' })

  const afterPromotion = await review()
  const promoted = afterPromotion.review_thread.comments.find((comment) => comment.body.includes('补充这一段'))
  const untouched = afterPromotion.review_thread.comments.find((comment) => comment.body.includes('转折更明确'))
  expect(promoted?.source).toBe('author')
  expect(untouched?.source).toBe('agent')

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.locator('.nova-review-comment-thread').filter({ hasText: 'AI 建议：补充这一段的具体动机。' })).toBeVisible()
  await page.screenshot({ path: screenshotPath('narrow-390.png'), fullPage: false, animations: 'disabled' })

  await page.setViewportSize({ width: 1440, height: 900 })
  const collapseButtons = page.getByRole('button', { name: '折叠', exact: true })
  if (await collapseButtons.count()) await collapseButtons.first().click()
  if (await collapseButtons.count()) await collapseButtons.first().click()
  await expect(page.locator('.nova-review-comment-thread')).toHaveCount(0)
  await page.screenshot({ path: screenshotPath('folded-agent-author.png'), fullPage: false, animations: 'disabled' })

  await submitAgentChatMessage(page, composer, 'Please apply the promoted review comment.')
  await expect(page.getByText('Review comments created.', { exact: true }).last()).toBeVisible()
  await expect.poll(() => streamRequests.filter((body) => Array.isArray(body.review_feedback) && (body.review_feedback as unknown[]).length > 0).length).toBeGreaterThan(0)
  const feedbackRequest = streamRequests.filter((body) => Array.isArray(body.review_feedback) && (body.review_feedback as unknown[]).length > 0).at(-1)!
  const feedback = feedbackRequest.review_feedback as Array<{ source?: string; review_thread_id?: string; comment_ids?: string[] }>
  expect(feedback).toHaveLength(1)
  expect(feedback[0].source).toBe('document')
  expect(feedback[0].review_thread_id).toBe(afterPromotion.review_thread.id)
  expect(feedback[0].comment_ids).toEqual([promoted?.id])
  expect(feedback[0].comment_ids).not.toContain(untouched?.id)

  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await page.screenshot({ path: screenshotPath('wide-light.png'), fullPage: true, animations: 'disabled' })

  await saveProjectFile(request, book.projectId, chapterPath, '# Agent review comments\n\n锚点已改写。\n')
  await page.reload()
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByRole('button', { name: /^agent review comments\b/i }).click()
  await expect(page.getByText('原文位置已变化', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('这条 AI 批注不会随消息发送。请先点击“交给 Agent 处理”，让它进入下一条消息。', { exact: true })).toBeVisible()
  await page.screenshot({ path: screenshotPath('anchor-invalid.png'), fullPage: true, animations: 'disabled' })

  expect((await readProjectFile(request, book.projectId, chapterPath)).content).toContain('锚点已改写。')
})
