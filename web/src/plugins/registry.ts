// 📌 影响范围：把后端返回的稳定页面 Key 映射为编译期本地组件；不加载远程代码。
import type { Component } from 'vue'
import { defineAsyncComponent } from 'vue'

// pluginPages 是插件专属页面的编译期注册表。
// 后端只返回稳定 Key，绝不返回组件路径、URL、HTML 或脚本；未注册的 Key 视为不存在。
const pluginPages: Record<string, Component> = {
  keyword_reply: defineAsyncComponent(() => import('./keyword_reply/Page.vue')),
  forbidden_message_monitor: defineAsyncComponent(() => import('./forbidden_message_monitor/Page.vue')),
}

// resolvePluginPage 按稳定页面 Key 查找本地组件。
// @param pageKey：后端返回的 admin_page_key。
// @returns 已注册组件；未注册时返回 null。
// ⚠️副作用说明：首次解析时触发对应代码块的异步加载。
export function resolvePluginPage(pageKey: string): Component | null {
  const result = Object.prototype.hasOwnProperty.call(pluginPages, pageKey) ? pluginPages[pageKey] : null

  // >>> 数据演变示例
  // 1. "keyword_reply" -> 本地异步组件。
  // 2. 未注册或伪造的 Key -> null -> 页面显示不存在。
  return result
}
