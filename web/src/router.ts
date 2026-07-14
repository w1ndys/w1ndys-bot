// 📌 影响范围：读取浏览器 history 和本地会话 Token；控制前端页面导航。
import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { sessionToken } from './session'

// loadLoginView 懒加载登录页。
// @param 无。
// @returns 登录页组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadLoginView() {
  const result = import('./views/LoginView.vue')

  // >>> 数据演变示例
  // 1. 首次进入/login -> 请求LoginView代码块 -> 渲染登录页。
  // 2. 再次进入/login -> 浏览器缓存命中 -> 渲染登录页。
  return result
}

// loadPluginsView 懒加载插件总览。
// @param 无。
// @returns 插件总览组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadPluginsView() {
  const result = import('./views/PluginsView.vue')

  // >>> 数据演变示例
  // 1. /plugins -> 请求PluginsView代码块 -> 渲染总览。
  // 2. 已缓存 -> 直接复用模块。
  return result
}

// loadCommandsView 懒加载插件命令管理页。
// @param 无。
// @returns 命令管理组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadCommandsView() {
  const result = import('./views/CommandsView.vue')

  // >>> 数据演变示例
  // 1. ping/commands -> 请求命令页代码块 -> 渲染表格。
  // 2. 切回命令页 -> 缓存模块复用。
  return result
}

// loadPermissionsView 懒加载插件权限管理页。
// @param 无。
// @returns 权限管理组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadPermissionsView() {
  const result = import('./views/PermissionsView.vue')

  // >>> 数据演变示例
  // 1. ping/permissions -> 请求权限页代码块 -> 渲染策略。
  // 2. 已缓存 -> 直接复用模块。
  return result
}

// loadSettingsView 懒加载系统设置页。
// @param 无。
// @returns 系统设置组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadSettingsView() {
  const result = import('./views/SettingsView.vue')

  // >>> 数据演变示例
  // 1. /settings -> 请求设置页代码块 -> 渲染设置。
  // 2. 已缓存 -> 直接复用模块。
  return result
}

// loadAuditLogsView 懒加载审计日志页。
// @param 无。
// @returns 审计日志组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadAuditLogsView() {
  const result = import('./views/AuditLogsView.vue')

  // >>> 数据演变示例
  // 1. 首次进入/audit-logs -> 请求审计页代码块 -> 渲染只读列表。
  // 2. 再次进入 -> 浏览器缓存命中 -> 直接渲染。
  return result
}

// loadPluginWorkspaceView 懒加载插件工作台外壳。
// @param 无。
// @returns 插件工作台组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadPluginWorkspaceView() {
  const result = import('./views/PluginWorkspaceView.vue')

  // >>> 数据演变示例
  // 1. /plugins/ping -> 请求工作台代码块 -> 渲染Tabs。
  // 2. 切换插件 -> 复用工作台模块。
  return result
}

// loadPluginOverviewView 懒加载插件概览页。
// @param 无。
// @returns 插件概览组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadPluginOverviewView() {
  const result = import('./views/PluginOverviewView.vue')

  // >>> 数据演变示例
  // 1. ping/overview -> 请求概览代码块 -> 渲染运行配置。
  // 2. 已缓存 -> 直接复用模块。
  return result
}

// loadPluginFeaturesView 懒加载插件功能页。
// @param 无。
// @returns 插件功能组件模块 Promise。
// ⚠️副作用说明：首次访问时发起前端代码块请求。
function loadPluginFeaturesView() {
  const result = import('./views/PluginFeaturesView.vue')

  // >>> 数据演变示例
  // 1. ping/features -> 请求功能页代码块 -> 渲染Manifest表格。
  // 2. 已缓存 -> 直接复用模块。
  return result
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: loadLoginView },
    { path: '/', redirect: '/plugins' },
    { path: '/plugins', name: 'plugins', component: loadPluginsView, meta: { requiresAuth: true } },
    {
      path: '/plugins/:pluginName',
      component: loadPluginWorkspaceView,
      meta: { requiresAuth: true },
      children: [
        { path: '', redirect: 'overview' },
        { path: 'overview', name: 'plugin-overview', component: loadPluginOverviewView },
        { path: 'features', name: 'plugin-features', component: loadPluginFeaturesView },
        { path: 'commands', name: 'plugin-commands', component: loadCommandsView },
        { path: 'permissions', name: 'plugin-permissions', component: loadPermissionsView },
      ],
    },
    { path: '/settings', name: 'settings', component: loadSettingsView, meta: { requiresAuth: true } },
    { path: '/audit-logs', name: 'audit-logs', component: loadAuditLogsView, meta: { requiresAuth: true } },
  ],
})

// guardRoute 统一处理未登录访问和已登录重复进入登录页。
// @param to：目标路由。
// @returns 重定向位置或允许导航的 true。
// ⚠️副作用说明：读取响应式会话 Token，可能改变导航目标。
function guardRoute(to: RouteLocationNormalized) {
  // [决策理由] 受保护页面没有 Token 时必须先登录。
  if (to.meta.requiresAuth && sessionToken.value === '') {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  // [决策理由] 已登录用户无需再次看到登录表单。
  if (to.name === 'login' && sessionToken.value !== '') {
    return { name: 'plugins' }
  }

  // >>> 数据演变示例
  // 1. 未登录访问/plugins -> /login?redirect=/plugins。
  // 2. 已登录访问/login -> /plugins。
  return true
}

router.beforeEach(guardRoute)

export default router
