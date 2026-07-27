<!-- 📌 影响范围：管理违禁复核、文本试判、训练样本和检测词库；操作结果统一通过应用级 Toast 展示。 -->
<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NDescriptions, NDescriptionsItem, NEmpty, NInput, NInputNumber, NModal, NPagination, NPopconfirm, NSelect, NSpace, NTabPane, NTabs, NTag, type DataTableColumns } from 'naive-ui'
import { h, onMounted, reactive, ref } from 'vue'
import { useAppFeedback } from '../../feedback'
import * as api from './api'

const feedback = useAppFeedback()
const pageSize = 20
const busy = ref(false)
const errorMessage = ref('')
const violations = ref<api.RecordItem<api.ViolationData>[]>([]), violationPage = ref(1), violationTotal = ref(0)
const samples = ref<api.RecordItem<api.SampleData>[]>([]), samplePage = ref(1), sampleTotal = ref(0)
const terms = ref<api.Term[]>([]), termPage = ref(1), termTotal = ref(0), termKind = ref('')
const combinations = ref<api.Combination[]>([]), combinationPage = ref(1), combinationTotal = ref(0)
const trialText = ref(''), trial = ref<api.RecordItem<api.TrialData> | null>(null), testedText = ref(''), sampleSaved = ref(false)
const termModal = ref(false), editingTerm = ref<api.Term | null>(null), termDraft = reactive({ kind: 'risk', text: '', weight: 0 })
const combinationModal = ref(false), combinationTerms = ref(''), combinationBonus = ref(0)
let loadSequence = 0

