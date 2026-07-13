<!-- 📌 影响范围：调用插件查询和修改 API；展示并更新后端插件运行状态。 -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { listPlugins, patchPlugin, type PluginState } from '../api'

const plugins = ref<PluginState[]>([])
const loading = ref(true)
const errorMessage = ref('')
const pendingName = ref('')

// loadPlugins 重新获取完整插件快照。
// @param 无。
// @returns Promise，在列表请求完成后结束。
// ⚠️副作用说明：发起网络请求并替换页面插件列表与错误状态。
async function loadPlugins(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    plugins.value = await listPlugins()
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
  // 1. API返回[admin,ping] -> 页面列表替换 -> loading=false。
  // 2. Token失效 -> errorMessage更新 -> loading=false。
}

// togglePlugin 切换插件启用状态并替换对应列表项。
// @param plugin：当前插件状态。
// @returns Promise，在 PATCH 请求完成后结束。
// ⚠️副作用说明：修改后端插件状态、审计记录和当前页面列表。
async function togglePlugin(plugin: PluginState): Promise<void> {
  pendingName.value = plugin.name
  errorMessage.value = ''
  try {
    const updated = await patchPlugin(plugin.name, { enabled: !plugin.enabled })
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
  // 1. ping:false -> PATCH true -> 列表ping:true。
  // 2. admin:true -> PATCH false被拒 -> 列表不变并显示错误。
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
  } finally {
    pendingName.value = ''
  }

  // >>> 数据演变示例
  // 1. ping优先级0 -> 输入100 -> PATCH -> 列表更新100。
  // 2. 输入20000 -> 后端拒绝 -> 列表保持并提示。
}

// replacePlugin 使用后端权威状态替换本地同名插件。
// @param updated：后端返回的最新插件状态。
// @returns 无。
// ⚠️副作用说明：修改响应式插件数组。
function replacePlugin(updated: PluginState): void {
  const index = plugins.value.findIndex((item) => item.name === updated.name)
  // [决策理由] 仅替换仍存在于当前列表的插件，避免并发刷新制造重复项。
  if (index >= 0) {
    plugins.value[index] = updated
  }

  // >>> 数据演变示例
  // 1. [ping旧]+ping新 -> index0替换 -> [ping新]。
  // 2. [admin]+ping新 -> index=-1 -> 列表不变。
}

onMounted(loadPlugins)
</script>

<template>
  <section>
    <div class="page-heading">
      <div>
        <span class="eyebrow">RUNTIME</span>
        <h1>插件管理</h1>
        <p class="muted">控制插件运行状态和事件处理优先级。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="loadPlugins">刷新</button>
    </div>
    <p v-if="errorMessage" class="error-message banner">{{ errorMessage }}</p>
    <div v-if="loading" class="empty-state">正在读取插件快照…</div>
    <div v-else-if="plugins.length === 0" class="empty-state">当前二进制没有可管理插件。</div>
    <div v-else class="plugin-grid">
      <article v-for="plugin in plugins" :key="plugin.name" class="panel plugin-card">
        <div class="plugin-card__header">
          <div>
            <span class="status-dot" :class="{ active: plugin.enabled }"></span>
            <span class="plugin-name">{{ plugin.display_name || plugin.name }}</span>
          </div>
          <span class="version">v{{ plugin.version }}</span>
        </div>
        <p class="muted plugin-description">{{ plugin.description || '暂无插件说明' }}</p>
        <dl class="meta-list">
          <div><dt>标识</dt><dd>{{ plugin.name }}</dd></div>
          <div><dt>可用</dt><dd>{{ plugin.available ? '是' : '否' }}</dd></div>
        </dl>
        <div class="plugin-actions">
          <label class="priority-field">
            <span>优先级</span>
            <input v-model.number="plugin.priority" type="number" min="-10000" max="10000" />
          </label>
          <button class="ghost-button" type="button" :disabled="pendingName === plugin.name" @click="savePriority(plugin)">保存</button>
          <button
            class="toggle-button"
            :class="{ enabled: plugin.enabled }"
            type="button"
            :disabled="pendingName === plugin.name || plugin.name === 'admin'"
            @click="togglePlugin(plugin)"
          >
            {{ plugin.enabled ? '已启用' : '已停用' }}
          </button>
        </div>
      </article>
    </div>
  </section>
</template>
