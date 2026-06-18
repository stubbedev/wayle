#!/usr/bin/env bash
# Recorder debug capture: runs the locally-built debug `wayle shell` with verbose
# recorder/ashpd/share-picker tracing, so the real in-shell ScreenCast failure is
# logged. Stops your running release shell for the duration, restores it on exit.
#
# Usage:
#   ./contrib/recorder-debug-capture.sh
#   ...then click Record in the bar, pick your monitor, let it run ~3s, stop it.
#   Ctrl-C this script when done. Log is at /tmp/wayle-recorder-debug.log
set -u

REPO="$(cd "$(dirname "$0")/.." && pwd)"
DEBUG_BIN="$REPO/target/debug/wayle"
LOG=/tmp/wayle-recorder-debug.log

if [ ! -x "$DEBUG_BIN" ]; then
  echo "debug binary missing; build it first:  nix develop -c cargo build -p wayle" >&2
  exit 1
fi

echo "Stopping running release shell..."
pkill -f '/bin/wayle shell' 2>/dev/null
sleep 1

echo "Launching debug shell -> $LOG"
echo "  (click Record, pick a monitor, record a few seconds, then Stop)"
RUST_LOG="info,wayle_recorder=trace,wayle_shell::services::recorder=trace,wayle_shell::services::share_picker=trace,wayle_shell::shell::share_picker=trace,ashpd=trace,zbus=warn" \
  "$DEBUG_BIN" shell >"$LOG" 2>&1 &
SHELL_PID=$!

cleanup() {
  echo; echo "Stopping debug shell..."
  kill "$SHELL_PID" 2>/dev/null
  wait "$SHELL_PID" 2>/dev/null
  echo "Log saved: $LOG"
  echo "Your normal shell will respawn via its service (or restart it manually)."
}
trap cleanup INT TERM

echo "Debug shell PID $SHELL_PID. Press Ctrl-C when you've reproduced the failure."
wait "$SHELL_PID"
