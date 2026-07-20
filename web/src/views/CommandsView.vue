<!-- 📌 影响范围：调用插件、功能与命令 API；修改后端功能触发词、审计记录和命令快照。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NForm, NFormItem, NInput, NModal, NRadioButton, NRadioGroup, NSelect, NSkeleton, NSpin, NTable, NTag } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  createCommand,
  deleteCommand,
  listCommands,
  listPluginFeatures,
  listPlugins,
  renameCommand,
  type CommandState,
  type FeatureState,
  type PluginState,
} from '../api'
import { useAppFeedback } from '../feedback'

const route = useRoute()
const fixedPluginName = String(route.params.pluginName ?? '')
const commands = ref<CommandState[]>([])
const plugins = ref<PluginState[]>([])
const features = ref<FeatureState[]>([])
const selectedPlugin = ref(fixedPluginName)
const selectedFeature = ref('')
const scopeType = ref<'global' | 'group'>('global')
const scopeID = ref('0')
const commandText = ref('')
const loading = ref(true)
const featureLoading = ref(false)
const saving = ref(false)
const pendingID = ref(0)
const errorMessage = ref('')
const featureRequestSequence = ref(0)
const featureLoaded = ref(false)
const deleteTarget = ref<CommandState | null>(null)
const feedback = useAppFeedback()

const pluginOptions = computed(buildPluginOptions)
const featureOptions = computed(buildFeatureOptions)
const groupIDError = computed(hasGroupIDError)
const commandError = computed(hasCommandError)

// buildPluginOptions 将可用插件转换为Select选项。
// @param 无；读取响应式插件列表。
// @returns 插件标签和值列表。
// ⚠️副作用说明：无。
function buildPluginOptions(): Array<{ label: string; value: string }> {
  const result: Array<{ label: string; value: string }> = []
  for (const item of plugins.value) {
    result.push({ label: item.display_name || item.name, value: item.name })
  }

  // >>> 数据演变示例
  // 1. [ping] -> [{label:Ping,value:ping}]。
  // 2. [] -> []。
  return result
}

// buildFeatureOptions 将功能元数据转换为Select选项。
// @param 无；读取响应式功能列表。
// @returns 功能展示名和稳定键列表。
// ⚠️副作用说明：无。
function buildFeatureOptions(): Array<{ label: string; value: string }> {
  const result: Array<{ label: string; value: string }> = []
  for (const item of features.value) {
    result.push({ label: `${item.display_name || item.key} · ${item.key}`, value: item.key })
  }

  // >>> 数据演变示例
  // 1. ping功能 -> Ping · ping选项。
  // 2. 空功能 -> 空选项。
  return result
}

// hasGroupIDError 判断当前群号输入是否已经形成错误。
// @param 无；读取响应式作用域和群号。
// @returns 群级非空输入无效时为true。
// ⚠️副作用说明：无。
function hasGroupIDError(): boolean {
  const result = scopeType.value === 'group' && scopeID.value !== '' && !isPositiveUint64(scopeID.value)

  // >>> 数据演变示例
  // 1. group+abc -> true。
  // 2. global+0 -> false。
  return result
}

// hasCommandError 判断触发词是否只含空白。
// @param 无；读取响应式触发词。
// @returns 已输入但trim后为空时为true。
// ⚠️副作用说明：无。
function hasCommandError(): boolean {
  const result = commandText.value.length > 0 && commandText.value.trim() === ''

  // >>> 数据演变示例
  // 1. "   " -> true。
  // 2. "ping"或空串 -> false。
  return result
}

