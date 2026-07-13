// 📌 影响范围：读取浏览器当前域名与会话 Token；调用后端 /api HTTP 接口。
import { setSessionToken, sessionToken } from './session'

export interface ApiEnvelope<T> {
  code: string
  message: string
  data: T
}

export interface LoginResult {
  token: string
  expires_in: number
}

export interface PluginState {
  name: string
  display_name: string
  description: string
  version: string
  available: boolean
  enabled: boolean
  priority: number
  config: Record<string, unknown>
}

export interface FeatureState {
  plugin_name: string
  key: string
  display_name: string
  description: string
  available: boolean
  default_commands: string[]
  default_permissions: Record<string, boolean>
}

export interface CommandState {
  id: number
  scope_type: 'global' | 'group'
  scope_id: string
  plugin_name: string
  feature_key: string
  command: string
  normalized_command: string
  enabled: boolean
}

export interface CommandCreate {
  scope_type: 'global' | 'group'
  scope_id: string
  plugin_name: string
  feature_key: string
  command: string
}

export interface PermissionState {
  id: number
  scope_type: 'global' | 'group'
  scope_id: string
  plugin_name: string
  feature_key: string
  subject_type: 'role' | 'user'
  subject_id: string
  effect: 'allow' | 'deny'
}

export type PermissionSet = Omit<PermissionState, 'id'>

// apiRequest 执行统一鉴权请求并解析 code/message/data 响应。
// @param path：以 /api 开头的接口路径；options：Fetch 请求参数。
// @returns 成功响应中的 data，失败时抛出包含后端 message 的 Error。
// ⚠️副作用说明：发起网络请求；401 时清理浏览器会话 Token。
export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')
  // [决策理由] 仅在存在请求体时声明 JSON，避免 GET 产生无意义预检差异。
  if (options.body) {
    headers.set('Content-Type', 'application/json')
  }
  // [决策理由] 登录接口没有 Token，其他请求存在会话时统一附加 Bearer 凭证。
  if (sessionToken.value !== '') {
    headers.set('Authorization', `Bearer ${sessionToken.value}`)
  }
  const response = await fetch(path, { ...options, headers })
  const envelope = (await response.json()) as ApiEnvelope<T>
  // [决策理由] 401 表示本地 Token 已失效，应立即清理以便路由返回登录页。
  if (response.status === 401) {
    setSessionToken('')
  }
  // [决策理由] HTTP 状态和业务码必须同时成功，防止代理异常被误作业务数据。
  if (!response.ok || envelope.code !== 'ok') {
    throw new Error(envelope.message || '请求失败')
  }

  // >>> 数据演变示例
  // 1. 200+code=ok+data插件列表 -> 返回插件数组。
  // 2. 401+unauthorized -> 清Token -> 抛出后端错误。
  return envelope.data
}

// login 使用唯一管理员 QQ 和共享环境密码登录。
// @param qq：SUPER_ADMIN_QQ；password：WEBUI_PASSWORD。
// @returns 登录 Token 与有效秒数。
// ⚠️副作用说明：发起登录网络请求；成功时保存 Token。
export async function login(qq: string, password: string): Promise<LoginResult> {
  const result = await apiRequest<LoginResult>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ qq, password }),
  })
  setSessionToken(result.token)

  // >>> 数据演变示例
  // 1. 正确QQ+密码 -> API Token -> localStorage -> 返回结果。
  // 2. 错误密码 -> apiRequest抛错 -> Token不变。
  return result
}

// listPlugins 获取插件元数据和当前运行配置。
// @param 无。
// @returns 按后端优先级排序的插件列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPlugins(): Promise<PluginState[]> {
  const result = apiRequest<PluginState[]>('/api/plugins')

  // >>> 数据演变示例
  // 1. 有效Token -> GET /api/plugins -> PluginState[]。
  // 2. 过期Token -> 清理会话并抛错。
  return result
}

