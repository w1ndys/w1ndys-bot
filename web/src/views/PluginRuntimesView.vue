<!-- 📌 影响范围：读写目标架构插件的全局与逐群开关；手工输入 QQ 群号。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NInput, NSpace, NSwitch, NTag, type DataTableColumns } from 'naive-ui'
import { h, onMounted, reactive, ref } from 'vue'
import { listPluginRuntimes, setPluginRuntimeEnabled, setPluginRuntimeGroupEnabled, type PluginRuntimeCommand, type PluginRuntimeGroup, type PluginRuntimeState } from '../api'
import PluginRuntimeConfigForm from '../components/PluginRuntimeConfigForm.vue'
import { useAppFeedback } from '../feedback'

const feedback = useAppFeedback()
const states = ref<PluginRuntimeState[]>([])
const loading = ref(false)
const saving = ref(false)
const errorMessage = ref('')
const groupInputs = reactive<Record<string, string>>({})
let loadSequence = 0
let mutationSequence = 0

const statusLabels: Record<string, string> = {
  disabled: '已停用',
  enabling: '启用中',
  ready: '运行中',
  disabling: '停用中',
  failed: '故障',
}

const roleLabels: Record<string, string> = {
  super_admin: '超级管理员',
  group_owner: '群主',
  group_admin: '群管理员',
  group_member: '群成员',
}

// loadStates 重新读取全部目标插件的权威运行状态。
// @param 无。
// @returns Promise<boolean>，当前请求成功完成时为 true。
// ⚠️副作用说明：发起网络请求并替换页面状态。
async function loadStates(): Promise<boolean> {
  const sequence = ++loadSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const next = await listPluginRuntimes()
    // [决策理由] 连续刷新时旧请求不得覆盖更新的快照。
    if (sequence !== loadSequence) return false
    states.value = next
  } catch (error) {
    // [决策理由] 过期请求的错误不应污染当前页面。
    if (sequence !== loadSequence) return false
    errorMessage.value = error instanceof Error ? error.message : '加载插件运行状态失败'
    return false
  } finally {
    // [决策理由] 只有最新请求可以结束 loading 状态。
    if (sequence === loadSequence) loading.value = false
  }

  // >>> 数据演变示例
  // 1. 有效会话 -> [echo{ready}] -> 渲染卡片。
  // 2. 网络失败 -> errorMessage -> false。
  return true
}

// replaceState 用写操作返回的权威状态替换列表中的同一插件。
// @param next：后端返回的插件权威状态。
// @returns 无。
// ⚠️副作用说明：替换响应式列表中的对应元素。
function replaceState(next: PluginRuntimeState): void {
  states.value = states.value.map((item) => (item.plugin_key === next.plugin_key ? next : item))

  // >>> 数据演变示例
  // 1. echo 从 v1 切到 v2 -> 列表中 echo 被替换。
  // 2. 其他插件 -> 保持原对象。
}

// changeGlobal 按乐观锁切换插件全局启用意图。
// @param state：当前插件状态；enabled：目标意图。
// @returns Promise，写入结束后完成。
// ⚠️副作用说明：写入后端状态与审计，并触发插件启停生命周期。
async function changeGlobal(state: PluginRuntimeState, enabled: boolean): Promise<void> {
  const sequence = ++mutationSequence
  saving.value = true
  try {
    const next = await setPluginRuntimeEnabled(state.plugin_key, enabled, state.version)
    // [决策理由] 只有最新写请求可以刷新列表并提示。
    if (sequence !== mutationSequence) return
    replaceState(next)
    // [决策理由] 意图已保存但生命周期失败时不能误报“已启用”，必须提示实际状态。
    if (next.status === 'failed') {
      feedback.warning(`${next.display_name} 意图已保存，但运行时进入故障状态`)
      return
    }
    feedback.success(enabled ? `${next.display_name} 已启用` : `${next.display_name} 已停用`)
  } catch (error) {
    // [决策理由] 过期写请求的错误不得覆盖新操作的反馈。
    if (sequence !== mutationSequence) return
    feedback.error(error, '切换插件开关失败', '，已重新读取最新状态')
    await loadStates()
  } finally {
    // [决策理由] 只有最新写请求可以解除单飞锁。
    if (sequence === mutationSequence) saving.value = false
  }

  // >>> 数据演变示例
  // 1. echo+true+v1 -> PATCH -> v2 且 status=ready。
  // 2. 陈旧版本 -> 409 -> 提示并重载。
}

