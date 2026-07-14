<!-- 📌 影响范围：调用审计日志 API；读取浏览器时区；只读展示管理员操作记录与前后 JSON 快照。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NDatePicker, NDrawer, NDrawerContent, NEmpty, NInput, NPagination, NSkeleton, NTable, NTag } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { getAuditLog, listAuditLogs, type AuditQuery, type AuditState, type AuditSummary } from '../api'

const items = ref<AuditSummary[]>([])
const loading = ref(true)
const detailLoading = ref(false)
const errorMessage = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const detail = ref<AuditState | null>(null)
const detailVisible = ref(false)
const listRequestSequence = ref(0)
const detailRequestSequence = ref(0)
const filters = reactive({ actorID: '', action: '', targetType: '', targetID: '', timeRange: null as [number, number] | null })
const pageCount = computed(calculatePageCount)

// calculatePageCount 计算后端总记录对应的页数。
// @param 无；读取总数和每页数量。
// @returns 至少为1的分页页数。
// ⚠️副作用说明：无。
function calculatePageCount(): number {
  const result = Math.max(1, Math.ceil(total.value / pageSize.value))

  // >>> 数据演变示例
  // 1. total=21,size=20 -> 1.05向上取整 -> 2页。
  // 2. total=0,size=20 -> 0与1取最大 -> 1页。
  return result
}

// buildQuery 将页面筛选转换为后端审计查询参数。
// @param 无；读取当前分页和筛选草稿。
// @returns 使用UTC RFC3339时间的审计查询。
// ⚠️副作用说明：无。
function buildQuery(): AuditQuery {
  const query: AuditQuery = { page: page.value, page_size: pageSize.value }
  const mappings = { actor_id: filters.actorID.trim(), action: filters.action.trim(), target_type: filters.targetType.trim(), target_id: filters.targetID.trim() }
  for (const [key, value] of Object.entries(mappings)) {
    // [决策理由] 只有非空精确筛选才应进入请求，空值表示不限制该维度。
    if (value !== '') {
      query[key as keyof typeof mappings] = value
    }
  }
  // [决策理由] 日期选择器输出本地时间戳，接口必须接收含时区语义的UTC RFC3339。
  if (filters.timeRange !== null) {
    query.start_time = new Date(filters.timeRange[0]).toISOString()
    query.end_time = new Date(filters.timeRange[1]).toISOString()
  }

  // >>> 数据演变示例
  // 1. action=plugin.enable+本地时间范围 -> trim动作+转UTC -> 精确查询。
  // 2. 筛选全空+page2 -> 不附加空字段 -> {page:2,page_size:20}。
  return query
}

