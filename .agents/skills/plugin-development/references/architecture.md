<!-- 📌 影响范围：概述插件目标架构及状态分类决策；无外部变量。 -->
# Plugin Architecture Decisions

Read `docs/plugin-architecture-v2.md` for the complete target design. It is a migration target, not proof that current interfaces exist.

## Stable boundaries

```text
code owns: plugin identity, commands, scopes, roles, config schema
database owns: operator state, config values, group gates, audit, business records
platform owns: dispatch, gates, identity, admin authentication, common WebUI shell
plugin owns: domain validation, repository, service, dedicated API and complex page
```

The command execution chain is `group event -> global Ready -> group Enabled -> code-declared role -> Handler`. Observation handlers also require global and group gates but do not apply command roles. Private messages do not enter target-architecture plugins. Missing state and unknown identity fail closed. All plugins and groups default to disabled.

## Choose the management model

- Use the common platform page when a plugin only needs status, global/group gates, and scalar configuration.
- Use the small ConfigSchema for bounded strings, numbers, booleans, enums, multiline text, and write-only secrets.
- Use plugin-owned tables, repositories, semantic APIs, and a dedicated compiled Vue page for growing records and complex operations.
- Share UI primitives and error behavior; do not force distinct domains into a universal resource protocol.

Management reads and safe offline edits may remain available while runtime is disabled. Operations that invoke OneBot, models, network engines, or group effects must recheck the required runtime gates.

## Avoid accidental platforms

Do not implement dynamic Go loading, arbitrary JSON Schema, browser-selected SQL, remote Vue components, generic action DSLs, or a second database permission system. Abstract only repeated behavior demonstrated by multiple real plugins.