// patchPlugin 修改插件启用状态或优先级。
// @param name：插件稳定名称；patch：只能包含 enabled 或 priority 之一。
// @returns 后端热刷新后的插件状态。
// ⚠️副作用说明：修改后端数据库、审计记录和插件运行快照。
export function patchPlugin(name: string, patch: { enabled: boolean } | { priority: number }): Promise<PluginState> {
  const result = apiRequest<PluginState>(`/api/plugins/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  })

  // >>> 数据演变示例
  // 1. ping+enabled=true -> PATCH -> 返回启用状态。
  // 2. admin+enabled=false -> 409 -> 抛出保护错误。
  return result
}

// listPluginFeatures 获取指定插件的 Manifest 功能元数据。
// @param pluginName：插件稳定名称。
// @returns 功能列表及默认触发词、默认权限。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPluginFeatures(pluginName: string): Promise<FeatureState[]> {
  const result = apiRequest<FeatureState[]>(`/api/plugins/${encodeURIComponent(pluginName)}/features`)

  // >>> 数据演变示例
  // 1. ping -> GET features -> [ping功能]。
  // 2. missing -> 404 -> 抛出插件不存在。
  return result
}

// listCommands 获取全部功能触发词。
// @param 无。
// @returns 全局与群级触发词列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listCommands(): Promise<CommandState[]> {
  const result = apiRequest<CommandState[]>('/api/commands')

  // >>> 数据演变示例
  // 1. 有效Token -> GET commands -> CommandState[]。
  // 2. Token失效 -> 清会话并抛错。
  return result
}

// createCommand 为插件功能新增触发词。
// @param input：作用域、功能目标与触发词。
// @returns 保存并热刷新后的触发词状态。
// ⚠️副作用说明：新增数据库命令、审计记录并刷新后端命令快照。
export function createCommand(input: CommandCreate): Promise<CommandState> {
  const result = apiRequest<CommandState>('/api/commands', { method: 'POST', body: JSON.stringify(input) })

  // >>> 数据演变示例
  // 1. ping.ping+测试 -> POST -> 新CommandState。
  // 2. 同作用域重复 -> 409 command_conflict -> 抛错。
  return result
}

// renameCommand 修改已有触发词文本。
// @param id：命令ID；command：新触发词。
// @returns 保存后的触发词状态。
// ⚠️副作用说明：更新数据库命令、审计记录并刷新后端命令快照。
export function renameCommand(id: number, command: string): Promise<CommandState> {
  const result = apiRequest<CommandState>(`/api/commands/${id}`, { method: 'PATCH', body: JSON.stringify({ command }) })

  // >>> 数据演变示例
  // 1. id1+延迟 -> PATCH -> 更新状态。
  // 2. 重复文本 -> 409 -> 抛错。
  return result
}

// deleteCommand 删除功能触发词。
// @param id：命令ID。
// @returns 删除成功后的空数据。
// ⚠️副作用说明：删除数据库命令、写审计并刷新后端命令快照。
export function deleteCommand(id: number): Promise<null> {
  const result = apiRequest<null>(`/api/commands/${id}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. id1 -> DELETE -> null。
  // 2. id404 -> 404 -> 抛错。
  return result
}

// listPermissions 获取全部显式权限策略。
// @param 无。
// @returns 权限策略列表。
// ⚠️副作用说明：发起鉴权网络请求。
export function listPermissions(): Promise<PermissionState[]> {
  const result = apiRequest<PermissionState[]>('/api/permissions')

  // >>> 数据演变示例
  // 1. 有效Token -> GET permissions -> PermissionState[]。
  // 2. Token失效 -> 清会话 -> 抛出鉴权错误。
  return result
}

// setPermission 新增权限策略或更新相同维度的效果。
// @param input：权限作用域、目标、主体和效果。
// @returns 保存并热刷新后的权限策略。
// ⚠️副作用说明：写入后端权限与审计记录，并刷新权限快照。
export function setPermission(input: PermissionSet): Promise<PermissionState> {
  const result = apiRequest<PermissionState>('/api/permissions', { method: 'POST', body: JSON.stringify(input) })

  // >>> 数据演变示例
  // 1. group123+ping+user200+allow -> UPSERT -> 返回策略。
  // 2. user+非数字QQ -> 400 -> 抛出校验错误。
  return result
}

// deletePermission 删除一条显式权限策略。
// @param id：权限策略主键。
// @returns 删除成功后的空数据。
// ⚠️副作用说明：删除后端权限、写入审计并刷新权限快照。
export function deletePermission(id: number): Promise<null> {
  const result = apiRequest<null>(`/api/permissions/${id}`, { method: 'DELETE' })

  // >>> 数据演变示例
  // 1. id8 -> DELETE -> null并回退下一层规则。
  // 2. id404 -> 404 -> 抛出不存在错误。
  return result
}
