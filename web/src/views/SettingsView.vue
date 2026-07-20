<!-- 📌 影响范围：调用系统设置 API；修改后端设置覆盖、审计记录和运行时设置快照。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NInput, NPopconfirm, NSpin, NTag } from 'naive-ui'
import { onMounted, reactive, ref } from 'vue'
import { listSettings, resetSetting, setSetting, type SettingState } from '../api'
import { useAppFeedback } from '../feedback'

const settings = ref<SettingState[]>([])
const drafts = reactive<Record<string, string>>({})
const loading = ref(true)
const pendingKey = ref('')
const errorMessage = ref('')
const feedback = useAppFeedback()

// loadPage 加载全部受控设置并同步编辑草稿。
// @param 无。
// @returns Promise，设置成功更新到页面时为 true，读取失败时为 false。
// ⚠️副作用说明：发起网络请求并替换设置列表和草稿。
async function loadPage(): Promise<boolean> {
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
    return false
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. prefix="!"+pageSize=20 -> 列表更新 -> drafts为可编辑字符串。
  // 2. API失败 -> errorMessage更新 -> loading=false并返回false。
  return true
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
    feedback.success('系统设置已保存并热更新')
  } catch (error) {
    feedback.error(error, '保存系统设置失败', '；数据库状态可能已变化，请刷新确认')
  } finally {
    pendingKey.value = ''
  }

  // >>> 数据演变示例
  // 1. command_prefix草稿="!" -> JSON字符串 -> PUT -> 卡片显示已覆盖。
  // 2. page_size草稿="abc" -> 本地校验失败 -> 不请求。
}

// restoreSetting 删除数据库覆盖并重新读取默认值。
// @param item：待恢复默认的设置状态。
// @returns Promise，在取消或恢复结束后完成。
// ⚠️副作用说明：可能删除设置覆盖、写审计、刷新快照并重新请求设置列表。
async function restoreSetting(item: SettingState): Promise<void> {
  // [决策理由] 未覆盖项已经使用默认值，不应发送必然404的删除请求。
  if (!item.overridden) {
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
    const refreshed = await loadPage()
    // [决策理由] 删除覆盖后必须成功重读默认状态才能确认页面已完成恢复。
    if (refreshed) {
      feedback.success('系统设置已恢复默认值')
    }
  } catch (error) {
    feedback.error(error, '恢复默认值失败', '；数据库状态可能已变化，请刷新确认')
  } finally {
    pendingKey.value = ''
  }

  // >>> 数据演变示例
  // 1. prefix已覆盖 -> 确认 -> DELETE -> 重载为默认"/"。
  // 2. 删除失败 -> 保留页面状态并提示刷新确认。
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
  <section class="settings-page">
    <header class="page-header">
      <div>
        <p class="section-label">系统级配置</p>
        <h1>系统设置</h1>
        <p class="page-description">管理允许在线调整的运行参数。密钥、管理员账号与连接凭据仍由部署环境统一维护。</p>
      </div>
      <NButton secondary type="primary" :loading="loading" :disabled="pendingKey !== ''" @click="loadPage">刷新设置</NButton>
    </header>

    <NAlert class="hot-update-alert" type="info" :show-icon="true">
      保存或恢复默认后会立即写入运行时快照，无需重启机器人。每次变更都会记录审计日志。
    </NAlert>
    <NAlert v-if="errorMessage" class="settings-alert" type="error" closable @close="errorMessage = ''">
      {{ errorMessage }}
    </NAlert>

    <NSpin :show="loading" description="正在读取系统设置…">
      <NEmpty v-if="!loading && settings.length === 0" description="当前版本没有开放可管理的系统设置" />
      <NCard v-else class="settings-panel" :bordered="true">
        <div class="settings-heading">
          <div>
            <h2>运行参数</h2>
            <p>数据库覆盖值优先于程序默认值；恢复默认会删除对应覆盖记录。</p>
          </div>
          <div class="legend" aria-label="设置来源图例">
            <NTag size="small">程序默认</NTag>
            <NTag type="warning" size="small">数据库覆盖</NTag>
          </div>
        </div>

        <div class="settings-list">
          <article v-for="item in settings" :key="item.key" class="setting-row">
            <div class="setting-identity">
              <div class="setting-title">
                <h3>{{ item.description }}</h3>
                <NTag :type="item.overridden ? 'warning' : 'default'" size="small">
                  {{ item.overridden ? '数据库覆盖' : '程序默认' }}
                </NTag>
              </div>
              <code>{{ item.key }}</code>
              <p>{{ item.overridden ? '当前值来自数据库，重启后仍然生效。' : '当前使用程序内置默认值，尚未写入数据库。' }}</p>
            </div>

            <label class="value-editor">
              <span>当前有效值</span>
              <NInput
                v-model:value="drafts[item.key]"
                :input-props="item.key === 'command_prefix' ? {} : { type: 'number' }"
                :placeholder="item.description"
                :disabled="pendingKey !== ''"
              />
            </label>

            <div class="setting-actions">
              <NButton type="primary" :loading="pendingKey === item.key" :disabled="pendingKey !== ''" @click="saveSetting(item)">
                保存并热更新
              </NButton>
              <NPopconfirm
                positive-text="确认恢复"
                negative-text="取消"
                @positive-click="restoreSetting(item)"
              >
                <template #trigger>
                  <NButton secondary :disabled="pendingKey !== '' || !item.overridden">恢复默认</NButton>
                </template>
                恢复后将立即删除“{{ item.description }}”的数据库覆盖并使用程序默认值。
              </NPopconfirm>
            </div>
          </article>
        </div>
      </NCard>
    </NSpin>
  </section>
</template>

<style scoped>
.settings-page {
  width: 100%;
}

.page-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1.5rem;
  margin-bottom: 1.5rem;
}

