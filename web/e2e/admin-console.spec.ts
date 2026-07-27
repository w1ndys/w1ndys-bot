// 📌 影响范围：拦截浏览器管理 API；验证目标插件架构导航和会话。
import { expect, test, type Page, type Route } from '@playwright/test'

const runtime = {
  plugin_key: 'ping', display_name: 'Ping', description: '连通性测试', admin_page_key: '', desired_enabled: true,
  version: 1, updated_at: '2026-07-13T02:00:00Z', status: 'ready', in_flight: 0, last_error: '', has_config: false, commands: [], groups: [],
}
const auditSummary = { id: 8, actor_id: '2769731875', actor_role: 'super_admin', channel: 'webui', action: 'plugin.enable', target_type: 'plugin', target_id: 'ping', success: true, error_message: '', request_id: 'req-8', created_at: '2026-07-13T02:00:00Z' }

async function fulfillJSON(route: Route, data: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status < 400 ? 'ok' : 'error', message: status < 400 ? '成功' : '凭据无效', data }) })
}

async function mockManagementAPI(page: Page): Promise<void> {
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path === '/api/auth/login') {
      expect(request.method()).toBe('POST')
      expect(request.postDataJSON()).toEqual({ qq: '2769731875', password: 'test-password' })
      await fulfillJSON(route, { token: 'e2e-token', expires_in: 3600 })
      return
    }
    expect(request.headers().authorization).toBe('Bearer e2e-token')
    expect(request.method()).toBe('GET')
    if (path === '/api/plugin-runtimes') await fulfillJSON(route, [runtime])
    else if (path === '/api/plugin-runtimes/echo/config') await fulfillJSON(route, { plugin_key: 'echo', schema: { fields: [] }, config: {}, version: 1, updated_at: '2026-07-13T02:00:00Z' })
    else if (path === '/api/audit-logs') await fulfillJSON(route, { items: [auditSummary], page: 1, page_size: 20, total: 1 })
    else if (path === '/api/audit-logs/8') await fulfillJSON(route, { ...auditSummary, before: { api_key: '[已脱敏]' }, after: { enabled: true } })
    else await fulfillJSON(route, null, 404)
  })
}

async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => sessionStorage.setItem('w1ndys_bot_token', 'e2e-token'))
}

test('登录后进入插件运行页', async ({ page }) => {
  await mockManagementAPI(page)
  await page.goto('/login')
  await page.getByPlaceholder('请输入 QQ 号').fill('2769731875')
  await page.getByPlaceholder('请输入管理密码').fill('test-password')
  await page.getByRole('button', { name: '进入管理中心' }).click()
  await expect(page).toHaveURL(/\/plugin-runtimes$/)
  await expect(page.getByText('Ping')).toBeVisible()
})

test('插件配置深链登录后恢复并保持插件导航', async ({ page }) => {
  await mockManagementAPI(page)
  await page.goto('/plugin-runtimes/echo/config')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/plugin-runtimes/echo/config')
  await page.getByPlaceholder('请输入 QQ 号').fill('2769731875')
  await page.getByPlaceholder('请输入管理密码').fill('test-password')
  await page.getByRole('button', { name: '进入管理中心' }).click()
  await expect(page).toHaveURL(/\/plugin-runtimes\/echo\/config$/)
  await expect(page.getByText('插件配置', { exact: true })).toBeVisible()

  const viewport = page.viewportSize()
  if (viewport !== null && viewport.width <= 1023) {
    await page.getByRole('button', { name: '功能菜单' }).click()
    await expect(page.locator('#mobile-admin-menu .n-menu-item-content--selected')).toContainText('插件运行')
  } else {
    await expect(page.locator('.desktop-sider .n-menu-item-content--selected')).toContainText('插件运行')
  }
})

test('错误密码不建立会话', async ({ page }) => {
  await page.route('**/api/auth/login', async (route) => {
    expect(route.request().postDataJSON()).toEqual({ qq: '2769731875', password: 'wrong-password' })
    await fulfillJSON(route, null, 401)
  })
  await page.goto('/login')
  await page.getByPlaceholder('请输入 QQ 号').fill('2769731875')
  await page.getByPlaceholder('请输入管理密码').fill('wrong-password')
  await page.getByRole('button', { name: '进入管理中心' }).click()
  await expect(page.getByText('凭据无效')).toBeVisible()
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.evaluate(() => sessionStorage.getItem('w1ndys_bot_token'))).resolves.toBeNull()
})

test('会话失效后保留插件运行页返回目标', async ({ page }) => {
  await seedSession(page)
  await page.route('**/api/plugin-runtimes', async (route) => {
    expect(route.request().method()).toBe('GET')
    expect(route.request().headers().authorization).toBe('Bearer e2e-token')
    await fulfillJSON(route, null, 401)
  })
  await page.goto('/plugin-runtimes')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/plugin-runtimes')
  await expect(page.evaluate(() => sessionStorage.getItem('w1ndys_bot_token'))).resolves.toBeNull()
})

test('旧插件工作台路由回归插件运行页', async ({ page }) => {
  await seedSession(page)
  await mockManagementAPI(page)
  await page.goto('/plugins/ping/commands')
  await expect(page).toHaveURL(/\/plugin-runtimes$/)
  await expect(page.getByText('Ping')).toBeVisible()
})

test('审计列表和详情保持只读脱敏', async ({ page }) => {
  await seedSession(page)
  await mockManagementAPI(page)
  await page.goto('/audit-logs')
  await expect(page.getByText('27******75')).toBeVisible()
  await page.getByRole('button', { name: '查看详情' }).click()
  await expect(page.locator('pre').filter({ hasText: '[已脱敏]' }).first()).toBeVisible()
})

test('桌面侧栏与移动抽屉只展示目标入口', async ({ page }) => {
  await seedSession(page)
  await mockManagementAPI(page)
  await page.goto('/plugin-runtimes')
  await expect(page.getByText('插件管理', { exact: true })).toHaveCount(0)
  const viewport = page.viewportSize()
  if (viewport !== null && viewport.width <= 1023) {
    await page.getByRole('button', { name: '功能菜单' }).click()
    const drawer = page.locator('#mobile-admin-menu')
    await expect(drawer.getByText('插件运行', { exact: true })).toBeVisible()
    await drawer.getByText('审计日志', { exact: true }).click()
  } else {
    const sider = page.locator('.desktop-sider')
    await expect(sider.getByText('插件运行', { exact: true })).toBeVisible()
    await sider.getByText('审计日志', { exact: true }).click()
  }
  await expect(page).toHaveURL(/\/audit-logs$/)
})
