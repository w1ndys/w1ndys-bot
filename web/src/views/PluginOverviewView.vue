<!-- 📌 影响范围：读取插件 API；修改当前插件启用状态、优先级和审计记录。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NDescriptions, NDescriptionsItem, NInputNumber, NSwitch, NTag } from 'naive-ui'
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { listPlugins, patchPlugin, type PluginState } from '../api'

const route = useRoute()
const plugin = ref<PluginState | null>(null)
const pending = ref(false)
const errorMessage = ref('')

// loadPluginOverview 读取路由指定插件的权威状态。
// @param 无。
// @returns Promise，在概览状态更新后结束。
// ⚠️副作用说明：发起插件列表请求并修改页面状态。
async function loadPluginOverview(): Promise<void> {
  const name = String(route.params.pluginName ?? '')
  errorMessage.value = ''
  try {
    const states = await listPlugins()
    for (const state of states) {
      // [决策理由] 只展示当前插件工作台对应的状态。
      if (state.name === name) {
        plugin.value = state
        return
      }
    }
    errorMessage.value = '插件不存在'
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载插件概览失败'
  }

  // >>> 数据演变示例
  // 1. route=ping+列表含ping -> plugin=ping。
  // 2. route=missing -> 显示插件不存在。
}

// savePriority 独立保存插件事件优先级。
// @param 无；读取当前插件响应式状态。
// @returns Promise，在优先级保存后结束。
// ⚠️副作用说明：更新数据库优先级、审计记录和运行时快照。
async function savePriority(): Promise<void> {
  // [决策理由] 插件未加载或已有保存进行中时不能发起写操作。
  if (plugin.value === null || pending.value) {
    return
  }
  pending.value = true
  errorMessage.value = ''
  try {
    plugin.value = await patchPlugin(plugin.value.name, { priority: Number(plugin.value.priority) })
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存插件优先级失败'
    await loadPluginOverview()
  } finally {
    pending.value = false
  }

  // >>> 数据演变示例
  // 1. ping优先级10 -> PATCH priority -> 状态刷新。
  // 2. 优先级越界 -> 显示错误 -> 重载权威状态。
}

// saveEnabled 独立保存插件启用状态。
// @param 无；读取当前插件响应式启用状态。
// @returns Promise，在启用状态保存后结束。
// ⚠️副作用说明：更新数据库启用状态、审计记录和运行时快照。
async function saveEnabled(): Promise<void> {
  // [决策理由] 插件未加载或已有保存进行中时不能发起写操作。
  if (plugin.value === null || pending.value) {
    return
  }
  pending.value = true
  errorMessage.value = ''
  try {
    plugin.value = await patchPlugin(plugin.value.name, { enabled: plugin.value.enabled })
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '保存插件启用状态失败'
    await loadPluginOverview()
  } finally {
    pending.value = false
  }

  // >>> 数据演变示例
  // 1. ping false切true -> PATCH enabled -> 状态刷新。
  // 2. admin尝试禁用 -> 后端拒绝 -> 重载为启用。
}

onMounted(loadPluginOverview)
</script>

<template>
  <NAlert v-if="errorMessage" class="workspace-alert" type="error">{{ errorMessage }}</NAlert>
  <NCard v-if="plugin" title="运行配置" embedded>
    <NDescriptions label-placement="left" :column="1">
      <NDescriptionsItem label="版本">v{{ plugin.version }}</NDescriptionsItem>
      <NDescriptionsItem label="可用状态"><NTag :type="plugin.available ? 'success' : 'error'">{{ plugin.available ? '可用' : '不可用' }}</NTag></NDescriptionsItem>
      <NDescriptionsItem label="启用插件"><div class="inline-setting"><NSwitch v-model:value="plugin.enabled" :disabled="plugin.name === 'admin' || pending" /><NButton size="small" secondary :disabled="plugin.name === 'admin' || pending" @click="saveEnabled">保存启停</NButton></div></NDescriptionsItem>
      <NDescriptionsItem label="事件优先级"><div class="inline-setting"><NInputNumber v-model:value="plugin.priority" :min="-10000" :max="10000" /><NButton size="small" secondary :loading="pending" @click="savePriority">保存优先级</NButton></div></NDescriptionsItem>
      <NDescriptionsItem label="当前配置"><pre class="config-preview">{{ JSON.stringify(plugin.config, null, 2) }}</pre></NDescriptionsItem>
    </NDescriptions>
  </NCard>
</template>
