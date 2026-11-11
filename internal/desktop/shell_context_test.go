package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (harness *shellTestHarness) rightClickID(id ui.ID) {
	harness.t.Helper()
	hit, ok := harness.findHit(id)
	if !ok {
		harness.t.Fatalf("control %v is not on screen", id)
	}
	harness.rightClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2)
}

func (harness *shellTestHarness) rightClick(x, y float32) {
	harness.t.Helper()
	if err := harness.driver.ClickButton(x, y, ui.PointerButtonSecondary); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *shellTestHarness) replaceFieldText(id ui.ID, current, replacement string) {
	harness.t.Helper()
	harness.clickID(id)
	if err := harness.driver.SelectText(0, utf8.RuneCountInString(current)); err != nil {
		harness.t.Fatal(err)
	}
	if err := harness.driver.Type(replacement); err != nil {
		harness.t.Fatal(err)
	}
}

func shellCommandCount(commands []domain.Command, kind domain.CommandKind) int {
	count := 0
	for _, command := range commands {
		if command.Kind == kind {
			count++
		}
	}
	return count
}

func findShellCommand(commands []domain.Command, kind domain.CommandKind) (domain.Command, bool) {
	for _, command := range commands {
		if command.Kind == kind {
			return command, true
		}
	}
	return domain.Command{}, false
}

func TestSelfRowAppearsWithNoOtherContactsAndOpensProfile(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.Nickname = "本机用户"
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.navContacts)

	if !harness.hasHit(harness.ui.ids.selfRow) {
		t.Fatal("the self row is missing when there are no contacts")
	}
	if !harness.hasText("本机用户") || !harness.hasText("个人资料 · 本机") ||
		!harness.hasText("还没有其他联系人") {
		t.Fatal("the self row did not show the local profile")
	}
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.ids.selfRow)
	harness.settle()
	if harness.ui.profile.open || !harness.hasText("用户类型") {
		t.Fatal("self selection must show the profile without editing")
	}
	harness.clickID(harness.ui.ids.contactTalk)
	harness.settle()
	if !harness.ui.profile.open || !harness.hasText("个人资料") {
		t.Fatal("clicking the self row did not open the profile editor")
	}
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	harness.settle()

	harness.rightClickID(harness.ui.ids.selfRow)
	harness.settle()
	if !harness.ui.shellMenu.open || harness.ui.shellMenu.kind != shellMenuSelf {
		t.Fatalf("self context menu state = %+v", harness.ui.shellMenu)
	}
	harness.clickID(harness.ui.ids.shellMenuProfile)
	harness.settle()
	if !harness.ui.profile.open {
		t.Fatal("the self context menu did not reopen the profile editor")
	}
}

