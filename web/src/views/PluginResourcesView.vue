<!-- 📌 影响范围：读取路由中的插件名与通用资源 API；资源写操作会修改插件业务数据并产生审计。 -->
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import PluginResourceTable from '../components/PluginResourceTable.vue'
import ForbiddenMessageTextTest from '../components/ForbiddenMessageTextTest.vue'

const route = useRoute()
const pluginName = computed(readPluginName)

// readPluginName 读取通用资源页所属插件的稳定路由参数。
// @param 无。
// @returns 插件稳定名称；参数异常时返回空字符串。
// ⚠️副作用说明：无。
function readPluginName(): string {
  const value = route.params.pluginName
  const result = typeof value === 'string' ? value : ''

  // >>> 数据演变示例
  // 1. pluginName="keyword_reply" -> "keyword_reply"。
  // 2. pluginName为数组 -> 类型无效 -> ""。
  return result
}
</script>

<template>
  <div>
    <ForbiddenMessageTextTest v-if="pluginName === 'forbidden_message_monitor'" />
    <PluginResourceTable :plugin-name="pluginName" />
  </div>
</template>
