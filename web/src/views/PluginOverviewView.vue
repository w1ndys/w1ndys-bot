<!-- 📌 影响范围：读取插件 API；独立修改当前插件启用状态和优先级，并产生审计记录与运行时热更新。 -->
<script setup lang="ts">
import { NAlert, NButton, NDescriptions, NDescriptionsItem, NEmpty, NInputNumber, NSkeleton, NSwitch, NTag } from 'naive-ui'
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { listPlugins, patchPlugin, type PluginState } from '../api'

const route = useRoute()
const plugin = ref<PluginState | null>(null)
const loading = ref(true)
const enabledSaving = ref(false)
const prioritySaving = ref(false)
const enabledDraft = ref(false)
const priorityDraft = ref<number | null>(0)
const errorMessage = ref('')
const successMessage = ref('')

// applyPluginState 将服务端权威状态同步到展示值和两个独立草稿。
// @param state：服务端返回的插件状态。
// @returns 无。
// ⚠️副作用说明：覆盖页面插件状态、启用草稿和优先级草稿。
function applyPluginState(state: PluginState): void {
  plugin.value = state
  enabledDraft.value = state.enabled
  priorityDraft.value = state.priority

  // >>> 数据演变示例
  // 1. state={enabled:true,priority:10} -> 两个草稿分别为 true、10。
  // 2. 保存后 state={enabled:false,priority:5} -> 页面权威状态与草稿同步。
}

// loadPluginOverview 读取路由指定插件的权威运行状态。
// @param 无。
// @returns Promise，在概览状态更新后结束。
// ⚠️副作用说明：发起插件列表请求并覆盖页面加载、错误与插件状态。
async function loadPluginOverview(): Promise<void> {
  const name = String(route.params.pluginName ?? '')
  loading.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const states = await listPlugins()
    let matched: PluginState | null = null
    for (const state of states) {
      // [决策理由] 插件稳定名称是概览路由与运行状态的唯一关联键。
      if (state.name === name) {
        matched = state
        break
      }
    }
    // [决策理由] 仅同步当前路由插件，防止跨插件状态误编辑。
    if (matched !== null) {
      applyPluginState(matched)
    } else {
      plugin.value = null
      errorMessage.value = '插件不存在或未编译进当前部署。'
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载插件概览失败'
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. route=ping -> 列表匹配 -> 同步权威状态与编辑草稿。
  // 2. API失败 -> 保留空状态 -> 显示错误与重试入口。
}

// savePriority 独立保存插件事件优先级。
// @param 无；读取当前插件和优先级草稿。
// @returns Promise，在优先级保存或恢复权威值后结束。
// ⚠️副作用说明：更新数据库、审计记录和运行时快照；失败时恢复服务端状态。
async function savePriority(): Promise<void> {
  // [决策理由] 空优先级、未加载状态或重复保存都不能形成有效写请求。
  if (plugin.value === null || priorityDraft.value === null || prioritySaving.value) {
    return
  }
  prioritySaving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const updated = await patchPlugin(plugin.value.name, { priority: priorityDraft.value })
    applyPluginState(updated)
    successMessage.value = '优先级已保存并热更新。'
  } catch (error) {
    const failureMessage = error instanceof Error ? error.message : '保存插件优先级失败'
    await loadPluginOverview()
    errorMessage.value = failureMessage
  } finally {
    prioritySaving.value = false
  }

  // >>> 数据演变示例
  // 1. priorityDraft=20 -> PATCH priority -> 权威值=20、提示已热更新。
  // 2. priorityDraft越界 -> API拒绝 -> 重载权威值、显示错误。
}

// saveEnabled 独立保存插件启用状态。
// @param 无；读取当前插件和启用草稿。
// @returns Promise，在启用状态保存或恢复权威值后结束。
// ⚠️副作用说明：更新数据库、审计记录和运行时快照；失败时恢复服务端状态。
async function saveEnabled(): Promise<void> {
  // [决策理由] 未加载、系统插件或重复保存时禁止发送启停请求。
  if (plugin.value === null || plugin.value.name === 'admin' || enabledSaving.value) {
    return
  }
  enabledSaving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const updated = await patchPlugin(plugin.value.name, { enabled: enabledDraft.value })
    applyPluginState(updated)
    successMessage.value = `插件已${updated.enabled ? '启用' : '停用'}并热更新。`
  } catch (error) {
    const failureMessage = error instanceof Error ? error.message : '保存插件启用状态失败'
    await loadPluginOverview()
    errorMessage.value = failureMessage
  } finally {
    enabledSaving.value = false
  }

  // >>> 数据演变示例
  // 1. ping启用草稿=false -> PATCH enabled -> 插件停用并热更新。
  // 2. admin启用草稿=false -> 前置阻止 -> 不发送请求。
}

onMounted(loadPluginOverview)
</script>

