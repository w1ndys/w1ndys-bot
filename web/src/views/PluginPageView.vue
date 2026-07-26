<!-- 📌 影响范围：按路由中的稳定页面 Key 渲染插件专属页面；不执行后端返回的组件名或脚本。 -->
<script setup lang="ts">
import { NEmpty } from 'naive-ui'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { resolvePluginPage } from '../plugins/registry'

const route = useRoute()

// readPageKey 读取路由中的稳定页面 Key。
// @param 无；读取 route.params.pageKey。
// @returns 字符串路由参数；类型异常时返回空字符串。
// ⚠️副作用说明：无。
function readPageKey(): string {
  const value = route.params.pageKey
  const result = typeof value === 'string' ? value : ''

  // >>> 数据演变示例
  // 1. "keyword_reply" -> "keyword_reply"。
  // 2. 数组参数 -> ""。
  return result
}

const page = computed(() => resolvePluginPage(readPageKey()))
</script>

<template>
  <component :is="page" v-if="page" />
  <NEmpty v-else description="当前版本没有这个插件页面" />
</template>
