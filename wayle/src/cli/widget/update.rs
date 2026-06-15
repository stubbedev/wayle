use super::super::CliAction;

/// Sends an output update to the widget identified by `id`.
///
/// # Errors
/// Returns an error if the shell's widget socket is unreachable or the server
/// rejects the request.
pub async fn execute(id: &str, output: &str) -> CliAction {
    wayle_ipc::widget_socket::send_widget_update(id, output)
        .await
        .map_err(|err| err.to_string())
}
