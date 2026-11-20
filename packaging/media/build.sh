#!/usr/bin/env bash
# Build the offline decoder from LGPL-only FFmpeg sources. Runs on native
# Linux/macOS hosts, or cross-compiles Windows on a Linux x86_64 host.
set -euo pipefail
platform=$1
arch=$2
mkdir -p "$3"
out=$(cd "$3" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
hash() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }
fetch() {
    if [[ ! -f $2 ]] || [[ $(hash "$2") != "$3" ]]; then
        curl --fail --location --connect-timeout 12 --retry 3 "$1" -o "$2"
    fi
    [[ $(hash "$2") == "$3" ]]
}
source_name=ffmpeg-8.1.3.tar.xz
fetch "https://ffmpeg.org/releases/$source_name" "$out/$source_name" 7138d28c96d9d3e3af4ee3d8cad72741f8ffb40da90c1112235dea3ecd3178a3
tar -xJf "$out/$source_name" -C "$work"
cd "$work/ffmpeg-8.1.3"
options=()
suffix=
case "$platform" in
    windows)
        llvm=llvm-mingw-20261117-ucrt-ubuntu-22.04-x86_64
        if [[ -n ${GOIM_LLVM_MINGW_ARCHIVE:-} ]]; then cp "$GOIM_LLVM_MINGW_ARCHIVE" "$work/llvm.tar.xz"; fi
        fetch "https://github.com/mstorsjo/llvm-mingw/releases/download/20261117/$llvm.tar.xz" "$work/llvm.tar.xz" e7a15a49a2ea95e9f3d8a9d0cfabe5052a4b10fb4777cc54de5a8c27b64bb701
        tar -xJf "$work/llvm.tar.xz" -C "$work"
        export PATH="$work/$llvm/bin:$PATH"
        cpu=x86_64; [[ $arch != arm64 ]] || cpu=aarch64
        options+=(--enable-cross-compile --target-os=mingw32 "--arch=$cpu" "--cross-prefix=$cpu-w64-mingw32-" "--cc=$cpu-w64-mingw32-clang" --extra-ldflags=-static)
        suffix=.exe ;;
    linux) options+=(--cc=musl-gcc --extra-ldflags=-static) ;;
    darwin) options+=(--cc=clang); export MACOSX_DEPLOYMENT_TARGET=13.0 ;;
    *) exit 2 ;;
esac
./configure --disable-autodetect --disable-doc --disable-debug --disable-network \
    --disable-everything --disable-ffplay --disable-ffprobe --disable-x86asm \
    --enable-ffmpeg --enable-protocol=file,pipe \
    --enable-demuxer=mov,matroska,avi,mpegps,mpegts,flv,asf,ogg \
    --enable-decoder=h264,hevc,mpeg4,mpeg1video,mpeg2video,mjpeg,vp8,vp9,theora,flv,wmv2,wmv3,vc1,aac,mp3,opus,vorbis,ac3,pcm_s16le,pcm_s24le,flac \
    --enable-parser=h264,hevc,mpeg4video,mpegvideo,mpegaudio,mjpeg,vp8,vp9,aac,opus,ac3 \
    --enable-encoder=ppm,pcm_s16le --enable-muxer=image2pipe,pcm_s16le \
    --enable-filter=scale,fps,format,aresample,aformat,trim,atrim,null,anull \
    "${options[@]}"
make -j"${GOIM_MEDIA_JOBS:-4}" "ffmpeg$suffix"
cp "ffmpeg$suffix" "$out/goim-media$suffix"
cp COPYING.LGPLv2.1 "$out/FFmpeg-LGPL-2.1.txt"