const localTime = (value: string) => {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(undefined, { hour12: false })
}
const parseKeywords = (value: string) => {
  try { const parsed: unknown = JSON.parse(value); return Array.isArray(parsed) ? parsed.join('、') : value } catch { return value }
}
async function loadAll(): Promise<boolean> {
  const sequence = ++loadSequence
  busy.value = true
  errorMessage.value = ''
  try {
    const [v, s, t, c] = await Promise.all([
      api.listViolations(violationPage.value, pageSize), api.listSamples(samplePage.value, pageSize),
      api.listTerms(termKind.value, termPage.value, pageSize), api.listCombinations(combinationPage.value, pageSize),
    ])
    if (sequence !== loadSequence) return false
    violations.value = v.items; violationTotal.value = v.total
    samples.value = s.items; sampleTotal.value = s.total
    terms.value = t.items; termTotal.value = t.total
    combinations.value = c.items; combinationTotal.value = c.total
    return true
  } catch (error) {
    if (sequence !== loadSequence) return false
    errorMessage.value = error instanceof Error ? error.message : '加载违禁监控数据失败'
    return false
  } finally { if (sequence === loadSequence) busy.value = false }
}
async function mutate(action: () => Promise<unknown>, success: string): Promise<boolean> {
  if (busy.value) return false
  busy.value = true
  try {
    await action()
    const refreshed = await loadAll()
    if (refreshed) feedback.success(success)
    return refreshed
  } catch (error) {
    feedback.error(error, '操作失败', '，数据可能已变化，请刷新后重试')
    await loadAll()
    return false
  } finally { busy.value = false }
}
async function runTrial(): Promise<void> {
  const text = trialText.value.trim()
  if (!text) { feedback.warning('请输入需要试判的文本'); return }
  busy.value = true; trial.value = null
  try { trial.value = await api.runTextTrial(text); testedText.value = text; sampleSaved.value = false; feedback.success('文本试判完成') }
  catch (error) { feedback.error(error, '文本试判失败') }
  finally { busy.value = false }
}
async function saveTrial(): Promise<void> {
  if (!trial.value || testedText.value !== trialText.value.trim() || sampleSaved.value) { feedback.warning('请先对当前文本完成一次新的试判'); return }
  sampleSaved.value = await mutate(() => api.createSample(testedText.value, String(trial.value!.id)), '训练样本已保存')
}
function openTerm(term: api.Term | null): void {
  editingTerm.value = term; termDraft.kind = term?.kind ?? 'risk'; termDraft.text = term?.text ?? ''; termDraft.weight = term?.weight ?? 0; termModal.value = true
}
async function saveTerm(): Promise<void> {
  const input = { kind: termDraft.kind, text: termDraft.text.trim(), weight: termDraft.kind === 'hard' ? 0 : termDraft.weight }
  if (!input.text) { feedback.warning('请输入词条文本'); return }
  const target = editingTerm.value
  if (await mutate(() => target ? api.updateTerm(target, input) : api.createTerm(input), target ? '词条已更新' : '词条已新增')) termModal.value = false
}
async function saveCombination(): Promise<void> {
  const parts = combinationTerms.value.split(/[\n,，]/).map((item) => item.trim()).filter(Boolean)
  if (parts.length < 2) { feedback.warning('组合规则至少需要两个词项'); return }
  if (await mutate(() => api.createCombination(parts, combinationBonus.value), '组合规则已新增')) combinationModal.value = false
}
const violationColumns: DataTableColumns<api.RecordItem<api.ViolationData>> = [
  { title: '群 / 用户', key: 'scope', render: (r) => `${r.data.group_id} / ${r.data.user_id}` },
  { title: '消息', key: 'data.msg_content', ellipsis: { tooltip: true }, render: (r) => r.data.msg_content },
  { title: '依据', key: 'reason', render: (r) => `${r.data.reason || '-'}${r.data.violations?.length ? `（${r.data.violations.join('、')}）` : ''}` },
  { title: '时间', key: 'time', render: (r) => localTime(r.data.message_time || r.data.created_at) },
  { title: '操作', key: 'actions', render: (r) => h(NSpace, null, { default: () => [
    h(NPopconfirm, { onPositiveClick: () => mutate(() => api.reviewViolation(r, '确认'), '违规已确认') }, { trigger: () => h(NButton, { size: 'small', type: 'error', disabled: busy.value }, { default: () => '确认违规' }), default: () => '确定记录该消息为违规吗？' }),
    h(NPopconfirm, { onPositiveClick: () => mutate(() => api.reviewViolation(r, '误报'), '已标记为误报') }, { trigger: () => h(NButton, { size: 'small', disabled: busy.value }, { default: () => '标记误报' }), default: () => '误报可能解除该记录对应的自动禁言，确定继续吗？' }),
  ] }) },
]
const sampleColumns: DataTableColumns<api.RecordItem<api.SampleData>> = [
  { title: '样本文本', key: 'text', render: (r) => r.data.msg_content }, { title: '特征', key: 'keywords', render: (r) => parseKeywords(r.data.keywords) || '无' },
  { title: '创建时间', key: 'created_at', render: (r) => localTime(r.data.created_at) },
  { title: '操作', key: 'actions', render: (r) => h(NPopconfirm, { onPositiveClick: () => mutate(() => api.deleteSample(r), '训练样本已删除') }, { trigger: () => h(NButton, { size: 'small', disabled: busy.value }, { default: () => '删除' }), default: () => '删除会回退候选词统计，确定继续吗？' }) },
]
const termColumns: DataTableColumns<api.Term> = [
  { title: '分类', key: 'kind', render: (r) => ({ hard: '硬拦截', risk: '风险加分', safe: '安全抵扣' }[r.kind] ?? r.kind) }, { title: '词条', key: 'text' }, { title: '权重', key: 'weight' }, { title: '更新时间', key: 'updated_at', render: (r) => localTime(r.updated_at) },
  { title: '操作', key: 'actions', render: (r) => h(NSpace, null, { default: () => [h(NButton, { size: 'small', disabled: busy.value, onClick: () => openTerm(r) }, { default: () => '编辑' }), h(NPopconfirm, { onPositiveClick: () => mutate(() => api.deleteTerm(r), '词条已删除') }, { trigger: () => h(NButton, { size: 'small', disabled: busy.value }, { default: () => '删除' }), default: () => '删除后检测引擎会立即重建，确定继续吗？' })] }) },
]
const combinationColumns: DataTableColumns<api.Combination> = [
  { title: '词项', key: 'terms', render: (r) => r.terms.join(' + ') }, { title: '加成', key: 'bonus' }, { title: '更新时间', key: 'updated_at', render: (r) => localTime(r.updated_at) },
  { title: '操作', key: 'actions', render: (r) => h(NPopconfirm, { onPositiveClick: () => mutate(() => api.deleteCombination(r), '组合规则已删除') }, { trigger: () => h(NButton, { size: 'small', disabled: busy.value }, { default: () => '删除' }), default: () => '确定删除该组合规则吗？' }) },
]
onMounted(loadAll)
</script>

