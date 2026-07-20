// 📌 影响范围：解析 API 与未知错误；通过 Naive Message 显示全局 Toast；进程内去重短时重复消息。
import { useMessage } from 'naive-ui'
import { ApiError } from './api'

export type FeedbackKind = 'success' | 'error' | 'warning'

export interface NormalizedFeedback {
  message: string
  silent: boolean
}

export interface AppFeedback {
  success(text: string): void
  error(cause: unknown, fallback: string, suffix?: string): boolean
  warning(text: string): void
}

const duplicateWindowMs = 1200
const emittedAt = new Map<string, number>()

// normalizeFeedback 将 API、Error 和未知抛出值转为稳定反馈。
// @param error：捕获的未知错误；fallback：稳定业务降级文案。
// @returns 用户可读消息与是否静默；401 返回 silent=true。
// ⚠️副作用说明：无。
export function normalizeFeedback(error: unknown, fallback: string): NormalizedFeedback {
  // [决策理由] 401 已由 API 层清理会话并将由路由守卫导向登录，不再弹出重复 Toast。
  if (error instanceof ApiError && error.status === 401) {
    return { message: '', silent: true }
  }
  // [决策理由] API 领域错误已由后端提供安全文案，优先保留。
  if (error instanceof ApiError && error.message.trim() !== '') {
    return { message: error.message, silent: false }
  }
  // [决策理由] 本地 Error 可能来自浏览器网络层，非空文案对排查有用。
  if (error instanceof Error && error.message.trim() !== '') {
    return { message: error.message, silent: false }
  }
  const result = { message: fallback, silent: false }

  // >>> 数据演变示例
  // 1. ApiError(409,"version conflict") -> {message:"version conflict",silent:false}。
  // 2. ApiError(401) -> {message:"",silent:true}；未知值 -> fallback。
  return result
}

// shouldEmitFeedback 判断同类同文案是否已在短窗口内显示。
// @param previousAt：上次显示时间，不存在时为 undefined；now：当前毫秒时间。
// @returns 首次或超过 1200ms 去重窗口时 true。
// ⚠️副作用说明：无。
export function shouldEmitFeedback(previousAt: number | undefined, now: number): boolean {
  const result = previousAt === undefined || now - previousAt >= duplicateWindowMs

  // >>> 数据演变示例
  // 1. undefined,1000 -> true。
  // 2. 1000,1500 -> false；1000,2200 -> true。
  return result
}

// useAppFeedback 创建统一 Toast 反馈入口。
// @param 无。
// @returns success、error、warning 方法；error 接受未知错误与降级文案。
// ⚠️副作用说明：读取 Naive Message Provider；方法调用时可显示全局 Toast。
export function useAppFeedback(): AppFeedback {
  const messageApi = useMessage()

  // emit 按类型显示去重 Toast。
  // @param kind：反馈类型；text：用户可读文案。
  // @returns 无。
  // ⚠️副作用说明：可更新去重时间并调用 Naive Message API。
  function emit(kind: FeedbackKind, text: string): void {
    const key = `${kind}:${text}`
    const now = Date.now()
    // [决策理由] 连续失败的组合请求可抛出同一文案，短时只展示一次避免堆叠。
    if (!shouldEmitFeedback(emittedAt.get(key), now)) {
      return
    }
    emittedAt.set(key, now)
    // [决策理由] 模块级去重表跨页共享，必须定期移除过期键避免长会话无界增长。
    if (emittedAt.size > 64) {
      for (const [candidate, emitted] of emittedAt) {
        // [决策理由] 只删除已超过去重窗口的键，保留当前活跃消息。
        if (now - emitted >= duplicateWindowMs) {
          emittedAt.delete(candidate)
        }
      }
      // [决策理由] 极端突发产生超过64种不同文案时仍需硬性限制模块级历史表。
      while (emittedAt.size > 64) {
        const oldest = emittedAt.keys().next().value as string | undefined
        // [决策理由] Map 声明非空但迭代器防御性返回空时必须停止，避免无进展循环。
        if (oldest === undefined) {
          break
        }
        emittedAt.delete(oldest)
      }
    }
    const duration = kind === 'error' ? 4500 : kind === 'warning' ? 3800 : 2800
    messageApi[kind](text, { duration })

    // >>> 数据演变示例
    // 1. success:"saved" 首次 -> 显示 2800ms。
    // 2. error:"failed" 500ms内重复 -> 跳过。
  }

  // success 显示成功 Toast。
  // @param text：成功文案。
  // @returns 无。
  // ⚠️副作用说明：可显示全局 Toast。
  function success(text: string): void {
    emit('success', text)
    // >>> 数据演变示例
    // 1. "已保存" -> success Toast。
    // 2. 短时重复 -> 去重。
  }

  // error 归一化并显示失败 Toast。
  // @param cause：未知错误；fallback：降级文案；suffix：需要始终附加的业务恢复提示。
  // @returns 是否实际尝试显示；401 静默时为 false。
  // ⚠️副作用说明：非静默时可显示全局 Toast。
  function error(cause: unknown, fallback: string, suffix = ''): boolean {
    const normalized = normalizeFeedback(cause, fallback)
    // [决策理由] 401 不应在自动跳转登录时重复打扰用户。
    if (normalized.silent) {
      return false
    }
    emit('error', `${normalized.message}${suffix}`)

    // >>> 数据演变示例
    // 1. ApiError(409)+恢复提示 -> 显示后端文案与恢复提示 -> true。
    // 2. ApiError(401) -> 静默 -> false。
    return true
  }

  // warning 显示警告 Toast。
  // @param text：警告文案。
  // @returns 无。
  // ⚠️副作用说明：可显示全局 Toast。
  function warning(text: string): void {
    emit('warning', text)
    // >>> 数据演变示例
    // 1. "版本冲突" -> warning Toast。
    // 2. 短时重复 -> 去重。
  }

  const result = { success, error, warning }

  // >>> 数据演变示例
  // 1. 页面写成功 -> feedback.success -> 绿色Toast。
  // 2. API 401 -> feedback.error -> 静默且由路由守卫处理。
  return result
}
