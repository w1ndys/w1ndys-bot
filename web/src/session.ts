// 📌 影响范围：读写浏览器 localStorage 中的 w1ndys_bot_token。
import { ref } from 'vue'

const tokenKey = 'w1ndys_bot_token'
export const sessionToken = ref(localStorage.getItem(tokenKey) || '')

// setSessionToken 保存新的登录凭证。
// @param token：后端签发的 JWT；空字符串表示退出登录。
// @returns 无。
// ⚠️副作用说明：修改响应式会话状态和浏览器 localStorage。
export function setSessionToken(token: string): void {
  sessionToken.value = token
  // [决策理由] 空 Token 表示明确退出，应移除持久化项而不是留下无效值。
  if (token === '') {
    localStorage.removeItem(tokenKey)
  } else {
    localStorage.setItem(tokenKey, token)
  }

  // >>> 数据演变示例
  // 1. token=abc -> ref=abc + localStorage=abc。
  // 2. token="" -> ref空 + localStorage删除。
}