func TestInitialAvatarFallbackUsesLastTwoUnicodeCharacters(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "ascii", text: "Alice", want: "ce"},
		{name: "cjk", text: "产品讨论组", want: "论组"},
		{name: "one grapheme", text: "我", want: "我"},
		{name: "emoji cluster", text: "Amy👩‍💻", want: "y👩‍💻"},
		{name: "combining mark", text: "Xe\u0301", want: "Xe\u0301"},
		{name: "trimmed", text: "  张三  ", want: "张三"},
		{name: "empty", text: " \t", want: "?"},
		{name: "invalid utf8", text: string([]byte{0xff, 0xfe}), want: "?"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := initialOf(test.text); got != test.want {
				t.Fatalf("initialOf(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}

func TestGroupHeaderAvatarUsesTheGroupNameNotTheMemberCount(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	threshold := float32(railWidth + sidebarWidth)
	for _, text := range harness.snapshot().Texts {
		if text.Value == "论组" && text.Bounds.X > threshold {
			return
		}
	}
	t.Fatal("the group header avatar did not show the group name's last two characters")
}

func TestHiddenConversationIsNotListedCountedOrReused(t *testing.T) {
	state := shellTestState(t)
	state.Conversations[1].Hidden = true
	state.Conversations[1].Unread = 7
	harness := newShellTestHarness(t, state)

	nav, ok := harness.findHit(harness.ui.ids.navMessages)
	if !ok || !strings.Contains(nav.Label, "2 条未读") || strings.Contains(nav.Label, "9 条未读") {
		t.Fatalf("message unread badge = %q, want only the visible conversation's unread count", nav.Label)
	}
	harness.clickID(harness.ui.ids.navMessages)
	harness.settle()
	if harness.hasHit(harness.ui.convRowIDs["conv-alice"]) {
		t.Fatal("a hidden conversation stayed in the message list")
	}

	harness.clickID(harness.ui.ids.navContacts)
	harness.settle()
	harness.clickID(harness.ui.peerRowIDs["peer-a"])
	harness.settle()
	if harness.ui.section != sectionContacts || harness.ui.selected != "conv-group" {
		t.Fatal("contact selection changed the chat")
	}
	harness.clickID(harness.ui.ids.contactTalk)
	harness.settle()
	command, found := findShellCommand(harness.backend.commandLog(), domain.StartChat)
	if !found || command.PeerID != "peer-a" {
		t.Fatalf("send-message action did not reopen the hidden direct conversation: %+v", command)
	}
}

func TestHiddenGroupConversationRemainsInContacts(t *testing.T) {
	state := shellTestState(t)
	state.Conversations[0].Hidden = true
	harness := newShellTestHarness(t, state)
	if harness.hasHit(harness.ui.convRowIDs["conv-group"]) {
		t.Fatal("a hidden group conversation stayed in the initial message list")
	}
	harness.clickID(harness.ui.ids.navContacts)
	harness.settle()
	if !harness.hasHit(harness.ui.convRowIDs["conv-group"]) {
		t.Fatal("a hidden group conversation disappeared from Contacts")
	}
	harness.clickID(harness.ui.convRowIDs["conv-group"])
	if !harness.hasText("群聊资料") {
		t.Fatal("group selection did not open its details")
	}
	harness.clickID(harness.ui.ids.contactTalk)
	if command := harness.lastCommand(); command.Kind != domain.StartChat || command.ConversationID != "conv-group" {
		t.Fatalf("reopen group: %+v", command)
	}
}

func TestConversationContextMenuPinsMutesAndDeletesWithConfirmation(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.ids.navMessages)
	harness.settle()

	harness.rightClickID(harness.ui.convRowIDs["conv-alice"])
	harness.settle()
	if !harness.ui.shellMenu.open || harness.ui.shellMenu.kind != shellMenuConversation ||
		harness.ui.shellMenu.target != "conv-alice" {
		t.Fatalf("conversation context menu state = %+v", harness.ui.shellMenu)
	}
	for _, id := range []ui.ID{harness.ui.ids.shellMenuPin, harness.ui.ids.shellMenuMute, harness.ui.ids.shellMenuDelete} {
		hit, ok := harness.findHit(id)
		if !ok || hit.Scope != harness.ui.ids.shellMenuScope {
			t.Fatalf("conversation menu item %v is missing from its scope", id)
		}
	}
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.ids.shellMenuPin)
	harness.settle()
	command, ok := findShellCommand(harness.backend.commandLog(), domain.TogglePin)
	if !ok || command.ConversationID != "conv-alice" {
		t.Fatalf("pin command = %+v", command)
	}
	if harness.ui.shellMenu.open {
		t.Fatal("conversation menu stayed open after pinning")
	}

	harness.rightClickID(harness.ui.convRowIDs["conv-alice"])
	harness.settle()
	harness.clickID(harness.ui.ids.shellMenuMute)
	harness.settle()
	command, ok = findShellCommand(harness.backend.commandLog(), domain.ToggleMute)
	if !ok || command.ConversationID != "conv-alice" {
		t.Fatalf("mute command = %+v", command)
	}

	harness.rightClickID(harness.ui.convRowIDs["conv-alice"])
	harness.settle()
	harness.clickID(harness.ui.ids.shellMenuDelete)
	harness.settle()
	if !harness.ui.confirm.open || harness.ui.confirm.title != "删除会话？" ||
		!strings.Contains(harness.ui.confirm.body, "本机") ||
		!strings.Contains(harness.ui.confirm.body, "重新出现") {
		t.Fatalf("delete confirmation = %+v", harness.ui.confirm)
	}
	if count := shellCommandCount(harness.backend.commandLog(), domain.DeleteConversation); count != 0 {
		t.Fatalf("delete command was sent before confirmation: %d", count)
	}
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.ids.confirmCancel)
	harness.settle()
	if count := shellCommandCount(harness.backend.commandLog(), domain.DeleteConversation); count != 0 {
		t.Fatalf("canceling confirmation still deleted the conversation: %d", count)
	}

	harness.rightClickID(harness.ui.convRowIDs["conv-alice"])
	harness.settle()
	harness.clickID(harness.ui.ids.shellMenuDelete)
	harness.settle()
	harness.clickID(harness.ui.ids.confirmRun)
	harness.settle()
	command, ok = findShellCommand(harness.backend.commandLog(), domain.DeleteConversation)
	if !ok || command.ConversationID != "conv-alice" {
		t.Fatalf("confirmed delete command = %+v", command)
	}
}

