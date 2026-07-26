<!-- 📌 影响范围：读写目标架构插件的小型标量配置；保存会写数据库、审计并热应用插件配置。 -->
<script setup lang="ts">
import { NAlert, NButton, NForm, NFormItem, NInput, NInputNumber, NSelect, NSkeleton, NSpace, NSwitch } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { getPluginRuntimeConfig, putPluginRuntimeConfig, type PluginConfigField, type PluginConfigSchema } from '../api'
import { useAppFeedback } from '../feedback'

const props = defineProps<{ pluginKey: string }>()
const feedback = useAppFeedback()
const schema = ref<PluginConfigSchema | null>(null)
const draft = ref<Record<string, unknown>>({})
const baseline = ref<Record<string, unknown>>({})
const version = ref(0)
const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
let loadSequence = 0
const dirty = computed(() => JSON.stringify(draft.value) !== JSON.stringify(baseline.value))

// buildDraft 将后端脱敏快照转换为表单草稿，secret 始终初始化为空。
// @param fields：Schema 字段；config：后端已补齐默认值的脱敏配置。
// @returns 可编辑且不包含历史 secret 的表单对象。
// ⚠️副作用说明：无，仅创建新对象。
function buildDraft(fields: PluginConfigField[], config: Record<string, unknown>): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const field of fields) {
    // [决策理由] secret 是 write-only 字段，空草稿表示保存时保留后端现有值。
    if (field.type === 'secret') {
      result[field.key] = ''
      continue
    }
    if (Object.prototype.hasOwnProperty.call(config, field.key)) {
      result[field.key] = config[field.key]
      continue
    }
    // [决策理由] 后端已按 Schema 补齐默认值，此处兜底仅防御异常快照。
    result[field.key] = field.type === 'boolean' ? false : field.type === 'integer' ? null : ''
  }

  // >>> 数据演变示例
  // 1. fields=[string,secret]+config={prefix:"x"} -> {prefix:"x",token:""}。
  // 2. boolean 缺失 -> {enabled:false}。
  return result
}

// buildPayload 创建完整非敏感配置，并仅包含用户实际填写的 secret。
// @param fields：当前 Schema 字段。
// @returns 满足后端 secret 省略保留语义的配置对象。
// ⚠️副作用说明：无。
function buildPayload(fields: PluginConfigField[]): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const field of fields) {
    const value = draft.value[field.key]
    // [决策理由] 未填写的 secret 必须整体省略，否则会把已保存的密钥覆盖成空串。
    if (field.type === 'secret' && (value === '' || value === undefined || value === null)) {
      continue
    }
    result[field.key] = value
  }

  // >>> 数据演变示例
  // 1. token 留空 -> 载荷不含 token -> 后端保留原值。
  // 2. token 填写新值 -> 载荷含明文 -> 后端整体替换。
  return result
}

// loadConfig 读取 Schema 与当前脱敏配置快照。
// @param 无；使用 pluginKey 属性。
// @returns Promise，在表单状态稳定后结束。
// ⚠️副作用说明：发起鉴权请求并覆盖草稿、版本与提示状态。
async function loadConfig(): Promise<void> {
  const requestedKey = props.pluginKey
  const sequence = ++loadSequence
  loading.value = true
  saving.value = false
  errorMessage.value = ''
  try {
    const state = await getPluginRuntimeConfig(requestedKey)
    // [决策理由] 切换插件后的旧请求不得覆盖新表单状态。
    if (sequence !== loadSequence || requestedKey !== props.pluginKey) return
    const next = buildDraft(state.schema.fields, state.config)
    schema.value = state.schema
    draft.value = next
    baseline.value = { ...next }
    version.value = state.version
  } catch (error) {
    if (sequence !== loadSequence || requestedKey !== props.pluginKey) return
    errorMessage.value = error instanceof Error ? error.message : '加载插件配置失败'
  } finally {
    if (sequence === loadSequence && requestedKey === props.pluginKey) loading.value = false
  }

  // >>> 数据演变示例
  // 1. echo -> Schema+v2 脱敏快照 -> 表单可编辑且基线一致。
  // 2. 请求失败 -> errorMessage -> 保留旧表单。
}

