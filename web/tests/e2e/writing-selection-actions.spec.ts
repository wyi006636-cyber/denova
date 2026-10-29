import { mkdirSync } from 'node:fs'
import { expect, test, type Locator, type Page } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'
import { openWritingAgent, submitAgentChatMessage } from '../support/agent-chat'

test('selection action sends the attached range and adopts a selected reply with undo', async ({ page, request }) => {
  test.setTimeout(90_000)
  const book = await createAndOpenBook(request, 'Writing selection actions')
  const chapterPath = 'chapters/selection-actions.md'
  const original = '# Selection actions\n\nOld prose.\n\nUntouched.'
  await createProjectFile(request, book.projectId, chapterPath, original)
  const screenshotDir = 'D:/vibe/denova-pr-review/selection-actions-screenshots'
  mkdirSync(screenshotDir, { recursive: true })

  await page.goto('/')
  await page.getByLabel('工作台侧边栏').getByRole('button', { name: '写作', exact: true }).click()
  await page.getByRole('button', { name: /^selection actions\b/i }).click()
  const editor = page.locator('.editor-content .ProseMirror')
  await expect(editor).toBeVisible()
  await editor.locator('p').nth(1).selectText()
  const darkToolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(darkToolbar).toBeVisible()
  await expectSelectionToolbarInsideEditor(page, darkToolbar)
  await expect(page.getByRole('button', { name: '替换原选段', exact: true })).toHaveCount(0)
  await page.screenshot({ path: `${screenshotDir}/wide-dark.png`, fullPage: true })
  await page.setViewportSize({ width: 390, height: 900 })
  // Selecting the complete paragraph leaves the selection head at its line
  // end, exercising the toolbar's right-edge clamp on a narrow viewport.
  await editor.locator('p').nth(1).selectText()
  const darkMobileToolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(darkMobileToolbar).toBeVisible()
  await expectSelectionToolbarInsideEditor(page, darkMobileToolbar)
  await page.screenshot({ path: `${screenshotDir}/narrow-dark.png`, fullPage: true })
  await page.setViewportSize({ width: 320, height: 900 })
  await editor.locator('p').nth(1).selectText()
  const narrow320Toolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(narrow320Toolbar).toBeVisible()
  await expectSelectionToolbarInsideEditor(page, narrow320Toolbar)
  await page.screenshot({ path: `${screenshotDir}/narrow-320.png`, fullPage: true })
  await page.setViewportSize({ width: 768, height: 900 })
  await editor.locator('p').nth(1).selectText()
  const mediumToolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(mediumToolbar).toBeVisible()
  await expectSelectionToolbarInsideEditor(page, mediumToolbar)
  await page.setViewportSize({ width: 1440, height: 900 })
  const currentSettings = await (await request.get('/api/settings')).json() as { revisions: { user: string }; user: { theme: string } }
  await request.patch('/api/settings', { data: { layer: 'user', base_revision: currentSettings.revisions.user, changes: { theme: 'light' } } })
  await page.reload()
  await expect(editor).toContainText('Selection actions')
  await editor.locator('p').nth(1).selectText()
  const lightToolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(lightToolbar).toBeVisible()
  await expectSelectionToolbarInsideEditor(page, lightToolbar)
  await page.screenshot({ path: `${screenshotDir}/wide-light.png`, fullPage: true })
  await page.setViewportSize({ width: 390, height: 900 })
  await editor.locator('p').nth(1).selectText()
  const mobileToolbar = page.getByRole('toolbar', { name: '选区动作' })
  await expect(mobileToolbar).toBeVisible()
  await expectSelectionToolbarInsideEditor(page, mobileToolbar)
  await page.screenshot({ path: `${screenshotDir}/narrow-light.png`, fullPage: true })
  await mobileToolbar.getByRole('button', { name: '润色', exact: true }).click()
  const mobileComposer = page.getByPlaceholder(/输入消息/).filter({ visible: true })
  await expect(mobileComposer).toBeVisible()
  await expect(mobileComposer).toContainText('请润色所附选区')
  await page.screenshot({ path: `${screenshotDir}/narrow-agent-draft.png`, fullPage: true })

  await page.setViewportSize({ width: 1440, height: 900 })
  const composer = await openWritingAgent(page)
  await expect(composer).toContainText('请润色所附选区')
  await submitAgentChatMessage(page, composer, 'Polish the attached selection for this deterministic reply.')
  const reply = page.getByText('Deterministic E2E response completed.', { exact: true })
  await expect(reply).toBeVisible()

  await reply.selectText()
  await expect(page.getByRole('button', { name: '替换原选段', exact: true })).toHaveCount(1)
  await page.screenshot({ path: `${screenshotDir}/reply-adoption-buttons.png`, fullPage: true })
  await page.getByRole('button', { name: '替换原选段', exact: true }).click()
  const preview = page.getByRole('dialog', { name: '预览采纳内容', exact: true })
  await expect(preview).toContainText(chapterPath)
  await page.screenshot({ path: `${screenshotDir}/replace-preview.png`, fullPage: true })
  await preview.getByRole('button', { name: '确认采纳', exact: true }).click()
  await expect(editor).toContainText('Deterministic E2E response completed.')

  await editor.click()
  await page.keyboard.press(process.platform === 'darwin' ? 'Meta+Z' : 'Control+Z')
  await expect(editor).toContainText('Old prose.')
  await expect(editor).toContainText('Untouched.')

  // A later ordinary turn must not inherit the previous action's replacement target.
  await submitAgentChatMessage(page, composer, 'Unrelated writing request. E2E_LONG_WRITING_REPLY')
  const longReply = page.locator('.chat-agent-message').last()
  await expect(longReply).toContainText('长候选正文')
  await expect(page.getByRole('button', { name: '替换原选段', exact: true })).toHaveCount(1)
  await longReply.selectText()
  await page.getByRole('button', { name: '插入到光标处', exact: true }).last().click()
  const longPreview = page.getByRole('dialog', { name: '预览采纳内容', exact: true })
  await expect(longPreview).toContainText('长候选正文')
  await page.screenshot({ path: `${screenshotDir}/long-text-preview.png`, fullPage: true })
})

