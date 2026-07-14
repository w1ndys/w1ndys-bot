<!-- 📌 影响范围：读取插件路由参数与插件 API；控制插件范围标题和工作台 Tab 导航。 -->
<script setup lang="ts">
import { NAlert, NButton, NEmpty, NSkeleton, NTab, NTabs } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { listPlugins, type PluginState } from '../api'

const route = useRoute()
const router = useRouter()
const pluginName = computed(readPluginName)
const plugin = ref<PluginState | null>(null)
const loading = ref(true)
const errorMessage = ref('')

// readPluginName 读取当前插件工作台的稳定路由参数。
// @param 无。
// @returns 插件稳定名称；参数异常时返回空字符串。
// ⚠️副作用说明：无。
function readPluginName(): string {
  const value = route.params.pluginName
  const result = typeof value === 'string' ? value : ''

  // >>> 数据演变示例
  // 1. route.params.pluginName="ping" -> 类型为字符串 -> "ping"。
  // 2. route.params.pluginName=["ping"] -> 类型异常 -> ""。
  return result
}

// loadPlugin 读取当前插件的权威元数据并区分错误与不存在状态。
// @param name：路由中的插件稳定名称。
// @returns Promise，在插件范围状态更新后结束。
// ⚠️副作用说明：发起插件列表请求并更新加载、错误与插件状态。
async function loadPlugin(name: string): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  plugin.value = null
  try {
    const states = await listPlugins()
    let matched: PluginState | null = null
    for (const state of states) {
      // [决策理由] 插件稳定名称是工作台路由与元数据的唯一关联键。
      if (state.name === name) {
        matched = state
        break
      }
    }
    plugin.value = matched
    // [决策理由] 不存在的插件必须与网络失败分开提示，避免管理员误判服务故障。
    if (matched === null) {
      errorMessage.value = '插件不存在或未包含在当前部署版本中。'
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载插件工作台失败'
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. name="ping" -> 列表匹配 ping -> plugin=ping、loading=false。
  // 2. name="missing" -> 无匹配项 -> plugin=null、显示不存在提示。
}

// navigateTab 切换当前插件工作台的局部功能页。
// @param name：目标命名路由。
// @returns Promise，在路由切换后结束。
// ⚠️副作用说明：修改浏览器 URL 和当前嵌套路由。
async function navigateTab(name: string): Promise<void> {
  await router.push({ name, params: { pluginName: pluginName.value } })

  // >>> 数据演变示例
  // 1. ping + plugin-features -> /plugins/ping/features -> 功能页。
  // 2. admin + plugin-permissions -> /plugins/admin/permissions -> 权限页。
}

// retryLoadPlugin 重试读取当前插件范围。
// @param 无；读取当前路由插件名。
// @returns Promise，在重试完成后结束。
// ⚠️副作用说明：重新发起插件列表请求并刷新页面状态。
async function retryLoadPlugin(): Promise<void> {
  await loadPlugin(pluginName.value)

  // >>> 数据演变示例
  // 1. 首次网络失败 -> 点击重试 -> 请求成功 -> 展示工作台。
  // 2. 插件确实不存在 -> 点击重试 -> 仍显示不存在提示。
}

watch(pluginName, loadPlugin, { immediate: true })
</script>

<template>
  <section class="workspace-shell" :aria-busy="loading">
    <header class="workspace-header">
      <div class="workspace-heading">
        <p class="workspace-breadcrumb">插件管理 / <code>{{ pluginName || '未知插件' }}</code></p>
        <div class="workspace-title-row">
          <h1>{{ plugin?.display_name || pluginName || '插件工作台' }}</h1>
          <span v-if="plugin" class="workspace-scope">当前范围：{{ plugin.name }}</span>
        </div>
        <p class="workspace-description">{{ plugin?.description || '查看插件能力并管理该插件的运行配置、命令与权限。' }}</p>
      </div>
    </header>

    <div v-if="loading" class="workspace-loading" aria-label="正在加载插件工作台">
      <NSkeleton text width="38%" />
      <NSkeleton text :repeat="2" />
    </div>

    <NAlert v-else-if="errorMessage" class="workspace-alert" type="error" title="无法打开插件工作台">
      <div class="workspace-error-content">
        <span>{{ errorMessage }}</span>
        <NButton size="small" secondary @click="retryLoadPlugin">重新加载</NButton>
      </div>
    </NAlert>

    <template v-else-if="plugin">
      <NTabs :value="String(route.name ?? '')" class="workspace-tabs" type="line" @update:value="navigateTab">
        <NTab name="plugin-overview">概览</NTab>
        <NTab name="plugin-features">所属功能</NTab>
        <NTab name="plugin-commands">命令管理</NTab>
        <NTab name="plugin-permissions">权限策略</NTab>
      </NTabs>
      <div class="workspace-content">
        <RouterView :key="pluginName" />
      </div>
    </template>

    <NEmpty v-else description="没有可展示的插件数据" />
  </section>
</template>

<style scoped>
.workspace-shell { min-width: 0; }
.workspace-header { margin-bottom: var(--space-6); }
.workspace-heading { max-width: 42.5rem; }
.workspace-breadcrumb { margin: 0 0 var(--space-2); color: var(--color-text-muted); font-size: var(--font-size-body-sm); line-height: var(--line-height-body-sm); }
.workspace-breadcrumb code, .workspace-scope { font-family: var(--font-mono); }
.workspace-title-row { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.workspace-title-row h1 { margin: 0; color: var(--color-text-primary); font-size: var(--font-size-h1); line-height: var(--line-height-h1); letter-spacing: var(--letter-spacing-tight); }
.workspace-scope { padding: var(--space-1) var(--space-2); border: 0.0625rem solid var(--color-border); border-radius: var(--radius-sm); color: var(--color-text-secondary); font-size: var(--font-size-caption); line-height: var(--line-height-caption); }
.workspace-description { margin: var(--space-2) 0 0; color: var(--color-text-secondary); font-size: var(--font-size-body); line-height: var(--line-height-body); }
.workspace-loading { display: grid; gap: var(--space-3); padding: var(--space-5); border: 0.0625rem solid var(--color-border); border-radius: var(--radius-md); background: var(--color-bg-card); }
.workspace-error-content { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); }
.workspace-tabs { border-bottom: 0.0625rem solid var(--color-border); }
.workspace-content { padding-top: var(--space-6); }
@media (max-width: 39.9375rem) {
  .workspace-header { margin-bottom: var(--space-4); }
  .workspace-title-row h1 { font-size: var(--font-size-h2); line-height: var(--line-height-h2); }
  .workspace-error-content { align-items: flex-start; flex-direction: column; }
  .workspace-tabs { overflow-x: auto; }
  .workspace-content { padding-top: var(--space-4); }
}
</style>
