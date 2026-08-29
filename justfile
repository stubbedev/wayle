set shell := ["bash", "-euo", "pipefail", "-c"]

# Run cargo inside the flake devShell so the GTK/clang build env is always
# present — no need to `nix develop` first. The Rust toolchain still comes from
# your PATH (rustup); the flake only supplies the native libraries + pkg-config.
cargo := "nix develop --command cargo"

# Default — list recipes.
default:
    @just --list --unsorted

# ─────────────────────────── Build & Run ───────────────────────────

# Build the release binary.
build:
    {{cargo}} build --release --locked

# Run the wayle binary (debug). Extra args pass through: `just run -- --help`.
run *args:
    {{cargo}} run --bin wayle -- {{args}}

# Debug logging for the dev recipes below. RUST_LOG drives the console, and
# WAYLE_FILE_LOG the rolling file in `just logs` — both default to `warn` and
# `warn,wayle_=info` otherwise, which hides the debug! lines that explain
# popover/dropdown behaviour. Exporting either variable yourself wins.
dev_log := 'RUST_LOG="${RUST_LOG:-warn,wayle_=debug}" WAYLE_FILE_LOG="${WAYLE_FILE_LOG:-warn,wayle_=debug}"'

# Run the settings GUI with SCSS hot-reload — for rapidly iterating on the
# settings/widget CSS. WAYLE_DEV=1 makes the app watch
# crates/wayle-styling/scss/** and recompile + reload live on save (~100ms),
# no restart. Edit the SCSS, watch the open window repaint.
dev-settings *args:
    {{dev_log}} WAYLE_DEV=1 {{cargo}} run --bin wayle-settings -- {{args}}

# Like dev-settings, plus the GTK inspector — pick any widget to see its CSS
# node names + classes so you know which selector to target. (Ctrl+Shift+D
# toggles the inspector in-app too.)
inspect-settings *args:
    {{dev_log}} WAYLE_DEV=1 GTK_DEBUG=interactive {{cargo}} run --bin wayle-settings -- {{args}}

# Run the shell with SCSS hot-reload (watches crates/wayle-styling/scss/**).
# Pass the subcommand: `just dev shell`.
dev *args:
    {{dev_log}} WAYLE_DEV=1 {{cargo}} run --bin wayle -- {{args}}

# Like dev, plus the GTK inspector for the bar/overlays — pick any widget to see
# its CSS node names + classes. (Ctrl+Shift+D toggles the inspector in-app too.)
# Pass the subcommand: `just inspect shell`.
inspect *args:
    {{dev_log}} WAYLE_DEV=1 GTK_DEBUG=interactive {{cargo}} run --bin wayle -- {{args}}

# Tail today's shell log. The dev recipes above write debug! lines here, so this
# is where to look after reproducing a bug — `just logs 200` for more backlog.
logs lines="80":
    tail -n {{lines}} -f "${XDG_STATE_HOME:-$HOME/.local/state}/wayle/wayle-shell.$(date +%F).log"

# Stop the packaged shell so a dev build can take over (only one instance may
# hold the com.wayle.shell bus name), run it, then restore the service on exit.
dev-shell *args:
    #!/usr/bin/env bash
    set -uo pipefail
    was_active=$(systemctl --user is-active wayle.service || true)
    if [ "$was_active" = "active" ]; then systemctl --user stop wayle.service; fi
    trap '[ "$was_active" = "active" ] && systemctl --user start wayle.service' EXIT
    just inspect shell {{args}}

# Run the greeter as a window on your current session for UI iteration: your
# user config for theming, throwaway state in /tmp. Login attempts only show an
# error unless a real greetd socket is around ($GREETD_SOCK), which is fine for
# visuals. WAYLE_GREETER_DEBUG=popup opens the session dropdown after 1s;
# WAYLE_GREETER_DEBUG=login=user:pass auto-submits (see app.rs).
dev-greeter *args:
    {{cargo}} run --bin wayle-greeter -- --config ~/.config/wayle/config.toml --state /tmp/wayle-greeter-dev/last-session {{args}}

# Remove target/ build artifacts.
clean:
    {{cargo}} clean

# ─────────────────────────── Format & Lint ───────────────────────────

# Format the workspace in place.
fmt:
    {{cargo}} fmt --all

# Format, then lint with clippy (warnings are errors).
lint: fmt
    {{cargo}} clippy --workspace --all-targets -- -D warnings

# Strict read-only check — same logic CI runs, for local pre-push.
# Fails if formatting would change or any lint fires.
lint-check:
    {{cargo}} fmt --all --check
    {{cargo}} clippy --workspace --all-targets -- -D warnings

