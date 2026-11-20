//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/neiwangtong"
)

func TestWindowsStockReceiverBoundary(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" || os.Getenv("GOIM_NWT_STOCK") == "" {
		t.Skip("isolated Windows stock runner")
	}
	stock, driver := os.Getenv("GOIM_NWT_STOCK"), os.Getenv("GOIM_NWT_DRIVER")
	bind, target := netip.MustParseAddr("127.0.0.2"), netip.MustParseAddrPort("127.0.0.1:9011")
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("GoIM QA %x", id[:4])
	phase := newLabChecks(options{timeout: 30 * time.Second, labLargeFile: true}, name, fmt.Sprintf("%x", id[:4]))
	acks := make(chan uint64, 2)
	// Stock binds some standard ports on all local addresses. Retain each
	// default when available, and record any shared-host exceptions.
	udpPort := func(port int, alternative int) int {
		c, err := net.ListenPacket("udp4", net.JoinHostPort(bind.String(), fmt.Sprint(port)))
		if err != nil {
			t.Logf("standard local UDP %d unavailable (%v); isolated source uses %d", port, err, alternative)
			return alternative
		}
		c.Close()
		return port
	}
	tcp := int(neiwangtong.AvatarPort)
	l, err := net.Listen("tcp4", net.JoinHostPort(bind.String(), fmt.Sprint(tcp)))
	if err != nil {
		// 0x4a4a has the same host and network representation on the
		// stock client and GoIM. This port is explicitly advertised in the
		// stock handshake when both peers share Windows' TCP stack.
		t.Logf("standard TCP %d unavailable (%v); using advertised 0x4a4a", tcp, err)
		tcp = 0x4a4a
	} else {
		l.Close()
	}
	cfg := neiwangtong.Config{ID: id, Profile: neiwangtong.Peer{Name: name}, GoIM: true, BindIP: bind,
		DiscoveryPort: udpPort(9011, 19011), ControlPort: udpPort(9012, 19012), RemoteControlPort: int(neiwangtong.ControlPort), TransferPort: tcp,
		DisableBroadcast: true, AllowRemote: func(ap netip.AddrPort) bool { return ap.Addr() == target.Addr() }}
	cfg.Callbacks.TextAcknowledged = func(_ string, n uint64) { publish(acks, n) }
	phase.attach(&cfg)
	s, err := neiwangtong.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	peer, err := s.ProbePeer(t.Context(), target, int(neiwangtong.ControlPort), int(neiwangtong.AvatarPort))
	if err != nil {
		t.Fatalf("direct loopback discovery/handshake: %v", err)
	}
	if peer.Version != "3.4.3055" || peer.GoIM {
		t.Fatalf("unexpected peer version %q", peer.Version)
	}
	t.Logf("established original client %s on loopback", peer.Version)
	if err := s.SendTextWithID(peer.ID, 201, "GoIM QA file transfer"); err != nil {
		t.Fatal(err)
	}
	if _, err := wait(t.Context(), 20*time.Second, acks, func(id uint64) bool { return id == 201 }); err != nil {
		t.Fatal("no application text receipt:", err)
	}
	t.Log("application text receipt for task 201 confirmed")
	run := func(size int64, id, selector uint64) {
		t.Helper()
		filename := fmt.Sprintf("GoIM-QA-%d-%x.bin", size, cfg.ID[:4])
		src := filepath.Join(t.TempDir(), filename)
		want, err := makeSample(src, size)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SendFileOfferWithIDs(peer.ID, id, selector, filename, size, time.Now().Unix(), src); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 18*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, driver, stock, "accept-file", name).CombinedOutput()
		if err != nil {
			t.Fatalf("stock receive control for %d bytes: %v %s", size, err, out)
		}
		_, err = wait(ctx, 15*time.Second, phase.ops, func(op labOperation) bool {
			return op.kind == neiwangtong.CmdOperateRecvFile && op.task == id && op.op == 2
		})
		if err != nil {
			t.Fatalf("stock did not acknowledge the receive control for %d bytes: %v", size, err)
		}
		t.Logf("original accepted task %d selector %d for %d bytes", id, selector, size)
		dest := filepath.Join(os.Getenv("PUBLIC"), "Nwt", "cache", "recv", name, filename)
		progress, err := phase.waitForFileSend(ctx, peer.ID, id, size)
		if err != nil || progress.err != nil || !progress.complete || progress.done != size {
			onDisk := int64(-1)
			if st, statErr := os.Stat(dest); statErr == nil {
				onDisk = st.Size()
			}
			t.Fatalf("stock stream %d: %+v %v; original progress %d; original output length %d", size, progress, err, phase.largeRecv.Load(), onDisk)
		}
		// The native receiver closes its exclusive file handle asynchronously
		// after the sender finishes, just as in the Wine acceptance fixture.
		deadline := time.Now().Add(time.Minute)
		for {
			detail, err := verifySample(dest, size, want)
			if err == nil {
				t.Log("stock receiver output", detail)
				return
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				t.Fatalf("stock receiver output %s: %v", dest, err)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	run(sampleSize, 101, 3511)
	run(largeSize, largeTask, 3513)
}
