package neiwangtong

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"image"
	"image/png"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGroupPhotoInfoFromOriginal(t *testing.T) {
	const owner = "b13446caf1184883a857be4d414e8177"
	const room = "1c1b62d2b8fe4e5fbc831994b87e88cc"
	const digest = "2812923b6855dbecbfbae183e7d16b37"
	const field = "<QGROUP_PHOTO_MD5>" + digest + "</QGROUP_PHOTO_MD5>"
	const info = "<X_QGROUP_PUSH_INFO><INFO><QGROUP_ID>" + room + "</QGROUP_ID><QGROUP_MASTER>" + owner +
		"</QGROUP_MASTER><QGROUP_INFO_VER>3</GROUP_INFO_VER>" + field + "</INFO></X_QGROUP_PUSH_INFO>"
	for _, tc := range []struct {
		data, sender, want string
		valid              bool
	}{
		{info, owner, digest, true},
		{strings.Replace(info, digest, "", 1), owner, "", true},
		{strings.Replace(info, field, "", 1), owner, "", false},
		{info, room, "", false},
		{strings.Replace(info, digest, "unknown", 1), owner, "", false},
	} {
		event, ok := parseGroupPhoto([]byte(tc.data), tc.sender)
		if ok != tc.valid || ok && (event.ID != room || *event.PhotoMD5 != tc.want || event.InfoVersion != 3) {
			t.Fatalf("group photo info %#v, valid=%t", event, ok)
		}
	}
}

func TestGroupPhotoTransferHonorsCurrentMembershipAndDigest(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 12, 12))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	sum := md5.Sum(data)
	digest := hex.EncodeToString(sum[:])
	const peer = "member"
	clientIP := netip.MustParseAddr("127.0.0.1")
	addr := netip.AddrPortFrom(clientIP, 10000)
	client := &Service{cfg: Config{BindIP: clientIP}, peers: map[string]*nativePeer{}}
	var allowed, revokeOnOpen atomic.Bool
	allowed.Store(true)
	server := &Service{peers: map[string]*nativePeer{peer: {peer: Peer{Online: true}, control: addr}}}
	server.cfg.Callbacks.GroupAvatar = func(id, hash string) []byte {
		if id != peer || hash != digest || !allowed.Load() {
			return nil
		}
		if revokeOnOpen.Load() {
			allowed.Store(false)
		}
		return data
	}
	testTransfer := func(wantSuccess bool) {
		transfer, err := startTransferServer(clientIP, 0, nil, nil, server.groupAvatarForPeer)
		if err != nil {
			t.Fatal(err)
		}
		client.peers[peer] = &nativePeer{peer: Peer{Online: true}, tcp: netip.AddrPortFrom(clientIP, uint16(transfer.Port()))}
		got, err := client.FetchGroupAvatar(context.Background(), peer, digest)
		transfer.Close()
		if (err == nil) != wantSuccess || err == nil && !bytes.Equal(got, data) {
			t.Fatalf("got %d bytes: %v", len(got), err)
		}
	}
	testTransfer(true)
	revokeOnOpen.Store(true)
	testTransfer(false)
	allowed.Store(true)
	revokeOnOpen.Store(false)
	server.peers["ambiguous"] = &nativePeer{peer: Peer{Online: true}, control: addr}
	testTransfer(false)
	delete(server.peers, "ambiguous")
	server.cfg.Callbacks.GroupAvatar = func(string, string) []byte { return []byte("stale") }
	testTransfer(false)
}

func TestGroupPhotoRejectsUnannouncedOrMalformedSelector(t *testing.T) {
	const digest = "2812923b6855dbecbfbae183e7d16b37"
	open := groupAvatarRequest(digest)
	if len(open) != 500 || open[160] != '2' || open[161] != 0 || groupAvatarDigest(open) != digest {
		t.Fatal("expected the stock UTF-16LE digest selector")
	}
	open[161] = 1
	if groupAvatarDigest(open) != "" || groupAvatarDigest(transferOpenRequest(0)) != "" {
		t.Fatal("accepted a malformed group photo name")
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if new(TransferServer).serveGroupAvatar(a, groupAvatarRequest(digest)) {
		t.Fatal("served an anonymous connection")
	}
}
