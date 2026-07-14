// 📌 影响范围：拦截浏览器管理写接口并维护内存测试状态；不访问真实数据库、NapCat或生产凭据。
import { expect, test, type Page, type Route } from '@playwright/test'

const plugin = { name: 'ping', display_name: 'Ping', description: '连通性测试', available: true, enabled: true, priority: 100, config: {} }
const feature = { plugin_name: 'ping', key: 'ping', display_name: '连通性测试', description: '回复延迟', available: true, default_commands: ['ping'], default_permissions: { member: true } }

// fulfill 返回统一WebAPI测试信封。
// @param route：被拦截请求；data：响应业务数据。
// @returns Promise，在响应完成后结束。
// ⚠️副作用说明：结束一次浏览器网络请求。
async function fulfill(route: Route, data: unknown): Promise<void> {
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 'ok', message: '成功', data }) })

  // >>> 数据演变示例
  // 1. data=新命令 -> JSON信封 -> 页面新增表格行。
  // 2. data=null -> JSON信封 -> 页面确认删除成功。
}

// seedSession 在应用初始化前写入隔离测试Token。
// @param page：Playwright页面。
// @returns Promise，在初始化脚本注册后结束。
// ⚠️副作用说明：仅写测试浏览器sessionStorage。
async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => sessionStorage.setItem('w1ndys_bot_token', 'e2e-token'))

  // >>> 数据演变示例
  // 1. 新上下文 -> 写e2e-token -> 路由允许进入管理页。
  // 2. 测试结束 -> 上下文销毁 -> Token同步消失。
}

// assertAuth 验证管理请求没有遗漏Bearer会话。
// @param route：当前管理API路由。
// @returns 当前请求供后续方法和请求体断言。
// ⚠️副作用说明：断言失败会终止当前测试。
function assertAuth(route: Route) {
  const request = route.request()
  expect(request.headers().authorization).toBe('Bearer e2e-token')

  // >>> 数据演变示例
  // 1. Authorization=Bearer e2e-token -> 断言通过 -> 返回request。
  // 2. 缺少Authorization -> 断言失败 -> 测试拒绝误通过。
  return request
}

// mockCommandWrites 模拟命令查询、新增和删除事务。
// @param page：Playwright页面。
// @returns Promise，在状态化路由注册后结束。
// ⚠️副作用说明：拦截管理API并修改函数内命令数组。
async function mockCommandWrites(page: Page): Promise<void> {
  let commands: unknown[] = []
  await page.route('**/api/**', async (route) => {
    const request = assertAuth(route)
    const path = new URL(request.url()).pathname
    const method = request.method()
    switch (`${method} ${path}`) {
      case 'GET /api/plugins':
        await fulfill(route, [plugin])
        break
      case 'GET /api/plugins/ping/features':
        await fulfill(route, [feature])
        break
      case 'GET /api/commands':
        await fulfill(route, commands)
        break
      case 'POST /api/commands': {
        const input = request.postDataJSON()
        expect(input).toEqual({ scope_type: 'global', scope_id: '0', plugin_name: 'ping', feature_key: 'ping', command: '测试延迟' })
        const created = { id: 21, ...input, normalized_command: '测试延迟', enabled: true }
        commands = [created]
        await fulfill(route, created)
        break
      }
      case 'DELETE /api/commands/21':
        commands = []
        await fulfill(route, null)
        break
      default:
        throw new Error(`未处理命令API：${method} ${path}`)
    }

    // >>> 数据演变示例
    // 1. POST命令 -> 校验精确body -> 内存新增id21 -> 返回权威状态。
    // 2. DELETE id21 -> 内存清空 -> 返回null。
  })

  // >>> 数据演变示例
  // 1. 初始commands=[] -> 注册路由 -> 页面显示空列表。
  // 2. 页面完成增删 -> commands再次=[] -> 真实数据库不变。
}

// mockPermissionWrites 模拟权限查询、保存和回退删除。
// @param page：Playwright页面。
// @returns Promise，在状态化路由注册后结束。
// ⚠️副作用说明：拦截管理API并修改函数内权限数组。
async function mockPermissionWrites(page: Page): Promise<void> {
  let permissions: unknown[] = []
  await page.route('**/api/**', async (route) => {
    const request = assertAuth(route)
    const path = new URL(request.url()).pathname
    const method = request.method()
    switch (`${method} ${path}`) {
      case 'GET /api/plugins':
        await fulfill(route, [plugin])
        break
      case 'GET /api/plugins/ping/features':
        await fulfill(route, [feature])
        break
      case 'GET /api/permissions':
        await fulfill(route, permissions)
        break
      case 'POST /api/permissions': {
        const input = request.postDataJSON()
        expect(input).toEqual({ scope_type: 'global', scope_id: '0', plugin_name: 'ping', feature_key: '', subject_type: 'role', subject_id: 'group_admin', effect: 'allow' })
        const saved = { id: 31, ...input }
        permissions = [saved]
        await fulfill(route, saved)
        break
      }
      case 'DELETE /api/permissions/31':
        permissions = []
        await fulfill(route, null)
        break
      default:
        throw new Error(`未处理权限API：${method} ${path}`)
    }

    // >>> 数据演变示例
    // 1. POST默认角色规则 -> 精确校验 -> 返回id31并展示。
    // 2. DELETE id31 -> 清空内存策略 -> 页面回退为空列表。
  })

  // >>> 数据演变示例
  // 1. 初始permissions=[] -> 保存allow规则 -> permissions=[id31]。
  // 2. 删除并回退 -> permissions=[] -> 真实权限快照不变。
}