# Run the test suite. Doctests separately: nextest doesn't run them.
test:
    {{cargo}} nextest run --workspace --no-fail-fast
    {{cargo}} test --doc --workspace

# Format, lint and test. Run before every release.
check: lint test

# Regenerate the committed config JSON schema from the Rust types. The schema's
# $id embeds the workspace version, so this must be re-run after a version bump
# (the release flow does this automatically) — CI fails if it drifts.
schema:
    {{cargo}} run -q -p wayle -- config schema --stdout | jq -S . > schema/wayle-config.schema.json

# ─────────────────────────── Dependencies ───────────────────────────

# Update Cargo.lock to the latest semver-compatible versions.
update:
    {{cargo}} update --workspace

# ─────────────────────────── Nix cache ───────────────────────────

# Build the flake package and push its closure to the self-hosted xilo
# cache (https://nix.stubbe.dev, cache `default/default`) — the same cache
# CI/releases push to (`xilo push default`). Requires the xilo client logged
# in (`xilo login https://nix.stubbe.dev --token <tok>`). Pull access is
# public via the flake's substituter.
cache-push:
    nix build '.?submodules=1#wayle' -L --accept-flake-config
    xilo push default/default ./result

release-preview:
    #!/usr/bin/env bash
    set -euo pipefail
    CURRENT_TAG=$(git tag -l 'v*.*.*' --sort=-v:refname | head -1)
    CURRENT_TAG=${CURRENT_TAG:-v0.0.0}
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)
    echo "Current tag: $CURRENT_TAG"
    echo "  release-major: v$((MAJOR + 1)).0.0"
    echo "  release-minor: v${MAJOR}.$((MINOR + 1)).0"
    echo "  release-patch: v${MAJOR}.${MINOR}.$((PATCH + 1))"

_release-checks:
    #!/usr/bin/env bash
    set -euo pipefail
    BRANCH=$(git rev-parse --abbrev-ref HEAD)
    DEFAULT_BRANCH=$(git rev-parse --abbrev-ref origin/HEAD 2>/dev/null | sed 's|^origin/||' || true)
    if [ -z "${DEFAULT_BRANCH:-}" ]; then
        DEFAULT_BRANCH=$(git remote show origin 2>/dev/null | awk '/HEAD branch/ {print $NF}' || echo master)
    fi
    if [ "$BRANCH" != "$DEFAULT_BRANCH" ]; then
        echo "Error: not on default branch '$DEFAULT_BRANCH' (currently '$BRANCH')." >&2
        exit 1
    fi
    just check
    if [ -n "$(git status --porcelain)" ]; then
        echo "Formatting/lint produced changes — staging + committing."
        git add -A
        git commit -m "chore: format code for release"
    fi

_release bump:
    #!/usr/bin/env bash
    set -euo pipefail
    just _release-checks
    CURRENT_TAG=$(git tag -l 'v*.*.*' --sort=-v:refname | head -1)
    CURRENT_TAG=${CURRENT_TAG:-v0.0.0}
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)
    case "{{bump}}" in
        major) NEW="$((MAJOR + 1)).0.0" ;;
        minor) NEW="${MAJOR}.$((MINOR + 1)).0" ;;
        patch) NEW="${MAJOR}.${MINOR}.$((PATCH + 1))" ;;
        *) echo "unknown bump kind: {{bump}}"; exit 1 ;;
    esac
    # Bump the workspace version BEFORE tagging. The release workflow
    # verifies the tag matches [workspace.package].version in Cargo.toml
    # and refuses to publish on a mismatch, so this must land first.
    sed -i -E '/\[workspace\.package\]/,/^\[/{s|^(version = )"[^"]*"|\1"'"$NEW"'"|}' Cargo.toml
    # Refresh Cargo.lock so `cargo build --locked` in CI sees the new version.
    cargo update --workspace
    # The schema $id embeds the version — regenerate so it doesn't drift and
    # fail the CI schema check (which is exactly what happened for v0.7.3).
    just schema
    if [ -n "$(git status --porcelain Cargo.toml Cargo.lock schema/wayle-config.schema.json)" ]; then
        git add Cargo.toml Cargo.lock schema/wayle-config.schema.json
        git commit -m "chore: release v${NEW}"
    fi
    git tag -a "v${NEW}" -m "v${NEW}"
    git push origin HEAD
    git push origin "v${NEW}"
    echo
    echo "Tagged v${NEW}."
    echo "Watch the release build: gh run watch || open https://github.com/stubbedev/wayle/actions"

release-patch: (_release "patch")
release-minor: (_release "minor")
release-major: (_release "major")
