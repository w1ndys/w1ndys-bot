<!-- 📌 影响范围：调用系统设置 API；修改后端设置覆盖、审计记录和运行时设置快照。 -->
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { listSettings, resetSetting, setSetting, type SettingState } from '../api'

const settings = ref<SettingState[]>([])
const drafts = reactive<Record<string, string>>({})
const loading = ref(true)
const pendingKey = ref('')
const errorMessage = ref('')

// loadPage 加载全部受控设置并同步编辑草稿。
// @param 无。
// @returns Promise，在设置读取完成后结束。
// ⚠️副作用说明：发起网络请求并替换设置列表和草稿。
async function loadPage(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const loaded = await listSettings()
    settings.value = loaded
    for (const key of Object.keys(drafts)) {
      delete drafts[key]
    }
    for (const item of loaded) {
      drafts[item.key] = String(item.value)
    }
  } catch (error) {
    setError(error, '加载系统设置失败')
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. prefix="!"+pageSize=20 -> 列表更新 -> drafts为可编辑字符串。
  // 2. API失败 -> errorMessage更新 -> loading=false。
}

// saveSetting 校验草稿类型并保存指定设置。
// @param item：待保存的设置状态。
// @returns Promise，在保存结束后完成。
// ⚠️副作用说明：可能写入设置与审计、热刷新后端快照并更新页面。
async function saveSetting(item: SettingState): Promise<void> {
  // [决策理由] 同一时刻只允许一个设置写操作，防止响应乱序覆盖页面状态。
  if (pendingKey.value !== '') {
    return
  }
  errorMessage.value = ''
  const value = parseDraft(item.key, drafts[item.key] ?? '')
  // [决策理由] null是页面用于表示输入校验失败的哨兵，不能发送到后端。
  if (value === null) {
    return
  }
  pendingKey.value = item.key
  try {
    const saved = await setSetting(item.key, value)
    replaceSetting(saved)
  } catch (error) {
    setError(error, '保存系统设置失败')
    errorMessage.value += '；数据库状态可能已变化，请刷新确认'
  } finally {
    pendingKey.value = ''
  }

  // >>> 数据演变示例
  // 1. command_prefix草稿="!" -> JSON字符串 -> PUT -> 卡片显示已覆盖。
  // 2. page_size草稿="abc" -> 本地校验失败 -> 不请求。
}

// restoreSetting 确认后删除数据库覆盖并重新读取默认值。
// @param item：待恢复默认的设置状态。
// @returns Promise，在取消或恢复结束后完成。
// ⚠️副作用说明：可能删除设置覆盖、写审计、刷新快照并重新请求设置列表。
async function restoreSetting(item: SettingState): Promise<void> {
  // [决策理由] 未覆盖项已经使用默认值，不应发送必然404的删除请求。
  if (!item.overridden) {
    return
  }
  // [决策理由] 恢复默认会立即影响运行时行为，必须二次确认。
  if (!window.confirm(`确定将“${item.description}”恢复为默认值吗？`)) {
    return
  }
  // [决策理由] 串行管理写操作，避免删除和保存交错。
  if (pendingKey.value !== '') {
    return
  }
  pendingKey.value = item.key
  errorMessage.value = ''
  try {
    await resetSetting(item.key)
    await loadPage()
  } catch (error) {
    setError(error, '恢复默认值失败')
    errorMessage.value += '；数据库状态可能已变化，请刷新确认'
  } finally {
    pendingKey.value = ''
  }

  // >>> 数据演变示例
  // 1. prefix已覆盖 -> 确认 -> DELETE -> 重载为默认"/"。
  // 2. 用户取消 -> 不请求且草稿不变。
}

