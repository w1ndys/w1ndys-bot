<!-- 📌 影响范围：读写插件群默认策略与单群覆盖；手工输入 QQ 群号。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NInput, NSpace, NSwitch, NTag, useMessage, type DataTableColumns } from 'naive-ui'
import { computed, h, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { deletePluginGroupOverride, getPluginGroupControl, setPluginGroupDefault, setPluginGroupOverride, type PluginGroupControlState, type PluginGroupOverride } from '../api'

const route = useRoute()
const message = useMessage()

// readPluginName 读取当前路由的插件稳定名称。
// @param 无；读取 route.params.pluginName。
// @returns 字符串路由参数；类型异常时返回空字符串。
// ⚠️副作用说明：无。
function readPluginName(): string {
  const value = route.params.pluginName
  const result = typeof value === 'string' ? value : ''

  // >>> 数据演变示例
  // 1. "keyword_reply" -> 字符串 -> "keyword_reply"。
  // 2. ["keyword_reply"] -> 类型异常 -> ""。
  return result
}

const pluginName = computed(readPluginName)
const state = ref<PluginGroupControlState | null>(null)
const groupID = ref('')
const loading = ref(false)
const errorMessage = ref('')
let loadSequence = 0
let mutationSequence = 0
const saving = ref(false)

// loadState 加载当前插件群控制快照。
// @param name：路由插件名。
// @returns Promise<boolean>，快照成功更新且仍属于当前路由时为 true。
// ⚠️副作用说明：发起网络请求并更新页面状态。
async function loadState(name: string): Promise<boolean> {
  const sequence = ++loadSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const next = await getPluginGroupControl(name)
    // [决策理由] 快速切换插件时旧请求不得覆盖新页面。
    if (sequence !== loadSequence || name !== pluginName.value) return false
    state.value = next
  } catch (error) {
    // [决策理由] 过期请求的错误也不应污染当前插件页面。
    if (sequence !== loadSequence || name !== pluginName.value) return false
    errorMessage.value = error instanceof Error ? error.message : '加载群控制失败'
    return false
  } finally {
    // [决策理由] 只有当前请求可以结束当前页的 loading 状态。
    if (sequence === loadSequence && name === pluginName.value) loading.value = false
  }

  // >>> 数据演变示例
  // 1. keyword_reply -> API快照 -> 渲染。
  // 2. 404 -> errorMessage -> false。
  return true
}

// changeDefault 保存群默认开关。
// @param enabled：目标值。
// @returns Promise，保存后结束。
// ⚠️副作用说明：写入后端并替换快照。
async function changeDefault(enabled: boolean): Promise<void> {
  // [决策理由] 未加载版本时不能执行 CAS。
  if (state.value === null) return
  const name = pluginName.value
  const sequence = ++mutationSequence
  saving.value = true
  try {
    await setPluginGroupDefault(name, enabled, state.value.default_version)
    // [决策理由] 只有当前插件的最新写请求可以刷新并提示。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    const refreshed = await loadState(name)
    // [决策理由] 写入成功但权威重读失败时不得误报“已更新”。
    if (!refreshed) return
    message.success('群默认策略已更新')
  } catch (error) {
    // [决策理由] 过期插件写入的错误不得污染新路由。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    message.error(error instanceof Error ? error.message : '更新失败')
    await loadState(name)
  } finally {
    // [决策理由] 只有最新写请求可以解除单飞锁。
    if (sequence === mutationSequence) saving.value = false
  }

  // >>> 数据演变示例
  // 1. true/v2 -> PATCH -> false/v3。
  // 2. 冲突 -> 提示 -> 重载。
}

// addOverride 为手工输入群号新增覆盖。
// @param enabled：单群目标值。
// @returns Promise，保存后结束。
// ⚠️副作用说明：写入后端并重载列表。
async function addOverride(enabled: boolean): Promise<void> {
  // [决策理由] 前端先拒绝非正整数，后端仍执行权威校验。
  if (!/^[1-9]\d{0,19}$/.test(groupID.value)) {
    message.error('请输入正确的 QQ 群号')
    return
  }
  const name = pluginName.value
  const sequence = ++mutationSequence
  saving.value = true
  try {
    await setPluginGroupOverride(name, groupID.value, enabled, 0)
    // [决策理由] 路由切换后不得清空新插件页的输入。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    groupID.value = ''
    await loadState(name)
  } catch (error) {
    // [决策理由] 只展示当前写请求错误。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    message.error(error instanceof Error ? error.message : '新增失败')
  } finally {
    // [决策理由] 最新写请求结束后恢复控件。
    if (sequence === mutationSequence) saving.value = false
  }

  // >>> 数据演变示例
  // 1. 100+true -> PUT v0 -> 列表刷新。
  // 2. abc -> 前端拒绝。
}

// changeOverride 使用 CAS 切换已有覆盖。
// @param item：当前覆盖；enabled：目标值。
// @returns Promise，保存后结束。
// ⚠️副作用说明：写入后端并重载列表。
async function changeOverride(item: PluginGroupOverride, enabled: boolean): Promise<void> {
  const name = pluginName.value
  const sequence = ++mutationSequence
  saving.value = true
  try {
    await setPluginGroupOverride(name, item.group_id, enabled, item.version)
    // [决策理由] 只刷新发起写操作的同一插件页。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    await loadState(name)
  } catch (error) {
    // [决策理由] 过期错误不得触发新路由重载。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    message.error(error instanceof Error ? error.message : '更新失败')
    await loadState(name)
  } finally {
    // [决策理由] 最新写请求完成后解锁。
    if (sequence === mutationSequence) saving.value = false
  }

  // >>> 数据演变示例
  // 1. group100/v1 -> false -> v2。
  // 2. 冲突 -> 重载。
}

