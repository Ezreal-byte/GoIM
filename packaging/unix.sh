#!/usr/bin/env bash
set -euo pipefail

version=$1
platform=$2
arch=$3
output=${4:-dist}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]]
[[ $arch == amd64 || $arch == arm64 ]]
output=$(cd "$output" && pwd)
name="GoIM_${version}_${platform}_${arch}"
payload="$output/$name"
work=$(mktemp -d "${TMPDIR:-/tmp}/goim-package.XXXXXX")
mounted=
cleanup() {
    if [[ -n $mounted ]]; then
        hdiutil detach "$mounted" >/dev/null || return
    fi
    rm -rf -- "$work"
}
trap cleanup EXIT

case "$platform" in
    linux)
        root="$work/deb"
        doc="$root/usr/share/doc/goim"
        install -d "$root/DEBIAN" "$root/usr/bin" "$root/usr/share" "$doc"
        install -m 755 "$payload/GoIM" "$payload/goim-snip" "$root/usr/bin/"
        cp -R "$payload/share/." "$root/usr/share/"
        cp -R "$payload/licenses" "$doc/"
        install -m 644 "$payload/LICENSE" "$doc/copyright"
        install -m 644 "$payload/README.md" "$payload/THIRD_PARTY_NOTICES.md" "$payload/build-info.json" "$doc/"
        # Debian uses a tilde so pre-release packages sort below the final version.
        deb_version=${version/-/~}
        cat > "$root/DEBIAN/control" <<EOF
Package: goim
Version: $deb_version
Section: net
Priority: optional
Architecture: $arch
Maintainer: GoIM maintainers <71426102+Ezreal-byte@users.noreply.github.com>
Installed-Size: $(du -sk "$root/usr" | cut -f1)
Depends: libx11-6, libx11-xcb1, libxcb1, libxkbcommon0, libxkbcommon-x11-0, libvulkan1
Recommends: mesa-vulkan-drivers, fonts-noto-cjk, xclip
Homepage: https://ezreal-byte.github.io/GoIM/
Description: Native LAN messenger and file sharing
 Direct and group chat, peer-to-peer transfers and browser file sharing.
EOF
        asset="$name.deb"
        dpkg-deb --root-owner-group --build "$root" "$output/$asset"
        [[ $(dpkg-deb --field "$output/$asset" Architecture) == "$arch" ]]
        [[ $(dpkg-deb --field "$output/$asset" Version) == "$deb_version" ]]
        dpkg-deb --extract "$output/$asset" "$work/check"
        cmp "$payload/GoIM" "$work/check/usr/bin/GoIM"
        cmp "$payload/goim-snip" "$work/check/usr/bin/goim-snip"
        test -f "$work/check/usr/share/applications/dev.goim.desktop.desktop"
        ;;
    darwin)
        mkdir "$work/image" "$work/mount"
        ditto "$payload/GoIM.app" "$work/image/GoIM.app"
        cp "$payload/README.md" "$payload/LICENSE" "$payload/THIRD_PARTY_NOTICES.md" "$payload/build-info.json" "$work/image/"
        cp -R "$payload/licenses" "$work/image/"
        ln -s /Applications "$work/image/Applications"
        asset="$name.dmg"
        hdiutil create -ov -fs HFS+ -volname GoIM -format UDZO -srcfolder "$work/image" "$output/$asset"
        hdiutil verify "$output/$asset"
        hdiutil attach -readonly -nobrowse -mountpoint "$work/mount" "$output/$asset"
        mounted="$work/mount"
        codesign --verify --deep --strict "$mounted/GoIM.app"
        cmp "$payload/GoIM.app/Contents/MacOS/GoIM" "$mounted/GoIM.app/Contents/MacOS/GoIM"
        cmp "$payload/GoIM.app/Contents/MacOS/goim-snip" "$mounted/GoIM.app/Contents/MacOS/goim-snip"
        test "$(readlink "$mounted/Applications")" = /Applications
        hdiutil detach "$mounted"
        mounted=
        ;;
    *) printf 'Unsupported platform: %s\n' "$platform" >&2; exit 2 ;;
esac

(cd "$output" && shasum -a 256 "$asset" | tee "$asset.sha256")
