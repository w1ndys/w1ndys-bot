<!-- 📌 影响范围：读取和更新指定插件声明式配置；保存会写数据库、审计并热应用插件配置。 -->
<script setup lang="ts">
import { NAlert, NButton, NEmpty, NForm, NFormItem, NInput, NInputNumber, NSelect, NSkeleton, NSwitch, NTag } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { ApiError, getPluginConfig, getPluginConfigSchema, putPluginConfig, type PluginConfigField, type PluginConfigSchema } from '../api'

const props = defineProps<{ pluginName: string }>()
const schema = ref<PluginConfigSchema | null>(null)
const draft = ref<Record<string, unknown>>({})
const baseline = ref<Record<string, unknown>>({})
const version = ref(0)
const loading = ref(true)
const saving = ref(false)
const unsupported = ref(false)
const conflict = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const loadSequence = ref(0)
const dirty = computed(() => JSON.stringify(draft.value) !== JSON.stringify(baseline.value))

// applySnapshot 将后端脱敏快照转换为表单草稿，secret 始终初始化为空。
// @param fields：Schema 字段；config：后端已应用默认值的脱敏配置。
// @returns 可编辑且不包含历史 secret 的表单对象。
// ⚠️副作用说明：无，仅创建新对象。
function applySnapshot(fields: PluginConfigField[], config: Record<string, unknown>): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const field of fields) {
    // [决策理由] secret 是 write-only 字段，空草稿表示保存时保留现有值。
    if (field.type === 'secret') {
      result[field.key] = ''
      continue
    }
    // [决策理由] 配置快照由后端规范化，存在的值应作为唯一权威展示值。
    if (Object.prototype.hasOwnProperty.call(config, field.key)) {
      result[field.key] = config[field.key]
      continue
    }
    // [决策理由] 兼容尚未规范化的旧快照，缺失值回退 Schema 默认值或控件安全空值。
    if (field.default !== undefined) {
      result[field.key] = field.default
    } else {
      result[field.key] = field.type === 'boolean' ? false : field.type === 'integer' ? null : ''
    }
  }

  // >>> 数据演变示例
  // 1. fields=[string,secret]+config={name:"x"} -> {name:"x",secret:""}。
  // 2. boolean缺失且无默认值 -> {enabled:false}。
  return result
}

// loadConfig 读取 Schema 与当前脱敏配置快照。
// @param 无；使用 pluginName 属性。
// @returns Promise，在表单状态稳定后结束。
// ⚠️副作用说明：发起两个鉴权请求并覆盖草稿、版本和提示状态。
async function loadConfig(): Promise<void> {
  const requestedPlugin = props.pluginName
  const requestSequence = ++loadSequence.value
  loading.value = true
  saving.value = false
  unsupported.value = false
  conflict.value = false
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const loadedSchema = await getPluginConfigSchema(requestedPlugin)
    const state = await getPluginConfig(requestedPlugin)
    // [决策理由] 路由切换后的旧请求不得覆盖新插件已经加载的表单状态。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName) {
      return
    }
    const nextDraft = applySnapshot(loadedSchema.fields, state.config)
    schema.value = loadedSchema
    draft.value = nextDraft
    baseline.value = { ...nextDraft }
    version.value = state.version
  } catch (error) {
    // [决策理由] 已被新插件加载替代的失败结果不应污染当前页面提示。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName) {
      return
    }
    // [决策理由] 不支持声明式配置是正常能力差异，应呈现空状态而非操作失败。
    if (error instanceof ApiError && error.code === 'plugin_config_not_supported') {
      unsupported.value = true
      schema.value = null
    } else {
      errorMessage.value = error instanceof Error ? error.message : '加载插件配置失败'
    }
  } finally {
    // [决策理由] 只有最新请求可以结束当前插件的加载状态。
    if (requestSequence === loadSequence.value && requestedPlugin === props.pluginName) {
      loading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. echo支持配置 -> Schema+v2快照 -> 表单可编辑且基线一致。
  // 2. echo请求未完成时切到admin -> 旧结果丢弃 -> admin状态保持权威。
}

// buildPayload 创建完整非敏感配置，并仅包含用户实际填写的 secret。
// @param fields：当前 Schema 字段。
// @returns 满足后端 secret 省略保留语义的配置对象。
// ⚠️副作用说明：无，仅复制表单值。
function buildPayload(fields: PluginConfigField[]): Record<string, unknown> {
  const payload: Record<string, unknown> = {}
  for (const field of fields) {
    const value = draft.value[field.key]
    // [决策理由] 空 secret 必须从请求中省略，后端才能保留历史秘密。
    if (field.type === 'secret' && value === '') {
      continue
    }
    // [决策理由] 无默认值的可选字段使用控件空值表达“未设置”，必须省略而非物化为无效或意外零值。
    if (!field.required && field.default === undefined && (value === null || value === '' || value === false)) {
      continue
    }
    payload[field.key] = value
  }

  // >>> 数据演变示例
  // 1. prefix=x,token="",optionalInteger=null -> {prefix:"x"}，空字段省略。
  // 2. prefix=x,token=new -> {prefix:"x",token:"new"}。
  return payload
}

// saveConfig 以读取版本保存配置并采用服务端返回快照重建基线。
// @param 无；读取当前 Schema、草稿和版本。
// @returns Promise，在保存成功或错误状态更新后结束。
// ⚠️副作用说明：可能更新数据库、审计和插件运行时配置。
async function saveConfig(): Promise<void> {
  // [决策理由] 未加载 Schema、无改动或已有保存时不发送重复写请求。
  if (schema.value === null || !dirty.value || saving.value) {
    return
  }
  saving.value = true
  const requestedPlugin = props.pluginName
  const requestSequence = loadSequence.value
  conflict.value = false
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const state = await putPluginConfig(requestedPlugin, buildPayload(schema.value.fields), version.value)
    // [决策理由] 保存期间切换插件后，旧插件响应不得覆盖新插件表单。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName) {
      return
    }
    const nextDraft = applySnapshot(schema.value.fields, state.config)
    draft.value = nextDraft
    baseline.value = { ...nextDraft }
    version.value = state.version
    successMessage.value = '配置已保存并热应用。'
  } catch (error) {
    // [决策理由] 路由切换后的旧保存结果不属于当前插件，不显示冲突或错误。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName) {
      return
    }
    // [决策理由] 版本冲突需要保留用户草稿，同时明确要求重新加载权威版本。
    if (error instanceof ApiError && error.code === 'plugin_config_conflict') {
      conflict.value = true
      errorMessage.value = '配置已被其他操作更新，请重新加载后再修改。'
    } else {
      errorMessage.value = error instanceof Error ? error.message : '保存插件配置失败'
    }
  } finally {
    // [决策理由] 新插件自己的保存状态不能被旧请求结束回调覆盖。
    if (requestSequence === loadSequence.value && requestedPlugin === props.pluginName) {
      saving.value = false
    }
  }

  // >>> 数据演变示例
  // 1. 草稿prefix=x,v2 -> PUT成功v3 -> 重建基线、dirty=false。
  // 2. 保存期间切换插件 -> 旧响应丢弃 -> 新插件表单保持权威。
}

