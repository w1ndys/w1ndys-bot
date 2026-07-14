<!-- 📌 影响范围：读取当前插件 Manifest 功能元数据；不修改任何后端状态。 -->
<script setup lang="ts">
import { NAlert, NButton, NDataTable, NEmpty, NSkeleton, NTag, type DataTableColumns } from 'naive-ui'
import { h, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { listPluginFeatures, type FeatureState } from '../api'

const route = useRoute()
const features = ref<FeatureState[]>([])
const loading = ref(true)
const errorMessage = ref('')
const columns: DataTableColumns<FeatureState> = [
  {
    title: '功能',
    key: 'display_name',
    minWidth: 180,
    render: renderFeatureName,
  },
  {
    title: '功能键',
    key: 'key',
    minWidth: 150,
    render: renderFeatureKey,
  },
  {
    title: '默认命令',
    key: 'default_commands',
    minWidth: 200,
    render: renderDefaultCommands,
  },
  {
    title: '默认权限',
    key: 'default_permissions',
    minWidth: 200,
    render: renderDefaultPermissions,
  },
  {
    title: '状态',
    key: 'available',
    width: 100,
    render: renderAvailability,
  },
]

// featureRowKey 返回Manifest功能稳定键作为表格行键。
// @param row：功能元数据行。
// @returns feature_key稳定标识。
// ⚠️副作用说明：无。
function featureRowKey(row: FeatureState): string {
  const result = row.key

  // >>> 数据演变示例
  // 1. Feature{key:ping} -> ping。
  // 2. Feature{key:plugin_list} -> plugin_list。
  return result
}

// renderFeatureName 渲染功能名称和 Manifest 描述。
// @param row：当前功能元数据。
// @returns 包含名称与描述的虚拟节点。
// ⚠️副作用说明：无。
function renderFeatureName(row: FeatureState) {
  const description = row.description || '暂无功能说明'
  const result = h('div', { class: 'feature-name' }, [
    h('strong', row.display_name || row.key),
    h('span', description),
  ])

  // >>> 数据演变示例
  // 1. display_name="Ping"+description="连通检查" -> 两行名称与说明。
  // 2. display_name=""+description="" -> 使用功能键与“暂无功能说明”。
  return result
}

// renderFeatureKey 以等宽只读文本渲染稳定功能键。
// @param row：当前功能元数据。
// @returns 功能键 code 虚拟节点。
// ⚠️副作用说明：无。
function renderFeatureKey(row: FeatureState) {
  const result = h('code', { class: 'feature-code' }, row.key)

  // >>> 数据演变示例
  // 1. key="ping" -> <code>ping</code>。
  // 2. key="plugin.enable" -> 保留完整稳定键并允许换行。
  return result
}

// renderDefaultCommands 渲染 Manifest 声明的默认触发命令。
// @param row：当前功能元数据。
// @returns 命令标签列表或“无默认命令”文本。
// ⚠️副作用说明：无。
function renderDefaultCommands(row: FeatureState) {
  // [决策理由] 空命令列表必须展示明确含义，不能让管理员误以为数据未加载。
  if (row.default_commands.length === 0) {
    const emptyResult = h('span', { class: 'feature-muted' }, '无默认命令')

    // >>> 数据演变示例
    // 1. default_commands=[] -> 显示“无默认命令”。
    // 2. default_commands=[] -> 不渲染空白单元格。
    return emptyResult
  }
  const tags = []
  for (const command of row.default_commands) {
    tags.push(h(NTag, { size: 'small' }, command))
  }
  const result = h('div', { class: 'feature-tags' }, tags)

  // >>> 数据演变示例
  // 1. ["ping"] -> 一个命令标签。
  // 2. ["ping","测试"] -> 两个可比较的命令标签。
  return result
}

// renderDefaultPermissions 渲染 Manifest 默认权限回退值。
// @param row：当前功能元数据。
// @returns 紧凑的只读 JSON 文本虚拟节点。
// ⚠️副作用说明：无。
function renderDefaultPermissions(row: FeatureState) {
  const value = JSON.stringify(row.default_permissions)
  const result = h('code', { class: 'feature-code' }, value)

  // >>> 数据演变示例
  // 1. {member:true} -> {"member":true} 等宽只读文本。
  // 2. [] -> []，明确表示 Manifest 默认权限为空。
  return result
}

// renderAvailability 渲染功能在当前版本中的可用状态。
// @param row：当前功能元数据。
// @returns 带文字的语义状态标签。
// ⚠️副作用说明：无。
function renderAvailability(row: FeatureState) {
  const result = h(NTag, { type: row.available ? 'success' : 'error', size: 'small' }, row.available ? '可用' : '不可用')

  // >>> 数据演变示例
  // 1. available=true -> Success 标签“可用”。
  // 2. available=false -> Error 标签“不可用”。
  return result
}

// loadFeatures 读取当前插件由 Manifest 声明的只读功能列表。
// @param 无。
// @returns Promise，在功能列表状态更新后结束。
// ⚠️副作用说明：发起功能元数据请求并更新加载、错误和列表状态。
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
  // 1. plugin=ping -> API返回一项 -> 表格显示只读功能元数据。
  // 2. API失败 -> 保留错误原因 -> loading=false并提供重试。
}

