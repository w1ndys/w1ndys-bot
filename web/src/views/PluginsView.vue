<!-- 📌 影响范围：调用插件查询和修改 API；展示并更新后端插件运行状态；跳转插件工作台路由。 -->
<script setup lang="ts">
import { NAlert, NButton, NEmpty, NInputNumber, NSpin, NSwitch, NTag } from 'naive-ui'
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { listPlugins, patchPlugin, type PluginState } from '../api'

const plugins = ref<PluginState[]>([])
const loading = ref(true)
const errorMessage = ref('')
const pendingName = ref('')
const router = useRouter()
const prioritySnapshots = new Map<string, number>()

// loadPlugins 重新获取完整插件快照。
// @param 无。
// @returns Promise，在列表请求完成后结束。
// ⚠️副作用说明：发起网络请求并替换页面插件列表与错误状态。
async function loadPlugins(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const states = await listPlugins()
    plugins.value = states
    prioritySnapshots.clear()
    for (const state of states) {
      prioritySnapshots.set(state.name, state.priority)
    }
  } catch (error) {
    // [决策理由] 网络层可能抛出非 Error 值，页面仍需稳定提示。
    if (error instanceof Error) {
      errorMessage.value = error.message
    } else {
      errorMessage.value = '加载插件失败'
    }
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. API 返回 [admin,ping] -> 页面清单替换 -> loading=false。
  // 2. Token 失效 -> errorMessage 更新 -> loading=false。
}

// togglePlugin 切换插件启用状态并替换对应列表项。
// @param plugin：当前插件状态；enabled：开关目标状态。
// @returns Promise，在 PATCH 请求完成后结束。
// ⚠️副作用说明：修改后端插件状态、审计记录和当前页面列表。
async function togglePlugin(plugin: PluginState, enabled: boolean): Promise<void> {
  pendingName.value = plugin.name
  errorMessage.value = ''
  try {
    const updated = await patchPlugin(plugin.name, { enabled })
    replacePlugin(updated)
  } catch (error) {
    // [决策理由] 受保护插件等业务错误应原样展示给管理员。
    if (error instanceof Error) {
      errorMessage.value = error.message
    } else {
      errorMessage.value = '修改插件状态失败'
    }
  } finally {
    pendingName.value = ''
  }

  // >>> 数据演变示例
  // 1. ping:false + enabled=true -> PATCH -> 清单 ping:true。
  // 2. admin:true + enabled=false -> 后端拒绝 -> 清单不变并显示错误。
}

// savePriority 保存插件优先级并替换对应列表项。
// @param plugin：包含用户输入优先级的插件状态。
// @returns Promise，在 PATCH 请求完成后结束。
// ⚠️副作用说明：修改后端优先级、审计记录和当前页面列表。
async function savePriority(plugin: PluginState): Promise<void> {
  pendingName.value = plugin.name
  errorMessage.value = ''
  try {
    const updated = await patchPlugin(plugin.name, { priority: Number(plugin.priority) })
    replacePlugin(updated)
  } catch (error) {
    // [决策理由] 后端范围或持久化错误应反馈给管理员。
    if (error instanceof Error) {
      errorMessage.value = error.message
    } else {
      errorMessage.value = '保存优先级失败'
    }
    const failureMessage = errorMessage.value
    plugin.priority = prioritySnapshots.get(plugin.name) ?? plugin.priority
    await loadPlugins()
    errorMessage.value = failureMessage
  } finally {
    pendingName.value = ''
  }

  // >>> 数据演变示例
  // 1. ping 优先级 0 -> 输入 100 -> PATCH -> 清单更新为 100。
  // 2. 输入 20000 -> 后端拒绝 -> 清单保持并提示。
}

// openWorkspace 进入指定插件的详情工作台。
// @param plugin：目标插件状态。
// @returns Promise，在路由导航完成后结束。
// ⚠️副作用说明：改变浏览器路由。
async function openWorkspace(plugin: PluginState): Promise<void> {
  await router.push({ name: 'plugin-overview', params: { pluginName: plugin.name } })

  // >>> 数据演变示例
  // 1. plugin.name=ping -> 跳转 ping 工作台概览。
  // 2. plugin.name=admin -> 跳转 admin 工作台概览。
}

// replacePlugin 使用后端权威状态替换本地同名插件。
// @param updated：后端返回的最新插件状态。
// @returns 无。
// ⚠️副作用说明：修改响应式插件数组。
function replacePlugin(updated: PluginState): void {
  let index = -1
  for (let candidate = 0; candidate < plugins.value.length; candidate += 1) {
    // [决策理由] 插件稳定名称用于定位需要替换的权威状态。
    if (plugins.value[candidate].name === updated.name) {
      index = candidate
      break
    }
  }
  // [决策理由] 仅替换仍存在于当前列表的插件，避免并发刷新制造重复项。
  if (index >= 0) {
    plugins.value[index] = updated
    prioritySnapshots.set(updated.name, updated.priority)
  }

  // >>> 数据演变示例
  // 1. [ping旧] + ping新 -> index=0 -> [ping新]。
  // 2. [admin] + ping新 -> index=-1 -> 列表不变。
}

onMounted(loadPlugins)
</script>

