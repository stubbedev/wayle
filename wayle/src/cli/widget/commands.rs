use clap::Subcommand;

/// Widget control commands.
#[derive(Subcommand, Debug)]
pub enum WidgetCommands {
    /// Push an output update to a bar widget by its config id.
    ///
    /// The output is interpreted exactly like the widget's own command output:
    /// plain text, or JSON with `text`/`alt`/`percentage`/`class`/`tooltip`
    /// fields (driving icon/color cycling).
    Update {
        /// Config id of the target widget (e.g. a custom module's `id`).
        id: String,
        /// Output payload (plain text, or a JSON object).
        output: String,
    },
}
