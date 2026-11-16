#!/bin/sh
set -eu

lab_user=nwtlab
lab_path=/usr/sbin:/usr/bin:/sbin:/bin

fail() { printf '%s\n' "$*" >&2; exit 1; }

case "${1:-}" in
    reply|send-file|accept-file)
        # Launched by the checker as the lab user. Wine and X11 connect to
        # the same private session over local sockets, not over the veth.
        original=$(winepath -w "$GOIM_NWT_ORIGINAL/ShiYeLine.exe")
        exec wine "${0%/*}/drive.exe" "$original" "$@"
        ;;
    reference)
        printf '%s\n' "$DISPLAY" > "$2/display"
        printf '%s\n' "$XAUTHORITY" > "$2/xauthority"
        openbox >/dev/null 2>&1 &
        wm=$!
        trap 'kill "$wm" 2>/dev/null || :' EXIT
        timeout 40s wineboot -u
        cd "$GOIM_NWT_ORIGINAL"
        wine ShiYeLine.exe
        exit
        ;;
    isolated) shift ;;
    *) exec unshare --uts "$0" isolated "$@" ;;
esac

[ "$#" -eq 1 ] || fail 'Specify the Linux path to the original client directory.'
[ "$(id -u)" -eq 0 ] || fail 'Use root in the dedicated lab distro to create its isolated links.'
[ "$(readlink /proc/self/ns/uts)" != "$(readlink /proc/1/ns/uts)" ] || fail 'Missing isolated hostname namespace.'
[ -z "${WSL_INTEROP:-}" ] || fail 'Disable Windows interop in the dedicated lab distro.'
if grep -Eq ' - (9p|drvfs) [A-Za-z]:' /proc/self/mountinfo; then
    fail 'Disable Windows drive mounts in the dedicated lab distro.'
fi
lab_home=$(getent passwd "$lab_user" | cut -d: -f6)
[ -n "$lab_home" ] || fail 'The dedicated lab user nwtlab is missing.'
original=$(realpath "$1")
[ -f "$original/ShiYeLine.exe" ] || fail 'Missing original ShiYeLine.exe.'
hostname goim-nwt

tools=$(realpath "${0%/*}")
work=$(mktemp -d /tmp/goim-nwt.XXXXXXXX)
chown "$lab_user" "$work"
tag=${work##*.}
reference="goim-nwt-ref-$tag"
client="goim-nwt-go-$tag"
ref_link="nr$tag"
go_link="ng$tag"
ref_created=
go_created=
link_created=
launcher=

as_lab() {
    ns=$1
    shift
    ip netns exec "$ns" runuser -u "$lab_user" -- env -i \
        PATH="$lab_path" HOME="$lab_home" USER="$lab_user" LC_ALL=C.UTF-8 \
        GOIM_NWT_ORIGINAL="$original" WINEPREFIX="$work/wine" WINEARCH=win32 WINEDEBUG=-all "$@"
}

cleanup() {
    if [ -n "$launcher" ]; then
        timeout 5s ip netns exec "$reference" runuser -u "$lab_user" -- \
            env WINEPREFIX="$work/wine" wineserver -k 2>/dev/null || :
        kill "$launcher" 2>/dev/null || :
        wait "$launcher" 2>/dev/null || :
    fi
    if [ -n "$ref_created" ]; then
        for pid in $(ip netns pids "$reference"); do kill "$pid" 2>/dev/null || :; done
        ip netns del "$reference" || :
    fi
    if [ -n "$go_created" ]; then
        for pid in $(ip netns pids "$client"); do kill "$pid" 2>/dev/null || :; done
        ip netns del "$client" || :
    fi
    if [ -n "$link_created" ]; then ip link del "$ref_link" 2>/dev/null || :; fi
    rm -rf -- "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

ip netns add "$reference"
ref_created=1
ip netns add "$client"
go_created=1
ip link add "$ref_link" type veth peer name "$go_link"
link_created=1
ip link set "$ref_link" netns "$reference"
ip link set "$go_link" netns "$client"
link_created=
ip -n "$reference" link set lo up
ip -n "$reference" addr add 10.77.0.2/30 dev "$ref_link"
ip -n "$reference" link set "$ref_link" up
ip -n "$client" link set lo up
ip -n "$client" addr add 10.77.0.1/30 dev "$go_link"
ip -n "$client" link set "$go_link" up

as_lab "$reference" xvfb-run -a -s '-screen 0 1280x800x24 -dpi 96 -nolisten tcp' \
    "$tools/run.sh" reference "$work" > "$work/startup.log" 2>&1 &
launcher=$!

tries=0
until ip netns exec "$reference" ss -lunH 'sport = :9011' | grep -q ':9011'; do
    tries=$((tries+1))
    if [ "$tries" -ge 160 ] || ! kill -0 "$launcher" 2>/dev/null; then
        tail -n 10 "$work/startup.log" >&2
        fail 'The original test client did not start its discovery listener.'
    fi
    sleep 0.25
done
# Let the first-run profile and notification loop finish starting after bind.
sleep 3
IFS= read -r display < "$work/display"
IFS= read -r authority < "$work/xauthority"
as_lab "$client" env DISPLAY="unix$display" XAUTHORITY="$authority" \
    "$tools/check" -bind 10.77.0.1 -target 10.77.0.2:9011 -peer GOIM-NWT \
    -text-receipt -lab-driver "$tools/run.sh" -lab-dir "$work" -timeout 20s