// saveConfig 按乐观锁保存配置并热应用。
// @param 无。
// @returns Promise，在保存结束后完成。
// ⚠️副作用说明：写入后端配置、审计并触发插件热应用。
async function saveConfig(): Promise<void> {
  if (schema.value === null) return
  const requestedKey = props.pluginKey
  saving.value = true
  try {
    const state = await putPluginRuntimeConfig(requestedKey, buildPayload(schema.value.fields), version.value)
    // [决策理由] 保存期间切换插件时不得把结果写回新表单。
    if (requestedKey !== props.pluginKey) return
    const next = buildDraft(state.schema.fields, state.config)
    draft.value = next
    baseline.value = { ...next }
    version.value = state.version
    feedback.success('插件配置已保存并热应用')
  } catch (error) {
    if (requestedKey !== props.pluginKey) return
    feedback.error(error, '保存插件配置失败', '，已重新读取最新配置')
    await loadConfig()
  } finally {
    if (requestedKey === props.pluginKey) saving.value = false
  }

  // >>> 数据演变示例
  // 1. prefix 改为 [bot] -> PUT v1 -> v2 且立即生效。
  // 2. 陈旧版本 -> 409 -> 提示并重载权威配置。
}

// updateField 写入单个字段草稿值。
// @param key：字段键；value：控件最新值。
// @returns 无。
// ⚠️副作用说明：替换草稿对象以触发脏值比对。
function updateField(key: string, value: unknown): void {
  draft.value = { ...draft.value, [key]: value }

  // >>> 数据演变示例
  // 1. prefix="[bot] " -> 草稿更新 -> dirty=true。
  // 2. 改回原值 -> dirty=false。
}

// enumOptions 构建枚举字段的下拉选项。
// @param field：枚举配置字段。
// @returns Naive UI 选项数组。
// ⚠️副作用说明：无。
function enumOptions(field: PluginConfigField): { label: string; value: string }[] {
  const result = (field.options ?? []).map((option) => ({ label: option, value: option }))

  // >>> 数据演变示例
  // 1. options=[a,b] -> [{label:a,value:a},{label:b,value:b}]。
  // 2. 无 options -> []。
  return result
}

watch(() => props.pluginKey, loadConfig)
onMounted(loadConfig)
</script>

<template>
  <NSpace vertical>
    <NSkeleton v-if="loading" text :repeat="2" />
    <NAlert v-else-if="errorMessage" type="error">{{ errorMessage }}</NAlert>
    <NForm v-else-if="schema" label-placement="top">
      <NFormItem v-for="field in schema.fields" :key="field.key" :label="field.display_name">
        <NSpace vertical style="width: 100%">
          <NSwitch v-if="field.type === 'boolean'" :value="draft[field.key] as boolean" :disabled="saving" @update:value="(value: boolean) => updateField(field.key, value)" />
          <NInputNumber v-else-if="field.type === 'integer'" :value="draft[field.key] as number | null" :disabled="saving" @update:value="(value: number | null) => updateField(field.key, value)" />
          <NSelect v-else-if="field.type === 'enum'" :value="draft[field.key] as string" :options="enumOptions(field)" :disabled="saving" @update:value="(value: string) => updateField(field.key, value)" />
          <NInput v-else-if="field.type === 'multiline'" type="textarea" :value="draft[field.key] as string" :disabled="saving" @update:value="(value: string) => updateField(field.key, value)" />
          <NInput v-else-if="field.type === 'secret'" type="password" show-password-on="click" placeholder="留空表示保留现有值" :value="draft[field.key] as string" :disabled="saving" @update:value="(value: string) => updateField(field.key, value)" />
          <NInput v-else :value="draft[field.key] as string" :disabled="saving" @update:value="(value: string) => updateField(field.key, value)" />
          <span v-if="field.description" class="config-hint">{{ field.description }}</span>
        </NSpace>
      </NFormItem>
      <NButton :disabled="saving || !dirty" @click="saveConfig">保存并热应用</NButton>
    </NForm>
  </NSpace>
</template>

<style scoped>
.config-hint {
  font-size: 12px;
  opacity: 0.7;
}
</style>
