#!/bin/sh
set -eu

lab_user=nwtlab
lab_path=/usr/sbin:/usr/bin:/sbin:/bin

fail() { printf '%s\n' "$*" >&2; exit 1; }

case "${1:-}" in
    reply|queue-text|send-file|accept-file|cancel-file|send-image|send-folder|probe-group|group-open|group-close|group-await-closed|group-reply|group-check|group-image|group-exit|group-exit-seen|group-kick-goim|chat-check|chat-recall-stock|chat-recall-check|chat-recall-gone|group-recall-stock|group-recall-check|group-recall-gone|stock-open-chat|stock-reply|stock-chat-check|stock-send-file|stock-accept-file|stock-presence|stock-avatar)
        # Launched by the checker as the lab user. Wine and X11 connect to
        # the same private session over local sockets, not over the veth.
        original=$(winepath -w "$GOIM_NWT_ORIGINAL/ShiYeLine.exe")
        exec wine "${0%/*}/drive.exe" "$original" "$@"
        ;;
    reference)
        printf '%s\n' "$DISPLAY" > "$2/display"
        # Each original can have a different test hostname. Keep the cookie
        # private, but let its driver present it from another namespace.
        display_auth="$2/display.auth"
        (umask 077; : > "$display_auth")
        xauth -f "$XAUTHORITY" nlist "$DISPLAY" | sed 's/^..../ffff/' | xauth -f "$display_auth" nmerge -
        [ -s "$display_auth" ] || fail 'Could not prepare the private display cookie.'
        printf '%s\n' "$display_auth" > "$2/xauthority"
        openbox >/dev/null 2>&1 &
        wm=$!
        trap 'kill "$wm" 2>/dev/null || :' EXIT
        timeout 40s wineboot -u
        desktop="$WINEPREFIX/drive_c/users/$USER/Desktop"
        if [ -L "$desktop" ]; then rm -f -- "$desktop"; fi
        mkdir -p -- "$desktop"
        cd "$GOIM_NWT_ORIGINAL"
        if [ "${3:-}" = --offline-send ]; then
            work=$2
            wine ShiYeLine.exe &
            app=$!
            while [ ! -f "$work/reference-stop.request" ]; do
                kill -0 "$app" 2>/dev/null || fail 'Original exited before its offline check.'
                sleep 0.1
            done
            timeout 5s wineserver -k
            wait "$app" || :
            [ -z "$(ss -lunH 'sport = :9011')" ] || fail 'Original discovery socket is still open.'
            : > "$work/reference-stopped"
            until [ -f "$work/reference-start.request" ]; do sleep 0.1; done
            wine ShiYeLine.exe &
            app=$!
            until ss -lunH 'sport = :9011' | grep -q ':9011'; do
                kill -0 "$app" 2>/dev/null || fail 'Original did not start again.'
                sleep 0.1
            done
            : > "$work/reference-started"
            wait "$app"
            exit
        fi
        wine ShiYeLine.exe
        exit
        ;;
    isolated) shift ;;
    *) exec unshare --uts "$0" isolated "$@" ;;
esac

[ "$#" -gt 0 ] && [ "$#" -le 2 ] || fail 'Specify the Linux path to the original client directory and at most one check mode.'
mode=${2:-}
case "$mode" in
    ''|--offline-replay|--offline-send|--resume-receive|--large-from-reference|--large-to-reference|--large-stock-control|--large-stock-boundary|--directory-from-reference|--directory-to-reference|--directory-resend-from-reference|--directory-resend-to-reference|--multi-group|--avatar-update|--text-recall|--group-recall|--group-dismiss|--group-kick) ;;
    *) fail 'Unknown sandbox option.' ;;
esac
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
peer_ns="$client"
ref_link="nr$tag"
go_link="ng$tag"
ref_created=
go_created=
link_created=
launcher=
peer_launcher=
peer_created=

as_lab() {
    ns=$1
    shift
    ip netns exec "$ns" runuser -u "$lab_user" -- env -i \
        PATH="$lab_path" HOME="$lab_home" USER="$lab_user" LC_ALL=C.UTF-8 \
        GOIM_NWT_ORIGINAL="$original" WINEPREFIX="$work/wine" WINEARCH=win32 WINEDEBUG=-all "$@"
}

