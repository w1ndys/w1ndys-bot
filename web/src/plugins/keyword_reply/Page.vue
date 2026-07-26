<!-- 📌 影响范围：读写指定群的关键词回复规则；写操作会修改数据库、审计并刷新插件运行快照。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NInput, NModal, NPagination, NSpace, NSwitch, type DataTableColumns } from 'naive-ui'
import { h, reactive, ref } from 'vue'
import { createKeywordRule, deleteKeywordRule, listKeywordRules, updateKeywordRule, type KeywordRule } from './api'
import { useAppFeedback } from '../../feedback'

const feedback = useAppFeedback()
const groupInput = ref('')
const groupID = ref(0)
const rules = ref<KeywordRule[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const saving = ref(false)
const errorMessage = ref('')
const editorVisible = ref(false)
const editing = ref<KeywordRule | null>(null)
const draft = reactive({ keyword: '', reply_content: '', enabled: true })
let loadSequence = 0

// loadRules 读取当前群的规则分页。
// @param 无；使用已确认的 groupID 与 page。
// @returns Promise，在列表状态稳定后结束。
// ⚠️副作用说明：发起鉴权请求并替换列表状态。
async function loadRules(): Promise<boolean> {
  if (groupID.value <= 0) return false
  const requestedGroup = groupID.value
  const sequence = ++loadSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await listKeywordRules(requestedGroup, page.value, pageSize)
    // [决策理由] 切换群号后的旧请求不得覆盖新群列表。
    if (sequence !== loadSequence || requestedGroup !== groupID.value) return false
    rules.value = result.items
    total.value = result.total
  } catch (error) {
    if (sequence !== loadSequence || requestedGroup !== groupID.value) return false
    errorMessage.value = error instanceof Error ? error.message : '加载关键词规则失败'
    return false
  } finally {
    if (sequence === loadSequence && requestedGroup === groupID.value) loading.value = false
  }

  // >>> 数据演变示例
  // 1. 群100 -> 该群规则分页 -> 渲染表格。
  // 2. 请求失败 -> errorMessage -> false。
  return true
}

// confirmGroup 确认手工输入的群号并加载该群规则。
// @param 无。
// @returns Promise，在首页加载后结束。
// ⚠️副作用说明：改变当前群作用域并发起请求。
async function confirmGroup(): Promise<void> {
  // [决策理由] 前端先拒绝非正整数，后端仍以路径参数为唯一可信来源再次校验。
  if (!/^[1-9]\d{0,18}$/.test(groupInput.value)) {
    feedback.warning('请输入正确的 QQ 群号')
    return
  }
  groupID.value = Number(groupInput.value)
  page.value = 1
  await loadRules()

  // >>> 数据演变示例
  // 1. 输入 100 -> 切换作用域 -> 加载该群规则。
  // 2. 输入 abc -> 前端拒绝 -> 不发请求。
}

// openEditor 打开新增或编辑弹窗。
// @param rule：待编辑规则；为 null 表示新增。
// @returns 无。
// ⚠️副作用说明：重置草稿并显示弹窗。
function openEditor(rule: KeywordRule | null): void {
  editing.value = rule
  draft.keyword = rule?.keyword ?? ''
  draft.reply_content = rule?.reply_content ?? ''
  draft.enabled = rule?.enabled ?? true
  editorVisible.value = true

  // >>> 数据演变示例
  // 1. null -> 空草稿 -> 新增模式。
  // 2. 已有规则 -> 填充草稿 -> 编辑模式。
}

