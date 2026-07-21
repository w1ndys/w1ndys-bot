<!-- 📌 影响范围：调用违禁消息监控文本试判 API；可能按插件配置向外部 LLM 发送输入文本，不触发 QQ 处置或持久化。 -->
<script setup lang="ts">
import { NButton, NCard, NDescriptions, NDescriptionsItem, NInput, NTag } from 'naive-ui'
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
const result = ref<TextTestResult | null>(null)
const decisionType = computed(resolveDecisionType)
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
</script>

<template>
  <NCard class="text-test-card" title="文本试判" size="small">
    <p class="text-test-description">使用当前已保存规则测试消息。不会计入发言、写入审核记录、禁言或撤回；中风险且启用大模型时，输入会发送到已配置的模型端点。</p>
    <NInput v-model:value="text" type="textarea" :autosize="{ minRows: 4, maxRows: 10 }" maxlength="4000" show-count placeholder="输入一条待检测的群消息" />
    <div class="text-test-actions">
      <NButton type="primary" :loading="loading" @click="runTextTest">开始测试</NButton>
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
.text-test-actions { display: flex; justify-content: flex-end; margin: var(--space-3) 0; }
.text-test-result { margin-top: var(--space-4); }
</style>