<template>
  <section class="overview-page" :aria-busy="loading">
    <div v-if="loading" class="overview-loading" aria-label="正在加载插件概览">
      <NSkeleton text :repeat="4" />
    </div>

    <template v-else>
      <NAlert v-if="errorMessage" class="workspace-alert" type="error" title="操作未完成">
        <div class="alert-content">
          <span>{{ errorMessage }}</span>
          <NButton size="small" secondary @click="loadPluginOverview">重新加载</NButton>
        </div>
      </NAlert>
      <NAlert v-if="successMessage" class="workspace-alert" type="success" closable @close="successMessage = ''">
        {{ successMessage }}
      </NAlert>

      <template v-if="plugin">
        <section class="overview-section">
          <div class="section-heading">
            <div>
              <h2>插件状态</h2>
              <p>状态来自当前部署和插件 Manifest，用于确认功能是否已编译并可运行。</p>
            </div>
            <NTag :type="plugin.available ? 'success' : 'error'">{{ plugin.available ? '当前部署可用' : '当前部署不可用' }}</NTag>
          </div>
          <NDescriptions class="status-descriptions" label-placement="left" :column="2" bordered>
            <NDescriptionsItem label="插件键"><code>{{ plugin.name }}</code></NDescriptionsItem>
            <NDescriptionsItem label="运行状态"><NTag :type="plugin.enabled ? 'success' : 'default'">{{ plugin.enabled ? '已启用' : '已停用' }}</NTag></NDescriptionsItem>
            <NDescriptionsItem label="配置来源"><NTag>数据库覆盖与 Manifest 默认值</NTag></NDescriptionsItem>
          </NDescriptions>
        </section>

        <section class="overview-section">
          <div class="section-heading">
            <div>
              <h2>运行配置</h2>
              <p>启停和优先级是两项独立配置，分别保存并立即热更新。</p>
            </div>
          </div>
          <div class="setting-list">
            <div class="setting-row">
              <div class="setting-copy">
                <h3>启用插件</h3>
                <p>{{ plugin.name === 'admin' ? '系统管理插件必须保持启用，无法在控制台停用。' : '停用后该插件不再处理新的 OneBot 事件。' }}</p>
              </div>
              <div class="setting-control">
                <NSwitch v-model:value="enabledDraft" :disabled="plugin.name === 'admin' || enabledSaving" aria-label="启用插件" />
                <NButton type="primary" size="small" :loading="enabledSaving" :disabled="plugin.name === 'admin' || enabledDraft === plugin.enabled" @click="saveEnabled">保存启停</NButton>
              </div>
            </div>
            <div class="setting-row">
              <div class="setting-copy">
                <h3>事件优先级</h3>
                <p>数值越小越先参与事件分发；范围为 -10000 至 10000。</p>
              </div>
              <div class="setting-control priority-control">
                <NInputNumber v-model:value="priorityDraft" :min="-10000" :max="10000" :disabled="prioritySaving" aria-label="事件优先级" />
                <NButton type="primary" size="small" :loading="prioritySaving" :disabled="priorityDraft === null || priorityDraft === plugin.priority" @click="savePriority">保存优先级</NButton>
              </div>
            </div>
          </div>
        </section>

        <section class="overview-section">
          <div class="section-heading">
            <div>
              <h2>当前配置快照</h2>
              <p>配置由后端管理，此处只读展示，不代表可以直接编辑 Manifest。</p>
            </div>
            <NTag type="info">只读</NTag>
          </div>
          <pre class="config-preview">{{ JSON.stringify(plugin.config, null, 2) }}</pre>
        </section>
      </template>

      <NEmpty v-else-if="!errorMessage" description="没有可展示的插件概览" />
    </template>
  </section>
</template>

<style scoped>
.overview-page { display: grid; gap: var(--space-4); }
.overview-loading, .overview-section { padding: var(--space-5); border: 0.0625rem solid var(--color-border); border-radius: var(--radius-md); background: var(--color-bg-card); }
.overview-loading { display: grid; gap: var(--space-3); }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); margin-bottom: var(--space-4); }
.section-heading h2, .setting-copy h3 { margin: 0; color: var(--color-text-primary); }
.section-heading h2 { font-size: var(--font-size-h3); line-height: var(--line-height-h3); }
.section-heading p, .setting-copy p { margin: var(--space-1) 0 0; color: var(--color-text-muted); font-size: var(--font-size-body-sm); line-height: var(--line-height-body-sm); }
.status-descriptions code, .config-preview { font-family: var(--font-mono); }
.setting-list { border-top: 0.0625rem solid var(--color-border); }
.setting-row { display: flex; align-items: center; justify-content: space-between; gap: var(--space-6); padding: var(--space-4) 0; border-bottom: 0.0625rem solid var(--color-border); }
.setting-copy { max-width: 35rem; }
.setting-copy h3 { font-size: var(--font-size-body); line-height: var(--line-height-body); }
.setting-control { display: flex; align-items: center; flex: 0 0 auto; gap: var(--space-3); }
.priority-control { width: 18rem; }
.priority-control :deep(.n-input-number) { flex: 1; }
.config-preview { max-height: 24rem; margin: 0; padding: var(--space-4); overflow: auto; border: 0.0625rem solid var(--color-border); border-radius: var(--radius-sm); background: var(--color-bg-canvas); color: var(--color-text-secondary); font-size: var(--font-size-body-sm); line-height: var(--line-height-body-sm); white-space: pre-wrap; overflow-wrap: anywhere; }
.alert-content { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); }
@media (max-width: 39.9375rem) {
  .overview-loading, .overview-section { padding: var(--space-4); }
  .setting-row { align-items: stretch; flex-direction: column; gap: var(--space-3); }
  .setting-control, .priority-control { justify-content: space-between; width: 100%; }
  .alert-content { align-items: flex-start; flex-direction: column; }
}
</style>
