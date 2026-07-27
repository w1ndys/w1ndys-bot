// 📌 影响范围：拦截浏览器目标插件开关接口并维护内存测试状态；不访问真实数据库、NapCat或生产凭据。
import { expect, test, type Page, type Route } from '@playwright/test'

interface RuntimeGroup {
  group_id: number
  enabled: boolean
  version: number
  updated_at: string
}

interface RuntimeState {
  plugin_key: string
  display_name: string
  description: string
  admin_page_key: string
  desired_enabled: boolean
  version: number
  updated_at: string
  status: string
  in_flight: number
  last_error: string
  has_config: boolean
  commands: { key: string; display_name: string; description: string; triggers: string[]; scope: string; allowed_roles: string[] }[]
  groups: RuntimeGroup[]
}

// buildState 构造一个默认关闭的目标插件运行状态。
// @param 无。
// @returns 与后端 RuntimeStateView 字段一致的测试状态。
// ⚠️副作用说明：无。
function buildState(): RuntimeState {
  const result: RuntimeState = {
    plugin_key: 'echo',
    display_name: 'Echo 回声',
    description: '回复命令后携带的文本',
    admin_page_key: '',
    desired_enabled: false,
    version: 1,
    updated_at: '2026-07-26T12:00:00Z',
    status: 'disabled',
    in_flight: 0,
    last_error: '',
    has_config: false,
    commands: [{ key: 'echo', display_name: '回声', description: '引用回复输入参数', triggers: ['echo', '回声'], scope: 'group', allowed_roles: ['group_admin', 'group_member', 'group_owner', 'super_admin'] }],
    groups: [],
  }

  // >>> 数据演变示例
  // 1. 初始 -> desired=false,status=disabled,groups=[]。
  // 2. 启用后 -> desired=true,status=ready。
  return result
}

// fulfill 返回统一WebAPI测试信封。
// @param route：被拦截请求；data：响应业务数据。
// @returns Promise，在响应完成后结束。
// ⚠️副作用说明：结束一次浏览器网络请求。
async function fulfill(route: Route, data: unknown): Promise<void> {
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 'ok', message: '成功', data }) })

  // >>> 数据演变示例
  // 1. data=运行状态 -> JSON信封 -> 页面渲染卡片。
  // 2. data=[] -> JSON信封 -> 页面显示空状态。
}

// fulfillConflict 返回乐观锁冲突响应。
// @param route：被拦截请求。
// @returns Promise，在响应完成后结束。
// ⚠️副作用说明：结束一次浏览器网络请求。
async function fulfillConflict(route: Route): Promise<void> {
  await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'plugin_runtime_conflict', message: '插件开关已被其他操作更新', data: null }) })

  // >>> 数据演变示例
  // 1. 陈旧版本 -> 409 -> 页面提示并重载。
  // 2. 正常版本 -> 不走此分支。
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

