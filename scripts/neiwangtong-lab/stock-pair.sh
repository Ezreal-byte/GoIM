# Sourced after run.sh has started the first original in a private namespace.
# A second fresh original lets us separate compatibility failures from failures
# that reproduce without GoIM under the same reference runtime.

stock_ref() {
    as_lab "$reference" env DISPLAY="unix$display" XAUTHORITY="$authority" "$tools/run.sh" "$@"
}

stock_peer() {
    as_lab "$peer_ns" env DISPLAY="unix$peer_display" XAUTHORITY="$peer_authority" \
        WINEPREFIX="$peer_work/wine" HOME="$peer_work/home" "$tools/run.sh" "$@"
}

stock_transfer() {
    stock_name=$1
    stock_size=$2
    stall_timeout=$3
    stock_source="$work/wine/drive_c/goim-qa/$stock_name"
    as_lab "$reference" truncate -s "$stock_size" "$stock_source" || return 1
    printf 'GoIM large-file test\n' | as_lab "$reference" dd of="$stock_source" \
        bs=1 seek="$((stock_size - 32))" conv=notrunc status=none || return 1
    stock_ref stock-send-file GOIM-NWT-2 "C:\\goim-qa\\$stock_name" >&2 || return 1
    stock_peer stock-accept-file GOIM-NWT >&2 || return 1

    stock_last_change=$(date +%s)
    stock_deadline=$((stock_last_change + 240))
    while :; do
        stock_dest=$(find "$peer_work/wine/drive_c" -xdev -type f -name "$stock_name" -print -quit)
        stock_now=$(date +%s)
        if [ -n "$stock_dest" ]; then
            stock_output_created=true
            current_bytes=$(stat -c %s -- "$stock_dest" 2>/dev/null) || current_bytes=0
            if [ "$current_bytes" -ne "$stock_bytes" ]; then
                stock_last_change=$stock_now
                stock_bytes=$current_bytes
            fi
            if [ "$stock_bytes" -eq "$stock_size" ]; then
                stock_hash=$(sha256sum -- "$stock_source") || return 1
                dest_hash=$(sha256sum -- "$stock_dest") || return 1
                stock_hash=${stock_hash%% *}
                [ "$stock_hash" = "${dest_hash%% *}" ] && return 0
                return 1
            fi
        fi
        [ "$stock_now" -lt "$stock_deadline" ] && [ "$((stock_now - stock_last_change))" -lt "$stall_timeout" ] || return 1
        sleep 0.5
    done
}

start_stock_peer() {
    peer_work="$work/peer"
    install -d -o "$lab_user" -- "$peer_work" "$peer_work/home"
    ip netns exec "$peer_ns" unshare --uts sh -c 'hostname goim-nwt-2; exec "$@"' sh \
        runuser -u "$lab_user" -- env -i PATH="$lab_path" LC_ALL=C.UTF-8 USER="$lab_user" \
        HOME="$peer_work/home" WINEPREFIX="$peer_work/wine" WINEARCH=win32 WINEDEBUG=-all GOIM_NWT_ORIGINAL="$original" \
        xvfb-run -a -s '-screen 0 1280x800x24 -dpi 96 -nolisten tcp' \
        "$tools/run.sh" reference "$peer_work" > "$work/peer-startup.log" 2>&1 &
    peer_launcher=$!
    tries=0
    until ip netns exec "$peer_ns" ss -lunH 'sport = :9011' | grep -q ':9011'; do
        tries=$((tries + 1))
        if [ "$tries" -ge 160 ] || ! kill -0 "$peer_launcher" 2>/dev/null; then
            tail -n 12 "$work/peer-startup.log" >&2
            fail 'The second original client did not start its discovery listener.'
        fi
        sleep 0.25
    done
    sleep 8
    IFS= read -r peer_display < "$peer_work/display"
    IFS= read -r peer_authority < "$peer_work/xauthority"
}

stock_pair() {
    case "$1" in
        control) stage=small_file; filename=GoIMQA-control.bin; size=98321; stall=10 ;;
        boundary) stage=4gib_file; filename=GoIMQA-boundary.bin; size=4295032849; stall=45 ;;
        *) fail 'Unknown original-pair file fixture.' ;;
    esac
    start_stock_peer
    if ! stock_ref stock-open-chat GOIM-NWT-2 >&2; then
        # Stock does not always populate the first client's search list
        # first. Accept an incoming test message from the second instead.
        if ! stock_peer stock-open-chat GOIM-NWT >&2 ||
            ! stock_peer stock-reply GOIM-NWT 'GoIM QA second original online' >&2; then
            printf '{"mode":"stock-pair","passed":false,"checks":[{"name":"original_pair_chat","status":"fail","detail":"the originals did not open a test conversation"}]}\n'
            return 1
        fi
    fi
    marker='离线补投 GoIM QA stock-to-stock control'
    if ! stock_ref stock-reply GOIM-NWT-2 "$marker" >&2 || ! stock_peer stock-chat-check GOIM-NWT "$marker" >&2; then
        printf '{"mode":"stock-pair","passed":false,"checks":[{"name":"original_pair_chat","status":"fail","detail":"the second original did not show the synthetic control message"}]}\n'
        return 1
    fi
    as_lab "$reference" mkdir -- "$work/wine/drive_c/goim-qa"
    stock_bytes=0
    stock_hash=
    stock_output_created=false
    if stock_transfer "$filename" "$size" "$stall"; then
        passed=true
        status=pass
        detail="$size bytes, SHA-256 $stock_hash"
    else
        passed=false
        status=fail
        if [ "$stock_output_created" = true ]; then
            detail="$stock_bytes of $size bytes observed in the original receiver's output file"
        else
            detail="the original receiver created no output for the $size byte file"
        fi
        tail -n 10 "$work/peer-startup.log" >&2
    fi
    printf '{"mode":"stock-pair","reference_version":"3.4.3055","passed":%s,"checks":[{"name":"original_pair_%s","status":"%s","detail":"%s"}]}\n' \
        "$passed" "$stage" "$status" "$detail"
    [ "$passed" = true ]
}
