<!-- 📌 影响范围：读取前端会话状态；渲染全局导航并执行浏览器路由跳转。 -->
<script setup lang="ts">
import { useRouter } from 'vue-router'
import { sessionToken, setSessionToken } from './session'

const router = useRouter()

// logout 清理会话并返回登录页。
// @param 无。
// @returns Promise，在导航结束后完成。
// ⚠️副作用说明：删除 localStorage Token 并改变浏览器路由。
async function logout(): Promise<void> {
  setSessionToken('')
  await router.push({ name: 'login' })

  // >>> 数据演变示例
  // 1. 已登录 -> 清Token -> /login。
  // 2. Token已空 -> 保持空 -> /login。
}
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <RouterLink class="brand" to="/plugins">
        <span class="brand-mark">W</span>
        <span>w1ndys-bot-webui</span>
      </RouterLink>
      <nav v-if="sessionToken" class="main-nav">
        <RouterLink to="/plugins">插件</RouterLink>
        <RouterLink to="/commands">功能触发词</RouterLink>
      </nav>
      <button v-if="sessionToken" class="ghost-button" type="button" @click="logout">退出登录</button>
    </header>
    <main class="page-container">
      <RouterView />
    </main>
  </div>
</template>
