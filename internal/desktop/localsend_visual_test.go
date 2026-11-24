//go:build vivahead

package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	vivaheadless "github.com/vivaui/viva/headless"
	"goim/internal/domain"
)

func TestLocalSendVisuals(t *testing.T) {
	output := os.Getenv("GOIM_LOCALSEND_SCREENSHOTS")
	if output == "" {
		t.Skip("set GOIM_LOCALSEND_SCREENSHOTS to render the LocalSend screens")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			state := shellMinimalState(t)
			state.Settings.Theme = theme
			state.Settings.LastSection = sectionLocalSend
			state.Settings.LocalSend.Enabled = true
			state.Settings.DownloadDir = `D:\GoIM\Downloads`
			state.LocalSend = domain.LocalSendStatus{Active: true, Alias: "周予安的电脑", Devices: []domain.LocalSendDevice{
				{ID: "ios", Name: "林晚的 iPhone", Model: "iOS", Type: "mobile", Address: "192.168.1.42:53317"},
				{ID: "android", Name: "工作平板", Model: "Android", Type: "tablet", Address: "192.168.1.63:53317"},
			}}
			state.Revision = 1
			backend := newShellTestBackend(state)
			shell := New(backend)
			defer shell.Close()
			cfg := shell.Config()
			cfg.Width, cfg.Height = 1150, 760
			session, err := vivaheadless.Open(cfg, nil, vivaheadless.Options{Scale: 1.25})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			shell.invalidator.Invalidate()
			shot := func(name string) {
				t.Helper()
				if err := session.Wait(350 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := session.Expect("layout", "clean"); err != nil {
					t.Fatal(err)
				}
				if err := session.Screenshot(filepath.Join(output, name+"-"+theme+".png")); err != nil {
					t.Fatal(err)
				}
			}
			shot("send")
			device, ok := findSessionHitLabel(session.Snapshot().Hits, "林晚的 iPhone")
			if !ok {
				t.Fatal("sample device is missing")
			}
			if err := session.Click(device.Bounds.X+device.Bounds.Width/2, device.Bounds.Y+device.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			shot("send-selected")
			state.Revision++
			state.LocalSend.Offer = domain.LocalSendOffer{ID: "ls-test", From: "林晚的 iPhone", Files: []domain.LocalSendFile{
				{ID: "pdf", Name: "项目交付清单.pdf", Size: 2936012},
				{ID: "image", Name: "现场照片/会议现场.jpg", Size: 6710886},
				{ID: "text", Name: "发布说明.txt", Size: 12288},
			}}
			backend.setState(state)
			shell.invalidator.Invalidate()
			if err := session.Expect("text", "接收请求", "within", "2000"); err != nil {
				t.Fatal(err)
			}
			shot("receive")

			click := func(label string) {
				t.Helper()
				hit, ok := findSessionHitLabel(session.Snapshot().Hits, label)
				if !ok {
					t.Fatalf("control %q is missing", label)
				}
				if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			state.Revision++
			state.LocalIP = "192.168.1.86"
			state.LocalSend.Port = 53317
			state.LocalSend.Offer = domain.LocalSendOffer{}
			now := time.Now()
			state.Transfers = []domain.Transfer{
				{ID: "ls-1", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: "林晚的 iPhone", Name: "现场照片.jpg", Direction: "receive", Status: "completed", Size: 6710886, UpdatedAt: now},
				{ID: "ls-2", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: "工作平板", Name: "项目资料 等 3 个文件", Direction: "send", Status: "partial", Size: 9646900, UpdatedAt: now.Add(-3 * time.Minute),
					LocalSendItems: []domain.LocalSendTransferItem{
						{ID: "a", Name: "项目交付清单.pdf", Status: "completed", Size: 2936012},
						{ID: "b", Name: "现场照片/会议现场.jpg", Status: "completed", Size: 6710886},
						{ID: "c", Name: "发布说明.txt", Status: "rejected", Size: 12288},
					}},
				{ID: "ls-3", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: "会议室电脑", Name: "演示材料.zip", Direction: "send", Status: "transferring", Size: 15039400, Done: 7602400, Speed: 10229760, UpdatedAt: now.Add(-6 * time.Minute)},
			}
			backend.setState(state)
			// State arrives through a background watcher in the app. Wake the
			// headless driver before advancing its virtual animation clock.
			shell.invalidator.Invalidate()
			if err := session.Expect("text", "可接收", "within", "2000"); err != nil {
				t.Fatal(err)
			}
			shot("ready")

			moving, err := session.Image()
			if err != nil {
				t.Fatal(err)
			}
			if err := session.Wait(250 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			next, err := session.Image()
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(moving.Pix, next.Pix) {
				t.Fatal("receive logo did not rotate")
			}
			if err := session.Wait(localSendLogoTurn); err != nil {
				t.Fatal(err)
			}
			still, err := session.Image()
			if err != nil {
				t.Fatal(err)
			}
			if err := session.Wait(300 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			settled, err := session.Image()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(still.Pix, settled.Pix) {
				t.Fatal("receive logo continued to animate after the entrance turn")
			}

			click("模块设置")
			shot("settings")
			scope := shell.tree.Sub(shell.panels.localSend.scope, shell.tree.Name("settings-dialog"))
			close, ok := findSessionHit(session.Snapshot().Hits, shell.tree.ID(scope, "cancel"))
			if !ok {
				t.Fatal("settings cancel is missing")
			}
			if err := session.Click(close.Bounds.X+close.Bounds.Width/2, close.Bounds.Y+close.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			click("记录")
			shot("records")
			click("文件清单")
			shot("record-files")

		})
	}
}
