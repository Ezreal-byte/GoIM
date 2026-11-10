//go:build vivahead

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

// TestPromoScreenshots uses the real GoIM shell and only synthetic, temporary
// media. No GoIM database, network or native window is opened.
func TestPromoScreenshots(t *testing.T) {
	outputDir := os.Getenv("GOIM_PROMO_DIR")
	if outputDir == "" {
		t.Skip("run scripts/promo-headless.ps1 to render the promo screenshots")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	fixture := promoState(t)
	backend := newShellTestBackend(fixture)
	shell := New(backend)
	defer shell.Close()
	config := shell.Config()
	config.Width, config.Height = 1560, 1040
	session, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	shell.invalidator.Invalidate()

	click := func(id ui.ID) {
		t.Helper()
		hit, ok := findSessionHit(session.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("control %v is missing from the promo", id)
		}
		if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	shot := func(name string) {
		t.Helper()
		if err := session.Move(800, 18); err != nil {
			t.Fatal(err)
		}
		if err := session.Wait(450 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatalf("%s layout: %v", name, err)
		}
		output := filepath.Join(outputDir, name+".png")
		if err := session.Screenshot(output); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", output)
	}
	waitText := func(label string) {
		t.Helper()
		if err := session.WaitFor(func(snapshot app.Snapshot) bool {
			for _, text := range snapshot.Texts {
				if text.Value == label {
					return true
				}
			}
			return false
		}, 2*time.Second); err != nil {
			t.Fatalf("waiting for %q: %v", label, err)
		}
	}
	setTheme := func(theme string) {
		t.Helper()
		state := backend.Snapshot()
		state.Settings.Theme = theme
		state.Revision++
		backend.setState(state)
		shell.invalidator.Invalidate()
		if err := session.WaitFor(func(_ app.Snapshot) bool {
			return shell.isDark() == (theme == "dark")
		}, 2*time.Second); err != nil {
			t.Fatal(err)
		}
	}

	// The chat is the hero view. Open its right-side files pane, leave a draft,
	// and retain a quiet pointer so the frame reads like an in-use desktop app.
	click(shell.ids.navMessages)
	click(shell.convRowIDs["north"])
	click(shell.ids.chatHeaderFile)
	splitter, ok := findSessionHit(session.Snapshot().Hits, shell.chatSurface().ids.historySplitter)
	if !ok {
		t.Fatal("missing history splitter")
	}
	x, y := splitter.Bounds.X+splitter.Bounds.Width/2, splitter.Bounds.Y+splitter.Bounds.Height/2
	if err := session.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := session.Move(x, y+43); err != nil {
		t.Fatal(err)
	}
	if err := session.Release(x, y+43); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.chatEditor)
	if err := session.Type("我再检查一遍分享链接，确认好就发给大家。"); err != nil {
		t.Fatal(err)
	}
	if err := session.Click(720, 120); err != nil {
		t.Fatal(err)
	}
	poster := fixture.File("poster")
	if err := session.WaitFor(func(_ app.Snapshot) bool {
		return bitmapReady(shell.fileBitmap(poster.SourcePath)) && bitmapReady(shell.avatarBitmap(fixture.Self.Avatar))
	}, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	for _, label := range []string{"北岭计划", "北岭的主视觉出来了", "聊天文件", "北岭计划 · 交付清单.pdf", "主视觉预览.png", "山野影像组", "确认好就发给大家"} {
		if err := session.Expect("text", label); err != nil {
			t.Fatal(err)
		}
	}
	shot("chat-light")

	click(shell.ids.navContacts)
	for _, label := range []string{"联系人", "设计与产品", "内容制作", "林晚", "许知远", "周予安"} {
		waitText(label)
		if err := session.Expect("text", label); err != nil {
			t.Fatal(err)
		}
	}
	shot("contacts-light")

	click(shell.ids.navTransfers)
	for _, label := range []string{"文件传输", "待接收 2", "北岭计划 · 交付资料.zip", "品牌字体授权包.zip"} {
		waitText(label)
		if err := session.Expect("text", label); err != nil {
			t.Fatal(err)
		}
	}
	shot("transfers-light")

	click(shell.ids.navShares)
	for _, label := range []string{"文件共享", "设备分享主页", "北岭计划 · 交付资料", "设计归档", "密码访问"} {
		waitText(label)
		if err := session.Expect("text", label); err != nil {
			t.Fatal(err)
		}
	}
	shot("shares-light")

	setTheme("dark")
	shot("shares-dark")
	click(shell.ids.navTransfers)
	shot("transfers-dark")
	click(shell.ids.navContacts)
	shot("contacts-dark")
	click(shell.ids.navMessages)
	click(shell.ids.chatHeaderFile)
	waitText("聊天文件")
	shot("chat-dark")
}

func promoState(t *testing.T) domain.State {
	t.Helper()
	dir := t.TempDir()
	date := time.Now()
	base := time.Date(date.Year(), date.Month(), date.Day(), 10, 42, 0, 0, date.Location())
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }
	settings := domain.DefaultSettings(dir)
	settings.Nickname = "周予安"
	settings.DesktopNotifications = false
	state := domain.State{
		Settings: settings, Revision: 1, ListenAddress: "192.168.50.18:47842",
		ContactGroups: []string{"设计与产品", "内容制作", "活动执行"},
		Self: domain.Peer{
			ID: "self", Name: "周予安", Online: true, Presence: domain.PresenceOnline,
			Signature: "把复杂协作留在局域网里",
		},
		DeviceShare: domain.DeviceShare{
			URL: "http://192.168.50.18:47842/", ReceiveDir: filepath.Join(dir, "网页接收"),
		},
	}
	state.Settings.Signature = state.Self.Signature
	state.Settings.Presence = domain.PresenceOnline

	names := []string{"周予安", "林晚", "许知远", "苏禾", "陈墨", "夏岚", "顾行之", "程小满", "沈白"}
	peerIDs := []string{"self", "lin", "xu", "su", "chen", "xia", "gu", "cheng", "shen"}
	for i, name := range names {
		avatar := filepath.Join(dir, fmt.Sprintf("avatar-%d.png", i))
		writePromoAvatar(t, avatar, i)
		if i == 0 {
			state.Self.Avatar = avatar
			state.Settings.Avatar = avatar
			continue
		}
		presence := domain.PresenceOnline
		switch i {
		case 3:
			presence = domain.PresenceBusy
		case 5, 8:
			presence = domain.PresenceOffline
		}
		state.Peers = append(state.Peers, domain.Peer{
			ID: peerIDs[i], Name: name, Avatar: avatar, Online: i != 5 && i != 8,
			Address:      fmt.Sprintf("192.168.50.%d:47842", 20+i),
			ContactGroup: state.ContactGroups[(i-1)%len(state.ContactGroups)], LastSeen: at(-30 - i),
			Presence: presence, Signature: "正在整理北岭计划的交付资料",
		})
	}
	state.Groups = []domain.Group{
		{ID: "north-team", Name: "北岭计划", OwnerID: "lin", Members: []string{"self", "lin", "xu", "su", "chen", "xia"}, Epoch: 1},
		{ID: "design-team", Name: "设计工作室", OwnerID: "self", Members: []string{"self", "lin", "xu", "su", "gu"}, Epoch: 1},
		{ID: "launch-team", Name: "秋季发布协作", OwnerID: "chen", Members: []string{"self", "chen", "cheng", "xia"}, Epoch: 1},
		{ID: "photo-team", Name: "山野影像组", OwnerID: "shen", Members: []string{"self", "shen", "xia", "lin"}, Epoch: 1},
	}
	state.Conversations = []domain.Conversation{
		{ID: "north", Kind: "group", GroupID: "north-team", Title: "北岭计划", Pinned: true, Preview: "苏禾：收到，下午按这版走。", UpdatedAt: at(0)},
		{ID: "design", Kind: "group", GroupID: "design-team", Title: "设计工作室", Pinned: true, Unread: 3, Preview: "林晚：色板我更新到文件里了", UpdatedAt: at(-3)},
		{ID: "c-su", Kind: "direct", PeerID: "su", Unread: 2, Preview: "两版排版都发你了", UpdatedAt: at(-7)},
		{ID: "launch", Kind: "group", GroupID: "launch-team", Title: "秋季发布协作", Unread: 5, Preview: "陈墨：发布排期已同步", UpdatedAt: at(-11)},
		{ID: "c-xu", Kind: "direct", PeerID: "xu", Preview: "[图片] 竖版也试了一张", UpdatedAt: at(-16)},
		{ID: "c-lin", Kind: "direct", PeerID: "lin", Preview: "我在工位，随时过来", UpdatedAt: at(-25)},
		{ID: "photo", Kind: "group", GroupID: "photo-team", Title: "山野影像组", Muted: true, Preview: "夏岚：[文件] 取景路线.pdf", UpdatedAt: at(-34)},
		{ID: "c-chen", Kind: "direct", PeerID: "chen", Preview: "下午三点对齐一下细节", UpdatedAt: at(-56)},
		{ID: "c-cheng", Kind: "direct", PeerID: "cheng", Unread: 1, Preview: "物料清单我补了两项", UpdatedAt: at(-63)},
		{ID: "c-gu", Kind: "direct", PeerID: "gu", Preview: "已经收到打包文件", UpdatedAt: at(-72)},
		{ID: "c-xia", Kind: "direct", PeerID: "xia", Preview: "路上信号不好，回去传原图", UpdatedAt: at(-86)},
	}

	posterPath := filepath.Join(dir, "visual.png")
	writePromoPoster(t, posterPath)
	stat, err := os.Stat(posterPath)
	if err != nil {
		t.Fatal(err)
	}
	state.Files = append(state.Files, domain.File{
		ID: "poster", Name: "主视觉预览.png", MIME: "image/png", InlineImage: true, Size: stat.Size(),
		SenderID: "lin", ConversationID: "north", SourcePath: posterPath, CreatedAt: at(-23),
		Shared: true, ShareURL: "http://192.168.50.18:47842/s/poster",
	})
	for _, item := range []struct {
		id, name, mime, sender string
		size                   int64
		minutes                int
	}{
		{"brief", "设计需求说明.pdf", "application/pdf", "su", 372 * 1024, -76},
		{"references", "山野采风素材.zip", "application/zip", "xia", 24 * 1024 * 1024, -64},
		{"colors", "品牌色板.png", "image/png", "shen", 286 * 1024, -53},
		{"copy", "首页文案.md", "text/markdown", "su", 7 * 1024, -38},
		{"grid", "竖版构图草案.png", "image/png", "xu", 0, -32},
		{"checklist", "北岭计划 · 交付清单.pdf", "application/pdf", "self", 684 * 1024, -5},
	} {
		path := ""
		// Only files actually previewed or marked locally available need an asset.
		if item.id == "grid" {
			path = filepath.Join(dir, "grid.png")
			writePromoGrid(t, path)
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			item.size = stat.Size()
		} else if item.id == "checklist" || item.id == "copy" || item.id == "brief" {
			path = filepath.Join(dir, item.id)
			if err := os.WriteFile(path, []byte("GoIM synthetic showcase file"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, item.size); err != nil {
				t.Fatal(err)
			}
		}
		state.Files = append(state.Files, domain.File{
			ID: item.id, Name: item.name, MIME: item.mime, Size: item.size, SenderID: item.sender,
			ConversationID: "north", SourcePath: path, CreatedAt: at(item.minutes),
		})
	}
	// A dense but realistic sharing surface: folders, password-protected links,
	// owned shares and member shares are all represented.
	state.Files = append(state.Files,
		domain.File{
			ID: "delivery", Name: "北岭计划 · 交付资料", Size: 1717986918, ShareFolder: true,
			ShareEntryCount: 32, SenderID: "self", Shared: true, CreatedAt: at(2),
			ShareURL: "http://192.168.50.18:47842/s/delivery",
		},
		domain.File{
			ID: "archive", Name: "设计归档", Size: 3650722201, ShareFolder: true,
			ShareEntryCount: 128, SenderID: "self", Shared: true, ShareProtected: true,
			CreatedAt: at(-1080), ShareURL: "http://192.168.50.18:47842/s/archive",
		},
		domain.File{
			ID: "peer-notes", Name: "北岭评审会议记录.pdf", MIME: "application/pdf",
			Size: 173001, SenderID: "lin", Shared: true, CreatedAt: at(-18),
		},
		domain.File{
			ID: "peer-assets", Name: "山野采风精选.zip", MIME: "application/zip",
			Size: 1201234, SenderID: "xia", Shared: true, CreatedAt: at(-61),
		},
	)
	// File transfer counts are fixtures. They illustrate the interface, not
	// network activity.
	checklist := state.File("checklist")
	checklist.Shared = true
	checklist.ShareURL = "http://192.168.50.18:47842/s/checklist"
	for index := range state.Files {
		if state.Files[index].ID == checklist.ID {
			state.Files[index] = checklist
			break
		}
	}
	for _, peer := range []string{"lin", "xu", "su", "chen", "xia"} {
		status, done, speed := "completed", checklist.Size, float64(0)
		if peer == "chen" {
			status, done, speed = "transferring", checklist.Size*72/100, 836*1024
		}
		state.Transfers = append(state.Transfers, domain.Transfer{
			ID: "send-" + peer, FileID: checklist.ID, ConversationID: "north", PeerID: peer,
			Direction: "send", Status: status, Name: checklist.Name, Size: checklist.Size, Done: done, Speed: speed, UpdatedAt: at(-4),
		})
	}
	state.Transfers = append(state.Transfers,
		domain.Transfer{
			ID: "incoming-checklist", FileID: "peer-offer", ConversationID: "north", PeerID: "lin",
			Direction: "receive", Status: "offered", Name: "北岭计划 · 验收清单.xlsx",
			Size: 25395, UpdatedAt: at(-1),
		},
		domain.Transfer{
			ID: "incoming-archive", FileID: "archive-offer", ConversationID: "north", PeerID: "xia",
			Direction: "receive", Status: "transferring", Name: "北岭计划 · 交付资料.zip",
			Size: 1717986918, Done: 1170000000, Speed: 25794970, UpdatedAt: at(0),
		},
		domain.Transfer{
			ID: "incoming-fonts", FileID: "font-offer", ConversationID: "north", PeerID: "su",
			Direction: "receive", Status: "offered", Name: "品牌字体授权包.zip",
			Size: 586 * 1024 * 1024, UpdatedAt: at(-9),
		},
		domain.Transfer{
			ID: "failed-render", FileID: "render-file", ConversationID: "north", PeerID: "xu",
			Direction: "send", Status: "failed", Name: "动态海报成片.mov",
			Size: 248 * 1024 * 1024, Done: 96 * 1024 * 1024, Error: "连接中断，可重新发送",
			UpdatedAt: at(-33),
		},
	)
	for i, m := range []struct {
		sender, kind, text, file string
		minutes                  int
	}{
		{"lin", "text", "北岭的主视觉出来了，大家看看这版日落配色。", "", -24},
		{"lin", "image", "", "poster", -23},
		{"xu", "text", "这个橘色刚好。首页就用这张，竖版我也排好了。", "", -16},
		{"self", "text", "好，我把发布前要确认的事项放到清单里。", "", -11},
		{"self", "file", checklist.Name, "checklist", -5},
		{"su", "text", "清单收到了，下午按这版走。", "", 0},
	} {
		state.Messages = append(state.Messages, domain.Message{
			ID: fmt.Sprintf("north-%d", i), ConversationID: "north", SenderID: m.sender, Kind: m.kind,
			Text: m.text, FileID: m.file, Status: "delivered", CreatedAt: at(m.minutes),
		})
	}
	return state
}
