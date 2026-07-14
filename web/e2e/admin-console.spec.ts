// 📌 影响范围：拦截浏览器 /api 请求；读写测试浏览器 sessionStorage；不访问真实数据库或 NapCat。
import { expect, test, type Page, type Route } from '@playwright/test'

const plugins = [
  { name: 'ping', display_name: 'Ping', description: '连通性测试', version: '1.0.0', available: true, enabled: true, priority: 100, config: {} },
]
const features = [
  { plugin_name: 'ping', key: 'ping', display_name: '连通性测试', description: '回复延迟', available: true, default_commands: ['ping'], default_permissions: { member: true } },
]
const auditSummary = { id: 8, actor_id: '2769731875', actor_role: 'super_admin', channel: 'webui', action: 'plugin.enable', target_type: 'plugin', target_id: 'ping', success: true, error_message: '', request_id: 'req-8', created_at: '2026-07-13T02:00:00Z' }

// fulfillJSON 返回统一code/message/data测试响应。
// @param route：Playwright拦截路由；data：响应业务数据；status：HTTP状态码。
// @returns Promise，在响应写回浏览器后结束。
// ⚠️副作用说明：结束一次被拦截的浏览器网络请求。
async function fulfillJSON(route: Route, data: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ code: status < 400 ? 'ok' : 'error', message: status < 400 ? '成功' : '凭据无效', data }) })

  // >>> 数据演变示例
  // 1. data=[ping]+status200 -> 统一成功信封 -> 浏览器收到插件列表。
  // 2. data=null+status401 -> 统一失败信封 -> 登录页显示凭据无效。
}

// mockManagementAPI 为管理控制台提供稳定且无数据库副作用的后端响应。
// @param page：当前Playwright页面。
// @returns Promise，在API拦截器注册完成后结束。
// ⚠️副作用说明：拦截该页面全部 /api 请求。
async function mockManagementAPI(page: Page): Promise<void> {
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    // [决策理由] 登录契约必须验证POST方法和字段名，防止错误请求被宽松Mock掩盖。
    if (path === '/api/auth/login') {
      expect(request.method()).toBe('POST')
      expect(request.postDataJSON()).toEqual({ qq: '2769731875', password: 'test-password' })
    } else {
      // [决策理由] 当前只读用例必须使用GET并携带会话Token，避免漏鉴权仍误通过。
      expect(request.method()).toBe('GET')
      expect(request.headers().authorization).toBe('Bearer e2e-token')
    }
    switch (path) {
      case '/api/auth/login':
        await fulfillJSON(route, { token: 'e2e-token', expires_in: 3600 })
        break
      case '/api/plugins':
        await fulfillJSON(route, plugins)
        break
      case '/api/plugins/ping/features':
        await fulfillJSON(route, features)
        break
      case '/api/commands':
      case '/api/permissions':
      case '/api/settings':
        await fulfillJSON(route, [])
        break
      case '/api/audit-logs':
        await fulfillJSON(route, { items: [auditSummary], page: 1, page_size: 20, total: 1 })
        break
      case '/api/audit-logs/8':
        await fulfillJSON(route, { ...auditSummary, before: { enabled: false, api_key: '[已脱敏]' }, after: { enabled: true, api_key: '[已脱敏]' } })
        break
      default:
        await fulfillJSON(route, null, 404)
    }

    // >>> 数据演变示例
    // 1. GET features+Bearer Token -> 契约断言通过 -> 返回ping功能。
    // 2. login字段错误或漏Token -> 契约断言失败 -> 测试不会误通过。
  })

  // >>> 数据演变示例
  // 1. 新页面 -> 注册/api拦截 -> 后续管理请求使用mock。
  // 2. 页面关闭 -> Playwright释放路由 -> 不影响真实服务。
}

// seedSession 在应用启动前写入测试会话。
// @param page：当前Playwright页面。
// @returns Promise，在初始化脚本注册后结束。
// ⚠️副作用说明：仅修改测试浏览器上下文的sessionStorage。
async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => sessionStorage.setItem('w1ndys_bot_token', 'e2e-token'))

  // >>> 数据演变示例
  // 1. 空测试上下文 -> 初始化脚本 -> 首次加载即为已登录。
  // 2. 新测试上下文 -> 独立sessionStorage -> 不读取真实Token。
}

