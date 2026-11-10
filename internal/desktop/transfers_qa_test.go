package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func transferDesignState(dir string) domain.State {
	now := time.Date(2026, 11, 10, 10, 24, 0, 0, time.Local)
	state := domain.State{
		Self:          domain.Peer{ID: "self", Name: "林晓"},
		ListenAddress: "127.0.0.1:47842",
		Peers:         []domain.Peer{{ID: "lin", Name: "林晓"}, {ID: "zhou", Name: "周宁"}, {ID: "xu", Name: "许言"}},
		Settings:      domain.DefaultSettings(dir),
		Revision:      1,
		Transfers: []domain.Transfer{
			{ID: "in", PeerID: "lin", Name: "验收清单.xlsx", Direction: "receive", Status: "offered", Size: 25395, UpdatedAt: now},
			{ID: "live", PeerID: "zhou", Name: "北岭计划 · 交付资料.zip", Direction: "receive", Status: "transferring", Size: 1717986918, Done: 1170000000, Speed: 25794970, UpdatedAt: now.Add(-time.Second)},
			{ID: "out", PeerID: "xu", Name: "品牌素材.zip", Direction: "send", Status: "offered", Size: 586 * 1024 * 1024, UpdatedAt: now.Add(-time.Minute)},
		},
	}
	for index, name := range []string{"项目说明.pdf", "设计初稿.zip", "周会纪要.txt", "发版检查.xlsx", "界面预览.png", "部署说明.txt", "品牌手册.pdf", "字体素材.zip"} {
		transfer := domain.Transfer{ID: fmt.Sprintf("history-%d", index), PeerID: "zhou", Name: name, Direction: "receive", Status: "completed", Size: 24000, Done: 24000, UpdatedAt: now.Add(-time.Duration(index+2) * time.Minute)}
		if index == 1 {
			transfer.Status, transfer.Error = "failed", "源文件不可用"
		}
		state.Transfers = append(state.Transfers, transfer)
	}
	return state
}

func TestTransferDesignScreenshots(t *testing.T) {
	dir := os.Getenv("GOIM_TRANSFER_QA")
	if dir == "" {
		t.Skip("set GOIM_TRANSFER_QA to save screenshots")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	state := transferDesignState(t.TempDir())
	backend := newShellTestBackend(state)
	shell := New(backend)
	defer shell.Close()
	cfg := shell.Config()
	cfg.Width, cfg.Height = 1536, 1024
	session, err := vivaheadless.Open(cfg, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	click := func(id ui.ID) {
		t.Helper()
		for _, hit := range session.Snapshot().Hits {
			if hit.ID == id {
				if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("missing control %v", id)
	}
	shot := func(name string) {
		t.Helper()
		if err := session.Move(380, 42); err != nil {
			t.Fatal(err)
		}
		if err := session.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
	heading := func(title string) ui.Text {
		t.Helper()
		for _, item := range session.Snapshot().Texts {
			if item.Value == title && item.Style.Size > 20 {
				return item
			}
		}
		t.Fatalf("missing page title %q", title)
		return ui.Text{}
	}
	click(shell.ids.navShares)
	shareHeading := heading("文件共享")
	click(shell.ids.navTransfers)
	transferHeading := heading("文件传输")
	if shareHeading.Style != transferHeading.Style || shareHeading.Bounds.X != transferHeading.Bounds.X ||
		shareHeading.Bounds.Y != transferHeading.Bounds.Y || shareHeading.Bounds.Height != transferHeading.Bounds.Height {
		t.Fatalf("transfer and share headings do not align: share=%+v, transfer=%+v", shareHeading, transferHeading)
	}
	shot("transfers-light")
	click(shell.panels.transferIncoming[0].ids.more)
	shot("transfers-menu")
	if err := session.Keys("esc"); err != nil {
		t.Fatal(err)
	}
	click(shell.panels.ids.transferFilters.At(panelTransferFinished))
	shot("transfers-history")
	click(shell.panels.ids.transferFilters.At(panelTransferPending))
	if err := session.Resize(800, 560); err != nil {
		t.Fatal(err)
	}
	shot("transfers-compact")
	if err := session.Resize(1536, 1024); err != nil {
		t.Fatal(err)
	}
	state.Settings.Theme = "dark"
	state.Revision++
	backend.setState(state)
	shell.invalidator.Invalidate()
	if err := session.WaitFor(func(_ app.Snapshot) bool { return shell.isDark() }, time.Second); err != nil {
		t.Fatal(err)
	}
	shot("transfers-dark")
}

func BenchmarkTransferPagePaint(b *testing.B) {
	shell := New(newShellTestBackend(transferDesignState(b.TempDir())))
	defer shell.Close()
	shell.section = sectionTransfers
	config := shell.Config()
	config.Width, config.Height = 1536, 1024
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		shell.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}
