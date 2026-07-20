---
name: plugin-development
description: Design, implement, or review w1ndys-bot plugins, including compile-time registration, lifecycle safety, schema-driven plugin configuration, generic WebUI forms, plugin-owned business-data CRUD, migrations, permissions, auditing, and tests. Use when adding a plugin, extending Plugin Runtime or Manifest, designing plugin config_json handling, building generic plugin administration APIs/UI, or deciding whether a plugin needs custom WebUI.
---

# Plugin Development

## Start with the repository contracts

1. Read `docs/guide.md` before changing event routing, permissions, lifecycle, Manifest synchronization, or Action Client behavior.
2. Read `docs/plugin-development.md`, `internal/plugin/`, and `plugins/echo/` before implementing a plugin.
3. Read [architecture.md](references/architecture.md) when the task involves configuration, plugin data, management APIs, or WebUI CRUD.
4. Preserve the repository's AI comment, migration, testing, and subagent-review requirements in `AGENTS.md`.

## Choose the extension model

- Keep plugins compiled into the repository and registered through `Manifest + Factory + Plugin`.
- Use runtime enable/disable for operational control; do not introduce Go `.so` loading.
- Treat future third-party dynamic plugins as out-of-process services over a narrow protocol rather than in-process arbitrary code.
- Keep plugin loading, plugin configuration, and plugin business-data CRUD as separate concerns.

## Classify the requested state

- Put stable identity, display metadata, feature keys, default commands, and default permissions in `Manifest`.
- Put small operator-controlled settings in `plugin_config.config_json` and expose them through a declarative `ConfigSchema`.
- Put growing, queryable, relational, or audited business records in versioned plugin tables with plugin-owned repositories.
- Never store tokens in Manifest, logs, audit snapshots, or readable config responses. Model secrets as write-only fields and preserve an existing secret when the update omits it.

## Build generic management surfaces

For ordinary settings, implement one generic config API and one schema-rendered Vue form. For repeated business records, declare an `AdminResource` and route generic resource endpoints to plugin-owned handlers. Keep authorization, schema validation, pagination, request IDs, optimistic concurrency, audit recording, and error mapping in the platform layer. Keep transactions, uniqueness, relationships, and domain rules in the plugin.

Use a custom Vue route only when a resource cannot be represented safely as fields, filters, tables, forms, and explicit actions. Typical exceptions are workflow editors, calendars, live streams, charts, and multi-resource transactional screens.

## Implement in stages

1. Introduce the smallest typed contracts required by one real plugin.
2. Implement configuration schema, validation, persistence, audit, conflict detection, and hot apply before generic business resources.
3. Prove the config form with a real plugin rather than designing a universal schema in isolation.
4. Add `AdminResource` only after a real plugin needs record CRUD.
5. Add custom-page registration only after the generic renderer has a demonstrated limitation.

## Verify boundaries

- Reject unknown fields, invalid types, unsafe filter/sort fields, stale versions, duplicate commands, and unauthorized operations on the server.
- Do not let generic handlers build SQL from client-provided table or column names.
- Make config application and data writes transactional where possible; compensate or isolate runtime state when hot apply fails.
- Ensure lifecycle calls are idempotent, context-aware, panic-safe, and isolated from active handlers.
- Test normal, boundary, authorization, conflict, rollback, audit, cancellation, lifecycle, and concurrent-refresh paths with fakes.
- Run `task lint`, `task test`, the relevant race tests, and `git diff --check` before delivery.

## Keep the architecture evolvable

Prefer an explicit, versioned subset of field types and capabilities over unrestricted JSON Schema or reflection-driven magic. Add a field type, validation rule, filter, or action only when a concrete plugin requires it. Maintain an escape hatch without making custom pages the default.
