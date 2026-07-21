<!-- 📌 影响范围：调用违禁消息监控文本试判与训练样本 API；试判不持久化，管理员确认投喂会写训练样本并影响后续学习。 -->
<script setup lang="ts">
import { NButton, NCard, NDescriptions, NDescriptionsItem, NInput, NPopconfirm, NTag } from 'naive-ui'
import { computed, ref } from 'vue'
import { createPluginResourceRecord } from '../api'
import { useAppFeedback } from '../feedback'

interface TextTestResult {
  decision: string
  stage: string
  risk_band: string
  local_score: number
  reason: string
  violations: string[]
  llm_used: boolean
  llm_risk_level?: string
  llm_total_score?: number
  suggested_action: string
}

const text = ref('')
const loading = ref(false)
const savingSample = ref(false)
const result = ref<TextTestResult | null>(null)
const testedText = ref('')
const savedSampleText = ref('')
const testedTrialId = ref(0)
const decisionType = computed(resolveDecisionType)
const canSaveSample = computed(() => result.value !== null && testedText.value !== '' && text.value.trim() === testedText.value && savedSampleText.value !== testedText.value)
const feedback = useAppFeedback()

// resolveDecisionType 根据中文结论选择视觉状态。
// @param 无；读取当前试判结果。
// @returns Naive UI 标签类型。
// ⚠️副作用说明：无。
function resolveDecisionType(): 'error' | 'warning' | 'success' | 'default' {
  // [决策理由] 违规结论需要最高可见度。
  if (result.value?.decision === '违规') {
    return 'error'
  }
  // [决策理由] 人工复核表示不确定状态，使用警告色。
  if (result.value?.decision === '人工复核') {
    return 'warning'
  }
  // [决策理由] 放行是明确安全结论，其余未知值保持中性。
  const type = result.value?.decision === '放行' ? 'success' : 'default'

  // >>> 数据演变示例
  // 1. decision=违规 -> error。
  // 2. decision=放行 -> success；result=null -> default。
  return type
}

// runTextTest 使用当前插件运行时配置试判输入文本。
// @param 无；读取text输入。
// @returns Promise，在结果或错误状态更新后结束。
// ⚠️副作用说明：调用受保护API；中风险且启用LLM时文本会发送至配置的模型端点。
async function runTextTest(): Promise<void> {
  const candidate = text.value.trim()
  // [决策理由] 空文本无需发起请求，前端与后端共同限制输入边界。
	if (candidate.length === 0) {
		result.value = null
		feedback.warning('请输入需要测试的消息文本。')
		return
	}
	loading.value = true
	result.value = null
	try {
		const record = await createPluginResourceRecord('forbidden_message_monitor', 'text_tests', { text: candidate })
		result.value = record.data as unknown as TextTestResult
		testedText.value = candidate
		testedTrialId.value = record.id
		feedback.success('文本试判完成')
	} catch (error) {
		feedback.error(error, '文本试判失败')
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. "普通聊天" -> POST -> 展示放行与本地分数。
  // 2. 空文本 -> 不请求 -> 展示输入提示。
}

// saveViolationSample 将本次已试判文本明确标记为违规训练正例。
// @param 无；读取testedText与当前试判结果。
// @returns Promise，在样本保存或错误反馈后结束。
// ⚠️副作用说明：复用本次服务端试判的可信风险词并持久化训练样本和候选证据，不再次调用LLM。
async function saveViolationSample(): Promise<void> {
  // [决策理由] 文本变化后旧试判结论失效，必须重新试判才能主动投喂。
  if (!canSaveSample.value) {
    feedback.warning('文本已变化，请重新试判后再保存样本。')
    return
  }
  savingSample.value = true
	try {
		await createPluginResourceRecord('forbidden_message_monitor', 'training_samples', { msg_content: testedText.value, trial_id: String(testedTrialId.value) })
		savedSampleText.value = testedText.value
		feedback.success('违规训练样本已保存并进入学习流程')
  } catch (error) {
    feedback.error(error, '保存违规训练样本失败')
  } finally {
    savingSample.value = false
  }

  // >>> 数据演变示例
  // 1. 已试判广告文本+确认 -> 服务端重新提取特征 -> 保存正例。
  // 2. 文本修改或重复样本 -> 前端拒绝或后端冲突Toast。
}
</script>

<template>
  <NCard class="text-test-card" title="文本试判" size="small">
    <p class="text-test-description">试判不会自动学习、计入发言、写入审核记录、禁言或撤回。只有主动点击“保存为违规样本”并确认后，文本才会进入Few-shot正例和候选词学习。</p>
    <NInput v-model:value="text" type="textarea" :autosize="{ minRows: 4, maxRows: 10 }" maxlength="4000" show-count placeholder="输入一条待检测的群消息" />
    <div class="text-test-actions">
      <NButton type="primary" :loading="loading" @click="runTextTest">开始测试</NButton>
      <NPopconfirm v-if="result" positive-text="确认投喂" negative-text="取消" @positive-click="saveViolationSample">
        <template #trigger><NButton type="warning" :disabled="!canSaveSample" :loading="savingSample">保存为违规样本</NButton></template>
        该操作会复用本次试判返回的风险词，并影响后续Few-shot与候选词权重；不会再次调用大模型或触发QQ处罚。确定继续吗？
      </NPopconfirm>
    </div>
    <NDescriptions v-if="result" class="text-test-result" bordered label-placement="left" :column="1">
      <NDescriptionsItem label="判定"><NTag :type="decisionType">{{ result.decision }}</NTag></NDescriptionsItem>
      <NDescriptionsItem label="检测阶段">{{ result.stage }}</NDescriptionsItem>
      <NDescriptionsItem label="本地分流">{{ result.risk_band }}</NDescriptionsItem>
      <NDescriptionsItem label="本地分数">{{ Number(result.local_score || 0).toFixed(2) }}</NDescriptionsItem>
      <NDescriptionsItem label="建议动作">{{ result.suggested_action }}</NDescriptionsItem>
      <NDescriptionsItem label="判定理由">{{ result.reason }}</NDescriptionsItem>
      <NDescriptionsItem label="命中特征">{{ result.violations?.join('、') || '无' }}</NDescriptionsItem>
      <NDescriptionsItem label="大模型">{{ result.llm_used ? `${result.llm_risk_level || '未知'} / ${result.llm_total_score ?? 0}` : '未调用' }}</NDescriptionsItem>
    </NDescriptions>
  </NCard>
</template>

<style scoped>
.text-test-card { margin-bottom: var(--space-6); }
.text-test-description { margin: 0 0 var(--space-4); color: var(--color-text-secondary); line-height: var(--line-height-body); }
.text-test-actions { display: flex; justify-content: flex-end; gap: var(--space-3); margin: var(--space-3) 0; }
.text-test-result { margin-top: var(--space-4); }
</style>
