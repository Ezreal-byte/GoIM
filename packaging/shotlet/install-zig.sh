#!/usr/bin/env bash
set -euo pipefail
# The helper keeps libc++ static and targets glibc 2.28, independently of the
# native build host. Use the same verified toolchain on both Linux architectures.
case "$(uname -m)" in
    x86_64) arch=x86_64; digest=02aa270f183da276e5b5920b1dac44a63f1a49e55050ebde3aecc9eb82f93239 ;;
    aarch64) arch=aarch64; digest=958ed7d1e00d0ea76590d27666efbf7a932281b3d7ba0c6b01b0ff26498f667f ;;
    *) echo 'Shotlet requires an amd64 or arm64 Linux builder' >&2; exit 1 ;;
esac
destination=${1:?Pass a toolchain cache directory}
mkdir -p "$destination"
destination=$(cd "$destination" && pwd)
name="zig-${arch}-linux-0.15.2"
curl --fail --location --retry 3 "https://ziglang.org/download/0.15.2/${name}.tar.xz" -o "$destination/${name}.tar.xz"
echo "$digest  $destination/${name}.tar.xz" | sha256sum --check -
tar -xf "$destination/${name}.tar.xz" -C "$destination"
if [[ -n ${GITHUB_PATH:-} ]]; then
    echo "$destination/$name" >> "$GITHUB_PATH"
fi
printf '%s\n' "$destination/$name"