onMounted(loadFeatures)
</script>

<template>
  <section class="features-page" :aria-busy="loading">
    <header class="section-heading">
      <div>
        <h2>所属功能</h2>
        <p>功能由当前部署版本的插件 Manifest 声明，仅供查看；命令与权限请在对应工作台中配置。</p>
      </div>
      <NTag type="info">Manifest 只读</NTag>
    </header>

    <div v-if="loading" class="features-loading" aria-label="正在加载功能列表">
      <NSkeleton text :repeat="5" />
    </div>

    <NAlert v-else-if="errorMessage" type="error" title="功能列表加载失败">
      <div class="error-content">
        <span>{{ errorMessage }}</span>
        <NButton size="small" secondary @click="loadFeatures">重新加载</NButton>
      </div>
    </NAlert>

    <NDataTable v-else-if="features.length > 0" class="features-table" :columns="columns" :data="features" :row-key="featureRowKey" :scroll-x="830" :single-line="false" />

    <NEmpty v-else description="该插件没有声明功能">
      <template #extra>当前 Manifest 中没有可展示的功能元数据。</template>
    </NEmpty>
  </section>
</template>

<style scoped>
.features-page { padding: var(--space-5); border: 0.0625rem solid var(--color-border); border-radius: var(--radius-md); background: var(--color-bg-card); }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); margin-bottom: var(--space-4); }
.section-heading h2 { margin: 0; color: var(--color-text-primary); font-size: var(--font-size-h3); line-height: var(--line-height-h3); }
.section-heading p { max-width: 42.5rem; margin: var(--space-1) 0 0; color: var(--color-text-muted); font-size: var(--font-size-body-sm); line-height: var(--line-height-body-sm); }
.features-loading { display: grid; gap: var(--space-3); min-height: 10rem; }
.error-content { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); }
:deep(.feature-name) { display: grid; gap: var(--space-1); }
:deep(.feature-name strong) { color: var(--color-text-primary); font-size: var(--font-size-body); line-height: var(--line-height-body); }
:deep(.feature-name span), :deep(.feature-muted) { color: var(--color-text-muted); font-size: var(--font-size-caption); line-height: var(--line-height-caption); }
:deep(.feature-code) { color: var(--color-text-secondary); font-family: var(--font-mono); font-size: var(--font-size-body-sm); overflow-wrap: anywhere; }
:deep(.feature-tags) { display: flex; flex-wrap: wrap; gap: var(--space-1); }
@media (max-width: 39.9375rem) {
  .features-page { padding: var(--space-4); }
  .section-heading { align-items: flex-start; flex-direction: column; }
  .error-content { align-items: flex-start; flex-direction: column; }
}
</style>