// loadPage 从后端读取当前筛选对应的审计页。
// @param 无。
// @returns Promise，在列表状态更新后结束。
// ⚠️副作用说明：发起网络请求并替换审计列表、总数和错误状态。
async function loadPage(): Promise<void> {
  const sequence = ++listRequestSequence.value
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await listAuditLogs(buildQuery())
    // [决策理由] 较早筛选的迟到响应不得覆盖管理员最新选择的结果。
    if (sequence !== listRequestSequence.value) {
      return
    }
    items.value = result.items
    total.value = result.total
    page.value = result.page
    pageSize.value = result.page_size
  } catch (error) {
    // [决策理由] 过期请求的错误不属于当前页面条件，不应覆盖最新结果或提示。
    if (sequence === listRequestSequence.value) {
      items.value = []
      total.value = 0
      setError(error, '加载审计日志失败')
    }
  } finally {
    // [决策理由] 旧请求不能提前结束新请求的加载状态。
    if (sequence === listRequestSequence.value) {
      loading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. page1请求成功 -> 替换items和total -> 渲染表格。
  // 2. 请求失败 -> 保留当前页数据 -> 显示错误并结束加载。
}

// applyFilters 从第一页应用当前筛选。
// @param 无。
// @returns Promise，在筛选结果加载后结束。
// ⚠️副作用说明：重置页码并发起审计列表请求。
async function applyFilters(): Promise<void> {
  page.value = 1
  await loadPage()

  // >>> 数据演变示例
  // 1. 当前第3页+action筛选 -> page重置1 -> 加载筛选首屏。
  // 2. 空筛选 -> page重置1 -> 加载完整日志首屏。
}

// clearFilters 清空所有服务端筛选并返回第一页。
// @param 无。
// @returns Promise，在完整首屏加载后结束。
// ⚠️副作用说明：重置筛选、页码并发起网络请求。
async function clearFilters(): Promise<void> {
  filters.actorID = ''
  filters.action = ''
  filters.targetType = ''
  filters.targetID = ''
  filters.timeRange = null
  await applyFilters()

  // >>> 数据演变示例
  // 1. actor+时间已填写 -> 全部置空 -> 加载全部首屏。
  // 2. 已为空 -> 状态保持空 -> 刷新全部首屏。
}

// changePage 切换服务端分页。
// @param nextPage：用户选择的目标页码。
// @returns Promise，在目标页加载后结束。
// ⚠️副作用说明：修改页码并发起审计列表请求。
async function changePage(nextPage: number): Promise<void> {
  page.value = nextPage
  await loadPage()

  // >>> 数据演变示例
  // 1. page1点击2 -> page=2 -> 请求第2页。
  // 2. page2点击1 -> page=1 -> 请求首屏。
}

// openDetail 读取并打开审计详情抽屉。
// @param item：列表中的审计摘要。
// @returns Promise，在完整详情读取后结束。
// ⚠️副作用说明：发起详情请求并打开只读抽屉。
async function openDetail(item: AuditSummary): Promise<void> {
  const sequence = ++detailRequestSequence.value
  detail.value = null
  detailVisible.value = true
  detailLoading.value = true
  errorMessage.value = ''
  try {
    const loaded = await getAuditLog(item.id)
    // [决策理由] 用户关闭或改看其他记录后，迟到详情不得重新打开或覆盖抽屉。
    if (sequence === detailRequestSequence.value && detailVisible.value) {
      detail.value = loaded
    }
  } catch (error) {
    // [决策理由] 只处理当前仍打开的详情请求错误。
    if (sequence === detailRequestSequence.value && detailVisible.value) {
      detailVisible.value = false
      setError(error, '加载审计详情失败')
    }
  } finally {
    // [决策理由] 过期详情请求不能关闭新详情的加载态。
    if (sequence === detailRequestSequence.value) {
      detailLoading.value = false
    }
  }

  // >>> 数据演变示例
  // 1. 点击id8 -> 先打开摘要 -> GET详情 -> 展示完整快照。
  // 2. 详情不存在 -> 清空抽屉 -> 页面显示错误。
}

// closeDetail 响应抽屉可见状态并关闭详情。
// @param visible：抽屉请求更新后的可见状态。
// @returns 无。
// ⚠️副作用说明：关闭时清空当前审计详情。
function closeDetail(visible: boolean): void {
  // [决策理由] 打开动作由openDetail负责，此处理器只接受Naive UI发出的关闭通知。
  if (!visible) {
    detailRequestSequence.value += 1
    detailVisible.value = false
    detail.value = null
  }

  // >>> 数据演变示例
  // 1. visible=false -> detail清空 -> 抽屉关闭。
  // 2. visible=true -> 保留当前详情 -> 不重复请求。
}

// maskIdentifier 隐藏QQ号、群号等长数字标识的中间部分。
// @param value：审计记录中的操作者或目标标识。
// @returns 长数字掩码文本；非数字业务键原样返回。
// ⚠️副作用说明：无。
function maskIdentifier(value: string): string {
  // [决策理由] 短值和非纯数字通常是插件名或设置键，遮盖会降低排障价值。
  if (!/^\d{5,}$/.test(value)) {
    return value
  }
  const result = `${value.slice(0, 2)}${'*'.repeat(value.length - 4)}${value.slice(-2)}`

  // >>> 数据演变示例
  // 1. 2769731875 -> 保留首尾2位 -> 27******75。
  // 2. command_prefix -> 非长数字 -> 原样返回。
  return result
}

// formatLocalTime 将后端UTC时间转换为浏览器所在时区文本。
// @param value：RFC3339 UTC时间字符串。
// @returns 本地日期时间；无效值返回原文。
// ⚠️副作用说明：读取浏览器语言和时区设置。
function formatLocalTime(value: string): string {
  const date = new Date(value)
  // [决策理由] 异常历史数据不应让整个表格渲染失败，应保留原始值供排查。
  if (Number.isNaN(date.getTime())) {
    return value
  }
  const result = date.toLocaleString(undefined, { hour12: false })

  // >>> 数据演变示例
  // 1. 2026-07-13T02:00:00Z+Asia/Shanghai -> 2026/7/13 10:00:00。
  // 2. bad-time -> Date无效 -> 原样返回bad-time。
  return result
}

// formatSnapshot 格式化只读JSON快照。
// @param value：API返回的任意JSON值。
// @returns 两空格缩进的JSON文本。
// ⚠️副作用说明：无。
function formatSnapshot(value: unknown): string {
  const result = JSON.stringify(redactSnapshot(value), null, 2) ?? 'null'

  // >>> 数据演变示例
  // 1. {enabled:true} -> 脱敏扫描 -> 多行缩进JSON。
  // 2. {password:"abc"} -> password命中 -> {password:"[已脱敏]"}。
  return result
}

// redactSnapshot 递归隐藏审计快照中的常见敏感字段。
// @param value：任意JSON兼容值。
// @returns 保持原结构但替换敏感字段值的新值。
// ⚠️副作用说明：无；不修改API响应原对象。
function redactSnapshot(value: unknown): unknown {
  // [决策理由] 数组必须逐项复制，避免嵌套对象绕过字段脱敏。
  if (Array.isArray(value)) {
    return value.map(redactSnapshot)
  }
  // [决策理由] null和非对象值没有字段名上下文，可以原样展示。
  if (value === null || typeof value !== 'object') {
    return value
  }
  const result: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    // [决策理由] 密码、令牌、密钥和凭证字段不得在管理页面明文回显。
    if (/(password|token|secret|credential)/i.test(key)) {
      result[key] = '[已脱敏]'
    } else {
      result[key] = redactSnapshot(child)
    }
  }

  // >>> 数据演变示例
  // 1. {config:{token:"abc"}} -> 递归config -> token替换为[已脱敏]。
  // 2. [{enabled:true},null] -> 数组逐项复制 -> 原结构保持不变。
  return result
}

