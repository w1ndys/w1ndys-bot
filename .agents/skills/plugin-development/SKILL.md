---
name: plugin-development
description: Design, add, change, or review w1ndys-bot compile-time plugins and their commands, lifecycle, global/group gates, configuration, plugin-owned storage, dedicated admin APIs, Vue pages, migrations, auditing, and tests. Use for every new plugin and whenever modifying plugin runtime, plugin configuration, plugin business data, plugin WebUI, or plugin management endpoints.
---

<!-- 📌 影响范围：规定插件开发的强制阶段门、参考资料路由和验收顺序；无外部变量。 -->
# Plugin Development

## Load only the required contracts

1. Read [architecture.md](references/architecture.md) for every plugin task.
2. Read [runtime.md](references/runtime.md) when adding commands, event handling, dependencies, lifecycle, registration, or global/group gates.
3. Read [storage.md](references/storage.md) when the plugin has configuration, migrations, repositories, snapshots, or business records.
4. Read [admin-webui.md](references/admin-webui.md) when adding management APIs or WebUI.
5. Read [testing-review.md](references/testing-review.md) before changing code and again before delivery.
6. Read `docs/guide.md` and the relevant `internal/` packages when changing the event chain, Action Client, Dispatcher, identity resolution, lifecycle manager, or database platform services.

The target architecture is documented in `docs/plugin-architecture-v2.md` and is not fully implemented yet. Inspect current code before using a target contract. Do not invent an unavailable interface; either implement the agreed migration stage or remain compatible with current code and state the limitation.

## Gate 0: write the design card

Before editing code, record:

- stable plugin key, purpose, commands, scopes, and code-declared roles;
- command-only versus observation/background behavior;
- scalar settings versus growing business data;
- WebUI operations and whether a dedicated page is required;
- external side effects, background work, and runtime dependencies;
- management operations that must work while the plugin is disabled.

Classify state before designing storage:

```text
small bounded runtime settings -> config_json + small ConfigSchema
growing/queryable/relational/audited records -> plugin tables + repository
complex business management -> dedicated API + dedicated Vue page
only status, gates, and scalar settings -> common platform page
```

Do not continue until the design card answers who may use the plugin, where it runs, where each state lives, and how administrators operate it.

## Gate 1: define contracts before implementation

Define stable identifiers, command roles, global/group gate semantics, configuration type, table ownership, API DTOs, page operations, lifecycle, and snapshot publication.

The target message chain is fixed:

```text
command match -> global Ready -> group Enabled -> code role -> Handler
```

Target-architecture plugins are group-only. Reject private messages before group gating; the QQ emergency interface belongs to platform management services. Observation handlers use `global Ready -> group Enabled -> observer`, without command-role authorization. Global lifecycle controls background tasks, and every group side effect from background work rechecks that group's gate.

Plugins must not query or duplicate platform gates. Missing state, unknown identity, invalid scope, and undeclared roles fail closed. Only proceed when contracts and failure behavior are reviewable without reading Handler internals.

## Gate 2: build the smallest registered skeleton

Implement stable metadata, compile-time registration, dependency checks, and idempotent lifecycle behavior. Add each entry type selected by the design card: commands declare triggers and roles; observers declare stable keys and supported group event types; background-only plugins declare no fake message entry and start work only through lifecycle. Add registration tests for duplicate or invalid declarations.

Prove that the Catalog discovers the plugin. For every applicable entry, prove fail-closed behavior: missing global/group state blocks commands and observers; background work starts only inside the controlled `enabling`/`OnEnable` transition and is prepared before `Ready`; background group effects recheck group state. Do not add storage or WebUI before this path is established.

## Gate 3: implement storage from the inside out

If the design card has no configuration or persistent business data, record that this gate is not applicable and skip it. Otherwise execute only the applicable steps below while preserving their relative order; scalar-only plugins do not create fake business repositories, and business-data-only plugins do not create fake configuration.

Implement in this order:

1. applicable configuration and domain types with server-side validation;
2. paired migrations for applicable business tables;
3. repository with fixed SQL and group isolation when business tables exist;
4. domain service and transactions when business writes exist;
5. immutable runtime snapshot publication when runtime state is cached;
6. tests for every selected storage capability and its concurrency boundary.

Do not put growing records in config JSON, trust a body `group_id`, accept client-selected SQL identifiers, mix local times, or query database configuration on the message hot path. Do not continue to management APIs until normal, cross-group, conflict, and rollback paths pass.

## Gate 4: implement management backend

If the common platform page fully covers this plugin and it has no plugin-specific management operation, record that this gate is not applicable and skip it.

Prefer semantic plugin-specific endpoints for complex business operations. The platform owns administrator authentication, trusted group context, request IDs, limits, error mapping, audit orchestration, timeout, and panic isolation. The plugin owns domain validation, transactions, repository calls, snapshot refresh, and external side-effect orchestration.

Every write endpoint needs authorization, strict input, cross-group, conflict, rollback, audit, and error-path tests. Permit offline configuration and history reads where safe; recheck global/group gates for OneBot, model, network-engine, or group side effects.

## Gate 5: implement WebUI

If the plugin needs only the common status, gate, and scalar-config page, record that no dedicated page is required and skip plugin-specific WebUI code.

Use a dedicated compiled Vue page for complex plugin business management and compose shared platform components. Use the common ConfigSchema form only for small scalar settings.

Register pages through a compile-time map. Never execute plugin-provided component names, URLs, HTML, scripts, expressions, or SQL. Use the application-level global Toast for operation feedback, convert UTC times at presentation, and handle loading, empty, error, conflict, disabled, narrow-screen, and duplicate-submit states.

Do not create a universal CRUD/page schema. Extract a higher-level shared component only after multiple real pages demonstrate the same stable behavior.

## Gate 6: connect runtime behavior

Connect the Handler after every applicable storage and management gate is complete. A command Handler consumes only an already matched and authorized typed context. An observation Handler consumes only group events that passed global and group gates. Background tasks start and stop with global lifecycle; before affecting a group they recheck its gate. Bound external calls, respect cancellation, keep lifecycle idempotent, release goroutines/timers/subscriptions, publish snapshots atomically, and keep SQL out of the hot path.

Do not claim atomicity across a database transaction and irreversible OneBot or HTTP side effects. Use an explicit retry/job boundary when required.

## Gate 7: independent review and verification

After every code change, use an independent subagent to review requirement fit, authorization, input validation, cross-group isolation, transactions, snapshot consistency, concurrency, resource cleanup, sensitive data, error handling, and missing tests. Fix findings and rerun checks.

Run at least:

```text
task lint
task test
task web-build       # when WebUI changes
relevant race tests
relevant e2e tests
git diff --check
```

Do not deliver while a required check is failing or skipped without a concrete explanation.

## Guardrails

- Keep plugins compiled in; use an out-of-process protocol if third-party distribution becomes real.
- Keep plugin state, configuration, and business records separate.
- Keep code-declared command roles read-only in WebUI.
- Keep QQ commands as a minimal emergency interface reusing the same application service.
- Do not build a plugin marketplace, universal CRUD engine, full JSON Schema system, generic SQL mapper, or dynamic frontend loader.
- Do not let a disabled plugin make its offline configuration or history irrecoverable.
