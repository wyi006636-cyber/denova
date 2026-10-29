import { expect, test } from '../support/fixtures'
import { createAndOpenBook } from '../support/api'

test('flushes an Agents edit before leaving for Writing without a prompt', async ({ page, request }) => {
  await createAndOpenBook(request, 'Config guard autosave')
  await page.goto('/')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await page.getByRole('button', { name: '上下文策略', exact: true }).click()
  const threshold = page.getByRole('spinbutton', { name: '触发阈值 (%)', exact: true }).first()
  await expect(threshold).toBeVisible()
  const before = Number(await threshold.inputValue())
  const next = before >= 98 ? before - 1 : before + 1
  await threshold.fill(String(next))
  await page.getByRole('button', { name: '写作', exact: true }).click()
  await expect(page.getByRole('button', { name: '写作', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(page.getByRole('alertdialog')).toHaveCount(0)

  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await expect.poll(async () => {
    const response = await request.get('/api/workspace/current')
    const current = await response.json() as { project_id: string }
    const settings = await request.get(`/api/projects/${encodeURIComponent(current.project_id)}/settings`)
    const body = await settings.json()
    return body.effective?.agent_context?.ide?.compaction_threshold
  }).toBe(next / 100)
})

test('shows the leave prompt after an Agents save fails', async ({ page, request, browserDiagnostics }) => {
  browserDiagnostics.allow(/http\.5xx: PATCH .*settings returned 500/)
  browserDiagnostics.allow(/console\.error: Failed to load resource: the server responded with a status of 500/)
  browserDiagnostics.allow(/console\.error: \[settings\] failed to save user settings for agents-view:/)
  await createAndOpenBook(request, 'Config guard failed save')
  let failSave = true
  await page.route(/\/api\/(?:projects\/[^/]+\/)?settings$/, async route => {
    if (route.request().method() !== 'PATCH' || !failSave) {
      await route.continue()
      return
    }
    await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'mock save failure' }) })
  })
  await page.goto('/')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await page.getByRole('button', { name: '上下文策略', exact: true }).click()
  const threshold = page.getByRole('spinbutton', { name: '触发阈值 (%)', exact: true }).first()
  await threshold.fill('77')
  await expect(page.getByText('配置保存失败', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '写作', exact: true }).click()
  await expect(page.getByRole('alertdialog')).toBeVisible()
  await expect(page.getByRole('alertdialog')).toContainText('保存失败')
  failSave = false
  await page.getByRole('button', { name: '继续编辑', exact: true }).click()
})




