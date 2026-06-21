//! D-Bus interface definitions shared between the Wayle CLI and shell daemon.

/// Shell D-Bus interface for GTK actions and IPC.
pub mod shell;

/// File chooser D-Bus client proxy.
pub mod file_chooser;

/// Idle inhibit D-Bus client proxy.
pub mod idle_inhibit;

/// Portal dialog host D-Bus client proxy (access/account/appchooser/launcher).
pub mod portal_dialogs;

/// Print host D-Bus client proxy.
pub mod print;

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
