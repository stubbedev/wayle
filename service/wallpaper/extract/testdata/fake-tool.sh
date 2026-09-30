#!/bin/sh
# Records one extractor invocation: argv (one per line) and, for
# wallust, the generated config and template.
tool=$(basename "$0")
out="$GOLDEN_OUT"
: > "$out/$tool.args"
for a in "$@"; do printf '%s\n' "$a" >> "$out/$tool.args"; done
if [ "$tool" = wallust ]; then
  prev=""
  for a in "$@"; do
    if [ "$prev" = "-C" ]; then cp "$a" "$out/wallust.toml"; fi
    if [ "$prev" = "--templates-dir" ]; then cp "$a/colors.json" "$out/colors.json.tmpl"; fi
    prev="$a"
  done
fi
if [ "$tool" = matugen ]; then
  printf '{"colors":{"primary":"#aabbcc"}}\n'
fi
if [ -n "$FAKE_FAIL" ]; then
  echo "  boom on stderr  " >&2
  exit 3
fi
exit 0
