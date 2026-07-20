<!-- 📌 影响范围：调用插件、功能与权限 API；修改后端权限策略、审计记录和运行时权限快照。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NForm, NFormItem, NInput, NModal, NRadioButton, NRadioGroup, NSelect, NSkeleton, NSpin, NTable, NTag } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { deletePermission, listPermissions, listPluginFeatures, listPlugins, setPermission, type FeatureState, type PermissionState, type PluginState } from '../api'
import { useAppFeedback } from '../feedback'

const route = useRoute()
const fixedPluginName = String(route.params.pluginName ?? '')
const permissions = ref<PermissionState[]>([])
const plugins = ref<PluginState[]>([])
const features = ref<FeatureState[]>([])
const scopeType = ref<'global' | 'group'>('global')
const scopeID = ref('0')
const pluginName = ref(fixedPluginName)
const featureKey = ref('')
const subjectType = ref<'role' | 'user'>('role')
const subjectID = ref('group_admin')
const effect = ref<'allow' | 'deny'>('allow')
const filterScope = ref<string | null>(null)
const filterSubject = ref('')
const loading = ref(true)
const saving = ref(false)
const pendingID = ref(0)
const errorMessage = ref('')
const featureRequestSequence = ref(0)
const featureLoading = ref(false)
const featureLoadFailed = ref(false)
const deleteTarget = ref<PermissionState | null>(null)
const feedback = useAppFeedback()

const roleOptions = [
  { value: 'super_admin', label: '最高管理员' },
  { value: 'group_owner', label: '群主' },
  { value: 'group_admin', label: '群管理员' },
  { value: 'member', label: '普通成员' },
]
const pluginOptions = computed(buildPluginOptions)
const featureOptions = computed(buildFeatureOptions)
const scopeFilterOptions = [{ label: '全部作用域', value: '' }, { label: '全局', value: 'global' }, { label: '群级', value: 'group' }]
const groupIDError = computed(hasGroupIDError)
const userIDError = computed(hasUserIDError)
const filteredPermissions = computed(filterPermissionList)

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

// buildFeatureOptions 构建插件全功能与具体功能选项。
// @param 无；读取响应式功能列表。
// @returns 以插件全部功能开头的选项列表。
// ⚠️副作用说明：无。
function buildFeatureOptions(): Array<{ label: string; value: string }> {
  const result = [{ label: '插件全部功能', value: '' }]
  for (const item of features.value) {
    result.push({ label: `${item.display_name || item.key} · ${item.key}`, value: item.key })
  }

  // >>> 数据演变示例
  // 1. ping功能 -> [全部功能,ping]。
  // 2. 空功能 -> [全部功能]。
  return result
}

// hasGroupIDError 判断群级规则的群号输入是否无效。
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

// hasUserIDError 判断指定用户QQ输入是否无效。
// @param 无；读取响应式主体类型和QQ号。
// @returns 用户主体非空输入无效时为true。
// ⚠️副作用说明：无。
function hasUserIDError(): boolean {
  const result = subjectType.value === 'user' && subjectID.value !== '' && !isPositiveUint64(subjectID.value)

  // >>> 数据演变示例
  // 1. user+abc -> true。
  // 2. role+group_admin -> false。
  return result
}

// filterPermissionList 根据作用域和主体筛选当前插件的显式权限规则。
// @param 无；读取响应式筛选条件和权限列表。
// @returns 与筛选条件匹配的权限策略。
// ⚠️副作用说明：无。
function filterPermissionList(): PermissionState[] {
  const query = filterSubject.value.trim().toLowerCase()
  const result: PermissionState[] = []
  for (const item of permissions.value) {
    const scopeMatches = !filterScope.value || item.scope_type === filterScope.value
    const subject = `${item.subject_type}:${item.subject_id}`.toLowerCase()
    const subjectMatches = query === '' || subject.includes(query)
    // [决策理由] 仅展示同时满足范围和主体条件的规则，插件隔离已在加载时完成。
    if (scopeMatches && subjectMatches) {
      result.push(item)
    }
  }

  // >>> 数据演变示例
  // 1. scope=group+query=123 -> 仅保留群级且主体含123的规则。
  // 2. 筛选均空 -> 每项条件为true -> 返回当前插件全部规则。
  return result
}