// loadPage 并行加载插件和触发词，并保持插件工作台的数据隔离。
// @param 无。
// @returns Promise，在页面基础数据加载结束后完成。
// ⚠️副作用说明：发起插件、命令和功能网络请求并修改页面状态。
async function loadPage(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const [pluginStates, commandStates] = await Promise.all([listPlugins(), listCommands()])
    plugins.value = []
    for (const item of pluginStates) {
      // [决策理由] 不可用插件不能成为新命令目标。
      if (item.available) {
        plugins.value.push(item)
      }
    }
    commands.value = []
    for (const item of commandStates) {
      // [决策理由] 工作台只允许当前插件命令进入可编辑列表。
      if (fixedPluginName === '' || item.plugin_name === fixedPluginName) {
        commands.value.push(item)
      }
    }
    // [决策理由] 非插件工作台入口需要安全默认值，避免表单指向空插件。
    if (selectedPlugin.value === '' && plugins.value.length > 0) {
      selectedPlugin.value = plugins.value[0].name
    }
    await loadFeatures(selectedPlugin.value)
  } catch (error) {
    setError(error, '加载功能触发词失败')
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. 固定插件ping+全量命令 -> 过滤非ping项 -> 展示ping命令。
  // 2. API失败 -> 保留空列表并显示错误 -> loading=false。
}

