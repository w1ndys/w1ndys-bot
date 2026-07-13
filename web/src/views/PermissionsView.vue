<!-- 📌 影响范围：调用插件、功能与权限 API；修改后端权限策略、审计记录和运行时权限快照。 -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  deletePermission,
  listPermissions,
  listPluginFeatures,
  listPlugins,
  setPermission,
  type FeatureState,
  type PermissionState,
  type PluginState,
} from '../api'

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
const filterScope = ref('')
const filterPlugin = ref('')
const filterSubject = ref('')
const loading = ref(true)
const saving = ref(false)
const pendingID = ref(0)
const errorMessage = ref('')
const featureRequestSequence = ref(0)
const featureLoading = ref(false)
const featureLoadFailed = ref(false)

const roleOptions = [
  { value: 'super_admin', label: '最高管理员' },
  { value: 'group_owner', label: '群主' },
  { value: 'group_admin', label: '群管理员' },
  { value: 'member', label: '普通成员' },
]

const filteredPermissions = computed(filterPermissionList)

// filterPermissionList 根据页面条件筛选显式权限规则。
// @param 无；读取响应式筛选条件和权限列表。
// @returns 与全部筛选条件匹配的权限策略。
// ⚠️副作用说明：无。
function filterPermissionList(): PermissionState[] {
  const result: PermissionState[] = []
  for (const item of permissions.value) {
    const scopeMatches = filterScope.value === '' || item.scope_type === filterScope.value
    const pluginMatches = filterPlugin.value === '' || item.plugin_name === filterPlugin.value
    const subject = `${item.subject_type}:${item.subject_id}`.toLowerCase()
    const subjectMatches = filterSubject.value.trim() === '' || subject.includes(filterSubject.value.trim().toLowerCase())
    // [决策理由] 列表只展示同时满足作用域、插件和主体条件的规则。
    if (scopeMatches && pluginMatches && subjectMatches) {
      result.push(item)
    }
  }

  // >>> 数据演变示例
  // 1. scope=group+plugin=ping -> 仅保留群级ping规则。
  // 2. 筛选均空 -> 每项三条件均true -> 返回全部规则。
  return result
}

// loadPage 并行加载权限和插件列表并初始化表单。
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
      // [决策理由] 插件工作台只能展示当前插件的权限，防止删除其他插件规则。
      if (fixedPluginName === '' || item.plugin_name === fixedPluginName) {
        permissions.value.push(item)
      }
    }
    plugins.value = []
    for (const item of pluginStates) {
      // [决策理由] 不可用插件不能成为新权限规则的目标。
      if (item.available) {
        plugins.value.push(item)
      }
    }
    // [决策理由] 默认选择首个可用插件，保证权限目标明确。
    if (pluginName.value === '' && plugins.value.length > 0) {
      pluginName.value = plugins.value[0].name
    }
  } catch (error) {
    setError(error, '加载权限策略失败')
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. permissions=[规则1]+plugins=[ping] -> 展示规则并选ping。
  // 2. API失败 -> 显示错误 -> loading=false。
}