// mockFailedLogin 模拟严格校验后的错误凭据响应。
// @param page：当前Playwright页面。
// @returns Promise，在登录拦截器注册完成后结束。
// ⚠️副作用说明：拦截该页面的登录请求并固定返回401。
async function mockFailedLogin(page: Page): Promise<void> {
  await page.route('**/api/auth/login', async (route) => {
    const request = route.request()
    expect(request.method()).toBe('POST')
    expect(request.postDataJSON()).toEqual({ qq: '2769731875', password: 'wrong-password' })
    await fulfillJSON(route, null, 401)

    // >>> 数据演变示例
    // 1. wrong-password+正确字段 -> 契约断言通过 -> 返回401。
    // 2. 前端字段名错误 -> 精确JSON断言失败 -> 用例失败。
  })

  // >>> 数据演变示例
  // 1. 登录页提交错误密码 -> 拦截命中 -> 显示凭据无效。
  // 2. 未提交登录 -> 无网络请求 -> 真实服务不受影响。
}

// mockExpiredSession 模拟已过期Token访问受保护资源。
// @param page：当前Playwright页面。
// @returns Promise，在插件接口401拦截器注册后结束。
// ⚠️副作用说明：拦截插件列表请求并固定返回401。
async function mockExpiredSession(page: Page): Promise<void> {
  await page.route('**/api/plugins', async (route) => {
    const request = route.request()
    expect(request.method()).toBe('GET')
    expect(request.headers().authorization).toBe('Bearer e2e-token')
    await fulfillJSON(route, null, 401)

    // >>> 数据演变示例
    // 1. GET plugins+旧Token -> 契约断言通过 -> 返回401。
    // 2. 请求漏Token -> 断言失败 -> 会话测试不会误通过。
  })

  // >>> 数据演变示例
  // 1. 已登录页面加载菜单 -> plugins返回401 -> Token被清理。
  // 2. 未访问plugins -> 拦截保持空闲 -> 不修改浏览器会话。
}

// testLoginAndNavigation 验证登录表单、会话写入和插件首页导航。
// @param page：Playwright注入的浏览器页面。
// @returns Promise，在断言完成后结束。
// ⚠️副作用说明：操作测试页面并调用mock登录接口。
async function testLoginAndNavigation({ page }: { page: Page }): Promise<void> {
  await mockManagementAPI(page)
  await page.goto('/login')
  await page.getByPlaceholder('请输入 QQ 号').fill('2769731875')
  await page.getByPlaceholder('请输入管理密码').fill('test-password')
  await page.getByRole('button', { name: '进入管理中心' }).click()
  await expect(page).toHaveURL(/\/plugins$/)
  await expect(page.getByRole('heading', { name: '插件管理' })).toBeVisible()
  await expect(page.evaluate(() => sessionStorage.getItem('w1ndys_bot_token'))).resolves.toBe('e2e-token')

  // >>> 数据演变示例
  // 1. 登录表单+mock成功 -> Token写入 -> 跳转/plugins。
  // 2. 插件API返回ping -> 菜单构建 -> 插件管理可见。
}

// testFailedLogin 验证错误密码不会建立会话或离开登录页。
// @param page：Playwright注入的浏览器页面。
// @returns Promise，在错误提示与会话断言完成后结束。
// ⚠️副作用说明：操作测试登录表单并调用mock失败接口。
async function testFailedLogin({ page }: { page: Page }): Promise<void> {
  await mockFailedLogin(page)
  await page.goto('/login')
  await page.getByPlaceholder('请输入 QQ 号').fill('2769731875')
  await page.getByPlaceholder('请输入管理密码').fill('wrong-password')
  await page.getByRole('button', { name: '进入管理中心' }).click()
  await expect(page.getByText('凭据无效')).toBeVisible()
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.evaluate(() => sessionStorage.getItem('w1ndys_bot_token'))).resolves.toBeNull()

  // >>> 数据演变示例
  // 1. 错误密码 -> POST返回401 -> 显示凭据无效且停留/login。
  // 2. 401无Token数据 -> sessionStorage保持空 -> 未建立会话。
}

