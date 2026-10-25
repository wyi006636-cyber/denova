import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile } from '../support/api'

test('checks a chapter and lore health without changing manuscript text', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Chapter lint browser')
  await createProjectFile(request, book.projectId, 'setting/banned-words.md', '秘密 -> 隐情\ninvalid -> too -> many\n')
  const manyBrokenReferences = Array.from({ length: 80 }, (_, index) => `[[${String.fromCodePoint(0x4e00 + index)}${index + 1}]]`).join(' ')
  await createProjectFile(request, book.projectId, 'chapters/001.md', [
    '# Chapter 001',
    '',
    '这是秘密。秘密。秘密。'.repeat(4),
    '红色的旗帜在风中展开。红色的旗帜在风中展开。红色的旗帜在风中展开。',
    manyBrokenReferences,
    '[[Missing lore]]',
  ].join('\n'))
  await createProjectFile(request, book.projectId, 'chapters/002.md', '# Chapter 002\n\n一场平静的雨。')

  const loreURL = `/api/projects/${encodeURIComponent(book.projectId)}/book/lore/items`
  for (const item of [
    { id: 'hero', name: '主角', keywords: ['别名'], content: '一名守夜人。' },
    { id: 'duplicate', name: '村庄', keywords: ['主角'], content: '村庄资料。' },
    { id: 'empty', name: '空条目', content: '# 空条目' },
    { id: 'unused', name: '未引用资料', content: '没有任何正文引用。' },
  ]) {
    const response = await request.post(loreURL, { data: item })
    expect(response.ok(), await response.text()).toBe(true)
  }

  await page.goto('/')
  const sidebar = page.getByLabel('工作台侧边栏')
  await sidebar.getByRole('button', { name: '写作', exact: true }).click()
  const chapter = page.locator('[data-chapter-path="chapters/001.md"]')
  await expect(chapter).toBeVisible()
  await chapter.getByRole('button').first().click()
  await expect(page.locator('.tiptap.ProseMirror').first()).toContainText('秘密')

  const checkButton = page.getByRole('button', { name: '检查本章', exact: true })
  await checkButton.click()
  const panel = page.getByRole('complementary', { name: '本章检查', exact: true })
  await expect(panel).toBeVisible()
  await expect(panel).toContainText('发现')
  await expect(panel.getByText('禁用词', { exact: true }).locator('..')).toContainText('1')
  await expect(panel.getByText('重复', { exact: true }).locator('..')).toContainText('2')
  await expect(panel.getByText('断链引用', { exact: true }).locator('..')).toContainText('81')
  await page.screenshot({ path: test.info().outputPath('chapter-lint-wide-dark.png'), animations: 'disabled' })

  const bannedIssue = panel.getByRole('button').filter({ hasText: '秘密' }).first()
  await expect(bannedIssue).toBeVisible()
  await bannedIssue.click()
  await expect(page.locator('.nova-chapter-lint-current')).toHaveCount(1)
  const firstHighlight = await page.locator('.nova-chapter-lint-current').boundingBox()
  expect(firstHighlight).not.toBeNull()
  await bannedIssue.click()
  await expect(page.locator('.nova-chapter-lint-current')).toHaveCount(1)
  const secondHighlight = await page.locator('.nova-chapter-lint-current').boundingBox()
  expect(secondHighlight).not.toBeNull()
  expect(Math.abs((secondHighlight?.y || 0) - (firstHighlight?.y || 0)) + Math.abs((secondHighlight?.x || 0) - (firstHighlight?.x || 0))).toBeGreaterThan(1)
  const panelBox = await panel.boundingBox()
  expect(panelBox).not.toBeNull()
  const targetBox = secondHighlight
  expect(targetBox).not.toBeNull()
  const overlaps = Boolean(panelBox && targetBox
    && targetBox.x < panelBox.x + panelBox.width
    && panelBox.x < targetBox.x + targetBox.width
    && targetBox.y < panelBox.y + panelBox.height
    && panelBox.y < targetBox.y + targetBox.height)
  expect(overlaps).toBe(false)
  await panel.getByRole('button', { name: '关闭本章检查', exact: true }).click()
  await expect(page.locator('.nova-chapter-lint-highlight')).toHaveCount(0)

  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await checkButton.click()
  await expect(page.getByRole('complementary', { name: '本章检查', exact: true })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('chapter-lint-wide-light.png'), animations: 'disabled' })
  await page.screenshot({ path: test.info().outputPath('chapter-lint-light-invalid.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: test.info().outputPath('chapter-lint-narrow-390.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.screenshot({ path: test.info().outputPath('chapter-lint-many-issues.png'), animations: 'disabled' })

  await page.getByRole('button', { name: '关闭本章检查', exact: true }).click()
  const cleanChapter = page.locator('[data-chapter-path="chapters/002.md"]')
  await cleanChapter.getByRole('button').first().click()
  await expect(page.locator('.tiptap.ProseMirror').filter({ hasText: '一场平静的雨' }).first()).toBeVisible()
  await checkButton.click()
  const cleanPanel = page.getByRole('complementary', { name: '本章检查', exact: true })
  await expect(cleanPanel).toContainText('发现 0 个问题')
  await expect(cleanPanel.getByText('没有发现问题', { exact: true }).first()).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('chapter-lint-empty.png'), animations: 'disabled' })
  await cleanPanel.getByRole('button', { name: '关闭本章检查', exact: true }).click()

  await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
  const loreLibrary = page.getByTestId('lore-library')
  await expect(loreLibrary).toBeVisible()
  await loreLibrary.getByRole('button', { name: '检查资料', exact: true }).click()
  const health = page.getByRole('dialog', { name: '资料健康检查', exact: true })
  await expect(health).toBeVisible()
  await expect(health).toContainText('重名或别名冲突')
  await expect(health).toContainText('断链引用')
  await expect(health).toContainText('正文中没有 [[ ]] 引用（信息）')
  await page.screenshot({ path: test.info().outputPath('chapter-lint-lore-health.png'), animations: 'disabled' })
})