.section-label {
  margin: 0 0 0.25rem;
  color: var(--color-primary);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.04em;
}

h1 {
  margin: 0;
  color: var(--color-text-primary);
  font-size: 1.75rem;
  line-height: 2.25rem;
  letter-spacing: -0.015em;
}

.page-description {
  max-width: 42.5rem;
  margin: 0.5rem 0 0;
  color: var(--color-text-secondary);
  font-size: 0.875rem;
  line-height: 1.375rem;
}

.hot-update-alert,
.settings-alert {
  margin-bottom: 1rem;
}

.settings-panel {
  border-color: var(--color-border);
  border-radius: 0.5rem;
  box-shadow: none;
}

.settings-panel :deep(.n-card__content) {
  padding: 0;
}

.settings-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1.5rem;
  padding: 1.25rem;
  border-bottom: 1px solid var(--color-border);
}

.settings-heading h2 {
  margin: 0;
  color: var(--color-text-primary);
  font-size: 1.125rem;
  line-height: 1.625rem;
}

.settings-heading p {
  margin: 0.25rem 0 0;
  color: var(--color-text-muted);
  font-size: 0.8125rem;
  line-height: 1.25rem;
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.settings-list {
  display: grid;
}

.setting-row {
  min-height: 9rem;
  display: grid;
  grid-template-columns: minmax(18rem, 1fr) minmax(13rem, 0.65fr) auto;
  align-items: center;
  gap: 1.5rem;
  padding: 1.25rem;
  border-bottom: 1px solid var(--color-divider);
}

.setting-row:last-child {
  border-bottom: 0;
}

.setting-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.setting-title h3 {
  margin: 0;
  color: var(--color-text-primary);
  font-size: 1rem;
  line-height: 1.5rem;
}

.setting-identity code {
  display: block;
  margin-top: 0.375rem;
  color: var(--color-text-secondary);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}

.setting-identity p {
  margin: 0.5rem 0 0;
  color: var(--color-text-muted);
  font-size: 0.75rem;
  line-height: 1.125rem;
}

.value-editor {
  display: grid;
  gap: 0.5rem;
  color: var(--color-text-secondary);
  font-size: 0.75rem;
}

.setting-actions {
  display: flex;
  gap: 0.5rem;
}

@media (max-width: 63.9375rem) {
  .setting-row {
    grid-template-columns: minmax(0, 1fr) minmax(12rem, 0.75fr);
  }

  .setting-actions {
    grid-column: 1 / -1;
  }
}

@media (max-width: 39.9375rem) {
  .page-header,
  .settings-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .setting-row {
    grid-template-columns: 1fr;
  }

  .setting-actions {
    grid-column: auto;
    flex-direction: column;
  }

  .setting-actions :deep(.n-button) {
    min-height: 2.75rem;
  }
}
</style>