func TestContactContextMenuOrganizesIntoExistingNewAndUngroupedGroups(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wanted string
	}{
		{name: "new group", input: "核心团队", wanted: "核心团队"},
		{name: "existing group", input: "工程团队", wanted: "工程团队"},
		{name: "ungrouped label", input: "未分组", wanted: ""},
		{name: "blank", input: "   ", wanted: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newShellTestHarness(t, shellTestState(t))
			harness.clickID(harness.ui.ids.navContacts)
			harness.rightClickID(harness.ui.peerRowIDs["peer-a"])
			harness.settle()
			if !harness.ui.shellMenu.open || harness.ui.shellMenu.kind != shellMenuContact {
				t.Fatalf("contact context menu state = %+v", harness.ui.shellMenu)
			}
			if !harness.hasHit(harness.ui.ids.shellMenuTalk) ||
				!harness.hasHit(harness.ui.ids.shellMenuOrganize) ||
				!harness.hasHit(harness.ui.ids.shellMenuNewGroup) {
				t.Fatal("contact context menu is missing an action")
			}
			harness.clickID(harness.ui.ids.shellMenuOrganize)
			harness.settle()
			if !harness.ui.organize.open || harness.ui.organize.peerID != "peer-a" {
				t.Fatalf("organizer state = %+v", harness.ui.organize)
			}
			harness.replaceFieldText(harness.ui.ids.organizeField, "产品与设计", test.input)
			harness.clickID(harness.ui.ids.organizeSave)
			harness.settle()
			command, ok := findShellCommand(harness.backend.commandLog(), domain.SetContactGroup)
			if !ok || command.PeerID != "peer-a" || command.Name != test.wanted {
				t.Fatalf("contact-group command = %+v, want name %q", command, test.wanted)
			}
			if harness.ui.organize.open {
				t.Fatal("organizer stayed open after saving")
			}
		})
	}
}

