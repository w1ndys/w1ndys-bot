<!-- 📌 影响范围：定义插件实现和独立复核的最低测试矩阵；无外部变量。 -->
# Plugin Testing and Review

## Minimum tests

- Registration: identifiers, duplicate triggers/observer keys, empty roles/event sets, unsupported event kinds, missing dependencies, background-only lifecycle registration.
- Dispatch: private rejection, global disabled, group disabled, wrong scope, each role, unknown identity, successful command Handler, gated observation event.
- Lifecycle: repeated enable/disable, enable failure, cancellation, drain, panic, background group-gate recheck, resource release.
- Configuration: defaults, unknown fields, type/range limits, secrets, stale versions, hot-apply failure.
- Repository: normal CRUD, not found, uniqueness, stale version, rollback, UTC, cross-group isolation.
- Admin API: authentication, group authorization, strict input, limits, conflict, audit, sanitized errors.
- WebUI: loading, empty, failure, conflict, duplicate submission, disabled state, responsive layout, global Toast.
- Runtime: concurrent refresh/handling and relevant race tests.

Use fakes for NapCat and external services. Do not depend on a real external database in unit tests unless the repository integration suite explicitly provides an isolated database.

## Independent review

After code changes, give an independent subagent the requirement and diff. Require evidence for requirement fit, authorization, input validation, group isolation, transaction and snapshot consistency, concurrency/resource risks, sensitive information, error handling, and missing tests.

The main agent must verify findings, fix confirmed issues, and rerun relevant checks before delivery.
