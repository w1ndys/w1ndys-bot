// 📌 影响范围：读取浏览器 history 和本地会话 Token；控制前端页面导航。
import { createRouter, createWebHistory } from 'vue-router'
import { sessionToken } from './session'
import LoginView from './views/LoginView.vue'
import PluginsView from './views/PluginsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView },
    { path: '/', redirect: '/plugins' },
    { path: '/plugins', name: 'plugins', component: PluginsView, meta: { requiresAuth: true } },
  ],
})

// 路由守卫统一处理未登录访问和已登录重复进入登录页。
// @param to：目标路由。
// @returns 重定向位置或允许导航的 true。
// ⚠️副作用说明：读取响应式会话 Token，可能改变导航目标。
router.beforeEach((to) => {
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
})

export default router