func TestSidebarContextMenuCollapsesAndExpandsContactGroups(t *testing.T) {
	state := shellMinimalState(t)
	state.Peers = []domain.Peer{
		{ID: "peer-a", Name: "Alice", ContactGroup: "产品", Online: true},
		{ID: "peer-b", Name: "Bob", ContactGroup: "工程", Online: false},
	}
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.navContacts)
	surface, ok := harness.findHit(harness.ui.ids.sidebarSurface)
	if !ok {
		t.Fatal("sidebar surface is missing")
	}
	x, y := surface.Bounds.X+24, surface.Bounds.Y+surface.Bounds.Height-4

	harness.rightClick(x, y)
	harness.settle()
	if !harness.ui.shellMenu.open || harness.ui.shellMenu.kind != shellMenuSidebar {
		t.Fatalf("sidebar context menu state = %+v", harness.ui.shellMenu)
	}
	for _, id := range []ui.ID{
		harness.ui.ids.shellMenuNewGroup, harness.ui.ids.shellMenuConnect,
		harness.ui.ids.shellMenuExpand, harness.ui.ids.shellMenuCollapse,
	} {
		if !harness.hasHit(id) {
			t.Fatalf("sidebar menu item %v is missing", id)
		}
	}
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.ids.shellMenuCollapse)
	harness.settle()
	if len(harness.ui.collapsed) != len(harness.ui.contacts) || len(harness.ui.collapsed) == 0 {
		t.Fatalf("collapse-all state = %+v for %d groups", harness.ui.collapsed, len(harness.ui.contacts))
	}
	if harness.hasHit(harness.ui.peerRowIDs["peer-a"]) || harness.hasHit(harness.ui.peerRowIDs["peer-b"]) {
		t.Fatal("collapsed contact groups still showed their peers")
	}

	harness.rightClick(x, y)
	harness.settle()
	harness.clickID(harness.ui.ids.shellMenuExpand)
	harness.settle()
	if len(harness.ui.collapsed) != 0 {
		t.Fatalf("expand-all left groups collapsed: %+v", harness.ui.collapsed)
	}
	if !harness.hasHit(harness.ui.peerRowIDs["peer-a"]) || !harness.hasHit(harness.ui.peerRowIDs["peer-b"]) {
		t.Fatal("expanding all groups did not restore their peers")
	}
}

func TestShellMenusAndOrganizerStayCleanAtBothSizesAndThemes(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{name: "default", w: 1440, h: 960},
		{name: "minimum", w: 1000, h: 700},
	}
	for _, size := range sizes {
		for _, theme := range []string{"light", "dark"} {
			t.Run(size.name+"/"+theme, func(t *testing.T) {
				state := shellTestState(t)
				state.Settings.Theme = theme
				harness := newShellTestHarnessAtSize(t, state, size.w, size.h)

				harness.clickID(harness.ui.ids.navMessages)
				harness.settle()
				harness.rightClickID(harness.ui.convRowIDs["conv-alice"])
				harness.settle()
				requireShellLayout(t, harness.snapshot())
				harness.clickID(harness.ui.ids.shellMenuDelete)
				harness.settle()
				if !harness.ui.confirm.open {
					t.Fatal("delete confirmation did not open")
				}
				requireShellLayout(t, harness.snapshot())
				harness.clickID(harness.ui.ids.confirmCancel)
				harness.settle()

				harness.clickID(harness.ui.ids.navContacts)
				harness.settle()
				harness.rightClickID(harness.ui.peerRowIDs["peer-a"])
				harness.settle()
				requireShellLayout(t, harness.snapshot())
				harness.clickID(harness.ui.ids.shellMenuOrganize)
				harness.settle()
				if !harness.ui.organize.open {
					t.Fatal("contact organizer did not open")
				}
				requireShellLayout(t, harness.snapshot())
			})
		}
	}
}

func TestChatImageThumbnailPreservesAspectRatio(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		maxWidth      float32
		wantW, wantH  float32
	}{
		{name: "wide", width: 2, height: 1, maxWidth: 620, wantW: 330, wantH: 165},
		{name: "tall", width: 1, height: 2, maxWidth: 620, wantW: 125, wantH: 250},
		{name: "narrow chat", width: 4, height: 1, maxWidth: 200, wantW: 200, wantH: 50},
		{name: "empty", width: 0, height: 0, maxWidth: 620, wantW: 0, wantH: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height := chatImageThumbnail(ui.Bitmap{Width: test.width, Height: test.height}, test.maxWidth)
			if width != test.wantW || height != test.wantH {
				t.Fatalf("thumbnail = %.1fx%.1f, want %.1fx%.1f", width, height, test.wantW, test.wantH)
			}
		})
	}
}