// mockRuntimeWrites 模拟运行状态查询、全局开关与群开关写入。
// @param page：Playwright页面；options.failGlobal 为 true 时全局写入返回冲突。
// @returns Promise，在状态化路由注册后结束。
// ⚠️副作用说明：拦截管理API并修改函数内运行状态。
async function mockRuntimeWrites(page: Page, options: { failGlobal?: boolean } = {}): Promise<void> {
  let state = buildState()
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    expect(request.headers().authorization).toBe('Bearer e2e-token')
    const path = new URL(request.url()).pathname
    const method = request.method()
    switch (`${method} ${path}`) {
      case 'GET /api/plugin-runtimes':
        await fulfill(route, [state])
        break
      case 'PATCH /api/plugin-runtimes/echo': {
        const input = request.postDataJSON()
        // [决策理由] 全局写入必须携带读取到的权威版本，缺失会绕过乐观锁。
        expect(input).toEqual({ enabled: true, expected_version: 1 })
        if (options.failGlobal === true) {
          await fulfillConflict(route)
          break
        }
        state = { ...state, desired_enabled: true, version: 2, status: 'ready' }
        await fulfill(route, state)
        break
      }
      case 'PUT /api/plugin-runtimes/echo/groups/100': {
        const input = request.postDataJSON()
        // [决策理由] 新增群记录必须提交版本 0，否则会被后端当作陈旧更新拒绝。
        expect(input).toEqual({ enabled: true, expected_version: 0 })
        state = { ...state, groups: [{ group_id: 100, enabled: true, version: 1, updated_at: '2026-07-26T12:05:00Z' }] }
        await fulfill(route, state)
        break
      }
      default:
        throw new Error(`未处理运行状态API：${method} ${path}`)
    }

    // >>> 数据演变示例
    // 1. PATCH enabled=true,v1 -> 返回 v2/ready -> 页面显示运行中。
    // 2. PUT group100,v0 -> 新增群记录 -> 表格出现该群。
  })

  // >>> 数据演变示例
  // 1. 初始 state=disabled -> 启用 -> state=ready。
  // 2. failGlobal=true -> PATCH 返回 409 -> 页面保持停用。
}

// testEnablePluginAndGroup 验证全局开关与群开关写入链路。
// @param page：Playwright注入页面。
// @returns Promise，在群开关生效后结束。
// ⚠️副作用说明：操作测试页面并修改mock运行状态。
async function testEnablePluginAndGroup({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockRuntimeWrites(page)
  await page.goto('/plugin-runtimes')
  await expect(page.getByText('Echo 回声')).toBeVisible()
  // 代码持有的触发词与身份必须只读展示。
  await expect(page.getByText('echo、回声')).toBeVisible()
  await expect(page.getByText('群管理员、群成员、群主、超级管理员')).toBeVisible()
  await expect(page.getByText('实际：已停用')).toBeVisible()

  await page.locator('.n-switch').first().click()
  await expect(page.getByText('Echo 回声 已启用')).toBeVisible()
  await expect(page.getByText('实际：运行中')).toBeVisible()

  await page.getByPlaceholder('手工输入 QQ 群号').fill('100')
  await page.getByRole('button', { name: '新增并开启' }).click()
  await expect(page.getByText('群 100 已开启')).toBeVisible()
  // 全局运行中且群开启时最终状态必须为生效中。
  await expect(page.getByText('生效中')).toBeVisible()

  // >>> 数据演变示例
  // 1. 切换全局开关 -> PATCH v1 -> 显示运行中。
  // 2. 新增群100 -> PUT v0 -> 表格显示生效中。
}

// testVersionConflictKeepsState 验证乐观锁冲突提示并保留权威状态。
// @param page：Playwright注入页面。
// @returns Promise，在冲突提示可见后结束。
// ⚠️副作用说明：操作测试页面并触发mock冲突响应。
async function testVersionConflictKeepsState({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockRuntimeWrites(page, { failGlobal: true })
  await page.goto('/plugin-runtimes')
  await page.locator('.n-switch').first().click()
  await expect(page.getByText('插件开关已被其他操作更新')).toBeVisible()
  // 冲突后必须回到后端权威状态，不能停留在乐观的本地值。
  await expect(page.getByText('意图：停用')).toBeVisible()
  await expect(page.getByText('实际：已停用')).toBeVisible()

  // >>> 数据演变示例
  // 1. 陈旧版本提交 -> 409 -> 提示并重载为停用。
  // 2. 重载后 -> 开关回到关闭位置。
}

test('目标插件可启用并开启单群', testEnablePluginAndGroup)
test('乐观锁冲突后回到权威状态', testVersionConflictKeepsState)

// mockRuntimeConfig 模拟带小型配置的插件运行状态与配置读写。
// @param page：Playwright页面。
// @returns Promise，返回已发生的配置读取次数查询函数。
// ⚠️副作用说明：拦截管理API并修改函数内配置状态。
async function mockRuntimeConfig(page: Page): Promise<() => number> {
  const state = { ...buildState(), has_config: true, desired_enabled: true, version: 2, status: 'ready' }
  let config = { response_prefix: '' }
  let version = 1
  let configReads = 0
  const schema = { fields: [{ key: 'response_prefix', display_name: '回复前缀', description: '添加到每条回复之前的文本', type: 'string', required: false }] }
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    switch (`${method} ${path}`) {
      case 'GET /api/plugin-runtimes':
        await fulfill(route, [state])
        break
      case 'GET /api/plugin-runtimes/echo/config':
        configReads += 1
        await fulfill(route, { plugin_key: 'echo', schema, config, version, updated_at: '2026-07-26T12:00:00Z' })
        break
      case 'PUT /api/plugin-runtimes/echo/config': {
        const input = request.postDataJSON()
        // [决策理由] 保存必须携带读取到的权威版本，缺失会绕过乐观锁。
        expect(input).toEqual({ config: { response_prefix: '[bot] ' }, expected_version: 1 })
        config = { response_prefix: '[bot] ' }
        version = 2
        await fulfill(route, { plugin_key: 'echo', schema, config, version, updated_at: '2026-07-26T12:10:00Z' })
        break
      }
      default:
        throw new Error(`未处理配置API：${method} ${path}`)
    }

    // >>> 数据演变示例
    // 1. GET config -> 空前缀 v1 -> 表单可编辑。
    // 2. PUT config -> [bot] v2 -> 表单基线更新。
  })

  // >>> 数据演变示例
  // 1. 初始 config={response_prefix:""} -> 保存 -> "[bot] "。
  // 2. has_config=false 的插件 -> 不渲染配置表单。
  return () => configReads
}

