<!-- 📌 影响范围：读取前端会话状态；配置全局主题、Toast 和管理导航。 -->
<script setup lang="ts">
import { NButton, NConfigProvider, NDialogProvider, NDrawer, NDrawerContent, NLayout, NLayoutContent, NLayoutSider, NMenu, NMessageProvider, type GlobalThemeOverrides, type MenuOption } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { sessionToken, setSessionToken } from './session'

const router = useRouter()
const route = useRoute()
const mobileDrawerVisible = ref(false)
const manualLogoutInProgress = ref(false)
const menuOptions: MenuOption[] = [
  { label: '插件运行', key: 'plugin-runtimes' },
  { label: '系统设置', key: 'settings' },
  { label: '审计日志', key: 'audit-logs' },
]
const activeMenuKey = computed(() => typeof route.name === 'string' ? route.name : '')
const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#8B5E3C', primaryColorHover: '#A87550', primaryColorPressed: '#70472D', primaryColorSuppl: '#8B5E3C',
    bodyColor: '#F7F4F0', cardColor: '#FFFFFF', modalColor: '#FFFFFF', popoverColor: '#FFFFFF', tableColor: '#FFFFFF',
    textColorBase: '#352F2B', textColor1: '#352F2B', textColor2: '#625850', textColor3: '#7B7067', borderColor: '#DED7CF',
    dividerColor: '#EEE9E3', successColor: '#2F7D4A', warningColor: '#B06B16', errorColor: '#C43D3D', infoColor: '#3F6F94',
    borderRadius: '8px', fontSize: '14px', fontFamily: 'Inter, "Noto Sans SC", "Microsoft YaHei", "PingFang SC", system-ui, sans-serif',
  },
  Button: { borderRadiusMedium: '8px', heightMedium: '36px', fontWeight: '700' },
  Card: { borderRadius: '8px', paddingMedium: '20px' },
  DataTable: { thColor: '#F7F4F0', thTextColor: '#625850', tdColorHover: '#F7F4F0', borderColor: '#EEE9E3' },
  Menu: { itemColorActive: '#F1E2D4', itemColorActiveHover: '#F1E2D4', itemTextColorActive: '#70472D', itemTextColorActiveHover: '#70472D', itemBorderRadius: '6px' },
  Tabs: { tabTextColorActiveLine: '#70472D', barColor: '#8B5E3C' },
}

async function logout(): Promise<void> {
  manualLogoutInProgress.value = true
  setSessionToken('')
  try { await router.push({ name: 'login' }) } finally { manualLogoutInProgress.value = false }
}

async function navigateMenu(key: string): Promise<void> {
  mobileDrawerVisible.value = false
  await router.push({ name: key })
}

function resolveSessionRedirect(): string {
  const browserPath = `${window.location.pathname}${window.location.search}`
  return route.fullPath === '/' && browserPath !== '/' ? browserPath : route.fullPath
}

watch(sessionToken, async (token) => {
  if (!manualLogoutInProgress.value && token === '' && route.name !== 'login') {
    await router.replace({ name: 'login', query: { redirect: resolveSessionRedirect() } })
  }
})
</script>

<template>
  <NConfigProvider :theme-overrides="themeOverrides">
    <NDialogProvider>
      <NMessageProvider>
        <div v-if="sessionToken" class="app-shell">
          <header class="topbar">
            <RouterLink class="brand" to="/plugin-runtimes"><span class="brand-mark">W</span><span>w1ndys-bot-webui</span></RouterLink>
            <div class="topbar-actions">
              <NButton class="mobile-menu-button" quaternary type="primary" aria-controls="mobile-admin-menu" :aria-expanded="mobileDrawerVisible" @click="mobileDrawerVisible = true">功能菜单</NButton>
              <NButton secondary type="primary" @click="logout">退出登录</NButton>
            </div>
          </header>
          <NLayout class="admin-layout" has-sider>
            <NLayoutSider class="desktop-sider" bordered :width="240" content-style="padding: 20px 12px;">
              <div class="sidebar-caption">管理功能</div>
              <NMenu :value="activeMenuKey" :options="menuOptions" @update:value="navigateMenu" />
            </NLayoutSider>
            <NLayoutContent class="admin-content" native-scrollbar><main class="page-container"><RouterView /></main></NLayoutContent>
          </NLayout>
          <NDrawer v-model:show="mobileDrawerVisible" placement="left" :width="280">
            <NDrawerContent id="mobile-admin-menu" title="管理功能" closable><NMenu :value="activeMenuKey" :options="menuOptions" @update:value="navigateMenu" /></NDrawerContent>
          </NDrawer>
        </div>
        <RouterView v-else-if="route.name === 'login'" />
        <main v-else class="session-transition">会话已结束，正在返回登录页…</main>
      </NMessageProvider>
    </NDialogProvider>
  </NConfigProvider>
</template>