<template>
  <section class="plugins-page">
    <header class="page-header">
      <div>
        <p class="section-label">运行时插件</p>
        <h1>插件管理</h1>
        <p class="page-description">查看当前二进制中的插件状态，并进入工作台管理功能、命令与权限。</p>
      </div>
      <NButton secondary type="primary" :loading="loading" :disabled="pendingName !== ''" @click="loadPlugins">
        刷新状态
      </NButton>
    </header>

    <NAlert v-if="errorMessage" class="page-alert" type="error" closable @close="errorMessage = ''">
      {{ errorMessage }}
    </NAlert>

    <NSpin :show="loading" description="正在读取插件快照…">
      <NEmpty v-if="!loading && plugins.length === 0" description="当前二进制没有可管理插件" />
      <div v-else class="plugin-list" role="list">
        <article v-for="plugin in plugins" :key="plugin.name" class="plugin-row" role="listitem">
          <div class="plugin-identity">
            <div class="plugin-title-line">
              <h2>{{ plugin.display_name || plugin.name }}</h2>
              <NTag :type="plugin.enabled ? 'success' : 'default'" size="small">
                {{ plugin.enabled ? '已启用' : '已停用' }}
              </NTag>
              <NTag v-if="!plugin.available" type="error" size="small">不可用</NTag>
            </div>
            <p>{{ plugin.description || '暂无插件说明' }}</p>
            <div class="plugin-meta">
              <code>{{ plugin.name }}</code>
              <span>v{{ plugin.version }}</span>
            </div>
          </div>

          <div class="priority-control">
            <label :for="`priority-${plugin.name}`">事件优先级</label>
            <div class="priority-input">
              <NInputNumber
                :id="`priority-${plugin.name}`"
                v-model:value="plugin.priority"
                :min="-10000"
                :max="10000"
                :disabled="pendingName !== '' || !plugin.available"
                size="small"
              />
              <NButton size="small" secondary :loading="pendingName === plugin.name" :disabled="pendingName !== '' || !plugin.available" @click="savePriority(plugin)">
                保存
              </NButton>
            </div>
          </div>

          <div class="status-control">
            <span>运行状态</span>
            <NSwitch
              :value="plugin.enabled"
              :loading="pendingName === plugin.name"
              :disabled="pendingName !== '' || plugin.name === 'admin' || !plugin.available"
              @update:value="togglePlugin(plugin, $event)"
            >
              <template #checked>启用</template>
              <template #unchecked>停用</template>
            </NSwitch>
            <small v-if="plugin.name === 'admin'">系统插件始终启用</small>
          </div>

          <NButton type="primary" secondary :disabled="!plugin.available" @click="openWorkspace(plugin)">
            进入工作台
          </NButton>
        </article>
      </div>
    </NSpin>
  </section>
</template>

<style scoped>
.plugins-page { width: 100%; }
.page-header { display: flex; align-items: flex-end; justify-content: space-between; gap: 1.5rem; margin-bottom: 1.5rem; }
.section-label { margin: 0 0 0.25rem; color: var(--color-primary); font-size: 0.75rem; font-weight: 700; letter-spacing: 0.04em; }
h1 { margin: 0; color: var(--color-text-primary); font-size: 1.75rem; line-height: 2.25rem; letter-spacing: -0.015em; }
.page-description { max-width: 42.5rem; margin: 0.5rem 0 0; color: var(--color-text-secondary); font-size: 0.875rem; line-height: 1.375rem; }
.page-alert { margin-bottom: 1rem; }
.plugin-list { overflow: hidden; border: 1px solid var(--color-border); border-radius: 0.5rem; background: var(--color-bg-card); }
.plugin-row { min-height: 7.5rem; display: grid; grid-template-columns: minmax(16rem, 1fr) 13rem 8rem auto; align-items: center; gap: 1.5rem; padding: 1.25rem; border-bottom: 1px solid var(--color-divider); }
.plugin-row:last-child { border-bottom: 0; }
.plugin-row:hover { background: var(--color-bg-surface); }
.plugin-title-line { display: flex; align-items: center; flex-wrap: wrap; gap: 0.5rem; }
.plugin-title-line h2 { margin: 0; color: var(--color-text-primary); font-size: 1.125rem; line-height: 1.625rem; }
.plugin-identity p { max-width: 35rem; margin: 0.375rem 0 0.5rem; color: var(--color-text-secondary); font-size: 0.8125rem; line-height: 1.25rem; }
.plugin-meta { display: flex; gap: 0.75rem; color: var(--color-text-muted); font-size: 0.75rem; }
code { color: var(--color-text-secondary); font-family: var(--font-mono); }
.priority-control, .status-control { display: grid; gap: 0.5rem; color: var(--color-text-secondary); font-size: 0.75rem; }
.priority-input { display: flex; gap: 0.5rem; }
.priority-input :deep(.n-input-number) { width: 7.5rem; }
.status-control small { color: var(--color-text-muted); line-height: 1.125rem; }
@media (max-width: 63.9375rem) {
  .plugin-row { grid-template-columns: minmax(0, 1fr) auto; }
  .priority-control, .status-control { grid-row: 2; }
}
@media (max-width: 39.9375rem) {
  .page-header { align-items: flex-start; flex-direction: column; }
  .plugin-row { grid-template-columns: 1fr; gap: 1rem; }
  .priority-control, .status-control { grid-row: auto; }
  .plugin-row > .n-button { width: 100%; min-height: 2.75rem; }
}
</style>
