<!-- 📌 影响范围：读取登录表单；调用 /api/auth/login；写入浏览器会话并跳转路由。 -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login } from '../api'

const qq = ref('')
const password = ref('')
const loading = ref(false)
const errorMessage = ref('')
const route = useRoute()
const router = useRouter()

// submitLogin 提交唯一管理员凭据并进入目标管理页。
// @param 无；数据来自响应式表单。
// @returns Promise，在登录请求和导航完成后结束。
// ⚠️副作用说明：调用登录 API、保存 Token、更新错误状态并改变路由。
async function submitLogin(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    await login(qq.value.trim(), password.value)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/plugins'
    await router.replace(redirect)
  } catch (error) {
    // [决策理由] Fetch 异常不一定是 Error 实例，必须提供稳定用户提示。
    if (error instanceof Error) {
      errorMessage.value = error.message
    } else {
      errorMessage.value = '登录失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. 正确凭据 -> 保存Token -> 跳转/plugins。
  // 2. 密码错误 -> 显示后端message -> 停留登录页。
}
</script>

<template>
  <section class="login-layout">
    <div class="login-copy">
      <span class="eyebrow">ONEBOT CONTROL CENTER</span>
      <h1>让机器人管理<br />保持清晰、可靠。</h1>
      <p>登录后管理插件状态、功能触发词、权限策略与审计记录。</p>
    </div>
    <form class="panel login-panel" @submit.prevent="submitLogin">
      <div>
        <span class="eyebrow">ADMIN ACCESS</span>
        <h2>最高管理员登录</h2>
        <p class="muted">使用 SUPER_ADMIN_QQ 与 WEBUI_PASSWORD。</p>
      </div>
      <label>
        <span>管理员 QQ</span>
        <input v-model="qq" autocomplete="username" inputmode="numeric" placeholder="请输入 QQ 号" required />
      </label>
      <label>
        <span>管理密码</span>
        <input v-model="password" autocomplete="current-password" type="password" placeholder="请输入环境密码" required />
      </label>
      <p v-if="errorMessage" class="error-message">{{ errorMessage }}</p>
      <button class="primary-button" type="submit" :disabled="loading">
        {{ loading ? '正在验证…' : '进入管理中心' }}
      </button>
    </form>
  </section>
</template>