// changeGroup 按乐观锁切换插件在单个群的开关。
// @param state：当前插件状态；groupID：群号；enabled：目标值；expectedVersion：0 表示尚无记录。
// @returns Promise，写入结束后完成。
// ⚠️副作用说明：写入后端群状态与审计，并刷新运行门禁。
async function changeGroup(state: PluginRuntimeState, groupID: number, enabled: boolean, expectedVersion: number): Promise<void> {
  const sequence = ++mutationSequence
  saving.value = true
  try {
    const next = await setPluginRuntimeGroupEnabled(state.plugin_key, groupID, enabled, expectedVersion)
    // [决策理由] 只有最新写请求可以刷新列表并提示。
    if (sequence !== mutationSequence) return
    replaceState(next)
    feedback.success(enabled ? `群 ${groupID} 已开启` : `群 ${groupID} 已关闭`)
  } catch (error) {
    // [决策理由] 过期写请求的错误不得覆盖新操作的反馈。
    if (sequence !== mutationSequence) return
    feedback.error(error, '切换群开关失败', '，已重新读取最新状态')
    await loadStates()
  } finally {
    // [决策理由] 只有最新写请求可以解除单飞锁。
    if (sequence === mutationSequence) saving.value = false
  }

  // >>> 数据演变示例
  // 1. echo+100+true+v0 -> PUT -> 新增群记录 v1。
  // 2. 群记录已存在时提交 v0 -> 409 -> 提示并重载。
}

// addGroup 为手工输入的群号新增开关记录。
// @param state：当前插件状态；enabled：新增记录的目标值。
// @returns Promise，写入结束后完成。
// ⚠️副作用说明：写入后端群状态并清空输入框。
async function addGroup(state: PluginRuntimeState, enabled: boolean): Promise<void> {
  const raw = groupInputs[state.plugin_key] ?? ''
  // [决策理由] 前端先拒绝非正整数，后端仍执行权威校验。
  if (!/^[1-9]\d{0,18}$/.test(raw)) {
    feedback.warning('请输入正确的 QQ 群号')
    return
  }
  const groupID = Number(raw)
  // [决策理由] 已存在的群号应使用列表中的开关切换，避免用 0 版本覆盖既有记录导致冲突。
  if (state.groups.some((item) => item.group_id === groupID)) {
    feedback.warning(`群 ${groupID} 已在列表中，请直接切换其开关`)
    return
  }
  await changeGroup(state, groupID, enabled, 0)
  groupInputs[state.plugin_key] = ''

  // >>> 数据演变示例
  // 1. 输入 100 -> 新增群记录 -> 清空输入框。
  // 2. 输入 abc -> 前端拒绝 -> 不发请求。
}

// statusLabel 将运行状态映射为中文文案。
// @param status：后端返回的运行状态。
// @returns 中文状态文案；未知状态原样返回。
// ⚠️副作用说明：无。
function statusLabel(status: string): string {
  const result = statusLabels[status] ?? status

  // >>> 数据演变示例
  // 1. "ready" -> "运行中"。
  // 2. 未知值 -> 原样返回。
  return result
}

// statusTagType 根据运行状态选择标签配色。
// @param status：后端返回的运行状态。
// @returns Naive UI 标签类型。
// ⚠️副作用说明：无。
function statusTagType(status: string): 'success' | 'error' | 'warning' | 'default' {
  // [决策理由] 故障必须与普通停用在视觉上区分，避免管理员误判为已正常关闭。
  if (status === 'failed') return 'error'
  if (status === 'ready') return 'success'
  if (status === 'enabling' || status === 'disabling') return 'warning'

  // >>> 数据演变示例
  // 1. "failed" -> error。
  // 2. "disabled" -> default。
  return 'default'
}

// diverged 判断管理员意图与实际运行状态是否分歧。
// @param state：当前插件状态。
// @returns 意图启用但未运行，或意图停用但仍在运行时为 true。
// ⚠️副作用说明：无。
function diverged(state: PluginRuntimeState): boolean {
  const running = state.status === 'ready'
  const result = state.desired_enabled !== running

  // >>> 数据演变示例
  // 1. desired=true,status=failed -> true。
  // 2. desired=true,status=ready -> false。
  return result
}

// groupInput 读取指定插件的群号输入值。
// @param pluginKey：稳定插件 Key。
// @returns 当前输入字符串；未输入时为空字符串。
// ⚠️副作用说明：无。
function groupInput(pluginKey: string): string {
  const result = groupInputs[pluginKey] ?? ''

  // >>> 数据演变示例
  // 1. 已输入 100 -> "100"。
  // 2. 未输入 -> ""。
  return result
}

