<!-- 📌 影响范围：调用插件、功能与命令 API；修改后端功能触发词、审计记录和命令快照。 -->
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
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
const saving = ref(false)
const pendingID = ref(0)
const errorMessage = ref('')
const featureRequestSequence = ref(0)

// loadPage 并行加载插件和触发词，并初始化功能选择。
// @param 无。
// @returns Promise，在页面基础数据完成后结束。
// ⚠️副作用说明：发起插件、命令和功能网络请求并修改页面状态。
async function loadPage(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const [pluginStates, commandStates] = await Promise.all([listPlugins(), listCommands()])
    plugins.value = pluginStates.filter((item) => item.available)
    commands.value = []
    for (const item of commandStates) {
      // [决策理由] 插件工作台只能展示当前插件的命令，防止跨插件误操作。
      if (fixedPluginName === '' || item.plugin_name === fixedPluginName) {
        commands.value.push(item)
      }
    }
    // [决策理由] 首次进入自动选择第一个可用插件，减少空表单操作。
    if (selectedPlugin.value === '' && plugins.value.length > 0) {
      selectedPlugin.value = plugins.value[0].name
    }
  } catch (error) {
    setError(error, '加载功能触发词失败')
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. plugins=[admin,ping]+commands=[ping] -> 初始化列表并选admin。
  // 2. API失败 -> errorMessage更新 -> loading=false。
}

// loadFeatures 在插件选择变化时读取其功能元数据。
// @param pluginName：当前选中的插件名。
// @returns Promise，在功能列表完成后结束。
// ⚠️副作用说明：发起功能元数据请求并重置功能选择。
async function loadFeatures(pluginName: string): Promise<void> {
  const requestSequence = ++featureRequestSequence.value
  features.value = []
  selectedFeature.value = ''
  // [决策理由] 空插件名没有可查询目标，直接保持空功能列表。
  if (pluginName === '') {
    return
  }
  try {
    const loaded = (await listPluginFeatures(pluginName)).filter((item) => item.available)
    // [决策理由] 快速切换插件时只允许最新请求更新功能列表。
    if (requestSequence !== featureRequestSequence.value || selectedPlugin.value !== pluginName) {
      return
    }
    features.value = loaded
    // [决策理由] 默认选中第一个可用功能，避免提交空 feature_key。
    if (features.value.length > 0) {
      selectedFeature.value = features.value[0].key
    }
  } catch (error) {
    // [决策理由] 过期请求的错误不应覆盖当前插件的成功状态。
    if (requestSequence === featureRequestSequence.value && selectedPlugin.value === pluginName) {
      setError(error, '加载插件功能失败')
    }
  }

  // >>> 数据演变示例
  // 1. plugin=ping -> [ping功能] -> selectedFeature=ping。
  // 2. plugin="" -> 空功能列表 -> 不请求。
}