// mockSettingWrites 模拟设置查询、覆盖保存和恢复默认。
// @param page：Playwright页面。
// @returns Promise，在状态化路由注册后结束。
// ⚠️副作用说明：拦截管理API并修改函数内设置状态。
async function mockSettingWrites(page: Page): Promise<void> {
  let setting = { key: 'command_prefix', value: '/', description: '命令前缀', overridden: false }
  await page.route('**/api/**', async (route) => {
    const request = assertAuth(route)
    const path = new URL(request.url()).pathname
    const method = request.method()
    switch (`${method} ${path}`) {
      case 'GET /api/plugins':
        await fulfill(route, [plugin])
        break
      case 'GET /api/settings':
        await fulfill(route, [setting])
        break
      case 'PUT /api/settings/command_prefix':
        expect(request.postDataJSON()).toEqual({ value: '!' })
        setting = { ...setting, value: '!', overridden: true }
        await fulfill(route, setting)
        break
      case 'DELETE /api/settings/command_prefix':
        setting = { ...setting, value: '/', overridden: false }
        await fulfill(route, null)
        break
      default:
        throw new Error(`未处理设置API：${method} ${path}`)
    }

    // >>> 数据演变示例
    // 1. PUT value=! -> 校验body -> overridden=true并返回。
    // 2. DELETE覆盖 -> 恢复默认/ -> 下一次GET返回默认状态。
  })

  // >>> 数据演变示例
  // 1. 初始prefix=/ -> 保存! -> 数据库覆盖样式。
  // 2. 确认恢复 -> DELETE后GET -> prefix=/且程序默认。
}

// testCommandCreateAndDelete 验证命令写入和危险删除确认链路。
// @param page：Playwright注入页面。
// @returns Promise，在命令恢复空列表后结束。
// ⚠️副作用说明：操作测试页面并修改mock命令状态。
async function testCommandCreateAndDelete({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockCommandWrites(page)
  await page.goto('/plugins/ping/commands')
  await page.getByPlaceholder('例如：ping 或 测试').fill('测试延迟')
  await page.getByRole('button', { name: '添加触发词' }).click()
  await expect(page.locator('tbody input')).toHaveValue('测试延迟')
  await page.getByRole('button', { name: '删除' }).click()
  await expect(page.getByText('删除后机器人将立即停止匹配这条命令')).toBeVisible()
  await page.getByRole('button', { name: '确认删除' }).click()
  await expect(page.getByText('当前插件还没有可管理的触发词')).toBeVisible()

  // >>> 数据演变示例
  // 1. 输入测试延迟 -> POST id21 -> 表格显示新命令。
  // 2. 删除并确认 -> DELETE id21 -> 恢复空状态。
}

// testPermissionSaveAndFallback 验证插件全功能角色权限保存和删除回退。
// @param page：Playwright注入页面。
// @returns Promise，在权限恢复空列表后结束。
// ⚠️副作用说明：操作测试页面并修改mock权限状态。
async function testPermissionSaveAndFallback({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockPermissionWrites(page)
  await page.goto('/plugins/ping/permissions')
  await page.getByRole('button', { name: '保存并热更新' }).click()
  await expect(page.getByText('角色：group_admin')).toBeVisible()
  await page.getByRole('button', { name: '删除并回退' }).first().click()
  await expect(page.getByText('删除后会立即恢复到下一层规则')).toBeVisible()
  await page.getByRole('button', { name: '删除并回退' }).last().click()
  await expect(page.getByText('没有符合条件的显式权限策略')).toBeVisible()

  // >>> 数据演变示例
  // 1. 默认全局群管理员allow -> POST id31 -> 表格显示角色规则。
  // 2. 确认删除并回退 -> DELETE id31 -> 空策略状态。
}

// testSettingOverrideAndRestore 验证设置覆盖、热更新标记和恢复默认。
// @param page：Playwright注入页面。
// @returns Promise，在设置恢复程序默认后结束。
// ⚠️副作用说明：操作测试页面并修改mock设置状态。
async function testSettingOverrideAndRestore({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockSettingWrites(page)
  await page.goto('/settings')
  const row = page.locator('.setting-row').filter({ hasText: 'command_prefix' })
  await row.locator('input').fill('!')
  await row.getByRole('button', { name: '保存并热更新' }).click()
  await expect(row.getByText('数据库覆盖').first()).toBeVisible()
  await row.getByRole('button', { name: '恢复默认' }).click()
  await page.getByRole('button', { name: '确认恢复' }).click()
  await expect(row.getByText('程序默认').first()).toBeVisible()
  await expect(row.locator('input')).toHaveValue('/')

  // >>> 数据演变示例
  // 1. prefix从/改! -> PUT -> 标记数据库覆盖。
  // 2. 确认恢复 -> DELETE+GET -> 输入恢复/且标记程序默认。
}

test('命令新增后可确认删除', testCommandCreateAndDelete)
test('权限保存后可删除回退', testPermissionSaveAndFallback)
test('系统设置可覆盖并恢复默认', testSettingOverrideAndRestore)
