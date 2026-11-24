#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

version=$1
platform=$2
arch=$3
output=${4:-dist}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]]
[[ $arch == amd64 || $arch == arm64 ]]
output=$(cd "$output" && pwd)
name="GoIM_${version}_${platform}_${arch}"
payload="$output/$name"
remote_preview=false
if [[ $version == *-rd-preview.* ]]; then
    remote_preview=true
    test -s "$payload/licenses/rustdesk/LICENSE"
    test -s "$payload/licenses/rustdesk/manifest.json"
fi
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
        install -m 755 "$script_dir/linux/preinst" "$root/DEBIAN/preinst"
        install -m 755 "$payload/GoIM" "$payload/goim-snip" "$payload/goim-media" "$root/usr/bin/"
        test -x "$payload/lib/goim/file-dialog/bin/zenity"
        [[ $(cat "$payload/lib/goim/file-dialog/architecture") == "$arch" ]]
        cp -R "$payload/lib" "$root/usr/"
        if $remote_preview; then
            test -x "$payload/lib/goim/remote/goim-rd"
        fi
        update_key=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["update_public_key"])' "$payload/build-info.json")
        if $remote_preview; then
            [[ -z $update_key ]]
        fi
        (cd "$script_dir/.." && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
            -ldflags "-s -w -X goim/internal/buildinfo.UpdatePublicKey=$update_key" \
            -o "$root/usr/lib/goim/deb-update" ./cmd/goim-deb-update)
        cp -R "$payload/share/." "$root/usr/share/"
        cp -R "$payload/licenses" "$doc/"
        install -m 644 "$payload/LICENSE" "$doc/copyright"
        install -m 644 "$payload/README.md" "$payload/THIRD_PARTY_NOTICES.md" "$payload/build-info.json" "$doc/"
        # Debian uses a tilde so pre-release packages sort below the final version.
        deb_version=$version
        if [[ $version == *-* ]]; then
            deb_version="${version%%-*}~${version#*-}"
        fi
        cat > "$root/DEBIAN/control" <<EOF
Package: goim
Version: $deb_version
Section: net
Priority: optional
Architecture: $arch
Maintainer: GoIM maintainers <71426102+Ezreal-byte@users.noreply.github.com>
Installed-Size: $(du -sk "$root/usr" | cut -f1)
Depends: libc6, libx11-6, libx11-xcb1, libxcb1, libxkbcommon0, libxkbcommon-x11-0, libvulkan1, libasound2
Recommends: mesa-vulkan-drivers, fonts-noto-cjk
Homepage: https://ezreal-byte.github.io/GoIM/
Description: Native LAN messenger and file sharing
 Direct and group chat, peer-to-peer transfers and browser file sharing.
EOF
        asset="$name.deb"
        dpkg-deb --root-owner-group --build "$root" "$output/$asset"
        [[ $(dpkg-deb --field "$output/$asset" Architecture) == "$arch" ]]
        [[ $(dpkg-deb --field "$output/$asset" Version) == "$deb_version" ]]
        [[ $(dpkg-deb --field "$output/$asset" Depends) != *zenity* ]]
        dpkg-deb --extract "$output/$asset" "$work/check"
        cmp "$payload/GoIM" "$work/check/usr/bin/GoIM"
        cmp "$payload/goim-media" "$work/check/usr/bin/goim-media"
        cmp "$payload/goim-snip" "$work/check/usr/bin/goim-snip"
        if $remote_preview; then
            cmp "$payload/lib/goim/remote/goim-rd" "$work/check/usr/lib/goim/remote/goim-rd"
            test -x "$work/check/usr/lib/goim/remote/goim-rd"
            diff -r "$payload/licenses/rustdesk" "$work/check/usr/share/doc/goim/licenses/rustdesk"
        fi
        test -x "$work/check/usr/lib/goim/deb-update"
        diff -r "$payload/lib/goim/file-dialog" "$work/check/usr/lib/goim/file-dialog"
        test -x "$work/check/usr/lib/goim/file-dialog/bin/zenity"
        test -f "$work/check/usr/share/applications/dev.goim.desktop.desktop"
        test -f "$work/check/usr/share/polkit-1/actions/dev.goim.desktop.policy"
        ;;
    darwin)
        # Check the archive the online updater actually downloads, including
        # the signatures of the nested helpers after ZIP extraction.
        mkdir "$work/online"
        unzip -q "$output/$name.zip" -d "$work/online"
        online="$work/online/GoIM.app"
        codesign --verify --deep --strict "$online"
        [[ $(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$online/Contents/Info.plist") == dev.goim.desktop ]]
        [[ $(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$online/Contents/Info.plist") == "${version%%-*}" ]]
        if $remote_preview; then
            helper=Contents/Helpers/GoIMRemote.app
            codesign --verify --strict "$online/$helper"
            [[ $(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$online/$helper/Contents/Info.plist") == dev.goim.desktop.remote-desktop ]]
            test -x "$online/$helper/Contents/MacOS/goim-rd"
        fi
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
        shotlet=GoIM.app/Contents/Helpers/ShotletCapture.app/Contents/MacOS/ShotletCapture
        cmp "$payload/$shotlet" "$mounted/$shotlet"
        test -x "$mounted/$shotlet"
        if $remote_preview; then
            remote=GoIM.app/$helper/Contents/MacOS/goim-rd
            cmp "$payload/$remote" "$mounted/$remote"
            test -x "$mounted/$remote"
            diff -r "$payload/licenses/rustdesk" "$mounted/licenses/rustdesk"
        fi
        test "$(readlink "$mounted/Applications")" = /Applications
        hdiutil detach "$mounted"
        mounted=
        ;;
    *) printf 'Unsupported platform: %s\n' "$platform" >&2; exit 2 ;;
esac

(cd "$output" && shasum -a 256 "$asset" | tee "$asset.sha256")
