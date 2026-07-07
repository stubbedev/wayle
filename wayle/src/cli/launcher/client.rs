//! Session client: streams rows to the daemon, awaits the outcome, prints
//! it rofi-style, and exits with the rofi-compatible code.

use tokio::io::AsyncReadExt;
use wayle_ipc::launcher_socket::{
    ClientError, FrameWriter, LauncherClient, ROW_CHUNK, Selected, ServerFrame,
};

use super::args::Invocation;

/// Drive a full session. Returns the process exit code.
pub async fn run(invocation: Invocation) -> i32 {
    let Invocation {
        options,
        replace,
        format,
        input_file,
        row_separator,
        ..
    } = invocation;
    let dmenu = options.dmenu;
    let prompt = options.prompt.clone().unwrap_or_default();

    let client = match LauncherClient::open(options, replace).await {
        Ok(client) => client,
        Err(error) => {
            eprintln!("wayle launcher: {error}");
            return 1;
        }
    };
    let (mut reader, writer) = client.split();

    let pump = dmenu.then(|| {
        tokio::spawn(pump_rows(
            writer,
            input_file,
            row_separator.unwrap_or_else(|| "\n".to_owned()),
        ))
    });

    let code = loop {
        let frame = tokio::select! {
            frame = reader.next_frame() => frame,
            _ = tokio::signal::ctrl_c() => break 1,
        };
        match frame {
            Ok(ServerFrame::Opened) => {}
            Ok(ServerFrame::Busy) => {
                eprintln!("wayle launcher: another session is active (use -replace)");
                break 1;
            }
            Ok(ServerFrame::Result {
                code,
                selected,
                filter,
            }) => {
                print_result(&format, &selected, &filter, &prompt);
                break code;
            }
            Ok(ServerFrame::Cancelled { code }) => break code,
            Ok(ServerFrame::Dump { items }) => {
                for item in items {
                    println!("{item}");
                }
                break 0;
            }
            Err(ClientError::Disconnected) => break 1,
            Err(error) => {
                eprintln!("wayle launcher: {error}");
                break 1;
            }
        }
    };
    if let Some(pump) = pump {
        pump.abort();
    }
    code
}

/// Read dmenu rows (stdin or `-input` file), split on the separator, and
/// stream them in chunks. Newline separation streams incrementally; a custom
/// separator reads to EOF first (rofi `-sep` is rare and lists are small).
async fn pump_rows(mut writer: FrameWriter, input_file: Option<String>, separator: String) {
    let rows: Vec<String> = match read_input(input_file).await {
        Ok(raw) => split_rows(&raw, &separator),
        Err(error) => {
            eprintln!("wayle launcher: reading input failed: {error}");
            Vec::new()
        }
    };
    for chunk in rows.chunks(ROW_CHUNK) {
        if writer.send_rows(chunk.to_vec()).await.is_err() {
            return;
        }
    }
    let _ = writer.finish_rows().await;
}

async fn read_input(input_file: Option<String>) -> std::io::Result<String> {
    match input_file {
        Some(path) => tokio::fs::read_to_string(path).await,
        None => {
            let mut raw = String::new();
            tokio::io::stdin().read_to_string(&mut raw).await?;
            Ok(raw)
        }
    }
}

fn split_rows(raw: &str, separator: &str) -> Vec<String> {
    let separator = match separator {
        "\\n" => "\n",
        "\\0" => "\0",
        "\\t" => "\t",
        other => other,
    };
    let mut rows: Vec<String> = raw.split(separator).map(ToOwned::to_owned).collect();
    // A trailing separator (usual for line-based input) yields one empty
    // phantom row — drop it.
    if rows.last().is_some_and(String::is_empty) {
        rows.pop();
    }
    rows
}

/// rofi `-format`: s selection text, i input index (0-based), d 1-based,
/// q quoted s, p prompt, f filter, F quoted filter.
fn print_result(format: &str, selected: &[Selected], filter: &str, prompt: &str) {
    let quote = |text: &str| shlex::try_quote(text).map(|q| q.into_owned());
    for row in selected {
        let line = match format {
            "i" => row.index.to_string(),
            "d" => (row.index + 1).to_string(),
            "q" => quote(&row.text).unwrap_or_else(|_| row.text.clone()),
            "p" => prompt.to_owned(),
            "f" => filter.to_owned(),
            "F" => quote(filter).unwrap_or_else(|_| filter.to_owned()),
            _ => row.text.clone(),
        };
        println!("{line}");
    }
    // Filter/prompt formats still print once when nothing was selected but
    // the accept was custom-with-empty-selection.
    if selected.is_empty() && matches!(format, "f" | "F" | "p") {
        let line = match format {
            "p" => prompt.to_owned(),
            "F" => quote(filter).unwrap_or_else(|_| filter.to_owned()),
            _ => filter.to_owned(),
        };
        println!("{line}");
    }
}

#[cfg(test)]
mod tests {
    use super::split_rows;

    #[test]
    fn rows_split_and_trailing_empty_dropped() {
        assert_eq!(split_rows("a\nb\n", "\n"), vec!["a", "b"]);
        assert_eq!(split_rows("a\nb", "\n"), vec!["a", "b"]);
        assert_eq!(split_rows("a|b|", "|"), vec!["a", "b"]);
        assert_eq!(split_rows("a\0b", "\\0"), vec!["a", "b"]);
    }
}
