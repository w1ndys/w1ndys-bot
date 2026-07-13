<!-- 📌 影响范围：读取当前插件 Manifest 功能元数据；不修改后端状态。 -->
<script setup lang="ts">
import { NAlert, NCard, NEmpty, NSpin, NTag } from 'naive-ui'
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { listPluginFeatures, type FeatureState } from '../api'

const route = useRoute()
const features = ref<FeatureState[]>([])
const loading = ref(true)
const errorMessage = ref('')

// loadFeatures 读取当前插件由Manifest声明的功能列表。
// @param 无。
// @returns Promise，在功能列表更新后结束。
// ⚠️副作用说明：发起功能元数据请求并修改页面状态。
async function loadFeatures(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    features.value = await listPluginFeatures(String(route.params.pluginName ?? ''))
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载插件功能失败'
  } finally {
    loading.value = false
  }

  // >>> 数据演变示例
  // 1. plugin=ping -> [ping功能] -> 卡片列表。
  // 2. API失败 -> 显示错误 -> loading=false。
}

onMounted(loadFeatures)
</script>

<template>
  <NAlert v-if="errorMessage" class="workspace-alert" type="error">{{ errorMessage }}</NAlert>
  <NSpin :show="loading">
    <NEmpty v-if="!loading && features.length === 0" description="该插件没有声明功能" />
    <div v-else class="feature-grid">
      <NCard v-for="feature in features" :key="feature.key" :title="feature.display_name || feature.key" embedded>
        <template #header-extra><NTag :type="feature.available ? 'success' : 'default'">{{ feature.available ? '可用' : '不可用' }}</NTag></template>
        <p class="muted">{{ feature.description || '暂无功能说明' }}</p>
        <dl class="feature-meta">
          <div><dt>功能键</dt><dd><code>{{ feature.key }}</code></dd></div>
          <div><dt>默认命令</dt><dd>{{ feature.default_commands.join('、') || '无' }}</dd></div>
          <div><dt>默认权限</dt><dd><code>{{ JSON.stringify(feature.default_permissions) }}</code></dd></div>
        </dl>
      </NCard>
    </div>
  </NSpin>
</template>
