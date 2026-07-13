<!-- 📌 影响范围：读取前端会话状态；配置 Naive UI 曲奇棕亮色主题；渲染后台侧栏并执行浏览器路由跳转。 -->
<script setup lang="ts">
import {
  NButton,
  NAlert,
  NConfigProvider,
  NDialogProvider,
  NDrawer,
  NDrawerContent,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NMenu,
  NMessageProvider,
  type GlobalThemeOverrides,
  type MenuOption,
} from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { listPlugins } from './api'
import { sessionToken, setSessionToken } from './session'

const router = useRouter()
const route = useRoute()
const mobileDrawerVisible = ref(false)
const manualLogoutInProgress = ref(false)
const menuError = ref('')
const menuOptions = ref<MenuOption[]>([])
const activeMenuKey = computed(resolveActiveMenuKey)
const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#9a6744',
    primaryColorHover: '#b17b55',
    primaryColorPressed: '#7c4f32',
    primaryColorSuppl: '#a9704a',
    bodyColor: '#f8f3eb',
    cardColor: '#fffdf9',
    modalColor: '#fffdf9',
    popoverColor: '#fffdf9',
    textColorBase: '#3f2d22',
    borderColor: '#dfd0c2',
    dividerColor: '#eadfd4',
    borderRadius: '10px',
  },
  Button: {
    borderRadiusMedium: '10px',
    fontWeight: '700',
  },
  Card: {
    borderRadius: '16px',
  },
}

// logout 清理会话并返回登录页。
// @param 无。
// @returns Promise，在导航结束后完成。
// ⚠️副作用说明：删除 localStorage Token 并改变浏览器路由。
async function logout(): Promise<void> {
  manualLogoutInProgress.value = true
  setSessionToken('')
  try {
    await router.push({ name: 'login' })
  } finally {
    manualLogoutInProgress.value = false
  }

  // >>> 数据演变示例
  // 1. 已登录 -> 清Token -> /login。
  // 2. Token已空 -> 保持空 -> /login。
}

// navigateMenu 根据侧栏菜单键进入对应管理页面。
// @param key：Vue Router稳定路由名称。
// @returns Promise，在导航完成后结束。
// ⚠️副作用说明：关闭移动端抽屉并改变浏览器路由。
async function navigateMenu(key: string): Promise<void> {
  mobileDrawerVisible.value = false
  // [决策理由] 插件二级菜单使用前缀携带稳定插件名，需要转换成插件工作台命名路由。
  if (key.startsWith('plugin:')) {
    await router.push({ name: 'plugin-overview', params: { pluginName: key.slice('plugin:'.length) } })
    return
  }
  await router.push({ name: key })

  // >>> 数据演变示例
  // 1. key=permissions -> 关闭抽屉 -> 跳转/permissions。
  // 2. key=settings -> 关闭抽屉 -> 跳转/settings。
}

// loadPluginMenu 从后端插件快照构建二级菜单。
// @param 无。
// @returns Promise，在插件菜单更新后结束。
// ⚠️副作用说明：发起插件查询请求并替换响应式菜单。
async function loadPluginMenu(): Promise<void> {
  // [决策理由] 未登录时插件接口必然返回401，不应发送无效请求。
  if (sessionToken.value === '') {
    menuOptions.value = [{ label: '系统设置', key: 'settings' }]
    return
  }
  menuError.value = ''
  try {
    const plugins = await listPlugins()
    const children: MenuOption[] = []
    for (const plugin of plugins) {
      // [决策理由] 当前二进制不可用的插件不应进入可操作二级菜单。
      if (plugin.available) {
        children.push({ label: plugin.display_name || plugin.name, key: `plugin:${plugin.name}` })
      }
    }
    menuOptions.value = [
      { label: '插件管理', key: 'plugins', children },
      { label: '系统设置', key: 'settings' },
    ]
  } catch (error) {
    menuError.value = error instanceof Error ? error.message : '插件菜单加载失败'
    menuOptions.value = [
      { label: '插件管理', key: 'plugins' },
      { label: '系统设置', key: 'settings' },
    ]
  }

  // >>> 数据演变示例
  // 1. API返回admin,ping -> 插件管理子菜单包含两项。
  // 2. API失败 -> 保留插件管理与系统设置一级入口。
}

