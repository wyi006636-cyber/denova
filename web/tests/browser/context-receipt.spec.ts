import { mkdir } from 'node:fs/promises'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'
import { openWritingAgent } from '../support/agent-chat'

test('shows this-turn read references and opens a chapter from the receipt', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Context receipt browser')
  await createProjectFile(request, book.projectId, 'chapters/001.md', '# Chapter 001\n\nBrowser fixture.')
  const longChapterPath = `chapters/${'long-file-name-'.repeat(8)}.md`
  const longLoreName = `A very long lore name for a character ${'with-many-words-'.repeat(5)}`
  const messagesWithReferences = [
      {
        id: 'receipt-user', role: 'user', metadata: {
          user_references: [{ kind: 'selection', label: 'chapters/001.md', start_line: 1, end_line: 2 }],
        }, parts: [{ type: 'text', text: 'Continue from the chapter.' }],
      },
      {
        id: 'receipt-tools', role: 'assistant', metadata: { run_id: 'receipt-run' }, parts: [
          { type: 'dynamic-tool', toolName: 'read', toolCallId: 'receipt-read', state: 'output-available', input: { path: 'chapters/001.md' }, output: JSON.stringify({
            schema: 'resource.read.v1', status: 'success', source: { kind: 'local_text', path: 'chapters/001.md' }, limits: { truncated: false },
          }) + '\n1\\t# Chapter 001' },
          { type: 'dynamic-tool', toolName: 'read', toolCallId: 'receipt-read-long', state: 'output-available', input: { path: longChapterPath }, output: JSON.stringify({
            schema: 'resource.read.v1', status: 'success', source: { kind: 'local_text', path: longChapterPath }, limits: { truncated: false },
          }) + '\n1\\tLong chapter' },
          { type: 'dynamic-tool', toolName: 'query_lore_items', toolCallId: 'receipt-lore', state: 'output-available', input: {}, output: `# Lore Items\n\n## Hero (character / major / resident)\nID: hero\n\n\`\`\`markdown\nHero body\n\`\`\`\n\n## ${longLoreName} (character / minor / auto)\nID: long-lore\n\n\`\`\`markdown\nLong lore body\n\`\`\`` },
        ],
      },
      { id: 'receipt-final', role: 'assistant', metadata: { run_id: 'receipt-run', display_phase: 'final' }, parts: [{ type: 'text', text: 'Done', state: 'done' }] },
  ]
  let includeReferences = true
  await page.route('**/session/messages?*', route => route.fulfill({ json: {
    messages: includeReferences ? messagesWithReferences : [
      { id: 'empty-user', role: 'user', parts: [{ type: 'text', text: 'Nothing to read.' }] },
      { id: 'empty-final', role: 'assistant', metadata: { run_id: 'empty-run', display_phase: 'final' }, parts: [{ type: 'text', text: 'No files were needed.', state: 'done' }] },
    ],
    page: { has_more: false, total: includeReferences ? 3 : 2 },
  } }))
  await page.goto('/')
  await openWritingAgent(page)

  const receipt = page.locator('[data-context-receipt]').filter({ visible: true })
  await expect(receipt.getByText('本轮参考', { exact: true })).toBeVisible()
  await receipt.getByRole('button', { name: '展开本轮参考' }).click()
  await expect(receipt.getByText('chapters/001.md', { exact: true }).first()).toBeVisible()
  await expect(receipt.getByText('Hero', { exact: true })).toBeVisible()

  const screenshotRoot = 'D:/vibe/denova-pr-review/context-receipt-screenshots'
  await mkdir(screenshotRoot, { recursive: true })
  await page.screenshot({ path: `${screenshotRoot}/context-receipt-wide-dark.png`, animations: 'disabled' })
  await page.screenshot({ path: `${screenshotRoot}/context-receipt-long-names.png`, animations: 'disabled' })
  await page.setViewportSize({ width: 390, height: 844 })
  const narrowAgentTab = page.getByRole('tab', { name: 'Agent', exact: true }).filter({ visible: true })
  if (await narrowAgentTab.count()) await narrowAgentTab.click()
  const narrowReceipt = page.locator('[data-context-receipt]').filter({ visible: true })
  if (await narrowReceipt.getByRole('button', { name: '展开本轮参考' }).count()) await narrowReceipt.getByRole('button', { name: '展开本轮参考' }).click()
  await expect(narrowReceipt.getByText('本轮参考', { exact: true })).toBeVisible()
  await page.screenshot({ path: `${screenshotRoot}/context-receipt-narrow-expanded.png`, animations: 'disabled' })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await page.waitForTimeout(100)
  await page.screenshot({ path: `${screenshotRoot}/context-receipt-wide-light.png`, animations: 'disabled' })

  const openedFile = page.waitForResponse(response => response.request().method() === 'GET'
    && response.url().includes(`/api/projects/${encodeURIComponent(book.projectId)}/files/file`)
    && new URL(response.url()).searchParams.get('path') === 'chapters/001.md')
  await receipt.getByRole('button', { name: 'chapters/001.md', exact: true }).first().click()
  await openedFile

  includeReferences = false
  await page.reload()
  await openWritingAgent(page)
  const emptyReceipt = page.locator('[data-context-receipt]').filter({ visible: true })
  await expect(emptyReceipt.getByText('本轮未读取内容', { exact: true })).toBeVisible()
  await page.screenshot({ path: `${screenshotRoot}/context-receipt-empty.png`, animations: 'disabled' })
})