// setError 将未知异常转换为稳定页面提示。
// @param error：捕获值；fallback：非Error时默认消息。
// @returns 无。
// ⚠️副作用说明：修改页面错误状态。
function setError(error: unknown, fallback: string): void {
  // [决策理由] API通常抛出Error，但运行时仍需兼容未知异常值。
  if (error instanceof Error) {
    errorMessage.value = error.message
  } else {
    errorMessage.value = fallback
  }

  // >>> 数据演变示例
  // 1. Error("无权限") -> 显示“无权限”。
  // 2. unknown -> 显示fallback。
}

onMounted(loadPage)
</script>

<template>
  <section class="audit-page">
    <header class="page-header">
      <div><p class="section-label">安全与可追溯性</p><h1>审计日志</h1><p class="page-description">查看 WebUI 与 QQ 管理操作。记录为只读数据，时间按当前浏览器时区显示。</p></div>
      <NButton secondary type="primary" :loading="loading" @click="loadPage">刷新日志</NButton>
    </header>

    <NAlert v-if="errorMessage" type="error" closable @close="errorMessage = ''">{{ errorMessage }}</NAlert>

    <NCard title="筛选记录" size="small">
      <div class="filters">
        <NInput v-model:value="filters.actorID" clearable placeholder="管理员 QQ（精确）" @keyup.enter="applyFilters" />
        <NInput v-model:value="filters.action" clearable placeholder="操作，例如 plugin.enable" @keyup.enter="applyFilters" />
        <NInput v-model:value="filters.targetType" clearable placeholder="资源类型，例如 plugin" @keyup.enter="applyFilters" />
        <NInput v-model:value="filters.targetID" clearable placeholder="资源 ID（精确）" @keyup.enter="applyFilters" />
        <NDatePicker v-model:value="filters.timeRange" type="datetimerange" clearable start-placeholder="开始时间" end-placeholder="结束时间" />
        <div class="filter-actions"><NButton type="primary" :loading="loading" @click="applyFilters">查询</NButton><NButton secondary :disabled="loading" @click="clearFilters">清空</NButton></div>
      </div>
    </NCard>

    <NCard title="操作记录" size="small">
      <template #header-extra><NTag size="small">共 {{ total }} 条</NTag></template>
      <div v-if="loading" class="skeleton-list"><NSkeleton v-for="index in 5" :key="index" text :repeat="2" /></div>
      <NEmpty v-else-if="items.length === 0" description="没有符合筛选条件的审计记录" />
      <template v-else>
        <div class="table-scroll">
          <NTable :single-line="false" size="small">
            <thead><tr><th>时间</th><th>操作者</th><th>操作</th><th>目标</th><th>结果</th><th>请求 ID</th><th class="action-column">操作</th></tr></thead>
            <tbody><tr v-for="item in items" :key="item.id"><td class="time-cell">{{ formatLocalTime(item.created_at) }}</td><td><code>{{ maskIdentifier(item.actor_id) }}</code><div class="cell-detail">{{ item.channel }} · {{ item.actor_role }}</div></td><td><code>{{ item.action }}</code></td><td><code>{{ item.target_type }} / {{ maskIdentifier(item.target_id) }}</code></td><td><NTag :type="item.success ? 'success' : 'error'" size="small">{{ item.success ? '成功' : '失败' }}</NTag></td><td><code>{{ item.request_id || '—' }}</code></td><td><NButton size="small" secondary type="primary" @click="openDetail(item)">查看详情</NButton></td></tr></tbody>
          </NTable>
        </div>
        <div class="pagination"><NPagination :page="page" :page-count="pageCount" :disabled="loading" @update:page="changePage" /></div>
      </template>
    </NCard>

    <NDrawer :show="detailVisible" placement="right" width="min(560px, 100vw)" @update:show="closeDetail">
      <NDrawerContent title="审计详情" closable>
        <div v-if="detailLoading" class="skeleton-list"><NSkeleton text :repeat="6" /></div>
        <div v-if="detail" class="detail-content" :aria-busy="detailLoading">
          <dl class="detail-list"><dt>记录 ID</dt><dd>{{ detail.id }}</dd><dt>本地时间</dt><dd>{{ formatLocalTime(detail.created_at) }}</dd><dt>操作者</dt><dd><code>{{ maskIdentifier(detail.actor_id) }}</code>（{{ detail.actor_role }} / {{ detail.channel }}）</dd><dt>操作</dt><dd><code>{{ detail.action }}</code></dd><dt>目标</dt><dd><code>{{ detail.target_type }} / {{ maskIdentifier(detail.target_id) }}</code></dd><dt>结果</dt><dd><NTag :type="detail.success ? 'success' : 'error'" size="small">{{ detail.success ? '成功' : '失败' }}</NTag></dd><dt>请求 ID</dt><dd><code>{{ detail.request_id || '—' }}</code></dd></dl>
          <NAlert v-if="detail.error_message" type="error" title="错误信息">{{ detail.error_message }}</NAlert>
          <section class="snapshot"><h3>变更前</h3><pre>{{ formatSnapshot(detail.before) }}</pre></section>
          <section class="snapshot"><h3>变更后</h3><pre>{{ formatSnapshot(detail.after) }}</pre></section>
        </div>
      </NDrawerContent>
    </NDrawer>
  </section>
