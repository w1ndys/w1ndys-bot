-- 📌 影响范围：删除群违禁消息监控的发言计数、白名单、违规审计、误判反馈与周期权重偏移表。
DROP TABLE IF EXISTS forbidden_monitor_weight_offsets;
DROP TABLE IF EXISTS forbidden_monitor_feedback_samples;
DROP TABLE IF EXISTS forbidden_monitor_violation_audits;
DROP TABLE IF EXISTS forbidden_monitor_whitelist;
DROP TABLE IF EXISTS forbidden_monitor_daily_speech_counts;