func TestImageMessageUsesAspectRatioThumbnailAfterDecode(t *testing.T) {
	path := writeShellTestPNG(t, "wide-preview.png", 2, 1)
	state := shellTestState(t)
	state.Files = append(state.Files, domain.File{
		ID: "file-image", Name: "wide-preview.png", Size: 128, MIME: "image/png",
		SenderID: "peer-a", ConversationID: "conv-group", SourcePath: path,
	})
	state.Messages = append(state.Messages, domain.Message{
		ID: "message-image", ConversationID: "conv-group", SenderID: "peer-a",
		Kind: "image", FileID: "file-image", Status: "delivered",
	})
	harness := newShellTestHarness(t, state)
	harness.waitFor(func() bool {
		return bitmapReady(harness.ui.fileBitmap(path))
	})
	harness.wake()
	hit, ok := harness.findHit(harness.ui.fileButtonIDs["file-image"])
	if !ok {
		t.Fatal("decoded image did not expose its thumbnail target")
	}
	if hit.Bounds.Width > 330.5 || hit.Bounds.Height > 250.5 {
		t.Fatalf("image thumbnail exceeded its bounds: %+v", hit.Bounds)
	}
	if ratio := hit.Bounds.Width / hit.Bounds.Height; ratio < 1.99 || ratio > 2.01 {
		t.Fatalf("image thumbnail distorted a 2:1 source: %+v (ratio %.2f)", hit.Bounds, ratio)
	}
}

func TestSquareAvatarCropUsesCenterPixelsAndLeavesTheSourceAlone(t *testing.T) {
	source := ui.Bitmap{
		Pix: []byte{
			1, 2, 3, 4, 10, 20, 30, 40, 100, 110, 120, 130, 200, 210, 220, 230,
			5, 6, 7, 8, 50, 60, 70, 80, 140, 150, 160, 170, 240, 250, 255, 255,
		},
		Width: 4, Height: 2, Stride: 16, ID: 41, Version: 1,
	}
	original := append([]byte(nil), source.Pix...)
	square, ok := squareBitmap(source)
	if !ok {
		t.Fatal("square crop was rejected")
	}
	if square.Width != 2 || square.Height != 2 || square.Stride != 8 || square.ID != 41 {
		t.Fatalf("cropped avatar = %dx%d stride %d id %d", square.Width, square.Height, square.Stride, square.ID)
	}
	want := []byte{
		10, 20, 30, 40, 100, 110, 120, 130,
		50, 60, 70, 80, 140, 150, 160, 170,
	}
	if !slicesEqualBytes(square.Pix, want) {
		t.Fatalf("center crop pixels = %v, want %v", square.Pix, want)
	}
	if !slicesEqualBytes(source.Pix, original) {
		t.Fatal("cropping changed the decoded source bitmap")
	}
}

func TestAvatarCacheMasksOnceAndKeepsStableIdentity(t *testing.T) {
	shell := New(nil)
	defer shell.Close()
	source := ui.Bitmap{
		Pix: []byte{
			1, 2, 3, 4, 10, 20, 30, 40, 100, 110, 120, 130, 200, 210, 220, 230,
			5, 6, 7, 8, 50, 60, 70, 80, 140, 150, 160, 170, 240, 250, 255, 255,
		},
		Width: 4, Height: 2, Stride: 16, ID: 41, Version: 1,
	}
	shell.imageCache["avatar.png"] = source
	shell.avatarImagePaths["avatar.png"] = true
	shell.cacheAvatarBitmap("avatar.png")

	first := shell.avatarBitmap("avatar.png")
	if first.Width != 2 || first.Height != 2 || first.Stride != 8 || first.ID == 0 || first.ID == 41 {
		t.Fatalf("cropped avatar = %dx%d stride %d id %d", first.Width, first.Height, first.Stride, first.ID)
	}
	firstPixels := append([]byte(nil), first.Pix...)
	shell.cacheAvatarBitmap("avatar.png")
	second := shell.avatarBitmap("avatar.png")
	if second.ID != first.ID || second.Version != first.Version || !slicesEqualBytes(second.Pix, firstPixels) {
		t.Fatal("unchanged avatar was cropped or assigned a new bitmap identity again")
	}
}