// testConfigSaveAndHotApply 验证小型配置表单的保存链路。
// @param page：Playwright注入页面。
// @returns Promise，在保存提示可见后结束。
// ⚠️副作用说明：操作测试页面并修改mock配置状态。
async function testConfigSaveAndHotApply({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  const getConfigReads = await mockRuntimeConfig(page)
  await page.goto('/plugin-runtimes')
  await expect(page.getByRole('button', { name: '打开插件配置' })).toBeVisible()
  await expect(page.locator('.n-form-item').filter({ hasText: '回复前缀' })).toHaveCount(0)
  expect(getConfigReads()).toBe(0)
  await page.getByRole('button', { name: '打开插件配置' }).click()
  await expect(page).toHaveURL(/\/plugin-runtimes\/echo\/config$/)
  // Naive UI 的表单标签不是原生 label，按表单项定位其输入框。
  const prefixInput = page.locator('.n-form-item').filter({ hasText: '回复前缀' }).locator('input')
  await expect(prefixInput).toBeVisible()
  // 未修改时保存按钮必须保持禁用，避免无意义写入与审计噪音。
  await expect(page.getByRole('button', { name: '保存并热应用' })).toBeDisabled()
  await prefixInput.fill('[bot] ')
  await page.getByRole('button', { name: '保存并热应用' }).click()
  await expect(page.getByText('插件配置已保存并热应用')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存并热应用' })).toBeDisabled()

  // >>> 数据演变示例
  // 1. 填写 [bot] -> PUT v1 -> v2 且基线重置。
  // 2. 保存后未再修改 -> 按钮回到禁用。
}

test('小型配置可保存并热应用', testConfigSaveAndHotApply)

test('未知插件配置深链安全显示错误', async ({ page }) => {
  await seedSession(page)
  await page.route('**/api/plugin-runtimes/not_registered/config', async (route) => {
    expect(route.request().headers().authorization).toBe('Bearer e2e-token')
    await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ code: 'plugin_not_found', message: '插件不存在', data: null }) })
  })
  await page.goto('/plugin-runtimes/not_registered/config')
  await expect(page.getByText('插件不存在')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存并热应用' })).toHaveCount(0)
})
