// 📌 影响范围：拦截浏览器关键词规则接口并维护内存测试状态；不访问真实数据库、NapCat或生产凭据。
import { expect, test, type Page, type Route } from '@playwright/test'

interface Rule {
  id: number
  group_id: number
  keyword: string
  reply_content: string
  enabled: boolean
  version: number
  updated_at: string
}

// fulfill 返回统一WebAPI测试信封。
// @param route：被拦截请求；data：响应业务数据。
// @returns Promise，在响应完成后结束。
// ⚠️副作用说明：结束一次浏览器网络请求。
async function fulfill(route: Route, data: unknown): Promise<void> {
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 'ok', message: '成功', data }) })

  // >>> 数据演变示例
  // 1. data=规则分页 -> 表格渲染。
  // 2. data={deleted:true} -> 页面移除该行。
}

// seedSession 在应用初始化前写入隔离测试Token。
// @param page：Playwright页面。
// @returns Promise，在初始化脚本注册后结束。
// ⚠️副作用说明：仅写测试浏览器sessionStorage。
async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => sessionStorage.setItem('w1ndys_bot_token', 'e2e-token'))

  // >>> 数据演变示例
  // 1. 新上下文 -> 写e2e-token -> 允许进入管理页。
  // 2. 上下文销毁 -> Token消失。
}

// mockKeywordRules 模拟按群隔离的规则查询与写入。
// @param page：Playwright页面。
// @returns Promise，在状态化路由注册后结束。
// ⚠️副作用说明：拦截管理API并修改函数内规则状态。
async function mockKeywordRules(page: Page): Promise<void> {
  let rules: Rule[] = []
  let nextID = 1
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    expect(request.headers().authorization).toBe('Bearer e2e-token')
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    if (path === '/api/plugins') {
      await fulfill(route, [])
      return
    }
    // 群作用域必须出现在路径里，避免请求体覆盖群号造成跨群写入。
    const listMatch = path.match(/^\/api\/plugins\/keyword_reply\/groups\/(\d+)\/rules$/)
    if (listMatch !== null && method === 'GET') {
      const groupID = Number(listMatch[1])
      const items = rules.filter((rule) => rule.group_id === groupID)
      await fulfill(route, { items, page: 1, page_size: 20, total: items.length })
      return
    }
    if (listMatch !== null && method === 'POST') {
      const groupID = Number(listMatch[1])
      const input = request.postDataJSON()
      expect(input).toEqual({ keyword: '你好', reply_content: '你好呀', enabled: true })
      rules = [...rules, { id: nextID++, group_id: groupID, ...input, version: 1, updated_at: '2026-07-26T12:00:00Z' }]
      await fulfill(route, rules[rules.length - 1])
      return
    }
    const itemMatch = path.match(/^\/api\/plugins\/keyword_reply\/groups\/(\d+)\/rules\/(\d+)$/)
    if (itemMatch !== null && method === 'DELETE') {
      const input = request.postDataJSON()
      // 删除必须携带乐观锁版本。
      expect(input).toEqual({ expected_version: 1 })
      rules = rules.filter((rule) => rule.id !== Number(itemMatch[2]))
      await fulfill(route, { deleted: true })
      return
    }
    throw new Error(`未处理关键词API：${method} ${path}`)

    // >>> 数据演变示例
    // 1. POST 群100 -> 规则加入群100 -> 群200 列表仍为空。
    // 2. DELETE v1 -> 规则移除 -> 列表为空。
  })

  // >>> 数据演变示例
  // 1. 初始 rules=[] -> 新增 -> 一条群100规则。
  // 2. 切换到群200 -> 列表为空 -> 证明按群隔离。
}

// testRuleLifecycleIsGroupScoped 验证专属页面的按群隔离与增删链路。
// @param page：Playwright注入页面。
// @returns Promise，在删除完成后结束。
// ⚠️副作用说明：操作测试页面并修改mock规则状态。
async function testRuleLifecycleIsGroupScoped({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await mockKeywordRules(page)
  await page.goto('/plugin-pages/keyword_reply')
  await expect(page.getByText('请先输入群号，规则按群隔离管理')).toBeVisible()

  await page.getByPlaceholder('手工输入 QQ 群号').fill('100')
  await page.getByRole('button', { name: '加载该群规则' }).click()
  await expect(page.getByText('群 100 的关键词规则')).toBeVisible()

  await page.getByRole('button', { name: '新增规则' }).click()
  await page.getByPlaceholder('关键词（与消息完全相等时触发）').fill('你好')
  await page.getByPlaceholder('回复内容').fill('你好呀')
  await page.getByRole('button', { name: '保存' }).click()
  await expect(page.getByText('关键词规则已新增')).toBeVisible()
  await expect(page.getByRole('cell', { name: '你好呀' })).toBeVisible()

  // 切换到另一个群必须看不到上一个群的规则。
  await page.getByPlaceholder('手工输入 QQ 群号').fill('200')
  await page.getByRole('button', { name: '加载该群规则' }).click()
  await expect(page.getByText('群 200 的关键词规则')).toBeVisible()
  await expect(page.getByRole('cell', { name: '你好呀' })).toHaveCount(0)

  await page.getByPlaceholder('手工输入 QQ 群号').fill('100')
  await page.getByRole('button', { name: '加载该群规则' }).click()
  await page.getByRole('button', { name: '删除' }).click()
  await expect(page.getByText('关键词规则已删除')).toBeVisible()
  await expect(page.getByRole('cell', { name: '你好呀' })).toHaveCount(0)

  // >>> 数据演变示例
  // 1. 群100 新增"你好" -> 表格出现该行。
  // 2. 切到群200 -> 列表为空 -> 证明群隔离。
}

// testUnknownPageKeyIsRejected 验证未注册页面 Key 不会渲染任何组件。
// @param page：Playwright注入页面。
// @returns Promise，在空状态可见后结束。
// ⚠️副作用说明：操作测试页面。
async function testUnknownPageKeyIsRejected({ page }: { page: Page }): Promise<void> {
  await seedSession(page)
  await page.route('**/api/plugins', async (route) => fulfill(route, []))
  await page.goto('/plugin-pages/not_registered')
  await expect(page.getByText('当前版本没有这个插件页面')).toBeVisible()

  // >>> 数据演变示例
  // 1. 未注册 Key -> 空状态。
  // 2. 已注册 Key -> 渲染本地组件。
}

test('关键词规则按群隔离且可增删', testRuleLifecycleIsGroupScoped)
test('未注册页面 Key 显示空状态', testUnknownPageKeyIsRejected)
