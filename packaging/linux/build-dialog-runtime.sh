#!/usr/bin/env bash
# Build on the target architecture. Nothing is downloaded at install/run time.
set -euo pipefail
dest=$(mkdir -p "$1" && cd "$1" && pwd)
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
version=3.44.5
sha256=29ac447f4246b1da783a3c28ddc80b0ef7c80eccd5836ad9140abf89841d0eab
for tool in xclip curl meson ninja pkg-config ldd dpkg-query readelf glib-compile-schemas update-mime-database gtk-update-icon-cache; do
    command -v "$tool" >/dev/null || { echo "Missing build tool: $tool (see packaging/linux/README.md)" >&2; exit 1; }
done
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
archive="$work/zenity.tar.xz"
if [[ -n ${GOIM_ZENITY_SOURCE:-} ]]; then
    cp "$GOIM_ZENITY_SOURCE" "$archive"
else
    curl --fail --location --retry 3 "https://download.gnome.org/sources/zenity/3.44/zenity-$version.tar.xz" -o "$archive"
fi
printf '%s  %s\n' "$sha256" "$archive" | sha256sum --check
tar -xJf "$archive" -C "$work"
# Keep the gettext catalog relocatable in both tar and /usr/lib installations.
sed -i 's/bindtextdomain (GETTEXT_PACKAGE, LOCALEDIR);/bindtextdomain (GETTEXT_PACKAGE, g_getenv ("GOIM_DIALOG_LOCALEDIR"));/' "$work/zenity-$version/src/main.c"
meson setup "$work/build" "$work/zenity-$version" --prefix=/usr --buildtype=release -Dwebkitgtk=false -Dlibnotify=false
ninja -C "$work/build"
DESTDIR="$work/install" ninja -C "$work/build" install
mkdir -p "$dest/bin" "$dest/lib" "$dest/share" "$dest/gtk" "$dest/pixbuf" "$dest/gio" "$dest/licenses" "$dest/sources"
install -m755 "$work/install/usr/bin/zenity" "$dest/zenity"
install -m755 "$script_dir/zenity" "$dest/bin/zenity"
cp -R "$work/install/usr/share/locale" "$dest/share/"
cp "$work/zenity-$version/COPYING" "$dest/licenses/zenity.txt"
tar -cJf "$dest/sources/zenity-$version.tar.xz" -C "$work" "zenity-$version"
printf 'zenity\t%s\t%s\n' "$version" "$sha256" > "$dest/packages.tsv"

# Bundle the full ELF closure, including a matching loader and libc. The
# wrapper uses this loader for its child only; GoIM never changes LD_LIBRARY_PATH.
declare -A packages=()
declare -A copied=()
record_package() {
    local path=$1 owner
    # dpkg also prints diversion notices before the actual owner on merged-/usr
    # systems. Only accept package identifiers, never the localized notices.
    owner=$(package_owner "$path") || true
    if [[ -z $owner ]]; then
        owner=$(package_owner "$(readlink -f "$path")") || true
    fi
    [[ -n $owner ]] || { echo "Cannot identify runtime library owner: $path" >&2; exit 1; }
    packages["$owner"]=1
}
package_owner() {
    LC_ALL=C dpkg-query -S "$1" 2>/dev/null |
        awk -F ': /' '!found && $1 ~ /^[a-z0-9][a-z0-9+.-]*(:[a-z0-9-]+)?$/ { print $1; found=1 }'
}
copy_closure() {
    local binary=$1 path
    ldd "$binary" > "$work/ldd.txt"
    if grep -q 'not found' "$work/ldd.txt"; then cat "$work/ldd.txt" >&2; exit 1; fi
    while IFS= read -r path; do
        [[ -z ${copied[$path]:-} ]] || continue
        cp -L "$path" "$dest/lib/$(basename "$path")"
        record_package "$path"
        copied["$path"]=1
    done < <(awk '/=> \// {print $3} /^[[:space:]]*\// {print $1}' "$work/ldd.txt")
}
copy_closure "$dest/zenity"
xclip=$(command -v xclip)
install -m755 "$xclip" "$dest/xclip"
record_package "$xclip"
copy_closure "$dest/xclip"
loader=$(readelf -l "$dest/zenity" | sed -n 's/.*interpreter: \(.*\)]/\1/p')
test -n "$loader"
cp -L "$loader" "$dest/ld.so"
record_package "$loader"

# GTK input methods and pixbuf decoders use dlopen, so ldd alone misses them.
gtk_libdir=$(pkg-config --variable=libdir gtk+-3.0)
pixbuf_libdir=$(pkg-config --variable=gdk_pixbuf_moduledir gdk-pixbuf-2.0)
for entry in "$gtk_libdir"/gtk-3.0/3.0.0/immodules/*.so; do
    [[ -f $entry ]] || continue
    cp -L "$entry" "$dest/gtk/"
    record_package "$entry"
    copy_closure "$entry"
done
for entry in "$pixbuf_libdir"/*.so; do
    [[ -f $entry ]] || continue
    cp -L "$entry" "$dest/pixbuf/"
    record_package "$entry"
    copy_closure "$entry"
done
gtk_query=$(find "$gtk_libdir" -name gtk-query-immodules-3.0 -type f -print -quit)
for entry in "$gtk_query" "$(pkg-config --variable=gdk_pixbuf_query_loaders gdk-pixbuf-2.0)"; do
    test -x "$entry"
    cp -L "$entry" "$dest/"
    record_package "$entry"
    copy_closure "$entry"
done
# Copy only package-owned resources, not unrelated applications' schemas/icons
# that happen to be installed on the build machine. Regenerate the data caches.
for package in libgtk-3-common gsettings-desktop-schemas shared-mime-info adwaita-icon-theme hicolor-icon-theme; do
    packages["$package"]=1
    while IFS= read -r path; do
        case "$path" in
            /usr/share/glib-2.0/schemas/*.xml|/usr/share/glib-2.0/schemas/*.override|/usr/share/mime/packages/*.xml|/usr/share/icons/Adwaita/*|/usr/share/icons/hicolor/*)
                [[ -f $path ]] || continue
                relative=${path#/usr/}
                mkdir -p "$dest/$(dirname "$relative")"
                cp -L "$path" "$dest/$relative"
                ;;
        esac
    done < <(dpkg-query -L "$package")
done
glib-compile-schemas "$dest/share/glib-2.0/schemas"
update-mime-database "$dest/share/mime"
gtk-update-icon-cache -q -t -f "$dest/share/icons/Adwaita"
for package in "${!packages[@]}"; do
    dpkg-query -W -f='${binary:Package}\t${Version}\t${source:Package}\t${source:Version}\n' "$package" >> "$dest/packages.tsv"
    doc="/usr/share/doc/${package%%:*}/copyright"
    test -f "$doc"
    cp -L "$doc" "$dest/licenses/${package//:/_}.txt"
done
sort -o "$dest/packages.tsv" "$dest/packages.tsv"
printf '%s\n' "$(dpkg --print-architecture)" > "$dest/architecture"
cp "$script_dir/README.md" "$dest/README.md"
# Fail the package build if any runtime ELF still has an unresolved dependency.
for binary in "$dest/xclip" "$dest/zenity" "$dest/gtk/"*.so "$dest/pixbuf/"*.so; do
    "$dest/ld.so" --library-path "$dest/lib" --list "$binary" > "$work/check.txt"
    if grep -q 'not found' "$work/check.txt"; then cat "$work/check.txt" >&2; exit 1; fi
done
du -sk "$dest"
