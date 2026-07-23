<!-- 📌 影响范围：规定插件注册、执行门禁、身份授权和生命周期边界；无外部变量。 -->
# Plugin Runtime

## Registration

Use stable machine keys for plugins and commands. Reject duplicates, invalid scopes, empty role sets, missing dependencies, and conflicting triggers during startup. Keep compile-time registration explicit and test that the Catalog exposes the plugin.

Commands declare triggers, group scope, code roles, and a Handler. Observers declare a stable key, a non-empty set of platform-supported group event kinds, and an Observer Handler. Reject duplicate observer keys, empty event sets, and unsupported event kinds. Background-only plugins register lifecycle capability without inventing a command or observer.

## Dispatch

All message commands pass one Dispatcher chain:

```text
normalize -> match -> global Ready -> group Enabled -> scope -> identity -> role set -> Handler
```

Reject private messages before the group gate. The target architecture supports group plugins; the QQ emergency interface is a platform management path. Observation handlers receive unmatched group events only after global and group gates and do not apply command roles. Plugins must not query gate tables, register bypass routes, repeat identity resolution, or authorize from mutable configuration. Super admin access must be declared explicitly.

## State and lifecycle

Persist administrator intent separately from actual runtime status. Only `Ready` admits new calls. Enable completely before admission. Disable admission first, drain in-flight calls, then release resources.

`OnEnable` and `OnDisable` must be idempotent, cancellation-aware, panic-safe, bounded, and responsible for their goroutines, timers, clients, and subscriptions. Report failed transitions without pretending the desired state succeeded at runtime.

Global lifecycle owns background task startup and cancellation. Start and prepare background work only inside the controlled `enabling`/`OnEnable` transition; admit plugin traffic only after that transition succeeds and the runtime becomes `Ready`. Before a background task reads or changes a specific group's live state, it must use the platform gate service to recheck that group. Disable cancels and drains background work before releasing resources.

## Handlers

Receive an already matched, gated, and authorized typed context. Keep handlers small, respect cancellation, add deadlines to external calls, and return errors with domain context but without secrets or raw private content.
