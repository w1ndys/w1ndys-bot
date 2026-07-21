<!-- 📌 影响范围：读取插件资源描述和分页记录；新增、编辑、删除会修改插件业务数据并产生审计。 -->
<script setup lang="ts">
import { NButton, NCard, NEmpty, NForm, NFormItem, NInput, NModal, NPagination, NPopconfirm, NSelect, NSkeleton, NSwitch, NTable, NTag } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import {
  ApiError,
  createPluginResourceRecord,
  deletePluginResourceRecord,
  listPluginResourceRecords,
  listPluginResources,
  updatePluginResourceRecord,
  type PluginResourceDescriptor,
  type PluginConfigField,
  type PluginResourceRecord,
} from '../api'
import { useAppFeedback } from '../feedback'

const props = defineProps<{ pluginName: string }>()
const descriptors = ref<PluginResourceDescriptor[]>([])
const resourceKey = ref('')
const records = ref<PluginResourceRecord[]>([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const loading = ref(true)
const saving = ref(false)
const feedback = useAppFeedback()
const modalVisible = ref(false)
const editingRecord = ref<PluginResourceRecord | null>(null)
const draft = ref<Record<string, unknown>>({})
const loadSequence = ref(0)
const recordsLoadSequence = ref(0)
const activeDescriptor = computed(() => descriptors.value.find(item => item.key === resourceKey.value) ?? null)
const listedFields = computed(() => activeDescriptor.value?.fields ?? [])
const editableFields = computed(() => {
  const descriptor = activeDescriptor.value
  // [决策理由] 插件证据字段必须展示但不能回传修改，只有未声明只读的字段进入表单载荷。
  if (descriptor === null) {
    return []
  }
  const readOnly = new Set(descriptor.read_only_fields ?? [])
  const result = descriptor.fields.filter(field => !readOnly.has(field.key))

  // >>> 数据演变示例
  // 1. fields=[content,status],readonly=[content] -> 编辑表单仅status。
  // 2. readonly缺失 -> 所有字段保持现有可编辑行为。
  return result
})
const resourceOptions = computed(() => descriptors.value.map(item => ({ label: item.display_name, value: item.key })))

// emptyValue 为字段类型创建安全的空表单值。
// @param field：资源字段描述。
// @returns boolean 字段返回 false，其余字段返回空字符串。
// ⚠️副作用说明：无。
function emptyValue(field: PluginConfigField): unknown {
  const result = field.type === 'boolean' ? false : ''

  // >>> 数据演变示例
  // 1. boolean -> false。
  // 2. multiline -> ""。
  return result
}

// displayValue 将未知资源字段值转换为安全表格文本。
// @param field：字段描述；value：记录中的未知值。
// @returns 适合表格展示的字符串。
// ⚠️副作用说明：无。
function displayValue(field: PluginConfigField, value: unknown): string {
  // [决策理由] boolean 由专用状态标签呈现，此返回值仅作为可访问文本和异常回退。
  if (field.type === 'boolean') {
    return value === true ? '启用' : '停用'
  }
  const result = typeof value === 'string' ? value : value === null || value === undefined ? '—' : String(value)

  // >>> 数据演变示例
  // 1. string值="你好" -> "你好"。
  // 2. 值缺失 -> "—"。
  return result
}

// loadDescriptors 读取插件资源能力并选择首个资源。
// @param 无；使用 pluginName 属性。
// @returns Promise，在描述和首屏记录稳定后结束。
// ⚠️副作用说明：发起鉴权请求并覆盖资源选择、分页和提示状态。
async function loadDescriptors(): Promise<void> {
  const requestedPlugin = props.pluginName
  const sequence = ++loadSequence.value
  recordsLoadSequence.value += 1
  loading.value = true
  descriptors.value = []
  records.value = []
  resourceKey.value = ''
  try {
    const result = await listPluginResources(requestedPlugin)
    // [决策理由] 插件切换后的旧请求不得覆盖当前插件资源描述。
    if (sequence !== loadSequence.value || requestedPlugin !== props.pluginName) {
      return
    }
    descriptors.value = result
    // [决策理由] 有资源时默认选择首项，确保页面无需插件专属路由即可使用。
    if (result.length > 0) {
      resourceKey.value = result[0].key
      page.value = 1
      pageSize.value = Math.min(20, result[0].max_page_size)
      await loadRecords(sequence)
    }
  } catch (error) {
    // [决策理由] 仅最新插件请求可以展示错误。
    if (sequence === loadSequence.value && requestedPlugin === props.pluginName) {
      // [决策理由] 未声明业务资源是普通插件的正常能力差异，应呈现空状态而非故障。
      if (error instanceof ApiError && error.code === 'plugin_resource_not_supported') {
        descriptors.value = []
      } else {
        feedback.error(error, '加载插件业务资源失败')
      }
    }
  } finally {
    // [决策理由] 旧请求不得提前结束新插件的加载动画。
    if (sequence === loadSequence.value && requestedPlugin === props.pluginName) {
      loading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. 插件声明rules -> 选择rules -> 加载第1页。
  // 2. 插件返回resource_not_supported -> descriptors=[] -> 展示空状态。
}

// loadRecords 读取当前资源分页记录。
// @param expectedSequence：可选的所属描述加载序号。
// @returns Promise，当前请求成功应用到页面时为 true，过期或失败时为 false。
// ⚠️副作用说明：发起鉴权网络请求并覆盖记录列表。
async function loadRecords(expectedSequence = loadSequence.value): Promise<boolean> {
  const requestedPlugin = props.pluginName
  const requestedResource = resourceKey.value
  const requestedPage = page.value
  const requestSequence = ++recordsLoadSequence.value
  // [决策理由] 未选择资源时没有合法列表端点。
  if (requestedResource === '') {
    return false
  }
  loading.value = true
  try {
    const result = await listPluginResourceRecords(requestedPlugin, requestedResource, page.value, pageSize.value)
    // [决策理由] 插件、资源或请求序号变化后旧分页结果已经失效。
    if (requestSequence !== recordsLoadSequence.value || expectedSequence !== loadSequence.value || requestedPlugin !== props.pluginName || requestedResource !== resourceKey.value || requestedPage !== page.value) {
      return false
    }
    records.value = result.items
    page.value = result.page
    pageSize.value = result.page_size
    total.value = result.total
  } catch (error) {
    // [决策理由] 当前资源的失败才应覆盖页面提示。
    if (requestSequence === recordsLoadSequence.value && expectedSequence === loadSequence.value && requestedPlugin === props.pluginName && requestedResource === resourceKey.value && requestedPage === page.value) {
      feedback.error(error, '加载资源记录失败')
    }
    return false
  } finally {
    // [决策理由] 当前资源请求完成后才结束加载状态。
    if (requestSequence === recordsLoadSequence.value && expectedSequence === loadSequence.value && requestedPlugin === props.pluginName && requestedResource === resourceKey.value && requestedPage === page.value) {
      loading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. page=1,size=20 -> 返回8条,total=8 -> 更新分页。
  // 2. 同页旧请求晚于写后刷新返回 -> requestSequence过期 -> 丢弃旧结果并返回false。
  return true
}

// switchResource 切换描述器资源并读取首屏。
// @param key：资源稳定键。
// @returns Promise，在首屏加载后结束。
// ⚠️副作用说明：修改当前资源与页码并发起列表请求。
async function switchResource(key: string): Promise<void> {
  resourceKey.value = key
  page.value = 1
  await loadRecords()

  // >>> 数据演变示例
  // 1. rules切到jobs -> page=1 -> 加载jobs。
  // 2. 当前空资源选择rules -> resourceKey=rules -> 加载首屏。
}

// openCreate 根据 Descriptor 初始化新增表单。
// @param 无。
// @returns 无。
// ⚠️副作用说明：打开弹窗并清空编辑记录和表单草稿。
function openCreate(): void {
  const descriptor = activeDescriptor.value
  // [决策理由] 未加载资源或资源禁止新增时不能打开无效表单。
  if (descriptor === null || !descriptor.can_create) {
    return
  }
  editingRecord.value = null
  draft.value = Object.fromEntries(descriptor.fields.map(field => [field.key, field.default ?? emptyValue(field)]))
  modalVisible.value = true

  // >>> 数据演变示例
  // 1. fields=[string,boolean] -> draft={string:"",boolean:false} -> 打开新增框。
  // 2. create=false -> 保持弹窗关闭。
}

// openEdit 使用记录快照初始化可编辑字段。
// @param record：待编辑的权威记录。
// @returns 无。
// ⚠️副作用说明：打开弹窗并复制记录数据作为草稿。
function openEdit(record: PluginResourceRecord): void {
  const descriptor = activeDescriptor.value
  // [决策理由] 未加载资源或资源禁止编辑时不能发送更新。
  if (descriptor === null || !descriptor.can_update) {
    return
  }
  editingRecord.value = record
  draft.value = Object.fromEntries(descriptor.fields.map(field => [field.key, record.data[field.key] ?? field.default ?? emptyValue(field)]))
  modalVisible.value = true

  // >>> 数据演变示例
  // 1. record.data={keyword:"hi"} -> draft复制hi -> 打开编辑框。
  // 2. edit=false -> 保持弹窗关闭。
}

// buildDraftPayload 仅输出当前操作允许的字段。
// @param fields：当前新增或编辑字段描述。
// @returns 待提交的字段对象。
// ⚠️副作用说明：无。
function buildDraftPayload(fields: PluginConfigField[]): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const field of fields) {
    result[field.key] = draft.value[field.key]
  }

  // >>> 数据演变示例
  // 1. 允许keyword、reply -> 草稿含两项 -> 输出两项。
  // 2. 草稿额外含id -> fields不含id -> 输出不含id。
  return result
}

// saveRecord 新增或按读取版本编辑资源记录。
// @param 无。
// @returns Promise，在保存与列表刷新后结束。
// ⚠️副作用说明：写入插件业务数据与审计；冲突时关闭弹窗并刷新权威列表。
async function saveRecord(): Promise<void> {
  const descriptor = activeDescriptor.value
  // [决策理由] 无资源描述或重复保存不能形成合法写请求。
  if (descriptor === null || saving.value) {
    return
  }
  const requestedPlugin = props.pluginName
  const requestedResource = descriptor.key
  const requestSequence = loadSequence.value
  const creating = editingRecord.value === null
  saving.value = true
  try {
    const payload = buildDraftPayload(editableFields.value)
    // [决策理由] 是否存在编辑快照决定调用新增或版本化更新端点。
    if (creating) {
      await createPluginResourceRecord(requestedPlugin, requestedResource, payload)
    } else {
      await updatePluginResourceRecord(requestedPlugin, requestedResource, editingRecord.value!.id, payload, editingRecord.value!.version)
    }
    // [决策理由] 插件或资源切换后，旧写响应不得关闭新弹窗或显示过期成功Toast。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName || requestedResource !== resourceKey.value) {
      return
    }
    modalVisible.value = false
    const refreshed = await loadRecords(requestSequence)
    // [决策理由] 只有权威记录列表成功重读后才能确认保存结果已展示。
    if (refreshed) {
      feedback.success(creating ? '记录已新增' : '记录已更新')
    }
  } catch (error) {
    // [决策理由] 已切换插件或资源的旧写错误不得污染当前页面反馈。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName || requestedResource !== resourceKey.value) {
      return
    }
    // [决策理由] 版本冲突必须放弃陈旧编辑上下文并刷新服务端权威列表。
    if (error instanceof ApiError && error.status === 409) {
      modalVisible.value = false
      feedback.warning('记录已被其他操作更新，列表已刷新，请重新编辑。')
      await loadRecords(loadSequence.value)
    } else {
      feedback.error(error, '保存记录失败')
    }
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. 新增草稿 -> POST -> 关闭弹窗并刷新列表。
  // 2. 编辑v1但服务端v2 -> 409 -> 关闭弹窗、提示并刷新。
}

// removeRecord 使用当前版本删除记录。
// @param record：待删除记录快照。
// @returns Promise，在删除与列表刷新后结束。
// ⚠️副作用说明：删除插件业务数据并产生审计记录。
async function removeRecord(record: PluginResourceRecord): Promise<void> {
  const descriptor = activeDescriptor.value
  // [决策理由] 未加载资源或资源禁止删除时不能发送删除请求。
  if (descriptor === null || !descriptor.can_delete) {
    return
  }
  const requestedPlugin = props.pluginName
  const requestedResource = descriptor.key
  const requestSequence = loadSequence.value
  try {
    await deletePluginResourceRecord(requestedPlugin, requestedResource, record.id, record.version)
    // [决策理由] 插件或资源切换后不得使用旧列表长度调整当前分页。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName || requestedResource !== resourceKey.value) {
      return
    }
    // [决策理由] 删除当前页最后一项时回退一页，避免停留在空的越界页。
    if (records.value.length === 1 && page.value > 1) {
      page.value -= 1
    }
    const refreshed = await loadRecords(requestSequence)
    // [决策理由] 删除结果必须在权威列表成功重读后才显示成功Toast。
    if (refreshed) {
      feedback.success('记录已删除')
    }
  } catch (error) {
    // [决策理由] 已切换插件或资源的旧删除错误不得污染当前页面反馈。
    if (requestSequence !== loadSequence.value || requestedPlugin !== props.pluginName || requestedResource !== resourceKey.value) {
      return
    }
    // [决策理由] 删除冲突需要刷新权威列表，避免继续操作陈旧版本。
    if (error instanceof ApiError && error.status === 409) {
      feedback.warning('记录已被其他操作更新，列表已刷新。')
      await loadRecords(loadSequence.value)
    } else {
      feedback.error(error, '删除记录失败')
    }
  }

  // >>> 数据演变示例
  // 1. id=3,v2 -> DELETE成功 -> 刷新当前页。
  // 2. id=3版本过期 -> 409 -> 提示并刷新列表。
}

// changePage 切换分页并读取记录。
// @param value：目标页码。
// @returns Promise，在分页加载后结束。
// ⚠️副作用说明：修改当前页并发起鉴权请求。
async function changePage(value: number): Promise<void> {
  page.value = value
  await loadRecords()

  // >>> 数据演变示例
  // 1. 1 -> 2 -> 加载第2页。
  // 2. 2 -> 1 -> 加载首屏。
}

watch(() => props.pluginName, loadDescriptors)
onMounted(loadDescriptors)
</script>

<template>
  <section class="resource-panel" :aria-busy="loading">
    <div class="resource-heading">
      <div>
        <h2>{{ activeDescriptor?.display_name || '插件业务数据' }}</h2>
        <p>{{ activeDescriptor?.description || '界面由插件资源描述自动生成。' }}</p>
      </div>
      <div class="resource-actions">
        <NSelect v-if="descriptors.length > 1" :value="resourceKey" :options="resourceOptions" aria-label="选择业务资源" @update:value="switchResource" />
        <NButton v-if="activeDescriptor?.can_create" type="primary" @click="openCreate">新增记录</NButton>
      </div>
    </div>

    <NSkeleton v-if="loading" text :repeat="4" />
    <NEmpty v-else-if="descriptors.length === 0" description="该插件未声明可管理的业务资源" />
    <template v-else-if="activeDescriptor">
      <div class="resource-table-scroll">
        <NTable striped single-line>
          <thead><tr><th v-for="field in listedFields" :key="field.key">{{ field.display_name }}</th><th v-if="activeDescriptor.can_update || activeDescriptor.can_delete" class="operation-column">操作</th></tr></thead>
          <tbody>
            <tr v-for="record in records" :key="record.id">
              <td v-for="field in listedFields" :key="field.key">
                <NTag v-if="field.type === 'boolean'" size="small" :type="record.data[field.key] === true ? 'success' : 'default'">{{ displayValue(field, record.data[field.key]) }}</NTag>
                <span v-else class="field-value">{{ displayValue(field, record.data[field.key]) }}</span>
              </td>
              <td v-if="activeDescriptor.can_update || activeDescriptor.can_delete" class="operation-cell">
                <NButton v-if="activeDescriptor.can_update" size="small" secondary @click="openEdit(record)">编辑</NButton>
                <NPopconfirm v-if="activeDescriptor.can_delete" positive-text="确认删除" negative-text="取消" @positive-click="removeRecord(record)">
                  <template #trigger><NButton size="small" type="error" tertiary>删除</NButton></template>
                  删除后无法恢复，确定继续吗？
                </NPopconfirm>
              </td>
            </tr>
          </tbody>
        </NTable>
      </div>
      <NEmpty v-if="records.length === 0" class="records-empty" description="暂无记录" />
      <NPagination v-if="total > pageSize" class="resource-pagination" :page="page" :page-size="pageSize" :item-count="total" @update:page="changePage" />
    </template>

    <NModal v-model:show="modalVisible" :mask-closable="!saving">
      <NCard class="resource-modal" :title="editingRecord === null ? `新增${activeDescriptor?.display_name ?? '记录'}` : `编辑${activeDescriptor?.display_name ?? '记录'}`" closable @close="modalVisible = false">
        <NForm label-placement="top" @submit.prevent="saveRecord">
          <NFormItem v-for="field in editableFields" :key="field.key" :label="field.display_name" :required="field.required" :feedback="field.description">
            <NSwitch v-if="field.type === 'boolean'" v-model:value="draft[field.key] as boolean" />
            <NSelect v-else-if="field.type === 'enum'" v-model:value="draft[field.key] as string" :options="(field.options ?? []).map(value => ({ label: value, value }))" />
            <NInput v-else v-model:value="draft[field.key] as string" :type="field.type === 'multiline' ? 'textarea' : 'text'" />
          </NFormItem>
          <div class="modal-actions"><NButton :disabled="saving" @click="modalVisible = false">取消</NButton><NButton attr-type="submit" type="primary" :loading="saving">保存</NButton></div>
        </NForm>
      </NCard>
    </NModal>
  </section>
</template>

<style scoped>
.resource-panel { padding: var(--space-5); border: 0.0625rem solid var(--color-border); border-radius: var(--radius-md); background: var(--color-bg-card); }
.resource-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); margin-bottom: var(--space-4); }
.resource-heading h2 { margin: 0; color: var(--color-text-primary); font-size: var(--font-size-h3); line-height: var(--line-height-h3); }
.resource-heading p { margin: var(--space-1) 0 0; color: var(--color-text-muted); font-size: var(--font-size-body-sm); line-height: var(--line-height-body-sm); }
.resource-actions, .operation-cell, .modal-actions { display: flex; align-items: center; gap: var(--space-3); }
.resource-actions :deep(.n-select) { min-width: 12rem; }
.resource-table-scroll { overflow-x: auto; }
.operation-column { width: 10rem; }
.operation-cell { white-space: nowrap; }
.field-value { display: block; max-width: 32rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.records-empty { padding: var(--space-8) 0; border-bottom: 0.0625rem solid var(--color-border); }
.resource-pagination { display: flex; justify-content: flex-end; margin-top: var(--space-4); }
.resource-modal { width: min(36rem, calc(100vw - 2rem)); }
.modal-actions { justify-content: flex-end; }
@media (max-width: 39.9375rem) {
  .resource-panel { padding: var(--space-4); }
  .resource-heading { flex-direction: column; }
  .resource-actions { width: 100%; }
  .resource-actions :deep(.n-select) { min-width: 0; flex: 1; }
}
</style>