func TestCircularAvatarMaskTransparentCornersOpaqueCenter(t *testing.T) {
	side := 16
	source := ui.Bitmap{
		Pix:     make([]byte, side*side*4),
		Width:   side,
		Height:  side,
		Stride:  side * 4,
		ID:      9,
		Version: 3,
	}
	for index := 0; index < len(source.Pix); index += 4 {
		source.Pix[index], source.Pix[index+1], source.Pix[index+2], source.Pix[index+3] = 80, 120, 200, 255
	}
	original := append([]byte(nil), source.Pix...)
	round, ok := circularBitmap(source)
	if !ok {
		t.Fatal("circular mask was rejected")
	}
	if round.Width != side || round.Height != side || round.Stride != source.Stride {
		t.Fatalf("masked avatar shape = %dx%d stride %d", round.Width, round.Height, round.Stride)
	}
	alphaAt := func(x, y int) byte { return round.Pix[y*round.Stride+x*4+3] }
	center := side / 2
	if alphaAt(0, 0) != 0 || alphaAt(side-1, 0) != 0 ||
		alphaAt(0, side-1) != 0 || alphaAt(side-1, side-1) != 0 {
		t.Fatal("circular avatar mask kept opaque corners")
	}
	if alphaAt(center, center) != 255 {
		t.Fatalf("circular avatar center alpha = %d, want 255", alphaAt(center, center))
	}
	if !slicesEqualBytes(source.Pix, original) {
		t.Fatal("circular masking changed the decoded source bitmap")
	}
}

func TestBitmapReadinessAcceptsTinyImagesAndRejectsPendingPlaceholder(t *testing.T) {
	if !bitmapReady(ui.Bitmap{Pix: make([]byte, 8), Width: 2, Height: 1, Stride: 8, Version: 1}) {
		t.Fatal("a valid 2x1 bitmap was treated as unready")
	}
	if bitmapReady(ui.Bitmap{Pix: make([]byte, 4), Width: 1, Height: 1, Stride: 4}) {
		t.Fatal("the pending 1x1 placeholder was treated as ready")
	}
	if !bitmapReady(ui.Bitmap{Pix: make([]byte, 4), Width: 1, Height: 1, Stride: 4, Version: 1}) {
		t.Fatal("a decoded 1x1 bitmap was treated as pending")
	}
}

func TestRefreshImagesQueuesAvatarAndBackgroundAssets(t *testing.T) {
	shell := New(nil)
	defer shell.Close()
	shell.state.Self.Avatar = `C:\images\self.png`
	shell.state.Settings.Avatar = `C:\images\settings.png`
	shell.state.Settings.Background = `C:\images\background.png`
	shell.refreshImages()
	for _, path := range []string{
		`C:\images\self.png`, `C:\images\settings.png`, `C:\images\background.png`,
	} {
		if !shell.mediaDesiredImages[path] {
			t.Fatalf("image %q was not queued for decode", path)
		}
	}
	if !shell.avatarImagePaths[`C:\images\self.png`] ||
		!shell.avatarImagePaths[`C:\images\settings.png`] ||
		shell.avatarImagePaths[`C:\images\background.png`] {
		t.Fatalf("avatar/background asset classification = %+v", shell.avatarImagePaths)
	}
}