async function expectSelectionToolbarInsideEditor(page: Page, toolbar: Locator) {
  const actionCount = await toolbar.getByRole('button').count()
  const geometry = await page.evaluate(() => {
    const toolbar = document.querySelector('[role="toolbar"][aria-label="选区动作"]') as HTMLElement | null
    const editor = document.querySelector('.editor-content .ProseMirror') as HTMLElement | null
    const surface = editor?.closest('.relative') as HTMLElement | null
    const rect = (element: HTMLElement | null) => element?.getBoundingClientRect().toJSON() ?? null
    const surfaceRect = surface?.getBoundingClientRect()
    const clip = surfaceRect ? {
      left: Math.max(surfaceRect.left, 0),
      right: Math.min(surfaceRect.right, window.innerWidth),
      top: Math.max(surfaceRect.top, 0),
      bottom: Math.min(surfaceRect.bottom, window.innerHeight),
    } : null
    return {
      viewportWidth: window.innerWidth,
      clip,
      toolbar: rect(toolbar),
      buttons: toolbar ? [...toolbar.querySelectorAll<HTMLElement>('button')].map(rect) : [],
    }
  })

  expect(geometry.clip, 'editor visible rectangle should exist').not.toBeNull()
  expect(geometry.toolbar, 'selection toolbar rectangle should exist').not.toBeNull()
  expect(actionCount, 'all seven selection actions should remain rendered').toBe(7)
  expect(actionCount, 'geometry should cover every rendered action').toBe(geometry.buttons.length)
  const clip = geometry.clip!
  for (const [index, button] of geometry.buttons.entries()) {
    expect(button, `selection action ${index} should have a rectangle`).not.toBeNull()
    expect(button!.left, `selection action ${index} left edge`).toBeGreaterThanOrEqual(clip.left - 0.5)
    expect(button!.right, `selection action ${index} right edge`).toBeLessThanOrEqual(clip.right + 0.5)
    expect(button!.top, `selection action ${index} top edge`).toBeGreaterThanOrEqual(clip.top - 0.5)
    expect(button!.bottom, `selection action ${index} bottom edge`).toBeLessThanOrEqual(clip.bottom + 0.5)
    expect(button!.left, `selection action ${index} viewport left edge`).toBeGreaterThanOrEqual(-0.5)
    expect(button!.right, `selection action ${index} viewport right edge`).toBeLessThanOrEqual(geometry.viewportWidth + 0.5)
  }
}
