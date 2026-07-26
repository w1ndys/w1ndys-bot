// 📌 影响范围：调用关键词回复插件的群规则管理接口；写操作会修改数据库、审计并刷新插件运行快照。
import { apiRequest } from '../../api'

export interface KeywordRule {
  id: number
  group_id: number
  keyword: string
  reply_content: string
  enabled: boolean
  version: number
  updated_at: string
}

export interface KeywordRulePage {
  items: KeywordRule[]
  page: number
  page_size: number
  total: number
}

export interface KeywordRuleInput {
  keyword: string
  reply_content: string
  enabled: boolean
}

// listKeywordRules 按群分页读取关键词规则。
// @param groupID：可信群号；page：页码；pageSize：每页条数。
// @returns 该群的规则分页结果。
// ⚠️副作用说明：发起鉴权网络请求。
export function listKeywordRules(groupID: number, page: number, pageSize: number): Promise<KeywordRulePage> {
  const result = apiRequest<KeywordRulePage>(`/api/plugins/keyword_reply/groups/${groupID}/rules?page=${page}&page_size=${pageSize}`)

  // >>> 数据演变示例
  // 1. 群100+page1 -> 该群规则分页。
  // 2. 群号非法 -> 400 -> 抛错。
  return result
}

// createKeywordRule 在指定群下新增关键词规则。
// @param groupID：可信群号；input：规则内容。
// @returns 新增后的规则。
// ⚠️副作用说明：写入数据库、审计并刷新插件运行快照。
export function createKeywordRule(groupID: number, input: KeywordRuleInput): Promise<KeywordRule> {
  const result = apiRequest<KeywordRule>(`/api/plugins/keyword_reply/groups/${groupID}/rules`, {
    method: 'POST',
    body: JSON.stringify(input),
  })

  // >>> 数据演变示例
  // 1. 群100+"你好" -> 新增 v1。
  // 2. 同群关键词重复 -> 409 -> 抛错。
  return result
}

// updateKeywordRule 按乐观锁更新指定群下的规则。
// @param groupID：可信群号；id：规则主键；expectedVersion：当前版本；input：规则内容。
// @returns 更新后的规则。
// ⚠️副作用说明：写入数据库、审计并刷新插件运行快照。
export function updateKeywordRule(groupID: number, id: number, expectedVersion: number, input: KeywordRuleInput): Promise<KeywordRule> {
  const result = apiRequest<KeywordRule>(`/api/plugins/keyword_reply/groups/${groupID}/rules/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ ...input, expected_version: expectedVersion }),
  })

  // >>> 数据演变示例
  // 1. 规则1+v1 -> 更新 -> v2。
  // 2. 陈旧版本 -> 409 -> 抛错。
  return result
}

// deleteKeywordRule 按乐观锁删除指定群下的规则。
// @param groupID：可信群号；id：规则主键；expectedVersion：当前版本。
// @returns 删除结果标记。
// ⚠️副作用说明：删除数据库记录、写审计并刷新插件运行快照。
export function deleteKeywordRule(groupID: number, id: number, expectedVersion: number): Promise<{ deleted: boolean }> {
  const result = apiRequest<{ deleted: boolean }>(`/api/plugins/keyword_reply/groups/${groupID}/rules/${id}`, {
    method: 'DELETE',
    body: JSON.stringify({ expected_version: expectedVersion }),
  })

  // >>> 数据演变示例
  // 1. 规则1+v2 -> 删除成功。
  // 2. 规则已被删除 -> 404 -> 抛错。
  return result
}
