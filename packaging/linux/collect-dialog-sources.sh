#!/usr/bin/env bash
# apt sources must include deb-src for the same repositories as the build.
set -euo pipefail
runtime=$(cd "$1" && pwd)
output=$(realpath -m "$2")
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
cp "$runtime"/sources/* "$work/"
cp "$runtime/packages.tsv" "$runtime/README.md" "$work/"
cd "$work"
while IFS=$'\t' read -r package version; do
    apt-get source --download-only "$package=$version"
done < <(awk -F '\t' 'NF == 4 {print $3 "\t" $4}' "$runtime/packages.tsv" | sort -u)
tar -cJf "$output" .
(cd "$(dirname "$output")" && sha256sum "$(basename "$output")" > "$(basename "$output").sha256")
