#!/bin/sh
set -eu

fail() { printf '%s\n' "$*" >&2; exit 1; }

case "${1:-}" in
    ipcmd)
        shift
        case "${1:-}" in
            send) [ "$#" = 2 ] || exit 1 ;;
            send-file)
                [ "$#" = 3 ] || exit 1
                file=$(realpath -e -- "$3")
                case "$file" in "$GOIM_IPMSG_WORK"/stock-file-*.bin) ;; *) exit 1 ;; esac
                [ -f "$3" ] || exit 1
                ;;
            recv|list|away|available|stop|start) [ "$#" = 1 ] || exit 1 ;;
            *) exit 1 ;;
        esac
        job=$(mktemp -d "$GOIM_IPMSG_WORK/command.XXXXXXXX")
        trap 'rm -f -- "$job/action" "$job/body" "$job/path" "$job/ready" "$job/out" "$job/status" "$job/status.tmp"; rmdir "$job/running" "$job" 2>/dev/null || :' EXIT
        printf '%s\n' "$1" > "$job/action"
        printf '%s' "${2:-}" > "$job/body"
        printf '%s' "${3:-}" > "$job/path"
        : > "$job/ready"
        tries=0
        until [ -f "$job/status" ]; do
            tries=$((tries+1))
            [ "$tries" -lt 400 ] || fail 'The private reference command timed out.'
            sleep 0.05
        done
        cat "$job/out"
        IFS= read -r status < "$job/status"
        exit "$status"
        ;;
    dispatch)
        # Disposable fixture only. The vendor's IPC helper must share its
        # resident process's network namespace as well as its Wine session.
        work=$2
        while :; do
            for job in "$work"/command.????????; do
                [ -f "$job/ready" ] && mkdir "$job/running" 2>/dev/null || continue
                (
                    rc=0
                    IFS= read -r action < "$job/action"
                    case "$action" in
                        send)
                            body=$(cat "$job/body")
                            timeout 12s wine "$GOIM_IPMSG_ORIGINAL/ipcmd.exe" /nostat send /noseal 10.78.0.1 "$body" > "$job/out" 2>&1 || rc=$?
                            ;;
                        send-file)
                            body=$(cat "$job/body")
                            file=$(winepath -w "$(cat "$job/path")")
                            timeout 12s wine "$GOIM_IPMSG_ORIGINAL/ipcmd.exe" /nostat send "/file=$file" /noseal 10.78.0.1 "$body" > "$job/out" 2>&1 || rc=$?
                            ;;
                        recv|list|stop|away|available)
                            case "$action" in
                                stop) set -- terminate ;;
                                away) set -- setabs 1 ;;
                                available) set -- setabs 0 ;;
                                *) set -- "$action" ;;
                            esac
                            timeout 12s wine "$GOIM_IPMSG_ORIGINAL/ipcmd.exe" /nostat "$@" > "$job/out" 2>&1 || rc=$?
                            ;;
                        start)
                            : > "$job/out"
                            if ss -lunH 'sport = :2425' | grep -q ':2425'; then
                                rc=125
                            else
                                wine "$GOIM_IPMSG_ORIGINAL/IPMsg.exe" </dev/null >> "$work/startup.log" 2>&1 &
                                for attempt in $(seq 1 60); do
                                    ss -lunH 'sport = :2425' | grep -q ':2425' && break
                                    sleep 0.1
                                done
                                if ! ss -lunH 'sport = :2425' | grep -q ':2425'; then
                                    tail -n 8 "$work/startup.log" > "$job/out"
                                    rc=125
                                fi
                            fi
                            ;;
                        *) rc=2; : > "$job/out" ;;
                    esac
                    printf '%s\n' "$rc" > "$job/status.tmp"
                    mv -- "$job/status.tmp" "$job/status"
                ) &
            done
            sleep 0.05
        done
        ;;
    reference)
        work=$2
        printf '%s\n' "$DISPLAY" > "$work/display"
        printf '%s\n' "$XAUTHORITY" > "$work/xauthority"
        timeout 40s wineboot -u
        wine reg add 'HKLM\System\CurrentControlSet\Control\ComputerName\ComputerName' /v ComputerName /t REG_SZ /d IPMSG-STOCK /f
        wine reg add 'HKLM\System\CurrentControlSet\Control\ComputerName\ActiveComputerName' /v ComputerName /t REG_SZ /d IPMSG-STOCK /f
        mkdir -p "$WINEPREFIX/drive_c/goim-qa/received"
        for key in 'HKCU\Software\HSTools\IPMsgEng' 'HKCU\Software\HSTools\IPMsg'; do
            wine reg add "$key" /v autoSaveDir /t REG_SZ /d 'C:\goim-qa\received' /f
            wine reg add "$key" /v autoSaveTout /t REG_DWORD /d 1 /f
            wine reg add "$key" /v autoSaveFlags2 /t REG_DWORD /d 1 /f
        done
        cd "$GOIM_IPMSG_ORIGINAL"
        wine IPMsg.exe &
        exec "$0" dispatch "$work"
        ;;
    isolated) shift ;;
    *) exec unshare --uts "$0" isolated "$@" ;;
