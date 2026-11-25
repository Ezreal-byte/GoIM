#!/usr/bin/env bash
set -euo pipefail

recipe=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
base=95cca3f661f635b3cc85c04935fe8d96d8f432ef
commit=9c3e4d75f67e561bec93960a81473c4993842315
destination=${1:?RustDesk checkout destination is required}

git init "$destination"
git -C "$destination" -c http.version=HTTP/1.1 fetch --depth=1 --no-tags https://github.com/rustdesk/rustdesk.git "$base"
git -C "$destination" bundle verify "$recipe/rustdesk-goim.bundle"
git -C "$destination" fetch --no-tags "$recipe/rustdesk-goim.bundle" refs/heads/codex/goim-headless
git -C "$destination" checkout --detach FETCH_HEAD
test "$(git -C "$destination" rev-parse HEAD)" = "$commit"
git -C "$destination" submodule update --init --recursive --depth=1