// parseDraft 将输入字符串转换为设置定义要求的JSON类型。
// @param key：设置键；draft：页面输入字符串。
// @returns 合法字符串或整数；校验失败返回null并设置错误。
// ⚠️副作用说明：校验失败时修改页面错误状态。
function parseDraft(key: string, draft: string): string | number | null {
  // [决策理由] 命令前缀属于JSON字符串，需保持用户输入而非转成数字。
  if (key === 'command_prefix') {
    const runeCount = Array.from(draft).length
    // [决策理由] 与后端保持一致：前缀必须为1至4个非空白字符且首尾无空白。
    if (draft.trim() !== draft || runeCount < 1 || runeCount > 4) {
      errorMessage.value = '命令前缀必须为 1 至 4 个非空白字符'
      return null
    }
    return draft
  }
  // [决策理由] 数值设置只接受无符号十进制整数，避免浮点或隐式转换。
  if (!/^[0-9]+$/.test(draft)) {
    errorMessage.value = '该设置必须填写整数'
    return null
  }
  const value = Number(draft)
  // [决策理由] 页面只提交安全整数，最终业务范围仍由后端白名单校验。
  if (!Number.isSafeInteger(value)) {
    errorMessage.value = '整数超出安全范围'
    return null
  }
  // [决策理由] 与后端白名单范围保持一致，尽早提供具体字段错误。
  if (key === 'audit_retention_days' && (value < 1 || value > 3650)) {
    errorMessage.value = '审计日志保留天数必须在 1 至 3650 之间'
    return null
  }
  // [决策理由] 默认分页大小过小或过大会影响管理列表可用性。
  if (key === 'default_page_size' && (value < 10 || value > 200)) {
    errorMessage.value = '默认分页大小必须在 10 至 200 之间'
    return null
  }

  // >>> 数据演变示例
  // 1. command_prefix+"!" -> runeCount=1 -> 返回"!"。
  // 2. default_page_size+"20" -> 安全整数 -> 返回20；"1.5"返回null。
  return value
}

// replaceSetting 用后端权威状态替换对应列表项和草稿。
// @param saved：保存成功后的设置状态。
// @returns 无。
// ⚠️副作用说明：修改响应式设置列表与草稿。
function replaceSetting(saved: SettingState): void {
  for (let index = 0; index < settings.value.length; index += 1) {
    // [决策理由] 设置键是稳定主键，只替换完全匹配的卡片。
    if (settings.value[index].key === saved.key) {
      settings.value[index] = saved
      drafts[saved.key] = String(saved.value)
      return
    }
  }

  // >>> 数据演变示例
  // 1. 列表含prefix -> 保存prefix -> 替换卡片并同步草稿。
  // 2. 列表不含目标键 -> 不修改列表。
}

// setError 将未知异常转换为稳定页面提示。
// @param error：捕获值；fallback：默认提示。
// @returns 无。
// ⚠️副作用说明：修改页面错误状态。
function setError(error: unknown, fallback: string): void {
  // [决策理由] API通常抛出Error，但仍需兼容未知异常值。
  if (error instanceof Error) {
    errorMessage.value = error.message
  } else {
    errorMessage.value = fallback
  }

  // >>> 数据演变示例
  // 1. Error("值无效") -> 显示值无效。
  // 2. unknown -> 显示fallback。
}

onMounted(loadPage)
</script>

<template>
  <section>
    <div class="page-heading">
      <div>
        <span class="eyebrow">SYSTEM SETTINGS</span>
        <h1>系统设置</h1>
        <p class="muted">仅开放可安全热更新的业务设置；密钥与管理员账号仍由环境变量管理。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading || pendingKey !== ''" @click="loadPage">刷新</button>
    </div>
    <p v-if="errorMessage" class="error-message banner">{{ errorMessage }}</p>

    <div v-if="loading" class="empty-state">正在读取系统设置…</div>
    <div v-else-if="settings.length === 0" class="empty-state">当前版本没有开放可管理的系统设置。</div>
    <div v-else class="settings-grid">
      <article v-for="item in settings" :key="item.key" class="panel setting-card">
        <div>
          <code>{{ item.key }}</code>
          <h2>{{ item.description }}</h2>
          <span class="setting-status" :class="{ overridden: item.overridden }">{{ item.overridden ? '数据库覆盖' : '默认值' }}</span>
        </div>
        <label>
          <span>当前值</span>
          <input
            v-model="drafts[item.key]"
            :type="item.key === 'command_prefix' ? 'text' : 'number'"
            :min="item.key === 'audit_retention_days' ? 1 : item.key === 'default_page_size' ? 10 : undefined"
            :max="item.key === 'audit_retention_days' ? 3650 : item.key === 'default_page_size' ? 200 : undefined"
          />
        </label>
        <div class="setting-actions">
          <button class="primary-button compact" type="button" :disabled="pendingKey !== ''" @click="saveSetting(item)">保存</button>
          <button class="ghost-button compact" type="button" :disabled="pendingKey !== '' || !item.overridden" @click="restoreSetting(item)">恢复默认</button>
        </div>
      </article>
    </div>
  </section>
</template>
