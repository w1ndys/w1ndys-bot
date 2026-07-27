// 📌 影响范围：读取浏览器 history 和本地会话 Token；控制前端页面导航。
import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { sessionToken } from './session'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('./views/LoginView.vue') },
    { path: '/', redirect: '/plugin-runtimes' },
    { path: '/plugin-runtimes', name: 'plugin-runtimes', component: () => import('./views/PluginRuntimesView.vue'), meta: { requiresAuth: true } },
    { path: '/plugin-runtimes/:pluginKey/config', name: 'plugin-runtime-config', component: () => import('./views/PluginRuntimeConfigView.vue'), meta: { requiresAuth: true, menuKey: 'plugin-runtimes' } },
    { path: '/plugin-pages/:pageKey', name: 'plugin-page', component: () => import('./views/PluginPageView.vue'), meta: { requiresAuth: true, menuKey: 'plugin-runtimes' } },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue'), meta: { requiresAuth: true } },
    { path: '/audit-logs', name: 'audit-logs', component: () => import('./views/AuditLogsView.vue'), meta: { requiresAuth: true } },
    { path: '/:pathMatch(.*)*', redirect: '/plugin-runtimes' },
  ],
})

function guardRoute(to: RouteLocationNormalized) {
  if (to.meta.requiresAuth && sessionToken.value === '') {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && sessionToken.value !== '') {
    return { name: 'plugin-runtimes' }
  }
  return true
}

router.beforeEach(guardRoute)
export default router