func TestRailLockEntryUsesLockSetupAndBlocksTheShell(t *testing.T) {
	t.Run("first lock requests setup", func(t *testing.T) {
		harness := newShellTestHarness(t, shellMinimalState(t))
		if !harness.hasHit(harness.ui.ids.navLock) {
			t.Fatal("the lock entry is missing from the rail")
		}
		harness.clickID(harness.ui.ids.navLock)
		harness.settle()
		if !harness.ui.lockUI.setup || !harness.ui.lockVisible() || !harness.hasText("设置锁定密码") {
			t.Fatalf("lock setup state = %+v", harness.ui.lockUI)
		}
		if harness.hasHit(harness.ui.ids.selfRow) || harness.hasHit(harness.ui.ids.sidebarSearch) {
			t.Fatal("the normal shell remained interactive behind lock setup")
		}
		requireShellLayout(t, harness.snapshot())

		typePasswordKeys(t, harness, harness.ui.lockUI.focus, "Pass-123!")
		typePasswordKeys(t, harness, harness.ui.lockUI.confirmID, "Pass-123!")
		if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		harness.settle()
		command, ok := findShellCommand(harness.backend.commandLog(), domain.SetLockPassword)
		if !ok || command.Text != "Pass-123!" || !command.Bool {
			t.Fatalf("lock-password command = %+v", command)
		}
	})

	t.Run("escape cancels setup", func(t *testing.T) {
		harness := newShellTestHarness(t, shellMinimalState(t))
		harness.clickID(harness.ui.ids.navLock)
		harness.settle()
		if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
		harness.settle()
		if harness.ui.lockUI.setup || harness.ui.lockVisible() {
			t.Fatal("Escape did not cancel lock setup")
		}
		if !harness.hasHit(harness.ui.ids.navMessages) {
			t.Fatal("the shell did not return after canceling lock setup")
		}
	})

	t.Run("configured password locks immediately", func(t *testing.T) {
		state := shellMinimalState(t)
		state.Settings.HasLockPassword = true
		harness := newShellTestHarness(t, state)
		harness.clickID(harness.ui.ids.navLock)
		harness.settle()
		command, ok := findShellCommand(harness.backend.commandLog(), domain.LockApp)
		if !ok || !harness.ui.lockVisible() || !harness.hasText("GoIM 已锁定") {
			t.Fatalf("lock request state = %+v, command = %+v", harness.ui.lockUI, command)
		}
	})
}

func typePasswordKeys(t *testing.T, harness *shellTestHarness, field ui.ID, value string) {
	t.Helper()
	harness.clickID(field)
	for _, char := range value {
		if err := harness.driver.KeyDown(ui.KeyUnknown, string(char)); err != nil {
			t.Fatal(err)
		}
		if err := harness.driver.KeyUp(ui.KeyUnknown, string(char)); err != nil {
			t.Fatal(err)
		}
	}
	if editor := harness.snapshot().Editor; editor == nil || !editor.Secure {
		t.Fatal("password input lost its secure editor")
	}
}

func TestPasswordKeysInSecuritySettingsAndUnlock(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.HasLockPassword = true
	for i := range state.Settings.Shortcuts {
		if state.Settings.Shortcuts[i].Action == shortcutActionQuickFind {
			state.Settings.Shortcuts[i].Keys = "P"
		}
	}
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navSettings)
	h.clickID(h.ui.panels.ids.settingsCategories.At(int(panelSettingsSecurity)))
	fields := &h.ui.panels.settingsExtra
	typePasswordKeys(t, h, fields.passwordOld, "Pass-123!")
	typePasswordKeys(t, h, fields.passwordNew, "Next-456?")
	typePasswordKeys(t, h, fields.passwordRepeat, "Next-456?")
	if err := h.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	change, ok := findShellCommand(h.backend.commandLog(), domain.SetLockPassword)
	if !ok || change.Password != "Pass-123!" || change.Text != "Next-456?" {
		t.Fatal("typed password change did not submit both values")
	}

	state.Locked = true
	state.Revision++
	h.publish(state)
	typePasswordKeys(t, h, h.ui.lockUI.focus, "Next-456?")
	if err := h.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	unlock, ok := findShellCommand(h.backend.commandLog(), domain.UnlockApp)
	if !ok || unlock.Password != "Next-456?" {
		t.Fatal("typed password did not reach the unlock command")
	}
}

func writeShellTestPNG(t *testing.T, name string, width, height int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.SetNRGBA(x, y, color.NRGBA{
				R: uint8(31 + (x*53)%180),
				G: uint8(96 + (y*47)%120),
				B: uint8(221 - (x*31)%130),
				A: 255,
			})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func slicesEqualBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
