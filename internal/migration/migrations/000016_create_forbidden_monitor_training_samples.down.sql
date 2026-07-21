-- 📌 影响范围：回滚WebUI违禁训练样本；会删除对应候选词证据并回退其计数需由应用重算。
DROP TABLE IF EXISTS forbidden_monitor_candidate_training_evidence;
DROP TABLE IF EXISTS forbidden_monitor_training_samples;
UPDATE forbidden_monitor_risk_candidates AS candidate
SET confirmed_count = evidence.count,
    learned_weight = CASE WHEN evidence.count >= 10 THEN 30 WHEN evidence.count >= 5 THEN 20 WHEN evidence.count >= 3 THEN 10 ELSE 0 END,
    version = candidate.version + 1
FROM (
    SELECT candidate_keyword.keyword, COUNT(audit_evidence.*)::INTEGER AS count
    FROM forbidden_monitor_risk_candidates AS candidate_keyword
    LEFT JOIN forbidden_monitor_candidate_evidence AS audit_evidence
      ON audit_evidence.keyword = candidate_keyword.keyword AND audit_evidence.active = TRUE
    GROUP BY candidate_keyword.keyword
) AS evidence
WHERE candidate.keyword = evidence.keyword;