// submitCommand 新增当前功能的全局或群级触发词。
// @param 无；输入来自响应式表单。
// @returns Promise，在保存完成后结束。
// ⚠️副作用说明：新增后端命令和审计记录，并修改当前页面列表。
async function submitCommand(): Promise<void> {
  errorMessage.value = ''
  // [决策理由] 插件、功能和非空文本缺一不可，避免依赖后端才发现空表单。
  if (selectedPlugin.value === '' || selectedFeature.value === '' || commandText.value.trim() === '') {
    errorMessage.value = '请选择插件和功能，并填写触发词'
    return
  }
  // [决策理由] 群级触发词必须指向具体群号。
  if (scopeType.value === 'group' && !isPositiveUint64(scopeID.value)) {
    errorMessage.value = '群级触发词必须填写数字群号'
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
  } catch (error) {
    setError(error, '新增触发词失败')
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. global+ping.ping+测试 -> POST -> 列表追加新触发词。
  // 2. group+群号空 -> 本地拒绝 -> 不请求。
}

// saveCommand 保存列表项中编辑后的触发词。
// @param command：当前列表项。
// @returns Promise，在重命名完成后结束。
// ⚠️副作用说明：更新后端命令和审计记录，并替换当前列表项。
async function saveCommand(command: CommandState): Promise<void> {
  pendingID.value = command.id
  errorMessage.value = ''
  try {
    const updated = await renameCommand(command.id, command.command.trim())
    const index = commands.value.findIndex((item) => item.id === updated.id)
    // [决策理由] 仅替换仍存在的列表项，避免并发删除后重新插入。
    if (index >= 0) {
      commands.value[index] = updated
    }
  } catch (error) {
    setError(error, '保存触发词失败')
  } finally {
    pendingID.value = 0
  }

  // >>> 数据演变示例
  // 1. id1 ping改延迟 -> PATCH -> 列表项替换。
  // 2. 与同作用域命令冲突 -> 提示错误 -> 保持页面输入。
}

// removeCommand 确认后删除指定触发词。
// @param command：待删除列表项。
// @returns Promise，在删除或取消后结束。
// ⚠️副作用说明：可能删除后端命令与审计记录，并移除当前列表项。
async function removeCommand(command: CommandState): Promise<void> {
  // [决策理由] 删除会立即影响机器人命令匹配，必须由管理员二次确认。
  if (!window.confirm(`确定删除触发词“${command.command}”吗？`)) {
    return
  }
  pendingID.value = command.id
  errorMessage.value = ''
  try {
    await deleteCommand(command.id)
    commands.value = commands.value.filter((item) => item.id !== command.id)
  } catch (error) {
    setError(error, '删除触发词失败')
  } finally {
    pendingID.value = 0
  }

  // >>> 数据演变示例
  // 1. 确认删除id1 -> DELETE -> 列表移除。
  // 2. 取消确认 -> 不请求且列表不变。
}

// setScope 切换作用域并维护合法 scope_id。
// @param value：global 或 group。
// @returns 无。
// ⚠️副作用说明：修改响应式作用域和群号字段。
function setScope(value: 'global' | 'group'): void {
  scopeType.value = value
  // [决策理由] 全局作用域固定使用0，群级切换时清空以要求明确输入。
  if (value === 'global') {
    scopeID.value = '0'
  } else if (scopeID.value === '0') {
    scopeID.value = ''
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
  // [决策理由] 群号必须使用非零十进制格式，禁止符号、空白和前导零。
  if (!/^[1-9][0-9]*$/.test(value)) {
    return false
  }
  const valid = BigInt(value) <= 18446744073709551615n

  // >>> 数据演变示例
  // 1. "123456" -> 正十进制且未溢出 -> true。
  // 2. "0"或超出uint64 -> false。
  return valid
}

// setError 将未知异常转换为稳定页面提示。
// @param error：捕获值；fallback：非 Error 时的默认消息。
// @returns 无。
// ⚠️副作用说明：修改页面错误状态。
function setError(error: unknown, fallback: string): void {
  // [决策理由] API 客户端通常抛出 Error，但运行时仍可能出现其他值。
  if (error instanceof Error) {
    errorMessage.value = error.message
  } else {
    errorMessage.value = fallback
  }

  // >>> 数据演变示例
  // 1. Error("重复") -> errorMessage=重复。
  // 2. unknown -> errorMessage=fallback。
}

watch(selectedPlugin, loadFeatures)
onMounted(loadPage)
</script>

<template>
  <section>
    <div class="page-heading">
      <div>
        <span class="eyebrow">COMMAND ROUTING</span>
        <h1>命令管理</h1>
        <p class="muted">管理当前插件各功能的全局或群级触发词。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="loadPage">刷新</button>
    </div>
    <p v-if="errorMessage" class="error-message banner">{{ errorMessage }}</p>

    <form class="panel command-form" @submit.prevent="submitCommand">
      <label v-if="fixedPluginName === ''">
        <span>插件</span>
        <select v-model="selectedPlugin" required>
          <option value="" disabled>选择插件</option>
          <option v-for="plugin in plugins" :key="plugin.name" :value="plugin.name">{{ plugin.display_name || plugin.name }}</option>
        </select>
      </label>
      <label>
        <span>功能</span>
        <select v-model="selectedFeature" required>
          <option value="" disabled>选择功能</option>
          <option v-for="feature in features" :key="feature.key" :value="feature.key">{{ feature.display_name || feature.key }}</option>
        </select>
      </label>
      <div class="scope-control">
        <span>作用域</span>
        <div class="segmented">
          <button type="button" :class="{ active: scopeType === 'global' }" @click="setScope('global')">全局</button>
          <button type="button" :class="{ active: scopeType === 'group' }" @click="setScope('group')">指定群</button>
        </div>
      </div>
      <label v-if="scopeType === 'group'">
        <span>群号</span>
        <input v-model="scopeID" inputmode="numeric" placeholder="QQ群号" required />
      </label>
      <label class="command-input">
        <span>新触发词</span>
        <input v-model="commandText" maxlength="128" placeholder="例如：ping 或 测试" required />
      </label>
      <button class="primary-button" type="submit" :disabled="saving || features.length === 0">{{ saving ? '保存中…' : '添加触发词' }}</button>
    </form>

    <div v-if="loading" class="empty-state">正在读取触发词…</div>
    <div v-else-if="commands.length === 0" class="empty-state">还没有可管理的功能触发词。</div>
    <div v-else class="panel table-wrap">
      <table>
        <thead><tr><th>作用域</th><th>目标功能</th><th>触发词</th><th>标准化</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="command in commands" :key="command.id">
            <td><span class="scope-badge">{{ command.scope_type === 'global' ? '全局' : `群 ${command.scope_id}` }}</span></td>
            <td><code>{{ command.plugin_name }}.{{ command.feature_key }}</code></td>
            <td><input v-model="command.command" maxlength="128" /></td>
            <td><code>{{ command.normalized_command }}</code></td>
            <td class="row-actions">
              <button class="ghost-button compact" type="button" :disabled="pendingID === command.id" @click="saveCommand(command)">保存</button>
              <button class="danger-button compact" type="button" :disabled="pendingID === command.id" @click="removeCommand(command)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