</template>

<style scoped>
.audit-page { display: grid; gap: var(--space-6); }
.filters { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--space-3); }
.filters :deep(.n-date-picker) { grid-column: span 2; }
.filter-actions { display: flex; gap: var(--space-2); grid-column: span 2; justify-content: flex-end; }
.table-scroll { overflow-x: auto; }
.table-scroll :deep(table) { min-width: 980px; }
.time-cell { white-space: nowrap; }
.cell-detail { color: var(--color-text-muted); font-size: var(--font-size-caption); margin-top: var(--space-1); }
.action-column { width: 92px; }
.skeleton-list { display: grid; gap: var(--space-4); min-height: 240px; }
.pagination { display: flex; justify-content: flex-end; padding-top: var(--space-5); }
.detail-content { display: grid; gap: var(--space-5); }
.detail-list { display: grid; grid-template-columns: 96px minmax(0, 1fr); margin: 0; row-gap: var(--space-3); }
.detail-list dt { color: var(--color-text-muted); }
.detail-list dd { margin: 0; overflow-wrap: anywhere; }
.snapshot h3 { font-size: var(--font-size-h5); margin: 0 0 var(--space-2); }
.snapshot pre { background: var(--color-bg-page); border: 1px solid var(--color-border); border-radius: var(--radius-md); font-family: var(--font-mono); font-size: var(--font-size-caption); line-height: 1.6; margin: 0; max-height: 320px; overflow: auto; padding: var(--space-4); white-space: pre-wrap; word-break: break-word; }
@media (max-width: 1023px) { .filters { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 639px) { .audit-page { gap: var(--space-4); } .filters { grid-template-columns: 1fr; } .filters :deep(.n-date-picker), .filter-actions { grid-column: span 1; } .filter-actions :deep(.n-button) { min-height: 44px; flex: 1; } .pagination { justify-content: center; } }
</style>
