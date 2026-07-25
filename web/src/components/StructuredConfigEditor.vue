<!-- 📌 影响范围：编辑结构化JSON字符串配置；仅在内存中解析和序列化，持久化由父级配置表单完成。 -->
<script setup lang="ts">
import { NButton, NInput, NInputNumber } from 'naive-ui'
import { ref, watch } from 'vue'

type StructuredKind = 'string_list_json' | 'weighted_terms_json' | 'combination_rules_json'

interface EditorRow {
  text: string
  weight: number | null
  terms: string
  bonus: number | null
}

const props = defineProps<{ modelValue: string; kind: StructuredKind }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const rows = ref<EditorRow[]>([])
const parseError = ref('')
let syncing = false

// emptyRow 创建当前编辑器类型的安全空行。
// @param 无；读取kind属性。
// @returns 可由对应控件编辑的空行。
// ⚠️副作用说明：无。
function emptyRow(): EditorRow {
  const result: EditorRow = { text: '', weight: 0, terms: '', bonus: 0 }

  // >>> 数据演变示例
  // 1. weighted_terms_json -> {text:"",weight:0}。
  // 2. combination_rules_json -> {terms:"",bonus:0}。
  return result
}

// loadValue 将服务端JSON字符串转换为结构化行。
// @param value：现有配置JSON字符串。
// @returns 无。
// ⚠️副作用说明：覆盖rows和字段内错误，不向父组件回写。
function loadValue(value: string): void {
  syncing = true
  parseError.value = ''
  try {
    const parsed = JSON.parse(value) as unknown
    // [决策理由] 服务端承诺根节点为数组，异常快照应在字段内明确显示而非伪造空配置。
    if (!Array.isArray(parsed)) {
      throw new Error('配置不是数组')
    }
    // [决策理由] 字符串列表只需要单列文本行。
    if (props.kind === 'string_list_json') {
      rows.value = parsed.map(item => {
        // [决策理由] 非字符串旧值无法无损编辑，必须阻止静默强制转换。
        if (typeof item !== 'string') {
          throw new Error('字符串列表包含非文本项')
        }
        return { ...emptyRow(), text: item }
      })
    } else if (props.kind === 'weighted_terms_json') {
      // [决策理由] 权重词条使用文本与数值双列，保持后端既有字段名。
      rows.value = parsed.map(item => {
        // [决策理由] 数组和null不具备词条对象语义，不能按普通对象读取。
        if (typeof item !== 'object' || item === null || Array.isArray(item)) {
          throw new Error('权重词条包含非对象项')
        }
        const record = item as Record<string, unknown>
        // [决策理由] 未知字段无法由结构化表单表达，允许编辑会在保存时静默丢失数据。
        if (Object.keys(record).some(key => key !== 'text' && key !== 'weight')) {
          throw new Error('权重词条包含未知字段')
        }
        // [决策理由] 文本和权重类型异常时继续编辑会覆盖原始数据。
        if (typeof record.text !== 'string' || typeof record.weight !== 'number' || !Number.isFinite(record.weight)) {
          throw new Error('权重词条必须包含文本text和有限数值weight')
        }
        return { ...emptyRow(), text: record.text, weight: record.weight }
      })
    } else {
      rows.value = parsed.map(item => {
        // [决策理由] 组合规则只能从普通对象安全恢复。
        if (typeof item !== 'object' || item === null || Array.isArray(item)) {
          throw new Error('组合规则包含非对象项')
        }
        const record = item as Record<string, unknown>
        // [决策理由] 组合规则只允许terms和bonus，保证前后端严格校验一致。
        if (Object.keys(record).some(key => key !== 'terms' && key !== 'bonus')) {
          throw new Error('组合规则包含未知字段')
        }
        // [决策理由] 组合词必须全部为文本且加分必须为有限数值，防止异常旧值被规范化覆盖。
        if (!Array.isArray(record.terms) || !record.terms.every(term => typeof term === 'string') || typeof record.bonus !== 'number' || !Number.isFinite(record.bonus)) {
          throw new Error('组合规则必须包含文本数组terms和有限数值bonus')
        }
        return { ...emptyRow(), terms: record.terms.join('，'), bonus: record.bonus }
      })
    }
  } catch (error) {
    rows.value = []
    parseError.value = error instanceof Error ? `现有配置无法解析：${error.message}` : '现有配置无法解析'
  } finally {
    syncing = false
  }

  // >>> 数据演变示例
  // 1. ["广告"]+string_list_json -> 一行“广告”。
  // 2. 非数组JSON -> rows=[]并显示字段错误。
}

