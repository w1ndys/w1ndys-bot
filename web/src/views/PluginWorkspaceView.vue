<!-- 📌 影响范围：读取插件路由参数和插件 API；控制插件工作台 Tab 导航。 -->
<script setup lang="ts">
import { NAlert, NSpin, NTab, NTabs } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { listPlugins, type PluginState } from '../api'

const route = useRoute()
const router = useRouter()
const pluginName = computed(readPluginName)
const plugin = ref<PluginState | null>(null)
const loading = ref(false)
const errorMessage = ref('')

// readPluginName 读取当前插件工作台路由参数。
// @param 无。
// @returns 稳定插件名，参数异常时为空字符串。
// ⚠️副作用说明：无。
function readPluginName(): string {
  const value = route.params.pluginName
  const result = typeof value === 'string' ? value : ''

  // >>> 数据演变示例
  // 1. /plugins/ping -> ping。
  // 2. 参数缺失或数组 -> 空字符串。
  return result
}

// loadPlugin 读取当前插件元数据用于工作台标题。
// @param name：路由中的插件稳定名称。
// @returns Promise，在插件元数据更新后结束。
// ⚠️副作用说明：发起插件列表请求并修改页面状态。
async function loadPlugin(name: string): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  plugin.value = null
  try {
    const states = await listPlugins()
    for (const state of states) {
      // [决策理由] 插件稳定名称是路由与Manifest之间的唯一关联键。
      if (state.name === name) {
        plugin.value = state
        break
      }
    }
    // [决策理由] 路由指向不存在插件时应明确提示，而非显示空工作台。
    if (plugin.value === null) {
      errorMessage.value = '插件不存在或当前版本不可用'
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载插件失败'
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. name=ping+列表含ping -> plugin=ping。
  // 2. name=missing -> plugin=null -> 显示不存在。
}

// navigateTab 切换当前插件工作台功能页。
// @param name：目标命名路由。
// @returns Promise，在路由切换后结束。
// ⚠️副作用说明：改变浏览器路由。
async function navigateTab(name: string): Promise<void> {
  await router.push({ name, params: { pluginName: pluginName.value } })

  // >>> 数据演变示例
  // 1. ping+plugin-commands -> /plugins/ping/commands。
  // 2. admin+plugin-permissions -> /plugins/admin/permissions。
}

watch(pluginName, loadPlugin, { immediate: true })
</script>

<template>
  <section>
    <div class="plugin-workspace-heading">
      <div>
        <span class="eyebrow">PLUGIN WORKSPACE</span>
        <h1>{{ plugin?.display_name || pluginName }}</h1>
        <p class="muted">{{ plugin?.description || '管理插件功能、命令和权限策略。' }}</p>
      </div>
      <code>{{ pluginName }}</code>
    </div>
    <NAlert v-if="errorMessage" type="error">{{ errorMessage }}</NAlert>
    <NSpin :show="loading">
      <NTabs v-if="plugin" :value="String(route.name ?? '')" type="line" animated @update:value="navigateTab">
        <NTab name="plugin-overview">概览</NTab>
        <NTab name="plugin-features">所属功能</NTab>
        <NTab name="plugin-commands">命令管理</NTab>
        <NTab name="plugin-permissions">权限策略</NTab>
      </NTabs>
      <div class="plugin-workspace-content">
        <RouterView v-if="plugin" :key="pluginName" />
      </div>
    </NSpin>
  </section>
</template>
