#!/usr/bin/env bash
set -euo pipefail

# Build against Focal so existing Kylin and UOS hosts can load the helper.
target=${1:?Rust target is required}
arch=${2:?GoIM architecture is required}
source_dir=$PWD/rustdesk
output=$PWD/goim-rd-bundle
deps=$(mktemp -d)
prefix=$deps/prefix
jobs=$(nproc)
export DEBIAN_FRONTEND=noninteractive

apt-get -o Acquire::Retries=3 update
apt-get -o Acquire::Retries=3 install -y --no-install-recommends \
  build-essential clang libclang-dev libpulse-dev libpam0g-dev libdbus-1-dev \
  libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev libayatana-appindicator3-dev \
  libxkbcommon-dev libxkbcommon-x11-dev libxcb-randr0-dev libxcb-shm0-dev \
  libxcb-xfixes0-dev libxcb-shape0-dev libgtk-3-dev libx11-dev libxrandr-dev \
  libxtst-dev libxdo-dev libssl-dev cmake ninja-build nasm pkg-config ca-certificates \
  curl git python3

curl --fail --retry 3 --silent --show-error https://sh.rustup.rs \
  | sh -s -- -y --profile minimal --default-toolchain "$RUST_VERSION"
export PATH="/root/.cargo/bin:$PATH"
rustup target add "$target"

fetch_archive() {
  local name=$1 url=$2 sha=$3 strip=$4
  curl --fail --location --retry 3 --silent --show-error "$url" -o "$deps/$name.tgz"
  printf '%s  %s\n' "$sha" "$deps/$name.tgz" | sha256sum -c
  mkdir "$deps/$name"
  tar -xzf "$deps/$name.tgz" -C "$deps/$name" --strip-components="$strip"
}

fetch_archive vpx https://codeload.github.com/webmproject/libvpx/tar.gz/refs/tags/v1.15.2 \
  26fcd3db88045dee380e581862a6ef106f49b74b6396ee95c2993a260b4636aa 1

fetch_archive opus https://codeload.github.com/xiph/opus/tar.gz/refs/tags/v1.5.2 \
  9480e329e989f70d69886ded470c7f8cfe6c0667cc4196d4837ac9e668fb7404 1
# Gitiles may regenerate archive bytes for the same source tree.
fetch_revision() {
  local name=$1 url=$2 revision=$3
  git init -q "$deps/$name"
  git -C "$deps/$name" -c http.version=HTTP/1.1 fetch --depth=1 --no-tags "$url" "$revision"
  git -C "$deps/$name" checkout --detach "$revision"
}
fetch_revision aom https://aomedia.googlesource.com/aom 03087864cf4bea6abb0d28f95cf7843511413d8f
fetch_revision yuv https://chromium.googlesource.com/libyuv/libyuv 0faf8dd0e004520a61a603a4d2996d5ecc80dc3f

(
  cd "$deps/vpx"
  ./configure --prefix="$prefix" --enable-static --disable-shared --enable-pic \
    --disable-examples --disable-tools --disable-docs --disable-unit-tests
  make -j "$jobs"
  make install
)
defaults=(-G Ninja -DCMAKE_BUILD_TYPE=Release "-DCMAKE_INSTALL_PREFIX=$prefix"
  -DCMAKE_INSTALL_LIBDIR=lib -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DBUILD_SHARED_LIBS=OFF)
cmake -S "$deps/aom" -B "$deps/aom-build" "${defaults[@]}" \
  -DENABLE_DOCS=OFF -DENABLE_EXAMPLES=OFF -DENABLE_TESTS=OFF -DENABLE_TOOLS=OFF
cmake --build "$deps/aom-build" -j "$jobs"
cmake --install "$deps/aom-build"
cmake -S "$deps/opus" -B "$deps/opus-build" "${defaults[@]}"
cmake --build "$deps/opus-build" -j "$jobs"
cmake --install "$deps/opus-build"
cmake -S "$deps/yuv" -B "$deps/yuv-build" "${defaults[@]}"
cmake --build "$deps/yuv-build" --target yuv -j "$jobs"
cp "$deps/yuv-build/libyuv.a" "$prefix/lib/"
cp -a "$deps/yuv/include/." "$prefix/include/"
cat > "$prefix/lib/pkgconfig/libyuv.pc" <<EOF
Name: libyuv
Description: YUV conversion and scaling
Version: 0.0
Cflags: -I$prefix/include
Libs: -L$prefix/lib -lyuv -lstdc++
EOF

export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"
export VPX_STATIC=1 AOM_STATIC=1 OPUS_STATIC=1 LIBYUV_STATIC=1
git config --global --add safe.directory "$source_dir"
git config --global --add safe.directory "$source_dir/libs/hbb_common"
cd "$source_dir"
cargo build --locked --release --target "$target" --bin goim-rd --features goim-remote,linux-pkg-config
helper="target/$target/release/goim-rd"
ldd "$helper" | tee "$deps/ldd.txt"
if grep -q 'not found' "$deps/ldd.txt"; then exit 1; fi
max_glibc=$(readelf --version-info "$helper" | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)*' \
  | sed 's/GLIBC_//' | sort -Vu | tail -1)
test -n "$max_glibc"
test "$(printf '%s\n' 2.31 "$max_glibc" | sort -V | tail -1)" = 2.31
printf 'Highest required GLIBC version is %s\n' "$max_glibc"
python3 res/goim-stage-helper.py --binary "$helper" --platform linux --architecture "$arch" --output "$output"
for name in vpx aom opus yuv; do
  printf '\n--- %s ---\n' "$name" >> "$output/NOTICE"
  for license in "$deps/$name"/{LICENSE,LICENSE.txt,COPYING,PATENTS}; do
    if [[ -f "$license" ]]; then cat "$license" >> "$output/NOTICE"; fi
  done
done