// serializeRows 把当前结构化行编码为兼容既有数据库的JSON字符串。
// @param 无；读取rows和kind。
// @returns 规范JSON字符串。
// ⚠️副作用说明：无。
function serializeRows(): string {
  let value: unknown
  // [决策理由] 字符串列表持久化为原有string[]结构。
  if (props.kind === 'string_list_json') {
    value = rows.value.map(row => row.text)
  } else if (props.kind === 'weighted_terms_json') {
    // [决策理由] 风险词和安全词共享{text,weight}稳定结构。
    value = rows.value.map(row => ({ text: row.text, weight: row.weight ?? 0 }))
  } else {
    value = rows.value.map(row => ({ terms: row.terms.split(/[,，]/u).map(term => term.trim()).filter(Boolean), bonus: row.bonus ?? 0 }))
  }
  const result = JSON.stringify(value)

  // >>> 数据演变示例
  // 1. 风险词“免费”+25 -> [{"text":"免费","weight":25}]。
  // 2. terms“免费，加群”+20 -> [{"terms":["免费","加群"],"bonus":20}]。
  return result
}

// commit 将当前行变化同步给父配置草稿。
// @param 无。
// @returns 无。
// ⚠️副作用说明：触发update:modelValue事件。
function commit(): void {
  // [决策理由] 正在应用父级快照时不得形成反向写入和脏状态抖动。
  if (syncing) {
    return
  }
  parseError.value = ''
  emit('update:modelValue', serializeRows())

  // >>> 数据演变示例
  // 1. 修改weight -> emit新JSON字符串。
  // 2. loadValue同步中 -> 不emit。
}

// addRow 追加一条空规则并同步草稿。
// @param 无。
// @returns 无。
// ⚠️副作用说明：修改rows并触发父级更新。
function addRow(): void {
  rows.value.push(emptyRow())
  commit()

  // >>> 数据演变示例
  // 1. [] -> [空行] -> emit。
  // 2. [规则A] -> [规则A,空行] -> emit。
}

// removeRow 删除指定规则并同步草稿。
// @param index：当前行下标。
// @returns 无。
// ⚠️副作用说明：修改rows并触发父级更新。
function removeRow(index: number): void {
  // [决策理由] 越界索引可能来自过期DOM事件，必须忽略。
  if (index < 0 || index >= rows.value.length) {
    return
  }
  rows.value.splice(index, 1)
  commit()

  // >>> 数据演变示例
  // 1. [A,B]+index0 -> [B] -> emit。
  // 2. index越界 -> rows不变。
}

watch(() => props.modelValue, value => {
  // [决策理由] 本组件刚发出的规范值无需反向重建行，避免输入过程中丢失焦点或中文输入法组合态。
  if (parseError.value === '' && value === serializeRows()) {
    return
  }
  loadValue(value)
}, { immediate: true })
watch(() => props.kind, () => loadValue(props.modelValue))
</script>

<template>
  <div class="structured-editor">
    <div v-for="(row, index) in rows" :key="index" class="structured-row">
      <NInput v-if="kind === 'string_list_json'" v-model:value="row.text" placeholder="输入关键词" @update:value="commit" />
      <template v-else-if="kind === 'weighted_terms_json'">
        <NInput v-model:value="row.text" placeholder="关键词" @update:value="commit" />
        <NInputNumber v-model:value="row.weight" placeholder="权重" @update:value="commit" />
      </template>
      <template v-else>
        <NInput v-model:value="row.terms" placeholder="关键词，使用逗号分隔" @update:value="commit" />
        <NInputNumber v-model:value="row.bonus" placeholder="组合加分" @update:value="commit" />
      </template>
      <NButton type="error" tertiary @click="removeRow(index)">删除</NButton>
    </div>
    <p v-if="parseError" class="structured-error">{{ parseError }}</p>
    <NButton secondary type="primary" :disabled="parseError !== ''" @click="addRow">添加一项</NButton>
  </div>
</template>

<style scoped>
.structured-editor { display: grid; gap: var(--space-3); width: 100%; }
.structured-row { display: grid; grid-template-columns: minmax(12rem, 1fr) minmax(8rem, 0.35fr) auto; gap: var(--space-2); align-items: center; }
.structured-row:has(> .n-input:first-child:last-of-type) { grid-template-columns: minmax(12rem, 1fr) auto; }
.structured-error { margin: 0; color: var(--color-danger); }
@media (max-width: 39.9375rem) { .structured-row { grid-template-columns: 1fr; } }
</style>