// testExpiredSessionRedirect 验证API 401会清理Token并保留原页面重定向目标。
// @param page：Playwright注入的浏览器页面。
// @returns Promise，在登录重定向与Token清理断言后结束。
// ⚠️副作用说明：写入测试Token、访问受保护页并调用mock 401接口。
async function testExpiredSessionRedirect({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockExpiredSession(page)
  await page.goto('/plugins')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/plugins')
  await expect(page.getByRole('heading', { name: '登录控制台' })).toBeVisible()
  await expect(page.evaluate(() => sessionStorage.getItem('w1ndys_bot_token'))).resolves.toBeNull()

  // >>> 数据演变示例
  // 1. /plugins+过期Token -> API 401 -> 清Token并转/login?redirect=/plugins。
  // 2. 登录页渲染 -> 会话为空 -> 不再请求插件菜单。
}

// testPluginFeatureInitialization 验证固定插件命令页首次加载功能且不误报空功能。
// @param page：Playwright注入的浏览器页面。
// @returns Promise，在功能选择和提示断言完成后结束。
// ⚠️副作用说明：操作测试页面并调用mock插件、命令及功能接口。
async function testPluginFeatureInitialization({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockManagementAPI(page)
  await page.goto('/plugins/ping/commands')
  await expect(page.getByRole('heading', { name: '命令管理' })).toBeVisible()
  await expect(page.getByText('插件管理 / ping / 命令')).toBeVisible()
  await expect(page.getByText('当前插件没有可用功能')).toHaveCount(0)
  await expect(page.getByText('连通性测试 · ping')).toBeVisible()

  // >>> 数据演变示例
  // 1. 固定ping路由 -> 初次请求features -> 默认选中连通性测试。
  // 2. features非空 -> featureLoaded=true -> 空功能警告不存在。
}

// testAuditListAndDetail 验证审计列表、标识掩码与只读详情抽屉。
// @param page：Playwright注入的浏览器页面。
// @returns Promise，在审计详情断言完成后结束。
// ⚠️副作用说明：操作测试页面并调用mock审计接口。
async function testAuditListAndDetail({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockManagementAPI(page)
  await page.goto('/audit-logs')
  await expect(page.getByRole('heading', { name: '审计日志' })).toBeVisible()
  await expect(page.getByText('27******75')).toBeVisible()
  await page.getByRole('button', { name: '查看详情' }).click()
  await expect(page.getByText('审计详情')).toBeVisible()
  await expect(page.locator('pre').filter({ hasText: '[已脱敏]' }).first()).toBeVisible()
  await expect(page.getByRole('dialog').getByText('req-8')).toBeVisible()

  // >>> 数据演变示例
  // 1. 审计摘要含完整QQ -> UI掩码 -> 27******75。
  // 2. 点击id8 -> 加载详情 -> 展示已脱敏前后快照与请求ID。
}

// testResponsiveNavigation 验证桌面侧栏与移动抽屉使用同一审计入口。
// @param page：Playwright注入的浏览器页面。
// @returns Promise，在响应式导航到审计页后结束。
// ⚠️副作用说明：根据测试视口点击侧栏或抽屉菜单并改变页面路由。
async function testResponsiveNavigation({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockManagementAPI(page)
  await page.goto('/plugins')
  const viewport = page.viewportSize()
  // [决策理由] 与styles.css保持一致，1023px及以下的手机和平板均使用移动抽屉。
  if (viewport !== null && viewport.width <= 1023) {
    await expect(page.getByRole('button', { name: '功能菜单' })).toBeVisible()
    await expect(page.locator('.desktop-sider')).toBeHidden()
    await page.getByRole('button', { name: '功能菜单' }).click()
    const drawer = page.locator('#mobile-admin-menu')
    await expect(drawer).toBeVisible()
    await drawer.getByText('审计日志', { exact: true }).click()
  } else {
    await expect(page.getByRole('button', { name: '功能菜单' })).toBeHidden()
    const sider = page.locator('.desktop-sider')
    await expect(sider).toBeVisible()
    await sider.getByText('审计日志', { exact: true }).click()
  }
  await expect(page).toHaveURL(/\/audit-logs$/)
  await expect(page.getByRole('heading', { name: '审计日志' })).toBeVisible()

  // >>> 数据演变示例
  // 1. Pixel5宽度393或iPad宽度768 -> 打开功能菜单抽屉 -> 点击审计日志。
  // 2. Desktop宽度1280 -> 使用常驻侧栏 -> 点击审计日志。
}

test('登录后进入插件管理', testLoginAndNavigation)
test('错误密码保持未登录状态', testFailedLogin)
test('会话失效后返回登录页', testExpiredSessionRedirect)
test('命令页首次加载插件功能', testPluginFeatureInitialization)
test('审计列表和详情保持只读脱敏', testAuditListAndDetail)
test('桌面侧栏与移动抽屉导航', testResponsiveNavigation)