// loadPage 加载权限和插件列表并执行插件工作台隔离。
// @param 无。
// @returns Promise，在基础数据加载结束后完成。
// ⚠️副作用说明：发起网络请求并更新页面状态。
async function loadPage(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const [permissionStates, pluginStates] = await Promise.all([listPermissions(), listPlugins()])
    permissions.value = []
    for (const item of permissionStates) {
      // [决策理由] 工作台只允许当前插件权限进入可删除列表。
      if (fixedPluginName === '' || item.plugin_name === fixedPluginName) {
        permissions.value.push(item)
      }
    }
    plugins.value = []
    for (const item of pluginStates) {
      // [决策理由] 不可用插件不能成为新权限目标。
      if (item.available) {
        plugins.value.push(item)
      }
    }
    // [决策理由] 非插件工作台入口需要选定一个合法插件后才能创建规则。
    if (pluginName.value === '' && plugins.value.length > 0) {
      pluginName.value = plugins.value[0].name
    }
    await loadFeatures(pluginName.value)
  } catch (error) {
    setError(error, '加载权限策略失败')
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. 固定ping+全量权限 -> 丢弃其他插件规则 -> 仅展示ping。
  // 2. API失败 -> 显示错误 -> loading=false。
}

// loadFeatures 读取当前插件功能并防止请求乱序污染。
// @param selectedPlugin：插件稳定名称。
// @returns Promise，在最新功能请求完成后结束。
// ⚠️副作用说明：发起网络请求并重置功能选择。
async function loadFeatures(selectedPlugin: string): Promise<void> {
  const sequence = ++featureRequestSequence.value
  featureLoading.value = selectedPlugin !== ''
  featureLoadFailed.value = false
  features.value = []
  featureKey.value = ''
  // [决策理由] 未选插件时没有合法功能查询目标。
  if (selectedPlugin === '') {
    featureLoading.value = false
    return
  }
  try {
    const loaded = await listPluginFeatures(selectedPlugin)
    // [决策理由] 过期响应不得覆盖当前插件功能。
    if (sequence !== featureRequestSequence.value || pluginName.value !== selectedPlugin) {
      return
    }
    for (const item of loaded) {
      // [决策理由] 不可用功能不能成为新权限目标。
      if (item.available) {
        features.value.push(item)
      }
    }
  } catch (error) {
    // [决策理由] 仅处理仍与当前插件对应的最新错误。
    if (sequence === featureRequestSequence.value && pluginName.value === selectedPlugin) {
      featureLoadFailed.value = true
      setError(error, '加载插件功能失败')
    }
  } finally {
    // [决策理由] 过期请求不能结束当前请求的加载态。
    if (sequence === featureRequestSequence.value && pluginName.value === selectedPlugin) {
      featureLoading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. ping -> 功能列表返回 -> 可选择插件全功能或ping功能。
  // 2. A后切B -> A响应过期 -> 只保留B结果。
}

// submitPermission 校验并保存角色或指定用户权限策略。
// @param 无；输入来自响应式表单。
// @returns Promise，在保存完成后结束。
// ⚠️副作用说明：写入权限和审计、热刷新权限快照并更新列表。
async function submitPermission(): Promise<void> {
  errorMessage.value = ''
  // [决策理由] 功能加载失败或写操作进行中时禁止提交不确定目标。
  if (featureLoading.value || featureLoadFailed.value || saving.value) {
    errorMessage.value = featureLoadFailed.value ? '请先重新加载插件功能' : '请等待当前操作完成'
    return
  }
  // [决策理由] 群级规则必须绑定合法正uint64群号。
  if (scopeType.value === 'group' && !isPositiveUint64(scopeID.value)) {
    errorMessage.value = '群级权限必须填写有效数字群号'
    return
  }
  // [决策理由] 用户主体必须绑定合法正uint64 QQ号。
  if (subjectType.value === 'user' && !isPositiveUint64(subjectID.value)) {
    errorMessage.value = '指定用户必须填写有效数字 QQ'
    return
  }
  // [决策理由] 插件和主体缺失会产生永远无法匹配的规则。
  if (pluginName.value === '' || subjectID.value === '') {
    errorMessage.value = '请选择插件并填写权限主体'
    return
  }
  saving.value = true
  try {
    const saved = await setPermission({ scope_type: scopeType.value, scope_id: scopeType.value === 'global' ? '0' : scopeID.value, plugin_name: pluginName.value, feature_key: featureKey.value, subject_type: subjectType.value, subject_id: subjectID.value, effect: effect.value })
    let index = -1
    for (let candidate = 0; candidate < permissions.value.length; candidate += 1) {
      // [决策理由] 使用后端ID定位UPSERT返回的权威规则。
      if (permissions.value[candidate].id === saved.id) {
        index = candidate
        break
      }
    }
    // [决策理由] 后端UPSERT可能更新已有规则或创建新规则。
    if (index >= 0) {
      permissions.value[index] = saved
    } else {
      permissions.value.push(saved)
    }
    feedback.success('权限策略已保存')
  } catch (error) {
    feedback.error(error, '保存权限策略失败')
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. group123+ping全功能+user200+allow -> UPSERT -> 新增或替换规则。
  // 2. user+QQ=abc -> 本地拒绝 -> 不发送请求。
}

// confirmRemovePermission 打开权限回退危险确认框。
// @param item：待删除权限策略。
// @returns 无。
// ⚠️副作用说明：修改删除确认框状态。
function confirmRemovePermission(item: PermissionState): void {
  deleteTarget.value = item

  // >>> 数据演变示例
  // 1. 点击群级拒绝规则 -> 弹框展示群号、主体和拒绝效果。
  // 2. 点击全局允许规则 -> 弹框展示全局及允许效果。
}

// removePermission 删除确认框中的显式规则并恢复权限回退链。
// @param 无；目标来自 deleteTarget。
// @returns Promise，在删除结束后完成。
// ⚠️副作用说明：删除权限与审计记录、刷新快照并更新列表。
async function removePermission(): Promise<void> {
  const item = deleteTarget.value
  // [决策理由] 没有明确目标时禁止发送删除请求。
  if (item === null) {
    return
  }
  pendingID.value = item.id
  errorMessage.value = ''
  try {
    await deletePermission(item.id)
    const remaining: PermissionState[] = []
    for (const permission of permissions.value) {
      // [决策理由] 仅移除后端已确认删除的权限规则。
      if (permission.id !== item.id) {
        remaining.push(permission)
      }
    }
    permissions.value = remaining
    deleteTarget.value = null
    feedback.success('权限策略已删除并恢复回退链')
  } catch (error) {
    feedback.error(error, '删除权限策略失败')
  } finally {
    pendingID.value = 0
  }

  // >>> 数据演变示例
  // 1. 删除id8成功 -> 表格移除 -> 运行时回退下一级规则。
  // 2. 删除失败 -> 保留规则和弹框 -> 显示错误。
}

// setScope 切换全局或群级作用域并修正群号。
// @param value：global 或 group。
// @returns 无。
// ⚠️副作用说明：修改页面作用域和群号输入。
function setScope(value: 'global' | 'group'): void {
  scopeType.value = value
  // [决策理由] 全局固定scope_id=0，群级不能沿用0。
  if (value === 'global') {
    scopeID.value = '0'
  } else {
    // [决策理由] 0不是合法QQ群号，切换到群级时必须检查并清空。
    if (scopeID.value === '0') {
      scopeID.value = ''
    }
  }

  // >>> 数据演变示例
  // 1. group切global -> scope_id=0。
  // 2. global切group -> scope_id清空。
}

// setSubjectType 切换角色或指定用户并设置安全初值。
// @param value：role 或 user。
// @returns 无。
// ⚠️副作用说明：修改主体类型和主体值。
function setSubjectType(value: 'role' | 'user'): void {
  subjectType.value = value
  subjectID.value = value === 'role' ? 'group_admin' : ''

  // >>> 数据演变示例
  // 1. user切role -> 默认群管理员。
  // 2. role切user -> 清空等待QQ号。
}

// clearFilters 清除权限列表筛选条件。
// @param 无。
// @returns 无。
// ⚠️副作用说明：重置页面筛选状态。
function clearFilters(): void {
  filterScope.value = null
  filterSubject.value = ''

  // >>> 数据演变示例
  // 1. group+QQ123 -> 清空 -> 展示全部当前插件规则。
  // 2. 已为空 -> 保持空筛选 -> 列表不变。
}

// isPositiveUint64 校验群号或QQ是否为正uint64十进制数。
// @param value：待校验字符串。
// @returns 合法且非零、未溢出uint64时为true。
// ⚠️副作用说明：无。
function isPositiveUint64(value: string): boolean {
  // [决策理由] 限制纯十进制格式，避免符号和空白被隐式接受。
  if (!/^[1-9][0-9]*$/.test(value)) {
    return false
  }
  const valid = BigInt(value) <= 18446744073709551615n

  // >>> 数据演变示例
  // 1. "2769731875" -> 合法且未溢出 -> true。
  // 2. "0"或超uint64 -> false。
  return valid
}

// setError 将未知异常转换为稳定页面提示。
// @param error：捕获值；fallback：默认提示。
// @returns 无。
// ⚠️副作用说明：修改页面错误状态。
function setError(error: unknown, fallback: string): void {
  // [决策理由] API通常抛出Error，但仍需兼容未知异常。
  if (error instanceof Error) {
    errorMessage.value = error.message
  } else {
    errorMessage.value = fallback
  }

  // >>> 数据演变示例
  // 1. Error("冲突") -> 显示冲突。
  // 2. unknown -> 显示fallback。
}

watch(pluginName, loadFeatures)
onMounted(loadPage)
</script>

<template>
  <section class="management-page">
    <NAlert v-if="errorMessage" type="error" closable @close="errorMessage = ''">{{ errorMessage }}</NAlert>
    <NAlert type="info" :bordered="false">空功能范围表示“当前插件全部功能”，不是未配置；拒绝规则与允许规则具有相同的作用域优先级。</NAlert>

    <NCard title="新增或更新规则" size="small">
      <template #header-extra><NTag type="info" size="small">当前插件：{{ pluginName || '未选择' }}</NTag></template>
      <NForm class="permission-form" label-placement="top" @submit.prevent="submitPermission">
        <NFormItem label="作用域" required><NRadioGroup :value="scopeType" @update:value="setScope"><NRadioButton value="global">全局</NRadioButton><NRadioButton value="group">指定群</NRadioButton></NRadioGroup></NFormItem>
        <NFormItem v-if="scopeType === 'group'" label="QQ群号" required :validation-status="groupIDError ? 'error' : undefined" :feedback="groupIDError ? '请输入有效的正整数 QQ 群号' : '规则仅在此群生效'"><NInput v-model:value="scopeID" inputmode="numeric" placeholder="例如：123456789" /></NFormItem>
        <NFormItem v-if="fixedPluginName === ''" label="插件" required><NSelect v-model:value="pluginName" :options="pluginOptions" placeholder="选择插件" /></NFormItem>
        <NFormItem label="功能范围" required><NSelect v-model:value="featureKey" :options="featureOptions" :loading="featureLoading" :disabled="featureLoading || featureLoadFailed" /></NFormItem>
        <NFormItem label="主体类型" required><NRadioGroup :value="subjectType" @update:value="setSubjectType"><NRadioButton value="role">角色</NRadioButton><NRadioButton value="user">指定 QQ</NRadioButton></NRadioGroup></NFormItem>
        <NFormItem v-if="subjectType === 'role'" label="角色" required><NSelect v-model:value="subjectID" :options="roleOptions" /></NFormItem>
        <NFormItem v-else label="用户 QQ" required :validation-status="userIDError ? 'error' : undefined" :feedback="userIDError ? '请输入有效的正整数 QQ 号' : '只对该 QQ 生效'"><NInput v-model:value="subjectID" inputmode="numeric" placeholder="例如：2769731875" /></NFormItem>
        <NFormItem label="效果" required><NSelect v-model:value="effect" :options="[{ label: '允许', value: 'allow' }, { label: '拒绝', value: 'deny' }]" /></NFormItem>
        <div class="form-action"><NButton v-if="featureLoadFailed" secondary type="warning" @click="loadFeatures(pluginName)">重试功能加载</NButton><NButton v-else attr-type="submit" type="primary" :loading="saving" :disabled="pluginName === '' || featureLoading">保存并热更新</NButton></div>
      </NForm>
    </NCard>

    <NCard title="显式权限规则" size="small">
      <template #header-extra><NTag size="small">{{ filteredPermissions.length }} / {{ permissions.length }} 条</NTag></template>
      <div class="filters"><NSelect v-model:value="filterScope" clearable :options="scopeFilterOptions" placeholder="全部作用域" /><NInput v-model:value="filterSubject" clearable placeholder="筛选角色或 QQ" /><NButton secondary :disabled="!filterScope && filterSubject === ''" @click="clearFilters">清空筛选</NButton></div>
      <div v-if="loading" class="skeleton-list"><NSkeleton v-for="index in 4" :key="index" text :repeat="2" /></div>
      <NEmpty v-else-if="filteredPermissions.length === 0" description="没有符合条件的显式权限策略"><template #extra><span class="muted">清除筛选，或在上方新增第一条规则。</span></template></NEmpty>
      <NSpin v-else :show="pendingID !== 0"><div class="table-scroll"><NTable :single-line="false" size="small"><thead><tr><th>作用域</th><th>目标</th><th>主体</th><th>效果</th><th class="actions-column">操作</th></tr></thead><tbody><tr v-for="item in filteredPermissions" :key="item.id"><td><NTag :type="item.scope_type === 'global' ? 'info' : 'warning'" size="small">{{ item.scope_type === 'global' ? '全局' : `群 ${item.scope_id}` }}</NTag></td><td><code>{{ item.plugin_name }}</code><div class="cell-detail">{{ item.feature_key ? `功能：${item.feature_key}` : '插件全部功能' }}</div></td><td><code>{{ item.subject_type === 'role' ? `角色：${item.subject_id}` : `QQ：${item.subject_id}` }}</code></td><td><NTag :type="item.effect === 'allow' ? 'success' : 'error'" size="small">{{ item.effect === 'allow' ? '允许' : '拒绝' }}</NTag></td><td><NButton size="small" secondary type="error" :disabled="pendingID !== 0" @click="confirmRemovePermission(item)">删除并回退</NButton></td></tr></tbody></NTable></div></NSpin>
    </NCard>

    <NModal :show="deleteTarget !== null" preset="card" title="删除显式权限规则" class="confirm-modal" :mask-closable="pendingID === 0" @close="deleteTarget = null">
      <NAlert type="error">删除后会立即恢复到下一层规则，最终结果可能变为允许或拒绝。请核对完整范围。</NAlert>
      <dl v-if="deleteTarget" class="impact-list"><dt>作用域</dt><dd>{{ deleteTarget.scope_type === 'global' ? '全局' : `群 ${deleteTarget.scope_id}` }}</dd><dt>插件 / 功能</dt><dd><code>{{ deleteTarget.plugin_name }} / {{ deleteTarget.feature_key || '全部功能' }}</code></dd><dt>主体</dt><dd>{{ deleteTarget.subject_type === 'role' ? `角色：${deleteTarget.subject_id}` : `QQ：${deleteTarget.subject_id}` }}</dd><dt>当前效果</dt><dd>{{ deleteTarget.effect === 'allow' ? '允许' : '拒绝' }}</dd></dl>
      <template #footer><div class="modal-actions"><NButton :disabled="pendingID !== 0" @click="deleteTarget = null">取消</NButton><NButton type="error" :loading="pendingID !== 0" @click="removePermission">删除并回退</NButton></div></template>
    </NModal>
  </section>
</template>

<style scoped>
.management-page { display: grid; gap: 24px; }
.permission-form { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 0 16px; }
.permission-form :deep(.n-form-item) { grid-column: span 3; }
.form-action { align-items: center; display: flex; grid-column: span 3; padding-top: 30px; }
.filters { display: grid; grid-template-columns: 180px minmax(220px, 1fr) auto; gap: 12px; margin-bottom: 16px; }
.table-scroll { overflow-x: auto; }
.table-scroll :deep(table) { min-width: 760px; }
.actions-column { width: 132px; }
.cell-detail { color: var(--color-text-muted); font-size: 12px; margin-top: 4px; }
.skeleton-list { display: grid; gap: 16px; min-height: 160px; }
.confirm-modal { width: min(420px, calc(100vw - 32px)); }
.impact-list { display: grid; grid-template-columns: 112px 1fr; margin: 20px 0 0; row-gap: 12px; }
.impact-list dt { color: var(--color-text-muted); }
.impact-list dd { margin: 0; overflow-wrap: anywhere; }
.modal-actions { display: flex; gap: 12px; justify-content: flex-end; }
@media (max-width: 1023px) { .permission-form :deep(.n-form-item) { grid-column: span 6; } .form-action { grid-column: span 6; } }
@media (max-width: 639px) { .management-page { gap: 16px; } .permission-form { display: block; } .form-action { padding-top: 0; } .form-action :deep(.n-button) { min-height: 44px; width: 100%; } .filters { grid-template-columns: 1fr; } .filters :deep(.n-button) { min-height: 44px; } }
</style>