esac

[ "$(id -u)" = 0 ] || fail 'Use root only in the dedicated WSL lab.'
[ "$(readlink /proc/self/ns/uts)" != "$(readlink /proc/1/ns/uts)" ] || fail 'Missing isolated hostname.'
[ -z "${WSL_INTEROP:-}" ] || fail 'Windows interop must be disabled.'
if grep -Eq ' - (9p|drvfs) [A-Za-z]:' /proc/self/mountinfo; then fail 'Windows drive mounts must be disabled.'; fi
[ "$#" = 1 ] || fail 'Specify the extracted original IP Messenger directory.'
original=$(realpath -- "$1")
[ -f "$original/IPMsg.exe" ] && [ -f "$original/ipcmd.exe" ] || fail 'Original IP Messenger and its command-line client are required.'
tools=$(CDPATH= cd -- "${0%/*}" && pwd)
[ -x "$tools/check" ] || fail 'The Linux GoIM checker is missing.'
hostname goim-ipmsg-lab
work=$(mktemp -d /tmp/goim-ipmsg.XXXXXXXX)
case "$work" in /tmp/goim-ipmsg.????????) ;; *) fail 'Unexpected test directory.' ;; esac
chown nwtlab "$work"
install -o nwtlab -m 0600 /dev/null "$work/startup.log"
tag=${work##*.}
reference="goim-ipmsg-ref-$tag"
client="goim-ipmsg-go-$tag"
ref_link="ir$tag"
go_link="ig$tag"
ref_created=
go_created=
launcher=
link_created=

as_lab() {
    ns=$1
    shift
    ip netns exec "$ns" runuser -u nwtlab -- env \
        LC_ALL=C.UTF-8 GOIM_IPMSG_SANDBOX=1 GOIM_IPMSG_ORIGINAL="$original" GOIM_IPMSG_WORK="$work" \
        WINEPREFIX="$work/wine" WINEARCH=win32 WINEDEBUG=-all "$@"
}

cleanup() {
    if [ -n "$launcher" ]; then
        timeout 5s ip netns exec "$reference" runuser -u nwtlab -- \
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
ip -n "$client" link set lo up
ip -n "$reference" addr add 10.78.0.2/30 brd + dev "$ref_link"
ip -n "$client" addr add 10.78.0.1/30 brd + dev "$go_link"
ip -n "$reference" link set "$ref_link" up
ip -n "$client" link set "$go_link" up
# The original announces absence and exit via limited broadcast. Both
# namespaces have only the private veth pair and no default route.
ip -n "$reference" route add 255.255.255.255/32 dev "$ref_link"
ip -n "$client" route add 255.255.255.255/32 dev "$go_link"
as_lab "$reference" xvfb-run -a -s '-screen 0 1024x768x24 -dpi 96 -nolisten tcp' \
    "$tools/run.sh" reference "$work" > "$work/startup.log" 2>&1 &
launcher=$!
tries=0
until ip netns exec "$reference" ss -lunH 'sport = :2425' | grep -q ':2425'; do
    tries=$((tries+1))
    if [ "$tries" -ge 200 ] || ! kill -0 "$launcher" 2>/dev/null; then
        tail -n 12 "$work/startup.log" >&2
        fail 'The original IP Messenger did not start.'
    fi
    sleep 0.2
done
sleep 1
IFS= read -r display < "$work/display"
IFS= read -r authority < "$work/xauthority"
as_lab "$client" env DISPLAY="unix$display" XAUTHORITY="$authority" \
    "$tools/check" -lab-dir "$work" -driver "$tools/run.sh"