// submitEditor 保存新增或更新。
// @param 无。
// @returns Promise，在保存结束后完成。
// ⚠️副作用说明：写入后端规则、审计并刷新插件运行快照。
async function submitEditor(): Promise<void> {
  const target = editing.value
  saving.value = true
  try {
    const input = { keyword: draft.keyword, reply_content: draft.reply_content, enabled: draft.enabled }
    if (target === null) {
      await createKeywordRule(groupID.value, input)
    } else {
      await updateKeywordRule(groupID.value, target.id, target.version, input)
    }
    editorVisible.value = false
    const refreshed = await loadRules()
    // [决策理由] 写入成功但权威重读失败时不得误报“已保存”。
    if (refreshed) feedback.success(target === null ? '关键词规则已新增' : '关键词规则已更新')
  } catch (error) {
    feedback.error(error, '保存关键词规则失败', '，请刷新后重试')
    await loadRules()
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. 新增"你好" -> POST -> 列表刷新。
  // 2. 陈旧版本 -> 409 -> 提示并重载。
}

// removeRule 按乐观锁删除规则。
// @param rule：目标规则。
// @returns Promise，在删除结束后完成。
// ⚠️副作用说明：删除后端记录、写审计并刷新插件运行快照。
async function removeRule(rule: KeywordRule): Promise<void> {
  saving.value = true
  try {
    await deleteKeywordRule(groupID.value, rule.id, rule.version)
    const refreshed = await loadRules()
    if (refreshed) feedback.success('关键词规则已删除')
  } catch (error) {
    feedback.error(error, '删除关键词规则失败', '，请刷新后重试')
    await loadRules()
  } finally {
    saving.value = false
  }

  // >>> 数据演变示例
  // 1. 规则1+v2 -> DELETE -> 列表移除。
  // 2. 已被他人删除 -> 404 -> 提示并重载。
}

// changePage 切换分页并重新加载。
// @param next：目标页码。
// @returns Promise，在加载完成后结束。
// ⚠️副作用说明：发起鉴权请求。
async function changePage(next: number): Promise<void> {
  page.value = next
  await loadRules()

  // >>> 数据演变示例
  // 1. 第2页 -> 请求 offset -> 渲染。
  // 2. 同页重复点击 -> 重新读取权威列表。
}

const columns: DataTableColumns<KeywordRule> = [
  { title: '关键词', key: 'keyword' },
  { title: '回复内容', key: 'reply_content', ellipsis: { tooltip: true } },
  {
    title: '启用',
    key: 'enabled',
    render: (rule) => h(NSwitch, { value: rule.enabled, disabled: true }),
  },
  {
    title: '操作',
    key: 'actions',
    render: (rule) =>
      h(NSpace, null, {
        default: () => [
          h(NButton, { size: 'small', disabled: saving.value, onClick: () => openEditor(rule) }, { default: () => '编辑' }),
          h(NButton, { size: 'small', disabled: saving.value, onClick: () => removeRule(rule) }, { default: () => '删除' }),
        ],
      }),
  },
]
</script>

<template>
  <NSpace vertical size="large">
    <NAlert v-if="errorMessage" type="error">{{ errorMessage }}</NAlert>
    <NCard title="选择群">
      <NSpace>
        <NInput v-model:value="groupInput" :disabled="loading" placeholder="手工输入 QQ 群号" />
        <NButton :disabled="loading" @click="confirmGroup">加载该群规则</NButton>
      </NSpace>
    </NCard>
    <NEmpty v-if="groupID <= 0" description="请先输入群号，规则按群隔离管理" />
    <NCard v-else :title="`群 ${groupID} 的关键词规则`">
      <template #header-extra>
        <NButton :disabled="saving" @click="openEditor(null)">新增规则</NButton>
      </template>
      <NSpace vertical>
        <NDataTable :columns="columns" :data="rules" :loading="loading" :bordered="false" size="small" />
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="changePage" />
      </NSpace>
    </NCard>
    <NModal v-model:show="editorVisible" preset="card" style="max-width: 520px" :title="editing ? '编辑关键词规则' : '新增关键词规则'">
      <NSpace vertical>
        <NInput v-model:value="draft.keyword" :disabled="saving" placeholder="关键词（与消息完全相等时触发）" />
        <NInput v-model:value="draft.reply_content" type="textarea" :disabled="saving" placeholder="回复内容" />
        <NSpace align="center">
          <NSwitch v-model:value="draft.enabled" :disabled="saving" />
          <span>启用该规则</span>
        </NSpace>
        <NButton :disabled="saving" @click="submitEditor">保存</NButton>
      </NSpace>
    </NModal>
  </NSpace>
</template>
