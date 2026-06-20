//! D-Bus interface definitions shared between the Wayle CLI and shell daemon.

/// Shell D-Bus interface for GTK actions and IPC.
pub mod shell;

/// Idle inhibit D-Bus client proxy.
pub mod idle_inhibit;

/// Screen recorder D-Bus client proxy.
pub mod recorder;

/// Screenshot D-Bus client proxy.
pub mod screenshot;

/// Share picker D-Bus client proxy.
pub mod share_picker;

/// Shell IPC D-Bus client proxy.
pub mod shell_ipc;

/// Unix-socket JSON-RPC protocol for runtime widget updates.
pub mod widget_socket;
