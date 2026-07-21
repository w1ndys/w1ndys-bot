-- 📌 影响范围：回滚违禁监控正向候选词与LLM每日请求计数；会删除对应学习统计。
DROP TABLE IF EXISTS forbidden_monitor_llm_usage_daily;
DROP TABLE IF EXISTS forbidden_monitor_candidate_evidence;
DROP TABLE IF EXISTS forbidden_monitor_risk_candidates;
