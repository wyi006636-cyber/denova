import { mkdir } from 'node:fs/promises'
import { expect, test } from '../support/fixtures'
import { createAndOpenBook, createProjectFile, readProjectFile } from '../support/api'

test('shows chapter goals in the outline and toolbar, then confirms one ledger recovery', async ({ page, request }) => {
  const book = await createAndOpenBook(request, 'Story ledger browser')
  const chapterPath = 'chapters/ch00001-opening.md'
  const planPath = 'setting/chapter-groups/group01.md'
  const ledgerPath = 'setting/story-ledger.md'
  const chapterContent = '# 第一章 开场\n\n沈宁走进旧站。'
  const planContent = `# 第一组\n\n### ch00001 开场\n\n- 目标：拿到旧印\n- 冲突：守印人识破假身份\n- 要埋：t-old-seal\n- 要收：\n- 不得越界：不要写出旧印主人\n- 字数预期：3000\n- 章末钩子：血纹在手里发烫\n`
  const ledgerContent = `# 故事线与伏笔\n\n## t-old-seal 旧印\n\n- 种类：伏笔\n- 一句话：师门旧印会暴露行踪。\n- 埋设章：ch00001\n- 计划回收：ch00018\n- 实际回收：\n- 弃用：否\n- 待确认：ch00001\n`
  await createProjectFile(request, book.projectId, chapterPath, chapterContent)
  await createProjectFile(request, book.projectId, planPath, planContent)
  await createProjectFile(request, book.projectId, ledgerPath, ledgerContent)

  await page.goto('/')
  const chapterRow = page.locator(`[data-chapter-path="${chapterPath}"]`)
  await expect(chapterRow).toBeVisible()
  await chapterRow.getByRole('button').first().click()
  await expect(chapterRow.getByText('拿到旧印', { exact: true })).toBeVisible()

  const goalButton = page.getByRole('button', { name: '查看本章目标', exact: true }).filter({ visible: true }).first()
  await expect(goalButton).toBeVisible()
  await goalButton.click()
  await expect(page.getByText('拿到旧印', { exact: true }).last()).toBeVisible()
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'))
  await page.waitForTimeout(100)
  const screenshotRoot = 'D:/vibe/denova-pr-review/story-ledger-screenshots'
  await mkdir(screenshotRoot, { recursive: true })
  await page.screenshot({ path: `${screenshotRoot}/chapter-goal-wide-light.png`, animations: 'disabled' })

  await page.getByRole('button', { name: '故事台账', exact: true }).first().click()
  await expect(page.getByRole('heading', { name: '故事台账', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '已埋未收', exact: true }).click()
  const entryRow = page.locator('button[aria-expanded="false"]').filter({ hasText: 't-old-seal' }).filter({ visible: true }).first()
  await entryRow.click()
  const openedChapter = page.waitForResponse((response) => response.request().method() === 'GET'
    && response.url().includes(`/api/projects/${encodeURIComponent(book.projectId)}/files/file`)
    && new URL(response.url()).searchParams.get('path') === chapterPath)
  await page.getByRole('button', { name: /打开 ch00001/ }).first().click()
  await openedChapter

  const ledgerTab = page.getByRole('tab', { name: 'story-ledger.md', exact: true }).filter({ visible: true })
  if (await ledgerTab.count()) await ledgerTab.click()
  await expect(page.getByRole('heading', { name: '故事台账', exact: true })).toBeVisible()
  const confirmButton = page.getByRole('button', { name: '确认回收', exact: true })
  if (await confirmButton.count() === 0) {
    const reopenedEntry = page.getByRole('button').filter({ hasText: 't-old-seal' }).filter({ visible: true }).first()
    await reopenedEntry.click()
  }
  await expect(confirmButton).toBeVisible()
  const before = await readProjectFile(request, book.projectId, ledgerPath)
  await confirmButton.click()
  await expect(page.getByText('已回收', { exact: true })).toBeVisible()
  const after = await readProjectFile(request, book.projectId, ledgerPath)
  const beforeLines = before.content.split(/\r?\n/)
  const afterLines = after.content.split(/\r?\n/)
  const changedLines = afterLines.filter((line, index) => line !== beforeLines[index])
  expect(changedLines).toEqual(['- 实际回收：ch00001', '- 待确认：'])
})
