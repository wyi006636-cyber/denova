import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, readProjectFile, saveProjectFile } from '../support/api'

test('shows chapter-scoped lore validity issues in health check', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Lore validity browser')
  await createProjectFile(request, book.projectId, 'chapters/ch00020-opening.md', '# Chapter 20\n\n早期章节。')
  await createProjectFile(request, book.projectId, 'chapters/ch00030-turn.md', '# Chapter 30\n\n转折章节。')
  const loreURL = `/api/projects/${encodeURIComponent(book.projectId)}/book/lore/items`
  for (const item of [
    {
      id: 'hero', name: '主角', type: 'character', content: '稳定身份。',
      spans: [{ id: 'future', from: 'ch00030', content: '第三十章后的身份。' }],
    },
  ]) {
    const response = await request.post(loreURL, { data: item })
    expect(response.ok(), await response.text()).toBe(true)
  }
  const itemsFile = await readProjectFile(request, book.projectId, 'setting/lore/items.json')
  const collection = JSON.parse(itemsFile.content) as { items: Array<Record<string, unknown>> }
  collection.items.push({
    id: 'broken', enabled: true, type: 'world', name: '坏范围', importance: 'minor',
    tags: [], keywords: [], load_mode: 'auto', content: '',
    spans: [{ id: 'reversed', from: 'ch00030', until: 'ch00020', content: '倒置范围。' }],
  }, {
    id: 'disconnected', enabled: true, type: 'world', name: '断前缀', importance: 'minor',
    tags: [], keywords: [], load_mode: 'auto', content: '',
    spans: [{ id: 'missing', from: 'ch00999', content: '尚未建章。' }],
  }, {
    id: 'invalid-prefix', enabled: true, type: 'world', name: '非法前缀', importance: 'minor',
    tags: [], keywords: [], load_mode: 'auto', content: '',
    spans: [{ id: 'invalid', from: 'chapter-30', content: '非法章节锚点。' }],
  })
  await saveProjectFile(request, book.projectId, 'setting/lore/items.json', JSON.stringify(collection, null, 2) + '\n')

  await page.goto('/')
  const sidebar = page.getByLabel('工作台侧边栏')
  await sidebar.getByRole('button', { name: '资料库', exact: true }).click()
  const library = page.getByTestId('lore-library')
  await expect(library).toBeVisible()
  await library.getByRole('button', { name: '检查资料', exact: true }).click()
  const health = page.getByRole('dialog', { name: '资料健康检查', exact: true })
  await expect(health).toBeVisible()
  await expect(health).toContainText('资料有效范围')
  await expect(health).toContainText('生效章不早于失效章')
  await expect(health).toContainText('没有对应的章节文件')
  await expect(health).toContainText('不是 ch##### 章节前缀')
  // Keep the acceptance group and its concrete diagnostics in the captured
  // viewport instead of leaving the panel scrolled on unrelated groups.
  for (const title of [/重名或别名冲突/, /正文为空或只有标题/, /断链引用/]) {
    await health.getByRole('button', { name: title }).click()
  }
  await expect(health).toContainText('生效章不早于失效章')
  await expect(health).toContainText('没有对应的章节文件')
  await expect(health).toContainText('不是 ch##### 章节前缀')
  await page.screenshot({ path: test.info().outputPath('lore-validity-wide-dark.png'), animations: 'disabled' })

  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await page.waitForTimeout(100)
  await page.screenshot({ path: test.info().outputPath('lore-validity-wide-light.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(health).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('lore-validity-narrow-390.png'), animations: 'disabled' })
})