// loadFeatures 读取选中插件的可用功能。
// @param selectedPlugin：插件稳定名称。
// @returns Promise，在最新功能请求完成后结束。
// ⚠️副作用说明：发起网络请求并重置功能选择。
async function loadFeatures(selectedPlugin: string): Promise<void> {
  const sequence = ++featureRequestSequence.value
  featureLoading.value = selectedPlugin !== ''
  featureLoadFailed.value = false
  features.value = []
  featureKey.value = ''
  // [决策理由] 未选择插件时没有合法的功能查询目标。
  if (selectedPlugin === '') {
    featureLoading.value = false
    return
  }
  try {
    const loaded = await listPluginFeatures(selectedPlugin)
    // [决策理由] 快速切换插件时，过期响应不能覆盖当前功能列表。
    if (sequence !== featureRequestSequence.value || pluginName.value !== selectedPlugin) {
      return
    }
    for (const item of loaded) {
      // [决策理由] 已下线功能不能成为新权限规则的目标。
      if (item.available) {
        features.value.push(item)
      }
    }
  } catch (error) {
    // [决策理由] 只有最新请求的错误与当前表单相关。
    if (sequence === featureRequestSequence.value && pluginName.value === selectedPlugin) {
      featureLoadFailed.value = true
      setError(error, '加载插件功能失败')
    }
  } finally {
    // [决策理由] 过期请求不能结束当前插件仍在进行的加载状态。
    if (sequence === featureRequestSequence.value && pluginName.value === selectedPlugin) {
      featureLoading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. ping -> API返回ping功能 -> features更新。
  // 2. 先选A后选B -> A响应过期 -> 仅保留B结果。
}

// submitPermission 保存角色或指定用户权限策略。
// @param 无；输入来自响应式表单。
// @returns Promise，在保存完成后结束。
// ⚠️副作用说明：写入权限与审计记录、热刷新权限快照并更新列表。
async function submitPermission(): Promise<void> {
  errorMessage.value = ''
  // [决策理由] 功能元数据未就绪或已有保存进行中时，禁止绕过按钮状态直接重复提交。
  if (featureLoading.value || featureLoadFailed.value || saving.value) {
    errorMessage.value = featureLoadFailed.value ? '请先重新加载插件功能' : '请等待当前操作完成'
    return
  }
  // [决策理由] 群级规则必须绑定数字群号，避免产生无法命中的策略。
  if (scopeType.value === 'group' && !isPositiveUint64(scopeID.value)) {
    errorMessage.value = '群级权限必须填写数字群号'
    return
  }
  // [决策理由] 指定用户主体必须是数字 QQ，避免无效用户规则。
  if (subjectType.value === 'user' && !isPositiveUint64(subjectID.value)) {
    errorMessage.value = '指定用户必须填写数字 QQ'
    return
  }
  // [决策理由] 权限必须指向已选择插件且主体不能为空。
  if (pluginName.value === '' || subjectID.value === '') {
    errorMessage.value = '请选择插件并填写权限主体'
    return
  }
  saving.value = true
  try {
    const saved = await setPermission({
      scope_type: scopeType.value,
      scope_id: scopeType.value === 'global' ? '0' : scopeID.value,
      plugin_name: pluginName.value,
      feature_key: featureKey.value,
      subject_type: subjectType.value,
      subject_id: subjectID.value,
      effect: effect.value,
    })
    let index = -1
    for (let candidate = 0; candidate < permissions.value.length; candidate += 1) {
      // [决策理由] 主键相同表示后端幂等更新了已有规则。
      if (permissions.value[candidate].id === saved.id) {
        index = candidate
        break
      }
    }
    // [决策理由] 后端幂等保存可能更新旧规则，也可能创建新规则。
    if (index >= 0) {
      permissions.value[index] = saved
    } else {
      permissions.value.push(saved)
    }
  } catch (error) {
    setError(error, '保存权限策略失败')
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. group123+ping全功能+user200+allow -> UPSERT -> 列表新增或替换。
  // 2. user+QQ=abc -> 本地拒绝 -> 不发送请求。
}

// removePermission 确认后删除显式权限规则。
// @param item：待删除权限策略。
// @returns Promise，在取消或删除结束后完成。
// ⚠️副作用说明：可能删除权限与审计记录、刷新快照并更新列表。
async function removePermission(item: PermissionState): Promise<void> {
  // [决策理由] 删除会立即改变权限回退结果，必须二次确认。
  if (!window.confirm('确定删除这条权限策略并恢复下一级规则吗？')) {
    return
  }
  pendingID.value = item.id
  errorMessage.value = ''
  try {
    await deletePermission(item.id)
    const remaining: PermissionState[] = []
    for (const permission of permissions.value) {
      // [决策理由] 仅移除后端已确认删除的主键，保留其他规则。
      if (permission.id !== item.id) {
        remaining.push(permission)
      }
    }
    permissions.value = remaining
  } catch (error) {
    setError(error, '删除权限策略失败')
  } finally {
    pendingID.value = 0
  }

  // >>> 数据演变示例
  // 1. 确认id8 -> DELETE -> 列表移除并回退下层规则。
  // 2. 取消 -> 无请求 -> 列表不变。
}

// setScope 切换全局或群级作用域并修正群号。
// @param value：global 或 group。
// @returns 无。
// ⚠️副作用说明：修改页面作用域和群号输入。
function setScope(value: 'global' | 'group'): void {
  scopeType.value = value
  // [决策理由] 全局规则固定scope_id=0，群级规则必须要求用户输入群号。
  if (value === 'global') {
    scopeID.value = '0'
  } else if (scopeID.value === '0') {
    // [决策理由] 从全局切到群级时不能沿用保留值0。
    scopeID.value = ''
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
  // 1. user切role -> subject_id=group_admin。
  // 2. role切user -> subject_id清空等待QQ。
}

// isPositiveUint64 校验群号或QQ是否为Go后端可解析的正uint64十进制数。
// @param value：待校验字符串。
// @returns 合法且非零、未溢出uint64时为true。
// ⚠️副作用说明：无。
function isPositiveUint64(value: string): boolean {
  // [决策理由] 先限制十进制格式，避免BigInt接受符号、空白等后端不接受的形式。
  if (!/^[1-9][0-9]*$/.test(value)) {
    return false
  }
  const valid = BigInt(value) <= 18446744073709551615n

  // >>> 数据演变示例
  // 1. "2769731875" -> 格式合法 -> 未超uint64 -> true。
  // 2. "0"或"18446744073709551616" -> 格式/范围失败 -> false。
  return valid
}

// setError 将未知异常转换为稳定页面提示。
// @param error：捕获值；fallback：默认提示。
// @returns 无。
// ⚠️副作用说明：修改页面错误状态。
function setError(error: unknown, fallback: string): void {
  // [决策理由] API 通常抛出Error，但仍需兼容未知运行时异常。
  if (error instanceof Error) {
    errorMessage.value = error.message
  } else {
    errorMessage.value = fallback
  }

  // >>> 数据演变示例
  // 1. Error("无权限") -> 显示无权限。
  // 2. unknown -> 显示fallback。
}

watch(pluginName, loadFeatures)
onMounted(loadPage)
</script>

<template>
  <section>
    <div class="page-heading">
      <div>
        <span class="eyebrow">ACCESS CONTROL</span>
        <h1>权限策略</h1>
        <p class="muted">为角色或指定 QQ 设置全局、群级的插件或功能权限。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="loadPage">刷新</button>
    </div>
    <p v-if="errorMessage" class="error-message banner">{{ errorMessage }}</p>

    <form class="panel permission-form" @submit.prevent="submitPermission">
      <div class="scope-control">
        <span>作用域</span>
        <div class="segmented">
          <button type="button" :class="{ active: scopeType === 'global' }" @click="setScope('global')">全局</button>
          <button type="button" :class="{ active: scopeType === 'group' }" @click="setScope('group')">指定群</button>
        </div>
      </div>
      <label v-if="scopeType === 'group'"><span>群号</span><input v-model="scopeID" inputmode="numeric" required /></label>
      <label v-if="fixedPluginName === ''"><span>插件</span><select v-model="pluginName" required><option value="" disabled>选择插件</option><option v-for="plugin in plugins" :key="plugin.name" :value="plugin.name">{{ plugin.display_name || plugin.name }}</option></select></label>
      <label><span>功能范围</span><select v-model="featureKey" :disabled="featureLoading || featureLoadFailed"><option value="">{{ featureLoading ? '正在加载功能…' : '插件全部功能' }}</option><option v-for="feature in features" :key="feature.key" :value="feature.key">{{ feature.display_name || feature.key }}</option></select></label>
      <div class="scope-control">
        <span>主体类型</span>
        <div class="segmented">
          <button type="button" :class="{ active: subjectType === 'role' }" @click="setSubjectType('role')">角色</button>
          <button type="button" :class="{ active: subjectType === 'user' }" @click="setSubjectType('user')">指定 QQ</button>
        </div>
      </div>
      <label v-if="subjectType === 'role'"><span>角色</span><select v-model="subjectID"><option v-for="role in roleOptions" :key="role.value" :value="role.value">{{ role.label }}</option></select></label>
      <label v-else><span>用户 QQ</span><input v-model="subjectID" inputmode="numeric" required /></label>
      <label><span>效果</span><select v-model="effect"><option value="allow">允许</option><option value="deny">拒绝</option></select></label>
      <button v-if="featureLoadFailed" class="ghost-button" type="button" @click="loadFeatures(pluginName)">重试功能加载</button>
      <button v-else class="primary-button" type="submit" :disabled="saving || pluginName === '' || featureLoading">{{ saving ? '保存中…' : '保存策略' }}</button>
    </form>

    <div class="permission-filters">
      <select v-model="filterScope"><option value="">全部作用域</option><option value="global">全局</option><option value="group">群级</option></select>
      <select v-if="fixedPluginName === ''" v-model="filterPlugin"><option value="">全部插件</option><option v-for="plugin in plugins" :key="plugin.name" :value="plugin.name">{{ plugin.display_name || plugin.name }}</option></select>
      <input v-model="filterSubject" placeholder="筛选角色或 QQ" />
      <button class="ghost-button" type="button" @click="filterScope = ''; filterPlugin = ''; filterSubject = ''">清空筛选</button>
    </div>

    <div v-if="loading" class="empty-state">正在读取权限策略…</div>
    <div v-else-if="filteredPermissions.length === 0" class="empty-state">没有符合条件的显式权限策略。</div>
    <div v-else class="panel table-wrap">
      <table>
        <thead><tr><th>作用域</th><th>目标</th><th>主体</th><th>效果</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="item in filteredPermissions" :key="item.id">
            <td><span class="scope-badge">{{ item.scope_type === 'global' ? '全局' : `群 ${item.scope_id}` }}</span></td>
            <td><code>{{ item.plugin_name }}{{ item.feature_key ? `.${item.feature_key}` : '（全部功能）' }}</code></td>
            <td><code>{{ item.subject_type === 'role' ? `角色：${item.subject_id}` : `QQ：${item.subject_id}` }}</code></td>
            <td><span class="scope-badge effect-badge" :class="item.effect">{{ item.effect === 'allow' ? '允许' : '拒绝' }}</span></td>
            <td><button class="danger-button compact" type="button" :disabled="pendingID === item.id" @click="removePermission(item)">删除并回退</button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
