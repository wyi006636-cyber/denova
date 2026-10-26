import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, readProjectFile, saveProjectFile } from '../support/api'
import { openWritingAgent } from '../support/agent-chat'

test('imports a source, lists cases, collects only a matching excerpt, and brings method into chat', async ({ page, request, browserDiagnostics }) => {
  browserDiagnostics.allow(/Failed to load resource: the server responded with a status of 400 .*\/book\/cases\/favorite/)
  browserDiagnostics.allow(/\[CasebookView\.tsx\] failed to toggle case status/)
  const book = await createAndOpenBook(request, 'Casebook browser')
  const indexPath = 'setting/case-index.md'
  await createProjectFile(request, book.projectId, indexPath, '# 写法案例\n\n## 已收藏\n\n## 待确认\n')

  await page.goto('/')
  const shortcut = page.getByRole('button', { name: '写法案例', exact: true }).first()
  await expect(shortcut).toBeVisible()
  await shortcut.click()
  await expect(page.getByRole('heading', { name: '写法案例', exact: true })).toBeVisible()
  await expect(page.getByText('还没有写法案例', { exact: true })).toBeVisible()

  const sourceFile = page.getByTestId('main').locator('input[type="file"]').first()
  await sourceFile.setInputFiles({ name: 'source.txt', mimeType: 'text/plain', buffer: Buffer.from('异常灯光在午夜自己亮了。') })
  await expect.poll(async () => (await readProjectFile(request, book.projectId, 'references/sources/source.txt')).content).toContain('异常灯光')
  await expect(page.getByText('还没有写法案例', { exact: true })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('casebook-empty-imported.png'), animations: 'disabled' })

  const sourcePath = 'references/sources/source.txt'
  const sourceContent = '第一行。\n异常灯光在午夜自己亮了。\n'
  await saveProjectFile(request, book.projectId, sourcePath, sourceContent)
  const validCasePath = 'setting/cases/c-opening.md'
  const invalidCasePath = 'setting/cases/c-broken.md'
  const orphanCasePath = 'setting/cases/c-orphan.md'
  await createProjectFile(request, book.projectId, validCasePath, '# c-opening 先给异常\n\n- 状态：待确认\n- 方向：开篇\n- 适用场景：章首\n- 方法：先写异常，再让人物处理。\n- 不要搬用：不要使用原作人物和原句。\n- 来源：references/sources/source.txt\n- 定位：未知\n- 原文摘录：异常灯光在午夜自己亮了。\n')
  await createProjectFile(request, book.projectId, invalidCasePath, '# c-broken 错误摘录\n\n- 状态：待确认\n- 方向：对白\n- 适用场景：需要对话时\n- 方法：用动作打断长台词。\n- 不要搬用：不要复制原作。\n- 来源：references/sources/source.txt\n- 定位：未知\n- 原文摘录：这句不存在。\n')
  await createProjectFile(request, book.projectId, orphanCasePath, '# c-orphan 未登记案例\n\n- 状态：待确认\n- 方向：节奏\n- 适用场景：转场\n- 方法：用短动作切换节奏。\n- 不要搬用：不要复制原作。\n- 来源：references/sources/source.txt\n- 定位：未知\n- 原文摘录：第一行。\n')
  const index = await readProjectFile(request, book.projectId, indexPath)
  await saveProjectFile(request, book.projectId, indexPath, `${index.content}\n- c-opening | 开篇 | 先写异常，再让人物处理。\n- c-broken | 对白 | 用动作打断长台词。\n- c-missing | 结构 | 文件已被删除。\n`)

  await page.reload()
  await page.getByRole('button', { name: '写法案例', exact: true }).first().click()
  await expect(page.getByText('c-opening', { exact: true })).toBeVisible()
  await expect(page.getByText('c-broken', { exact: true })).toBeVisible()
  await expect(page.getByText('c-orphan', { exact: true })).toBeVisible()
  await expect(page.getByText('未登记', { exact: true })).toBeVisible()
  await expect(page.getByText('c-missing', { exact: true })).toBeVisible()
  await expect(page.getByText('案例文件不存在', { exact: true })).toBeVisible()
  const openingRow = page.getByRole('button', { name: /c-opening/ }).first()
  await openingRow.click()
  await expect(page.getByText('异常灯光在午夜自己亮了。', { exact: true })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('casebook-list-wide-dark.png'), animations: 'disabled' })

  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await page.screenshot({ path: test.info().outputPath('casebook-list-wide-light.png'), animations: 'disabled' })
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'))

  const collectResponse = page.waitForResponse(response => response.request().method() === 'POST' && response.url().includes('/book/cases/favorite'))
  await page.getByRole('button', { name: '收藏', exact: true }).click()
  await collectResponse
  await expect(page.getByText('已收藏', { exact: true }).first()).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('casebook-favorite-success.png'), animations: 'disabled' })

  const brokenRow = page.getByRole('button', { name: /c-broken/ }).first()
  await brokenRow.click()
  await page.getByRole('button', { name: '收藏', exact: true }).last().click()
  await expect(page.getByText('原文摘录在来源中找不到，未收藏。', { exact: true })).toBeVisible()
  await expect(page.getByText('c-broken', { exact: true })).toBeVisible()
  await expect(page.getByText('正在读取案例库…', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('casebook-quote-failure.png'), animations: 'disabled' })

  const composer = await openWritingAgent(page)
  await openingRow.click()
  await page.getByRole('button', { name: '带入对话', exact: true }).click()
  await expect(composer).toContainText('先写异常，再让人物处理。')
  await expect(composer).not.toContainText('异常灯光在午夜自己亮了。')

  await page.setViewportSize({ width: 390, height: 844 })
  const narrowAgentTab = page.getByRole('tab', { name: 'Agent', exact: true })
  if (await narrowAgentTab.count()) await narrowAgentTab.click()
  await expect(page.getByPlaceholder(/输入消息/).filter({ visible: true })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('casebook-narrow-390.png'), animations: 'disabled' })
})
