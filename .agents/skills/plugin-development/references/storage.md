<!-- 📌 影响范围：规定插件配置、业务表、群隔离、事务和运行时快照；无外部变量。 -->
# Plugin Storage

## Classify state

Use config JSON only for a bounded set of scalar runtime settings. Use versioned plugin-owned tables for records that grow, require queries, relationships, auditing, pagination, or independent lifecycle.

## Tables and repositories

Add paired migrations; never edit deployed migrations. Store time as `TIMESTAMPTZ` and normalize to UTC. Group-owned rows carry `group_id`, and uniqueness, reads, updates, and deletes must preserve group isolation.

Treat the verified path/context group as authoritative. Reject or ignore no body group identifier: do not accept one at all. Use fixed parameterized SQL and explicit mapping for supported filters or sorts.

## Consistency

Use optimistic versions for concurrent edits. Keep domain writes and audit records transactional where practical. Publish immutable runtime snapshots only after successful validation and persistence, and keep database reads out of the message hot path.

Do not hold database transactions across OneBot, model, or HTTP calls. Use jobs/outbox or an explicit retryable state when an external side effect must follow persistence.

## Secrets

Model credentials as write-only configuration fields. Omitted secret updates preserve the existing value. Never return secrets from read APIs or include them in logs, errors, audit snapshots, or manifests.
