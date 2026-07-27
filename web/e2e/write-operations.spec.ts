// 📌 影响范围：拦截系统设置写接口并维护内存测试状态。
import { expect, test, type Page, type Route } from '@playwright/test'

async function fulfill(route: Route, data: unknown): Promise<void> {
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 'ok', message: '成功', data }) })
}

async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => sessionStorage.setItem('w1ndys_bot_token', 'e2e-token'))
}

test('系统设置可覆盖并恢复默认', async ({ page }) => {
  let setting = { key: 'command_prefix', value: '/', description: '命令前缀', overridden: false }
  await seedSession(page)
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    expect(request.headers().authorization).toBe('Bearer e2e-token')
    const operation = `${request.method()} ${new URL(request.url()).pathname}`
    if (operation === 'GET /api/settings') await fulfill(route, [setting])
    else if (operation === 'PUT /api/settings/command_prefix') {
      expect(request.postDataJSON()).toEqual({ value: '!' })
      setting = { ...setting, value: '!', overridden: true }
      await fulfill(route, setting)
    } else if (operation === 'DELETE /api/settings/command_prefix') {
      setting = { ...setting, value: '/', overridden: false }
      await fulfill(route, null)
    } else throw new Error(`未处理设置 API：${operation}`)
  })

  await page.goto('/settings')
  const row = page.locator('.setting-row').filter({ hasText: 'command_prefix' })
  await row.locator('input').fill('!')
  await row.getByRole('button', { name: '保存并热更新' }).click()
  await expect(page.getByText('系统设置已保存并热更新')).toBeVisible()
  await expect(row.getByText('数据库覆盖').first()).toBeVisible()
  await row.getByRole('button', { name: '恢复默认' }).click()
  await page.getByRole('button', { name: '确认恢复' }).click()
  await expect(row.getByText('程序默认').first()).toBeVisible()
  await expect(row.locator('input')).toHaveValue('/')
})
