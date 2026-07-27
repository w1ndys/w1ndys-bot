// 📌 影响范围：拦截违禁监控专属 API 并维护内存测试状态；不访问真实数据库、NapCat 或外部模型。
import { expect, test, type Page, type Route } from '@playwright/test'

async function fulfill(route: Route, data: unknown): Promise<void> {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ code: 'ok', message: '成功', data }),
  })
}

async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => sessionStorage.setItem('w1ndys_bot_token', 'e2e-token'))
}

async function mockForbiddenMonitor(page: Page): Promise<void> {
  let terms: Array<Record<string, unknown>> = []
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    expect(request.headers().authorization).toBe('Bearer e2e-token')
    const url = new URL(request.url())
    const key = `${request.method()} ${url.pathname}`
    switch (key) {
      case 'GET /api/plugins/forbidden_message_monitor/violations':
      case 'GET /api/plugins/forbidden_message_monitor/training-samples':
      case 'GET /api/plugins/forbidden_message_monitor/combinations':
        await fulfill(route, { items: [], page: 1, page_size: 20, total: 0 })
        return
      case 'GET /api/plugins/forbidden_message_monitor/terms':
        await fulfill(route, { items: terms, page: 1, page_size: 20, total: terms.length })
        return
      case 'POST /api/plugins/forbidden_message_monitor/text-trials': {
        expect(request.postDataJSON()).toEqual({ text: '加微信领取资料' })
        await fulfill(route, {
          id: 17,
          version: 1,
          data: {
            decision: '违规', stage: 'weighted_score', risk_band: 'high', local_score: 85,
            reason: '命中引流词', violations: ['微信'], llm_used: false, suggested_action: 'block',
          },
        })
        return
      }
      case 'POST /api/plugins/forbidden_message_monitor/training-samples':
        expect(request.postDataJSON()).toEqual({ msg_content: '加微信领取资料', trial_id: '17' })
        await fulfill(route, { id: 3, version: 1, data: { msg_content: '加微信领取资料', keywords: '["微信"]', created_at: '2026-07-26T12:00:00Z' } })
        return
      case 'POST /api/plugins/forbidden_message_monitor/terms': {
        expect(request.postDataJSON()).toEqual({ kind: 'risk', text: '加微信', weight: 25 })
        terms = [{ id: 5, kind: 'risk', text: '加微信', weight: 25, version: 1, updated_at: '2026-07-26T12:00:00Z' }]
        await fulfill(route, terms[0])
        return
      }
      default:
        throw new Error(`未处理违禁监控 API：${key}`)
    }
  })
}

test('违禁监控专属页面可试判、投喂并新增词条', async ({ page }) => {
  await seedSession(page)
  await mockForbiddenMonitor(page)
  await page.goto('/plugin-pages/forbidden_message_monitor')

  await page.getByText('文本试判', { exact: true }).click()
  await page.locator('textarea').fill('加微信领取资料')
  await page.getByRole('button', { name: '开始试判' }).click()
  await expect(page.getByText('命中引流词')).toBeVisible()
  await page.getByRole('button', { name: '保存为违规样本' }).click()
  await page.getByRole('button', { name: 'Confirm' }).click()
  await expect(page.getByText('训练样本已保存')).toBeVisible()

  await page.getByText('词条', { exact: true }).click()
  await page.getByRole('button', { name: '新增词条' }).click()
  await page.getByPlaceholder('词条文本').fill('加微信')
  await page.locator('.n-input-number input').fill('25')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('词条已新增')).toBeVisible()
  await expect(page.getByRole('cell', { name: '加微信' })).toBeVisible()
})
