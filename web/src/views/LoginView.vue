<!-- 📌 影响范围：读取登录表单；调用 /api/auth/login；写入浏览器会话并跳转路由。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NText } from 'naive-ui'
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
  // 1. 正确凭据 -> 保存 Token -> 跳转 /plugins。
  // 2. 密码错误 -> 显示后端 message -> 停留登录页。
}
</script>

<template>
  <main class="login-page">
    <section class="login-shell" aria-labelledby="login-title">
      <header class="login-brand">
        <span class="brand-symbol" aria-hidden="true">W</span>
        <div>
          <strong>w1ndys-bot</strong>
          <NText depth="3">OneBot 管理控制台</NText>
        </div>
      </header>

      <NCard class="login-card" :bordered="true">
        <div class="login-heading">
          <p class="section-label">管理员访问</p>
          <h1 id="login-title">登录控制台</h1>
          <NText depth="3">使用部署环境中配置的最高管理员凭据。</NText>
        </div>

        <NAlert v-if="errorMessage" type="error" :show-icon="true" closable @close="errorMessage = ''">
          {{ errorMessage }}
        </NAlert>

        <NForm class="login-form" label-placement="top" :show-feedback="false" @submit.prevent="submitLogin">
          <NFormItem label="管理员 QQ">
            <NInput
              v-model:value="qq"
              autocomplete="username"
              inputmode="numeric"
              placeholder="请输入 QQ 号"
              size="large"
            />
          </NFormItem>
          <NFormItem label="管理密码">
            <NInput
              v-model:value="password"
              autocomplete="current-password"
              type="password"
              show-password-on="click"
              placeholder="请输入管理密码"
              size="large"
            />
          </NFormItem>
          <NButton
            attr-type="submit"
            type="primary"
            size="large"
            block
            :loading="loading"
            :disabled="loading || qq.trim() === '' || password === ''"
          >
            进入管理中心
          </NButton>
        </NForm>

        <footer class="login-note">
          凭据仅用于当前部署实例，连续失败时请检查 <code>SUPER_ADMIN_QQ</code> 与 <code>WEBUI_PASSWORD</code>。
        </footer>
      </NCard>
    </section>
  </main>
</template>

<style scoped>
.login-page {
  min-height: calc(100vh - 6rem);
  display: grid;
  place-items: center;
  padding: 4rem 1rem;
}

.login-shell {
  width: min(100%, 26.25rem);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
}

.login-brand > div {
  display: grid;
  gap: 0.125rem;
}

.login-brand strong {
  color: var(--color-text-primary);
  font-size: 1rem;
}

.brand-symbol {
  width: 2.25rem;
  height: 2.25rem;
  display: grid;
  place-items: center;
  border-radius: 0.5rem;
  color: var(--color-bg-card);
  background: var(--color-primary);
  font-weight: 700;
}

.login-card {
  border-color: var(--color-border);
  border-radius: 0.5rem;
  box-shadow: 0 1px 2px rgba(29, 26, 24, 0.06);
}

.login-heading {
  margin-bottom: 1.5rem;
}

.section-label {
  margin: 0 0 0.5rem;
  color: var(--color-primary);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.04em;
}

h1 {
  margin: 0 0 0.5rem;
  color: var(--color-text-primary);
  font-size: 1.75rem;
  line-height: 2.25rem;
  letter-spacing: -0.015em;
}

.login-form {
  margin-top: 1.25rem;
}

.login-form :deep(.n-form-item) {
  margin-bottom: 1rem;
}

.login-note {
  margin-top: 1.25rem;
  padding-top: 1rem;
  border-top: 1px solid var(--color-divider);
  color: var(--color-text-muted);
  font-size: 0.75rem;
  line-height: 1.125rem;
}

code {
  color: var(--color-text-secondary);
  font-family: "JetBrains Mono", "SFMono-Regular", Consolas, monospace;
  overflow-wrap: anywhere;
}

@media (max-width: 39.9375rem) {
  .login-page {
    min-height: calc(100vh - 4rem);
    padding: 2rem 1rem;
  }
}
</style>