cleanup() {
    if [ -n "$peer_launcher" ]; then
        timeout 5s ip netns exec "$peer_ns" runuser -u "$lab_user" -- \
            env WINEPREFIX="$work/peer/wine" wineserver -k 2>/dev/null || :
        kill "$peer_launcher" 2>/dev/null || :
        wait "$peer_launcher" 2>/dev/null || :
    fi
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
    if [ -n "$peer_created" ]; then
        for pid in $(ip netns pids "$peer_ns"); do kill "$pid" 2>/dev/null || :; done
        ip netns del "$peer_ns" || :
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
ip -n "$reference" link set "$ref_link" up
ip -n "$client" link set lo up
ip -n "$client" link set "$go_link" up
if [ "$mode" = --multi-group ]; then
    # A private Ethernet segment inside reference joins GoIM and both
    # originals. Nothing is connected to a host interface.
    peer_ns="goim-nwt-peer-$tag"
    ip netns add "$peer_ns"
    peer_created=1
    ip -n "$reference" link add nwtlan type bridge
    ip -n "$reference" link add nwtuplink type veth peer name nwtpeer netns "$peer_ns"
    ip -n "$reference" link set "$ref_link" master nwtlan
    ip -n "$reference" link set nwtuplink master nwtlan
    ip -n "$reference" link set nwtuplink up
    ip -n "$reference" addr add 10.77.0.2/29 brd + dev nwtlan
    ip -n "$reference" link set nwtlan up
    ip -n "$client" addr add 10.77.0.1/29 brd + dev "$go_link"
    ip -n "$peer_ns" link set lo up
    ip -n "$peer_ns" addr add 10.77.0.3/29 brd + dev nwtpeer
    ip -n "$peer_ns" link set nwtpeer up
else
    ip -n "$reference" addr add 10.77.0.2/30 brd + dev "$ref_link"
    ip -n "$client" addr add 10.77.0.1/30 brd + dev "$go_link"
fi

as_lab "$reference" xvfb-run -a -s '-screen 0 1280x800x24 -dpi 96 -nolisten tcp' \
    "$tools/run.sh" reference "$work" "$mode" > "$work/startup.log" 2>&1 &
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
case "$mode" in
    --large-stock-*)
        . "$tools/stock-pair.sh"
        stock_pair "${mode#--large-stock-}"
        exit
        ;;
    --multi-group)
        . "$tools/stock-pair.sh"
        start_stock_peer
        ;;
esac
set -- -bind 10.77.0.1 -target 10.77.0.2:9011 -peer GOIM-NWT \
    -text-receipt -lab-driver "$tools/run.sh" -lab-dir "$work"
case "$mode" in
--large-*)
    set -- "$@" -timeout 20s -lab-large-file -lab-direction "${mode#--large-}"
    ;;
--offline-replay)
    set -- "$@" -timeout 16s -lab-offline-replay
    ;;
--offline-send)
    set -- "$@" -timeout 3m -lab-offline-send
    ;;
--resume-receive)
    set -- "$@" -timeout 40s -lab-resume-receive
    ;;
--directory-resend-*)
    set -- "$@" -timeout 20s -lab-directory-resend -lab-direction "${mode#--directory-resend-}"
    ;;
--directory-*)
    set -- "$@" -timeout 20s -lab-directory-retry -lab-direction "${mode#--directory-}"
    ;;
--multi-group)
    set -- "$@" -timeout 60s -lab-multi-group
    ;;
--avatar-update)
    set -- "$@" -timeout 20s -lab-avatar-update
    ;;
--text-recall|--group-recall|--group-dismiss|--group-kick)
    set -- "$@" -timeout 60s "-lab-${mode#--}"
    ;;
*)
    set -- "$@" -timeout 20s
    ;;
esac
as_lab "$client" env DISPLAY="unix$display" XAUTHORITY="$authority" "$tools/check" "$@" || {
    tail -n 18 "$work/startup.log" >&2
    exit 1
}
