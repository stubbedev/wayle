#!/usr/bin/env bash
# Captures the Rust `wayle icons` behavior the Go CLI is held to
# (TestIconsCLIMatchesRust): each step's stdout, stderr and exit code,
# run in a throwaway home with no network access needed.
#
#   cmd/wayle/testdata/icons/capture.sh "$(command -v wayle)" cmd/wayle/testdata/icons
set -eu
bin=$1
out=$(realpath "$2")
here=$(dirname "$(realpath "$0")")
root=$(mktemp -d)
trap 'rm -rf -- "${root:?}"' EXIT

export HOME=$root/home XDG_DATA_HOME=$root/data XDG_CONFIG_HOME=$root/config XDG_DATA_DIRS=
mkdir -p "$HOME" "$XDG_CONFIG_HOME/wayle"
cp -r "$here/input" "$root/input"
cp "$here/config.toml" "$XDG_CONFIG_HOME/wayle/config.toml"

steps=$here/steps.txt
n=0
while IFS= read -r line; do
	[ -z "$line" ] && continue
	n=$((n + 1))
	# shellcheck disable=SC2086 # the step's words
	set -- $line
	args=()
	for a in "$@"; do args+=("${a//@ROOT@/$root}"); done
	code=0
	(cd "$root" && "$bin" icons "${args[@]}") >"$root/stdout" 2>"$root/stderr" || code=$?
	name=$(printf '%02d' "$n")
	sed "s|$root|@ROOT@|g" "$root/stdout" >"$out/$name.stdout"
	sed "s|$root|@ROOT@|g" "$root/stderr" >"$out/$name.stderr"
	echo "$code" >"$out/$name.code"
done <"$steps"