// resetDraft 放弃未保存修改并恢复最近一次服务端快照。
// @param 无。
// @returns 无。
// ⚠️副作用说明：覆盖当前表单草稿并清理保存提示。
function resetDraft(): void {
  draft.value = { ...baseline.value }
  errorMessage.value = ''
  successMessage.value = ''
  conflict.value = false

  // >>> 数据演变示例
  // 1. baseline={prefix:""},draft={prefix:"x"} -> draft恢复为空。
  // 2. secret草稿=new -> secret恢复为空且不会写入。
}

watch(() => props.pluginName, loadConfig)
onMounted(loadConfig)
</script>

<template>
  <section class="config-panel" :aria-busy="loading">
    <div class="config-heading">
      <div>
        <h2>插件配置</h2>
        <p>表单由插件 Schema 自动生成，保存后立即热应用。敏感字段不会回显。</p>
      </div>
      <NTag v-if="schema" size="small">版本 {{ version }}</NTag>
    </div>

    <NSkeleton v-if="loading" text :repeat="3" />
    <NEmpty v-else-if="unsupported" description="该插件暂无声明式配置" />
    <template v-else>
      <NAlert v-if="errorMessage" class="config-alert" :type="conflict ? 'warning' : 'error'" title="配置未保存">
        <div class="alert-content"><span>{{ errorMessage }}</span><NButton size="small" secondary @click="loadConfig">重新加载</NButton></div>
      </NAlert>
      <NAlert v-if="successMessage" class="config-alert" type="success" closable @close="successMessage = ''">{{ successMessage }}</NAlert>
      <NForm v-if="schema && schema.fields.length > 0" label-placement="top" @submit.prevent="saveConfig">
        <NFormItem v-for="field in schema.fields" :key="field.key" :label="field.display_name" :required="field.required" :feedback="field.type === 'secret' ? `${field.description ? `${field.description}；` : ''}留空将保留当前值` : field.description">
          <NSwitch v-if="field.type === 'boolean'" v-model:value="draft[field.key] as boolean" />
          <NInputNumber v-else-if="field.type === 'integer'" v-model:value="draft[field.key] as number | null" :precision="0" />
          <NSelect v-else-if="field.type === 'enum'" v-model:value="draft[field.key] as string" :options="(field.options ?? []).map(value => ({ label: value, value }))" />
          <NInput v-else v-model:value="draft[field.key] as string" :type="field.type === 'multiline' ? 'textarea' : field.type === 'secret' ? 'password' : 'text'" :show-password-on="field.type === 'secret' ? 'click' : undefined" :placeholder="field.type === 'secret' ? '留空以保留当前值' : undefined" />
        </NFormItem>
        <div class="config-actions">
          <span class="dirty-hint">{{ dirty ? '有未保存修改' : '已与服务端同步' }}</span>
          <NButton secondary :disabled="!dirty || saving" @click="resetDraft">撤销修改</NButton>
          <NButton attr-type="submit" type="primary" :loading="saving" :disabled="!dirty">保存配置</NButton>
        </div>
      </NForm>
      <NEmpty v-else-if="schema" description="该插件未声明可编辑字段" />
    </template>
  </section>
</template>

<style scoped>
.config-panel { padding: var(--space-5); border: 0.0625rem solid var(--color-border); border-radius: var(--radius-md); background: var(--color-bg-card); }
.config-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); margin-bottom: var(--space-4); }
.config-heading h2 { margin: 0; color: var(--color-text-primary); font-size: var(--font-size-h3); line-height: var(--line-height-h3); }
.config-heading p { margin: var(--space-1) 0 0; color: var(--color-text-muted); font-size: var(--font-size-body-sm); line-height: var(--line-height-body-sm); }
.config-alert { margin-bottom: var(--space-4); }
.alert-content, .config-actions { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); }
.config-actions { justify-content: flex-end; padding-top: var(--space-2); }
.dirty-hint { margin-right: auto; color: var(--color-text-muted); font-size: var(--font-size-body-sm); }
@media (max-width: 39.9375rem) {
  .config-panel { padding: var(--space-4); }
  .alert-content { align-items: flex-start; flex-direction: column; }
  .config-actions { align-items: stretch; flex-direction: column; }
  .dirty-hint { margin-right: 0; }
}
</style>