<template>
  <section class="monitor-page" :aria-busy="busy">
    <NAlert v-if="errorMessage" type="error" title="数据加载失败"><NSpace justify="space-between"><span>{{ errorMessage }}</span><NButton size="small" @click="loadAll">重试</NButton></NSpace></NAlert>
    <NTabs type="line" animated>
      <NTabPane name="review" tab="违规复核"><NCard title="待复核记录"><NDataTable :columns="violationColumns" :data="violations" :loading="busy" :scroll-x="900" /><NEmpty v-if="!busy && !violations.length" description="当前没有待复核记录" /><NPagination v-if="violationTotal" v-model:page="violationPage" :page-size="pageSize" :item-count="violationTotal" @update:page="loadAll" /></NCard></NTabPane>
      <NTabPane name="trial" tab="文本试判"><NCard title="文本试判"><p class="hint">试判可能调用外部模型并消耗额度，但不会触发群内处罚；主动保存后才进入学习流程。</p><NInput v-model:value="trialText" type="textarea" maxlength="4000" show-count :disabled="busy" :autosize="{ minRows: 4, maxRows: 10 }" /><NSpace justify="end"><NButton type="primary" :loading="busy" @click="runTrial">开始试判</NButton><NPopconfirm v-if="trial" @positive-click="saveTrial"><template #trigger><NButton type="warning" :disabled="busy || sampleSaved || testedText !== trialText.trim()">保存为违规样本</NButton></template>保存后会影响后续学习，确定继续吗？</NPopconfirm></NSpace><NDescriptions v-if="trial" bordered :column="1"><NDescriptionsItem label="判定"><NTag>{{ trial.data.decision }}</NTag></NDescriptionsItem><NDescriptionsItem label="检测阶段">{{ trial.data.stage }}</NDescriptionsItem><NDescriptionsItem label="本地分数">{{ trial.data.local_score }}</NDescriptionsItem><NDescriptionsItem label="理由">{{ trial.data.reason }}</NDescriptionsItem><NDescriptionsItem label="命中特征">{{ trial.data.violations?.join('、') || '无' }}</NDescriptionsItem><NDescriptionsItem label="大模型">{{ trial.data.llm_used ? `${trial.data.llm_risk_level || '未知'} / ${trial.data.llm_total_score ?? 0}` : '未调用' }}</NDescriptionsItem></NDescriptions></NCard></NTabPane>
      <NTabPane name="samples" tab="训练样本"><NCard title="违规正例"><NDataTable :columns="sampleColumns" :data="samples" :loading="busy" :scroll-x="760" /><NEmpty v-if="!busy && !samples.length" description="暂无训练样本，可从文本试判中添加" /><NPagination v-if="sampleTotal" v-model:page="samplePage" :page-size="pageSize" :item-count="sampleTotal" @update:page="loadAll" /></NCard></NTabPane>
      <NTabPane name="terms" tab="词条"><NCard title="检测词条"><template #header-extra><NButton :disabled="busy" @click="openTerm(null)">新增词条</NButton></template><NSelect v-model:value="termKind" class="filter" :options="[{label:'全部分类',value:''},{label:'硬拦截',value:'hard'},{label:'风险加分',value:'risk'},{label:'安全抵扣',value:'safe'}]" @update:value="termPage=1; loadAll()" /><NDataTable :columns="termColumns" :data="terms" :loading="busy" :scroll-x="700" /><NEmpty v-if="!busy && !terms.length" description="当前分类暂无词条" /><NPagination v-if="termTotal" v-model:page="termPage" :page-size="pageSize" :item-count="termTotal" @update:page="loadAll" /></NCard></NTabPane>
      <NTabPane name="combinations" tab="组合规则"><NCard title="组合加成规则"><template #header-extra><NButton :disabled="busy" @click="combinationTerms=''; combinationBonus=0; combinationModal=true">新增组合</NButton></template><NDataTable :columns="combinationColumns" :data="combinations" :loading="busy" :scroll-x="650" /><NEmpty v-if="!busy && !combinations.length" description="暂无组合规则" /><NPagination v-if="combinationTotal" v-model:page="combinationPage" :page-size="pageSize" :item-count="combinationTotal" @update:page="loadAll" /></NCard></NTabPane>
    </NTabs>
    <NModal v-model:show="termModal" preset="card" style="max-width:520px" :title="editingTerm ? '编辑词条' : '新增词条'"><NSpace vertical><NSelect v-model:value="termDraft.kind" :disabled="busy" :options="[{label:'硬拦截',value:'hard'},{label:'风险加分',value:'risk'},{label:'安全抵扣',value:'safe'}]" /><NInput v-model:value="termDraft.text" :disabled="busy" maxlength="100" show-count placeholder="词条文本" /><NInputNumber v-model:value="termDraft.weight" :disabled="busy || termDraft.kind === 'hard'" :min="0" :max="1000" /><NButton type="primary" :loading="busy" @click="saveTerm">保存</NButton></NSpace></NModal>
    <NModal v-model:show="combinationModal" preset="card" style="max-width:520px" title="新增组合规则"><NSpace vertical><NInput v-model:value="combinationTerms" type="textarea" :disabled="busy" placeholder="每行一个词项，2 至 8 个" /><NInputNumber v-model:value="combinationBonus" :disabled="busy" :min="0" :max="1000" /><NButton type="primary" :loading="busy" @click="saveCombination">保存</NButton></NSpace></NModal>
  </section>
</template>

<style scoped>
.monitor-page { width: 100%; min-width: 0; }
.monitor-page :deep(.n-card), .monitor-page :deep(.n-alert) { margin-bottom: var(--space-4); }
.monitor-page :deep(.n-pagination), .monitor-page :deep(.n-descriptions), .monitor-page :deep(.n-space) { margin-top: var(--space-4); }
.hint { color: var(--color-text-secondary); line-height: var(--line-height-body); }
.filter { width: 180px; margin-bottom: var(--space-4); }
@media (max-width: 640px) { .filter { width: 100%; } .monitor-page :deep(.n-card__content) { padding: var(--space-3); } }
</style>