// updateGroupInput 写入指定插件的群号输入值。
// @param pluginKey：稳定插件 Key；value：输入框最新值。
// @returns 无。
// ⚠️副作用说明：修改响应式输入表。
function updateGroupInput(pluginKey: string, value: string): void {
  groupInputs[pluginKey] = value

  // >>> 数据演变示例
  // 1. echo+"100" -> 记录输入。
  // 2. echo+"" -> 清空输入。
}

// buildGroupColumns 构建指定插件的群开关表格列。
// @param state：当前插件状态。
// @returns 绑定该插件写操作的列定义。
// ⚠️副作用说明：列中的开关会在切换时发起写请求。
function buildGroupColumns(state: PluginRuntimeState): DataTableColumns<PluginRuntimeGroup> {
  const result: DataTableColumns<PluginRuntimeGroup> = [
    { title: '群号', key: 'group_id' },
    {
      title: '群开关',
      key: 'enabled',
      render: (item) =>
        h(NSwitch, {
          value: item.enabled,
          disabled: saving.value,
          'onUpdate:value': (value: boolean) => changeGroup(state, item.group_id, value, item.version),
        }),
    },
    {
      title: '最终状态',
      key: 'effective',
      render: (item) => {
        // [决策理由] 全局未运行时群开关不生效，最终状态必须显示为未生效而不是启用。
        const effective = state.status === 'ready' && item.enabled
        return h(NTag, { type: effective ? 'success' : 'default' }, { default: () => (effective ? '生效中' : '未生效') })
      },
    },
  ]

  // >>> 数据演变示例
  // 1. status=ready+群开启 -> "生效中"。
  // 2. status=disabled+群开启 -> "未生效"。
  return result
}

const commandColumns: DataTableColumns<PluginRuntimeCommand> = [
  { title: '命令', key: 'display_name' },
  { title: '触发词', key: 'triggers', render: (item) => item.triggers.join('、') },
  { title: '允许身份', key: 'allowed_roles', render: (item) => item.allowed_roles.map((role) => roleLabels[role] ?? role).join('、') },
]

onMounted(loadStates)
</script>

<template>
  <NSpace vertical size="large">
    <NAlert v-if="errorMessage" type="error">{{ errorMessage }}</NAlert>
    <NSpace align="center" justify="space-between">
      <span>触发词与允许身份由代码持有，此处只读展示；数据库仅保存开关意图。</span>
      <NButton :disabled="loading || saving" @click="loadStates">刷新</NButton>
    </NSpace>
    <NEmpty v-if="!loading && states.length === 0" description="当前二进制没有目标架构插件" />
    <NCard v-for="state in states" :key="state.plugin_key" :title="state.display_name" :loading="loading">
      <template #header-extra>
        <NSpace align="center">
          <NTag :type="state.desired_enabled ? 'info' : 'default'">意图：{{ state.desired_enabled ? '启用' : '停用' }}</NTag>
          <NTag :type="statusTagType(state.status)">实际：{{ statusLabel(state.status) }}</NTag>
        </NSpace>
      </template>
      <NSpace vertical size="large">
        <span>{{ state.description }}</span>
        <NAlert v-if="state.last_error" type="error" title="最近一次运行错误">{{ state.last_error }}</NAlert>
        <NAlert v-else-if="diverged(state)" type="warning">管理员意图与实际运行状态不一致，插件当前不接收新调用。</NAlert>
        <NSpace align="center">
          <NSwitch :value="state.desired_enabled" :disabled="saving" @update:value="(value: boolean) => changeGlobal(state, value)" />
          <span>全局开关（关闭时保留群开关数据，但所有群均不生效）</span>
        </NSpace>
        <NDataTable :columns="commandColumns" :data="state.commands" :bordered="false" size="small" />
        <PluginRuntimeConfigForm v-if="state.has_config" :plugin-key="state.plugin_key" />
        <NSpace>
          <NInput :value="groupInput(state.plugin_key)" :disabled="saving" placeholder="手工输入 QQ 群号" @update:value="(value: string) => updateGroupInput(state.plugin_key, value)" />
          <NButton :disabled="saving" @click="addGroup(state, true)">新增并开启</NButton>
          <NButton :disabled="saving" @click="addGroup(state, false)">新增并关闭</NButton>
        </NSpace>
        <NDataTable :columns="buildGroupColumns(state)" :data="state.groups" :bordered="false" size="small" />
      </NSpace>
    </NCard>
  </NSpace>
</template>
