//go:build vivahead

package desktop

import (
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
			state.Revision++
			state.LocalSend.Offer = domain.LocalSendOffer{ID: "ls-test", From: "林晚的 iPhone", Files: []domain.LocalSendFile{
				{ID: "pdf", Name: "项目交付清单.pdf", Size: 2936012},
				{ID: "image", Name: "现场照片/会议现场.jpg", Size: 6710886},
				{ID: "text", Name: "发布说明.txt", Size: 12288},
			}}
			backend.setState(state)
			shot("receive")
		})
	}
}
