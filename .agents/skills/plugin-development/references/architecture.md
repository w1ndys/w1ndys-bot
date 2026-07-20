# Plugin Configuration and Admin Architecture

## Decision summary

Keep the current compile-time plugin model. Add declarative configuration and declarative admin resources as optional capabilities. Render ordinary management UI generically, while plugins retain ownership of business validation, persistence, transactions, and hot refresh.

```text
compiled package
  -> Manifest + Factory + Plugin
  -> optional Configurable
  -> optional AdminResourceProvider

WebUI schema renderer
  -> platform management API
  -> authorization + validation + audit + conflict detection
  -> plugin config/resource handler
  -> plugin repository
```

## Separate the three concerns

### Code loading

Continue using blank imports, Catalog registration, Factory dependency injection, and PluginManager lifecycle control. This project is a source-built personal bot, so dynamic Go loading adds compatibility and security cost without solving configuration or UI reuse.

If third-party distribution becomes a real requirement, define an out-of-process protocol with explicit capabilities, authentication, deadlines, health checks, and failure isolation.

### Operator configuration

Use configuration for small settings that change plugin behavior, such as endpoints, timeouts, templates, feature switches, and credentials. Persist values in `plugin_config.config_json`, but require a declared schema and server-side plugin validation.

Suggested conceptual contracts:

```go
type Configurable interface {
    ConfigSchema() ConfigSchema
    ValidateConfig(context.Context, json.RawMessage) error
    ApplyConfig(context.Context, json.RawMessage) error
}
```

The actual implementation must follow the repository's mandatory code-comment format.

Start with a small field vocabulary:

- string, multiline string, integer, number, boolean
- enum and multi-enum
- URL and duration
- secret/write-only string
- homogeneous list
- grouped object only when a real plugin needs it

Include stable key, display label, description, required/default constraints, validation limits, sensitivity, and editability. Do not expose arbitrary HTML, executable expressions, SQL, or client-selected component names.

Generic endpoints can follow:

```text
GET /api/plugins/{plugin}/config/schema
GET /api/plugins/{plugin}/config
PUT /api/plugins/{plugin}/config
```

Configuration writes should perform authorization, strict decoding, schema validation, plugin validation, optimistic version checking, persistence with audit, and hot apply. If hot apply fails after persistence, compensate to the previous snapshot or isolate the plugin and report both errors.

### Business data

Use business tables for records that grow, need pagination/filtering, participate in relationships, or have their own lifecycle. Examples include subscriptions, scheduled jobs, keyword rules, and check-in records. Add paired SQL migrations and a plugin-owned repository; do not turn `config_json` into a document database.

An optional resource descriptor can declare:

- stable resource key and display metadata
- field schema and readable/listable/editable flags
- allowed filters and sorts
- create/update/delete capabilities
- explicit row actions
- pagination limits and optimistic version field

Suggested conceptual handler:

```go
type AdminResourceHandler interface {
    List(context.Context, ResourceQuery) (ResourcePage, error)
    Get(context.Context, string) (ResourceRecord, error)
    Create(context.Context, json.RawMessage) (ResourceRecord, error)
    Update(context.Context, string, json.RawMessage) (ResourceRecord, error)
    Delete(context.Context, string, string) error
}
```

The platform must route only to registered resource keys and validated fields. The plugin handler must implement domain rules and fixed SQL. Never accept a database table name, SQL fragment, or unrestricted column name from the browser.

Generic endpoints can follow:

```text
GET    /api/plugins/{plugin}/resources
GET    /api/plugins/{plugin}/resources/{resource}
POST   /api/plugins/{plugin}/resources/{resource}
GET    /api/plugins/{plugin}/resources/{resource}/{id}
PATCH  /api/plugins/{plugin}/resources/{resource}/{id}
DELETE /api/plugins/{plugin}/resources/{resource}/{id}
```

## WebUI rendering

Build two reusable surfaces:

- `PluginConfigForm`: schema-driven grouped form with secret preservation, validation errors, dirty state, save conflict handling, and reset behavior.
- `PluginResourceTable`: descriptor-driven columns, safe filters, pagination, create/edit dialog, delete confirmation, version conflict handling, and explicit actions.

The server is authoritative. Browser validation improves usability but never replaces authorization or server-side validation.

Allow a custom plugin route only for UI that cannot be represented by the generic vocabulary, such as graph/workflow editing, calendar scheduling, streaming logs, dashboards, drag-and-drop ordering, or multi-resource transactions.

## Cross-cutting requirements

- Authorize every read and write; do not trust plugin names, resource names, field names, roles, or QQ identifiers from the client.
- Record actor, channel, request ID, target, before/after snapshots, success, and sanitized errors in audit logs.
- Redact or omit secrets from reads and audits. Treat an omitted write-only field as “preserve existing value,” not “clear it.”
- Use optimistic versions or ETags for concurrent edits. Return explicit conflicts instead of last-write-wins.
- Bound page size, filter complexity, payload size, lifecycle duration, and external calls.
- Keep runtime snapshots atomic. On persistence/hot-refresh divergence, compensate or quarantine rather than silently serving stale state.
- Test transaction rollback, audit failure, refresh failure, compensation failure, cancellation, stale versions, duplicate keys, unknown fields, concurrent writes, and sensitive-field redaction.

## Delivery sequence

1. Define the minimal `ConfigSchema` and `Configurable` contracts.
2. Implement generic config read/write service with strict validation, audit, optimistic concurrency, and hot apply.
3. Implement the generic Vue configuration form.
4. Convert one real plugin and validate the vocabulary.
5. Define `AdminResource` from a real record-management need.
6. Implement generic resource API and table/form UI.
7. Add custom-page registration only for a proven gap.

Do not design a plugin marketplace, unrestricted dynamic loader, universal SQL CRUD engine, or full JSON Schema implementation as part of the first configuration milestone.
