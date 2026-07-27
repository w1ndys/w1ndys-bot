<!-- 📌 影响范围：根据稳定插件 Key 加载独立的标量配置页面；保存会写数据库、审计并热应用配置。 -->
<script setup lang="ts">
import { NButton, NCard, NEmpty, NSpace } from 'naive-ui'
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import PluginRuntimeConfigForm from '../components/PluginRuntimeConfigForm.vue'

const route = useRoute()
const pluginKey = computed(() => typeof route.params.pluginKey === 'string' ? route.params.pluginKey : '')
</script>

<template>
  <NSpace vertical size="large">
    <RouterLink :to="{ name: 'plugin-runtimes' }"><NButton>返回插件运行</NButton></RouterLink>
    <NCard v-if="pluginKey" title="插件配置">
      <template #header-extra>{{ pluginKey }}</template>
      <PluginRuntimeConfigForm :plugin-key="pluginKey" />
    </NCard>
    <NEmpty v-else description="缺少插件标识" />
  </NSpace>
</template>