// loadFeatures 读取选中插件可用功能并防止请求乱序覆盖。
// @param pluginName：当前选中的插件稳定名称。
// @returns Promise，在最新功能请求完成后结束。
// ⚠️副作用说明：发起功能元数据请求并重置功能选择。
async function loadFeatures(pluginName: string): Promise<void> {
  const sequence = ++featureRequestSequence.value
  featureLoading.value = pluginName !== ''
  featureLoaded.value = false
  features.value = []
  selectedFeature.value = ''
  // [决策理由] 空插件没有合法查询目标，应直接清除加载态。
  if (pluginName === '') {
    featureLoading.value = false
    return
  }
  try {
    const states = await listPluginFeatures(pluginName)
    const loaded: FeatureState[] = []
    for (const item of states) {
      // [决策理由] 不可用功能不能绑定新命令。
      if (item.available) {
        loaded.push(item)
      }
    }
    // [决策理由] 快速切换插件时，过期响应不得污染当前插件表单。
    if (sequence !== featureRequestSequence.value || selectedPlugin.value !== pluginName) {
      return
    }
    features.value = loaded
    featureLoaded.value = true
    // [决策理由] 命令必须绑定真实功能，默认选择首个可用项。
    if (loaded.length > 0) {
      selectedFeature.value = loaded[0].key
    }
  } catch (error) {
    // [决策理由] 只展示当前插件最新请求产生的错误。
    if (sequence === featureRequestSequence.value && selectedPlugin.value === pluginName) {
      setError(error, '加载插件功能失败')
    }
  } finally {
    // [决策理由] 过期请求不能提前关闭新请求的加载态。
    if (sequence === featureRequestSequence.value && selectedPlugin.value === pluginName) {
      featureLoading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. ping -> 返回可用功能ping -> 默认选中ping。
  // 2. 先选A再选B -> A响应过期 -> 仅B更新页面。
}

// submitCommand 校验并新增当前功能的全局或群级触发词。
// @param 无；输入来自响应式表单。
// @returns Promise，在保存完成后结束。
// ⚠️副作用说明：新增后端命令和审计记录，并修改当前页面列表。
async function submitCommand(): Promise<void> {
  errorMessage.value = ''
  // [决策理由] 保存中、插件功能未就绪或空触发词均不得提交。
  if (saving.value || selectedPlugin.value === '' || selectedFeature.value === '' || commandText.value.trim() === '') {
    errorMessage.value = '请选择可用功能并填写非空触发词'
    return
  }
  // [决策理由] 群级命令必须绑定后端可解析的正 uint64 群号。
  if (scopeType.value === 'group' && !isPositiveUint64(scopeID.value)) {
    errorMessage.value = '群级触发词必须填写有效数字群号'
    return
  }
  saving.value = true
  try {
    const created = await createCommand({
      scope_type: scopeType.value,
      scope_id: scopeType.value === 'global' ? '0' : scopeID.value,
      plugin_name: selectedPlugin.value,
      feature_key: selectedFeature.value,
      command: commandText.value.trim(),
    })
    commands.value.push(created)
    commandText.value = ''
    feedback.success('触发词已新增')
  } catch (error) {
    feedback.error(error, '新增触发词失败')
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. global+ping.ping+测试 -> POST -> 表格新增规则并清空输入。
  // 2. group+空群号 -> 本地拒绝 -> 不发送请求。
}

// saveCommand 保存行内编辑后的触发词。
// @param command：当前命令列表项。
// @returns Promise，在重命名完成后结束。
// ⚠️副作用说明：更新后端命令和审计记录，并替换当前列表项。
async function saveCommand(command: CommandState): Promise<void> {
  // [决策理由] 空触发词无法参与匹配，必须在网络请求前拦截。
  if (command.command.trim() === '') {
    errorMessage.value = '触发词不能为空'
    return
  }
  pendingID.value = command.id
  errorMessage.value = ''
  try {
    const updated = await renameCommand(command.id, command.command.trim())
    let index = -1
    for (let candidate = 0; candidate < commands.value.length; candidate += 1) {
      // [决策理由] 使用后端ID定位需要替换的权威命令行。
      if (commands.value[candidate].id === updated.id) {
        index = candidate
        break
      }
    }
    // [决策理由] 并发删除后不得把已删除行重新插回列表。
    if (index >= 0) {
      commands.value[index] = updated
    }
    feedback.success('触发词已保存')
  } catch (error) {
    feedback.error(error, '保存触发词失败')
  } finally {
    pendingID.value = 0
  }

  // >>> 数据演变示例
  // 1. id1的ping改为延迟 -> PATCH -> 权威响应替换行。
  // 2. 输入全空格 -> 本地拒绝 -> 后端不变。
}

// confirmRemoveCommand 打开包含完整影响范围的危险确认框。
// @param command：待删除命令。
// @returns 无。
// ⚠️副作用说明：修改删除确认框状态。
function confirmRemoveCommand(command: CommandState): void {
  deleteTarget.value = command

  // >>> 数据演变示例
  // 1. 点击群级ping命令 -> deleteTarget=该行 -> 弹框展示群号和功能。
  // 2. 点击全局命令 -> deleteTarget=该行 -> 弹框展示全局范围。
}

// removeCommand 删除确认框中的触发词。
// @param 无；目标来自 deleteTarget。
// @returns Promise，在删除结束后完成。
// ⚠️副作用说明：删除后端命令与审计记录，并移除当前列表项。
async function removeCommand(): Promise<void> {
  const command = deleteTarget.value
  // [决策理由] 没有确认目标时禁止发送无法确定主键的删除请求。
  if (command === null) {
    return
  }
  pendingID.value = command.id
  errorMessage.value = ''
  try {
    await deleteCommand(command.id)
    const remaining: CommandState[] = []
    for (const item of commands.value) {
      // [决策理由] 仅移除后端已确认删除的目标命令。
      if (item.id !== command.id) {
        remaining.push(item)
      }
    }
    commands.value = remaining
    deleteTarget.value = null
    feedback.success('触发词已删除')
  } catch (error) {
    feedback.error(error, '删除触发词失败')
  } finally {
    pendingID.value = 0
  }

  // >>> 数据演变示例
  // 1. 确认id1 -> DELETE成功 -> 表格移除且关闭弹框。
  // 2. DELETE失败 -> 保留行与弹框 -> 显示错误。
}

// setScope 切换作用域并维护合法 scope_id。
// @param value：global 或 group。
// @returns 无。
// ⚠️副作用说明：修改响应式作用域和群号字段。
function setScope(value: 'global' | 'group'): void {
  scopeType.value = value
  // [决策理由] 全局固定使用0，群级必须由管理员明确输入群号。
  if (value === 'global') {
    scopeID.value = '0'
  } else {
    // [决策理由] 保留值0不是合法QQ群号，切换到群级时必须检查并清空。
    if (scopeID.value === '0') {
      scopeID.value = ''
    }
  }

  // >>> 数据演变示例
  // 1. group切global -> scope_id=0。
  // 2. global切group -> scope_id清空等待输入。
}

// isPositiveUint64 校验群号是否为Go后端可解析的正uint64十进制数。
// @param value：待校验群号字符串。
// @returns 合法且非零、未溢出uint64时为true。
// ⚠️副作用说明：无。
function isPositiveUint64(value: string): boolean {
  // [决策理由] 先限制十进制格式，避免 BigInt 接受符号或空白。
  if (!/^[1-9][0-9]*$/.test(value)) {
    return false
  }
  const valid = BigInt(value) <= 18446744073709551615n

  // >>> 数据演变示例
  // 1. "123456" -> 格式合法且未溢出 -> true。
  // 2. "0"或超出uint64 -> false。
  return valid
}

// setError 将未知异常转换为稳定页面提示。
// @param error：捕获值；fallback：非 Error 时的默认消息。
// @returns 无。
// ⚠️副作用说明：修改页面错误状态。
function setError(error: unknown, fallback: string): void {
  // [决策理由] API 通常抛出 Error，但运行时仍可能出现其他值。
  if (error instanceof Error) {
    errorMessage.value = error.message
  } else {
    errorMessage.value = fallback
  }

  // >>> 数据演变示例
  // 1. Error("命令冲突") -> 显示后端错误。
  // 2. unknown -> 显示fallback。
}

watch(selectedPlugin, loadFeatures)
onMounted(loadPage)
</script>

<template>
  <section class="management-page">
    <NAlert v-if="errorMessage" type="error" closable @close="errorMessage = ''">{{ errorMessage }}</NAlert>

    <NCard title="新增触发词" size="small" :bordered="true">
      <template #header-extra><NTag type="info" size="small">当前插件：{{ selectedPlugin || '未选择' }}</NTag></template>
      <NForm class="command-form" label-placement="top" @submit.prevent="submitCommand">
        <NFormItem v-if="fixedPluginName === ''" label="插件" required>
          <NSelect v-model:value="selectedPlugin" :options="pluginOptions" placeholder="选择插件" />
        </NFormItem>
        <NFormItem label="功能" required>
          <NSelect v-model:value="selectedFeature" :options="featureOptions" :loading="featureLoading" :disabled="features.length === 0" placeholder="选择已注册功能" />
        </NFormItem>
        <NFormItem label="作用域" required>
          <NRadioGroup :value="scopeType" @update:value="setScope">
            <NRadioButton value="global">全局</NRadioButton>
            <NRadioButton value="group">指定群</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <NFormItem v-if="scopeType === 'group'" label="QQ群号" required :validation-status="groupIDError ? 'error' : undefined" :feedback="groupIDError ? '请输入有效的正整数 QQ 群号' : '该触发词只在此群生效'">
          <NInput v-model:value="scopeID" inputmode="numeric" placeholder="例如：123456789" />
        </NFormItem>
        <NFormItem label="新触发词" required :validation-status="commandError ? 'error' : undefined" :feedback="commandError ? '触发词不能只包含空白' : '最多 128 个字符，不需要输入系统命令前缀'">
          <NInput v-model:value="commandText" maxlength="128" show-count clearable placeholder="例如：ping 或 测试" />
        </NFormItem>
        <div class="form-action"><NButton attr-type="submit" type="primary" :loading="saving" :disabled="featureLoading || features.length === 0">添加触发词</NButton></div>
      </NForm>
      <NAlert v-if="featureLoaded && !featureLoading && selectedPlugin !== '' && features.length === 0" type="warning" :bordered="false">当前插件没有可用功能，无法创建触发词。</NAlert>
    </NCard>

    <NCard title="已配置命令" size="small">
      <template #header-extra><NTag size="small">共 {{ commands.length }} 条</NTag></template>
      <div v-if="loading" class="skeleton-list"><NSkeleton v-for="index in 4" :key="index" text :repeat="2" /></div>
      <NEmpty v-else-if="commands.length === 0" description="当前插件还没有可管理的触发词">
        <template #extra><span class="muted">可在上方选择功能和范围后添加第一条命令。</span></template>
      </NEmpty>
      <NSpin v-else :show="pendingID !== 0">
        <div class="table-scroll">
          <NTable :single-line="false" size="small">
            <thead><tr><th>作用域</th><th>目标功能</th><th>触发词</th><th>标准化值</th><th class="actions-column">操作</th></tr></thead>
            <tbody>
              <tr v-for="command in commands" :key="command.id">
                <td><NTag :type="command.scope_type === 'global' ? 'info' : 'warning'" size="small">{{ command.scope_type === 'global' ? '全局' : `群 ${command.scope_id}` }}</NTag></td>
                <td><code>{{ command.plugin_name }}.{{ command.feature_key }}</code></td>
                <td><NInput v-model:value="command.command" maxlength="128" size="small" :disabled="pendingID !== 0" /></td>
                <td><code>{{ command.normalized_command }}</code></td>
                <td class="row-actions"><NButton size="small" secondary type="primary" :loading="pendingID === command.id" :disabled="pendingID !== 0" @click="saveCommand(command)">保存</NButton><NButton size="small" secondary type="error" :disabled="pendingID !== 0" @click="confirmRemoveCommand(command)">删除</NButton></td>
              </tr>
            </tbody>
          </NTable>
        </div>
      </NSpin>
    </NCard>

    <NModal :show="deleteTarget !== null" preset="card" title="删除触发词" class="confirm-modal" :mask-closable="pendingID === 0" @close="deleteTarget = null">
      <NAlert type="error">删除后机器人将立即停止匹配这条命令，此操作会写入审计日志。</NAlert>
      <dl v-if="deleteTarget" class="impact-list"><dt>插件 / 功能</dt><dd><code>{{ deleteTarget.plugin_name }}.{{ deleteTarget.feature_key }}</code></dd><dt>作用域</dt><dd>{{ deleteTarget.scope_type === 'global' ? '全局' : `群 ${deleteTarget.scope_id}` }}</dd><dt>触发词</dt><dd>{{ deleteTarget.command }}</dd></dl>
      <template #footer><div class="modal-actions"><NButton :disabled="pendingID !== 0" @click="deleteTarget = null">取消</NButton><NButton type="error" :loading="pendingID !== 0" @click="removeCommand">确认删除</NButton></div></template>
    </NModal>
  </section>
</template>

<style scoped>
.management-page { display: grid; gap: 24px; }
.command-form { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 0 16px; }
.command-form :deep(.n-form-item) { grid-column: span 3; }
.command-form :deep(.n-form-item:nth-last-of-type(1)) { grid-column: span 5; }
.form-action { align-items: center; display: flex; grid-column: span 1; padding-top: 30px; }
.table-scroll { overflow-x: auto; }
.table-scroll :deep(table) { min-width: 820px; }
.actions-column { width: 148px; }
.row-actions { display: flex; gap: 8px; white-space: nowrap; }
.skeleton-list { display: grid; gap: 16px; min-height: 160px; }
.confirm-modal { width: min(420px, calc(100vw - 32px)); }
.impact-list { display: grid; grid-template-columns: 112px 1fr; margin: 20px 0 0; row-gap: 12px; }
.impact-list dt { color: var(--color-text-muted); }
.impact-list dd { margin: 0; overflow-wrap: anywhere; }
.modal-actions { display: flex; gap: 12px; justify-content: flex-end; }
@media (max-width: 1023px) { .command-form :deep(.n-form-item) { grid-column: span 6; } .form-action { grid-column: span 6; } }
@media (max-width: 639px) { .management-page { gap: 16px; } .command-form { display: block; } .form-action { padding-top: 0; } .form-action :deep(.n-button) { min-height: 44px; width: 100%; } .row-actions :deep(.n-button) { min-height: 44px; } }
</style>
