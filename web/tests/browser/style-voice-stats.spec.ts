import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'

test('shows offline style statistics, chapter truncation and character voice findings', async ({ page, request }) => {
  test.setTimeout(60_000)
  const book = await createAndOpenBook(request, 'Style voice stats browser')
  for (let index = 1; index <= 13; index++) {
    const chapter = String(index).padStart(3, '0')
    await createProjectFile(request, book.projectId, `chapters/${chapter}.md`, [
      `# Chapter ${chapter}`,
      '',
      index === 1 ? '沈宁说：「在下不会退。」' : `这是第${index}章的正文。`,
      '',
      '红色旗帜在风中展开。红色旗帜在风中展开。',
    ].join('\n'))
  }
  const loreURL = `/api/projects/${encodeURIComponent(book.projectId)}/book/lore/items`
  const loreResponse = await request.post(loreURL, {
    data: {
      type: 'character',
      type_source: 'manual',
      name: '沈宁',
      keywords: ['阿宁'],
      content: '## 说话方式\n\n- 禁忌说法：在下、鄙人\n- 示例：在下只是示例。',
    },
  })
  expect(loreResponse.ok(), await loreResponse.text()).toBe(true)

  await page.goto('/')
  const sidebar = page.getByLabel('工作台侧边栏')
  await sidebar.getByRole('button', { name: '写作', exact: true }).click()
  const firstChapter = page.locator('[data-chapter-path="chapters/001.md"]')
  await expect(firstChapter).toBeVisible()
  await firstChapter.getByRole('button').first().click()
  await expect(page.locator('.tiptap.ProseMirror').first()).toContainText('在下')

  const checkButton = page.getByRole('button', { name: '检查本章', exact: true })
  await checkButton.click()
  const panel = page.getByRole('complementary', { name: '本章检查', exact: true })
  await expect(panel).toBeVisible()
  await panel.getByRole('tab', { name: '统计', exact: true }).click()
  const stats = panel.getByTestId('style-fingerprint-panel')
  await expect(stats).toContainText('章数')
  await expect(stats).toContainText('字数')
  await expect(stats).toContainText('1')

  const chapterChecks = stats.locator('input[type="checkbox"]')
  await expect(chapterChecks).toHaveCount(13)
  await page.screenshot({ path: test.info().outputPath('style-stats-wide-dark.png'), animations: 'disabled' })
  for (let index = 1; index < 13; index++) await chapterChecks.nth(index).check()
  await expect(stats).toContainText('已选 13 章')
  await stats.getByRole('button', { name: '统计多章', exact: true }).click()
  await expect(stats).toContainText('只统计了前 12 章')
  await page.screenshot({ path: test.info().outputPath('style-stats-truncated.png'), animations: 'disabled' })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: test.info().outputPath('style-stats-narrow-390.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await page.waitForTimeout(100)
  await page.screenshot({ path: test.info().outputPath('style-stats-wide-light.png'), animations: 'disabled' })

  await panel.getByRole('tab', { name: '问题', exact: true }).click()
  const voiceGroup = panel.getByText('人物口吻', { exact: true }).locator('..')
  await expect(voiceGroup).toContainText('1')
  const voiceIssue = panel.getByRole('button').filter({ hasText: '在下' }).first()
  await expect(voiceIssue).toBeVisible()
  await voiceIssue.click()
  await expect(page.locator('.nova-chapter-lint-current')).toHaveCount(1)
  await page.screenshot({ path: test.info().outputPath('style-voice-warning-highlight.png'), animations: 'disabled' })

  await panel.getByRole('button', { name: '关闭本章检查', exact: true }).click()

  const emptyBook = await createAndOpenBook(request, 'Style voice stats empty')
  await createProjectFile(request, emptyBook.projectId, 'chapters/001.md', '# Empty voice chapter\n\n只有叙述，没有人物资料。')
  await page.goto('/')
  await sidebar.getByRole('button', { name: '写作', exact: true }).click()
  const emptyChapter = page.locator('[data-chapter-path="chapters/001.md"]')
  await expect(emptyChapter).toBeVisible()
  await emptyChapter.getByRole('button').first().click()
  await expect(page.locator('.tiptap.ProseMirror').filter({ hasText: '只有叙述' }).first()).toBeVisible()
  await page.getByRole('button', { name: '检查本章', exact: true }).filter({ visible: true }).first().click()
  const cleanPanel = page.getByRole('complementary', { name: '本章检查', exact: true })
  const cleanVoiceGroup = cleanPanel.getByText('人物口吻', { exact: true }).locator('..')
  await expect(cleanVoiceGroup).toContainText('0')
  await expect(cleanPanel.getByText('没有发现问题', { exact: true }).last()).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('style-voice-empty.png'), animations: 'disabled' })
})