// resolveActiveMenuKey 将当前路由映射为侧栏高亮键。
// @param 无；读取当前响应式路由。
// @returns 插件二级键或系统级路由名。
// ⚠️副作用说明：无。
function resolveActiveMenuKey(): string {
  const pluginName = route.params.pluginName
  // [决策理由] 插件工作台所有Tab都应持续高亮同一个插件二级菜单。
  if (typeof pluginName === 'string' && pluginName !== '') {
    return `plugin:${pluginName}`
  }
  const name = route.name
  const value = typeof name === 'string' ? name : ''

  // >>> 数据演变示例
  // 1. /plugins/ping/commands -> plugin:ping。
  // 2. /settings -> settings。
  return value
}

// handleSessionTokenChange 在会话被API清理后立即返回登录页。
// @param token：最新会话Token，空字符串表示失效或退出。
// @returns Promise，在必要的登录重定向完成后结束。
// ⚠️副作用说明：Token失效时可能替换浏览器路由。
async function handleSessionTokenChange(token: string): Promise<void> {
  // [决策理由] 主动退出已负责导航登录页，不应由401恢复逻辑附加原页面redirect或重复导航。
  if (manualLogoutInProgress.value) {
    return
  }
  // [决策理由] 401会在API层清空Token，必须立即离开受保护页面且保留原目标供重新登录。
  if (token === '' && route.name !== 'login') {
    await router.replace({ name: 'login', query: { redirect: route.fullPath } })
  }
  // [决策理由] 登录成功后应立即加载当前账号可见的插件二级菜单。
  if (token !== '') {
    await loadPluginMenu()
  }

  // >>> 数据演变示例
  // 1. /permissions+Token清空 -> /login?redirect=/permissions。
  // 2. 主动退出或登录页Token为空 -> 不追加redirect且不重复导航。
}

watch(sessionToken, handleSessionTokenChange)
onMounted(loadPluginMenu)
</script>

<template>
  <NConfigProvider :theme-overrides="themeOverrides">
    <NDialogProvider>
      <NMessageProvider>
        <div v-if="sessionToken" class="app-shell">
          <header class="topbar">
            <RouterLink class="brand" to="/plugins">
              <span class="brand-mark">W</span>
              <span>w1ndys-bot-webui</span>
            </RouterLink>
            <div class="topbar-actions">
              <NButton class="mobile-menu-button" quaternary type="primary" aria-controls="mobile-admin-menu" :aria-expanded="mobileDrawerVisible" @click="mobileDrawerVisible = true">功能菜单</NButton>
              <NButton secondary type="primary" @click="logout">退出登录</NButton>
            </div>
          </header>
          <NLayout class="admin-layout" has-sider>
            <NLayoutSider class="desktop-sider" bordered :width="224" content-style="padding: 22px 12px;">
              <div class="sidebar-caption">管理功能</div>
              <NAlert v-if="menuError" class="menu-alert" type="error" size="small"><NButton text type="primary" @click="loadPluginMenu">重试加载插件</NButton></NAlert>
              <NMenu :value="activeMenuKey" :options="menuOptions" :default-expanded-keys="['plugins']" @update:value="navigateMenu" />
            </NLayoutSider>
            <NLayoutContent class="admin-content" content-style="min-height: calc(100vh - 68px);">
              <main class="page-container">
                <RouterView />
              </main>
            </NLayoutContent>
          </NLayout>
          <NDrawer v-model:show="mobileDrawerVisible" placement="left" :width="280">
            <NDrawerContent id="mobile-admin-menu" title="管理功能" closable>
              <NAlert v-if="menuError" class="menu-alert" type="error" size="small"><NButton text type="primary" @click="loadPluginMenu">重试加载插件</NButton></NAlert>
              <NMenu :value="activeMenuKey" :options="menuOptions" :default-expanded-keys="['plugins']" @update:value="navigateMenu" />
            </NDrawerContent>
          </NDrawer>
        </div>
        <RouterView v-else-if="route.name === 'login'" />
        <main v-else class="session-transition">会话已结束，正在返回登录页…</main>
      </NMessageProvider>
    </NDialogProvider>
  </NConfigProvider>
</template>
