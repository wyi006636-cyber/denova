import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'
import { openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'

test('selection action sends the attached range and adopts a selected reply with undo', async ({ page, request }) => {
  test.setTimeout(50_000)
  const book = await createAndOpenBook(request, 'Writing selection actions')
  const chapterPath = 'chapters/selection-actions.md'
  const original = '# Selection actions\n\nOld prose.\n\nUntouched.'
  await createProjectFile(request, book.projectId, chapterPath, original)

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByRole('button', { name: /^selection actions\b/i }).click()
  const editor = page.locator('.editor-content .ProseMirror')
  await expect(editor).toBeVisible()
  const screenshotDir = 'D:/vibe/denova-pr-review/selection-actions-screenshots'
  await page.screenshot({ path: `${screenshotDir}/wide-dark.png`, fullPage: true })
  const currentSettings = await (await request.get('/api/settings')).json() as { revisions: { user: string }; user: { theme: string } }
  await request.patch('/api/settings', { data: { layer: 'user', base_revision: currentSettings.revisions.user, changes: { theme: 'light' } } })
  await page.reload()
  await expect(editor).toContainText('Selection actions')
  await page.setViewportSize({ width: 390, height: 900 })
  await page.screenshot({ path: `${screenshotDir}/narrow-light.png`, fullPage: true })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.screenshot({ path: `${screenshotDir}/wide-light.png`, fullPage: true })
  const lightSettings = await (await request.get('/api/settings')).json() as { revisions: { user: string } }
  await request.patch('/api/settings', { data: { layer: 'user', base_revision: lightSettings.revisions.user, changes: { theme: 'dark' } } })
  await page.reload()
  await expect(editor).toContainText('Selection actions')
  await page.setViewportSize({ width: 390, height: 900 })
  await page.screenshot({ path: `${screenshotDir}/narrow-dark.png`, fullPage: true })
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(editor).toBeVisible()
  await editor.locator('p').nth(1).selectText()

  const toolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(toolbar).toBeVisible()
  await toolbar.getByRole('button', { name: '润色', exact: true }).click()

  const composer = await openWritingAgent(page)
  await expect(composer).toContainText('请润色所附选区')
  await submitAgentChatMessage(page, composer, 'Polish the attached selection for this deterministic reply.')
  const reply = page.getByText('Deterministic E2E response completed.', { exact: true })
  await expect(reply).toBeVisible()

  await reply.selectText()
  await page.getByRole('button', { name: '替换原选段', exact: true }).click()
  const preview = page.getByRole('dialog', { name: '预览采纳内容', exact: true })
  await expect(preview).toContainText(chapterPath)
  await preview.getByRole('button', { name: '确认采纳', exact: true }).click()
  await expect(editor).toContainText('Deterministic E2E response completed.')

  await editor.click()
  await page.keyboard.press(process.platform === 'darwin' ? 'Meta+Z' : 'Control+Z')
  await expect(editor).toContainText('Old prose.')
  await expect(editor).toContainText('Untouched.')
})