// removeOverride 删除覆盖并恢复继承。
// @param item：当前覆盖。
// @returns Promise，删除后结束。
// ⚠️副作用说明：删除后端覆盖并重载列表。
async function removeOverride(item: PluginGroupOverride): Promise<void> {
  const name = pluginName.value
  const sequence = ++mutationSequence
  saving.value = true
  try {
    await deletePluginGroupOverride(name, item.group_id, item.version)
    // [决策理由] 只重载发起删除的同一插件页。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    await loadState(name)
  } catch (error) {
    // [决策理由] 过期删除错误不得污染新路由。
    if (sequence !== mutationSequence || name !== pluginName.value) return
    message.error(error instanceof Error ? error.message : '删除失败')
  } finally {
    // [决策理由] 最新删除请求完成后解锁。
    if (sequence === mutationSequence) saving.value = false
  }

  // >>> 数据演变示例
  // 1. group100/v2 -> DELETE -> 继承默认。
  // 2. 冲突 -> 保留当前行。
}

// renderOverrideSwitch 渲染单群覆盖开关。
// @param item：当前覆盖行。
// @returns 绑定当前值、保存状态与更新处理器的 NSwitch VNode。
// ⚠️副作用说明：用户切换后调用 changeOverride 发起写请求。
function renderOverrideSwitch(item: PluginGroupOverride) {
  const result = h(NSwitch, { value: item.enabled, disabled: saving.value, 'onUpdate:value': changeOverride.bind(undefined, item) })

  // >>> 数据演变示例
  // 1. enabled=true,saving=false -> 可交互开关。
  // 2. saving=true -> disabled开关。
  return result
}

// effectiveLabel 计算单群覆盖的最终文本。
// @param item：当前覆盖行。
// @returns 同时满足插件全局启用和覆盖启用时为“启用”，否则为“停用”。
// ⚠️副作用说明：无。
function effectiveLabel(item: PluginGroupOverride): string {
  const result = state.value?.plugin_enabled && item.enabled ? '启用' : '停用'

  // >>> 数据演变示例
  // 1. global=true,override=true -> "启用"。
  // 2. global=false,override=true -> "停用"。
  return result
}

// renderEffectiveTag 渲染单群最终状态标签。
// @param item：当前覆盖行。
// @returns 根据全局开关与覆盖值着色的 NTag VNode。
// ⚠️副作用说明：无。
function renderEffectiveTag(item: PluginGroupOverride) {
  const enabled = Boolean(state.value?.plugin_enabled && item.enabled)
  const result = h(NTag, { type: enabled ? 'success' : 'default' }, { default: effectiveLabel.bind(undefined, item) })

  // >>> 数据演变示例
  // 1. 最终启用 -> success标签。
  // 2. 最终停用 -> default标签。
  return result
}

// inheritActionLabel 返回删除覆盖操作文本。
// @param 无。
// @returns 固定文本“恢复继承”。
// ⚠️副作用说明：无。
function inheritActionLabel(): string {
  const result = '恢复继承'

  // >>> 数据演变示例
  // 1. 已启用覆盖 -> "恢复继承"。
  // 2. 已停用覆盖 -> "恢复继承"。
  return result
}

// renderOverrideAction 渲染恢复继承按钮。
// @param item：当前覆盖行。
// @returns 绑定删除处理器与保存禁用状态的 NButton VNode。
// ⚠️副作用说明：用户点击后调用 removeOverride 删除覆盖。
function renderOverrideAction(item: PluginGroupOverride) {
  const result = h(NButton, { size: 'small', disabled: saving.value, onClick: removeOverride.bind(undefined, item) }, { default: inheritActionLabel })

  // >>> 数据演变示例
  // 1. saving=false -> 可点击恢复继承。
  // 2. saving=true -> 按钮禁用。
  return result
}

// handlePluginNameChange 处理插件路由切换。
// @param name：新路由插件名。
// @returns 无。
// ⚠️副作用说明：使过期写请求失效、解除页面写锁并发起新读请求。
function handlePluginNameChange(name: string): void {
  mutationSequence++
  saving.value = false
  void loadState(name)

  // >>> 数据演变示例
  // 1. keyword_reply -> echo -> 旧mutation失效 -> 加载echo。
  // 2. 首次进入keyword_reply -> 加载群策略。
}

const columns: DataTableColumns<PluginGroupOverride> = [
  { title: '群号', key: 'group_id' },
  { title: '策略', key: 'enabled', render: renderOverrideSwitch },
  { title: '最终状态', key: 'effective', render: renderEffectiveTag },
  { title: '操作', key: 'actions', render: renderOverrideAction },
]

watch(pluginName, handlePluginNameChange, { immediate: true })
</script>

<template>
  <NSpace vertical size="large">
    <NAlert v-if="errorMessage" type="error">{{ errorMessage }}</NAlert>
    <NAlert v-if="state && !state.plugin_enabled" type="warning">插件全局已关闭，所有群的最终状态均为停用。</NAlert>
    <NCard title="群默认策略" :loading="loading">
      <NSpace align="center"><NSwitch v-if="state" :value="state.default_enabled" :disabled="saving" @update:value="changeDefault" /><span>{{ state?.default_enabled ? '未覆盖的群默认启用' : '未覆盖的群默认停用' }}</span></NSpace>
    </NCard>
    <NCard title="新增单群覆盖">
      <NSpace><NInput v-model:value="groupID" :disabled="saving" placeholder="手工输入 QQ 群号" /><NButton :disabled="saving" @click="addOverride(true)">设为启用</NButton><NButton :disabled="saving" @click="addOverride(false)">设为停用</NButton></NSpace>
    </NCard>
    <NDataTable :columns="columns" :data="state?.overrides ?? []" :loading="loading" />
  </NSpace>
</template>
