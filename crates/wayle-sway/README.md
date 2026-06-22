# wayle-sway

Reactive bindings to the [sway](https://swaywm.org/) compositor over its i3
IPC socket.

`SwayService::new` connects to `$SWAYSOCK`, subscribes to sway's `workspace`
and `window` events, and exposes compositor state through `Property<T>` fields
that stay in sync automatically. On every relevant event the service re-queries
`GET_WORKSPACES` / `GET_TREE` and refreshes the reactive fields, preserving
`Arc` identity for entities that still exist so per-field watchers fire
minimally.
