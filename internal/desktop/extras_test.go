package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func writeExtraTestPNG(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{R: 31, G: 96, B: 221, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 229, G: 239, B: 255, A: 255})
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

func TestProfileEditorSavesNicknameAndChosenAvatar(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	avatar := writeExtraTestPNG(t, "头像.png")
	if err := harness.driver.QueueOpenFiles([]string{avatar}, nil); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.profileAvatar)
	if !harness.ui.profile.open || !harness.hasText("个人资料") {
		t.Fatal("own avatar did not open the profile editor")
	}
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.ids.profileField)
	if err := harness.driver.SelectText(0, utf8.RuneCountInString(harness.ui.profile.nickname.Text())); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Type("新昵称"); err != nil {
		t.Fatal(err)
	}
	harness.clickID(harness.ui.ids.profileChooseImage)
	if harness.ui.profile.avatar != avatar {
		t.Fatalf("profile avatar = %q, want %q", harness.ui.profile.avatar, avatar)
	}
	request, ok := harness.driver.LastOpenFilesRequest()
	if !ok || request.AllowMultiple || request.Title != "选择头像" ||
		len(request.Filters) == 0 || request.Filters[0].Pattern != "*.png;*.jpg;*.jpeg;*.gif" {
		t.Fatalf("profile image picker request = %+v", request)
	}
	harness.waitFor(func() bool {
		return harness.ui.avatarBitmap(avatar).Width > 1
	})
	if bitmap := harness.ui.avatarBitmap(avatar); bitmap.Width <= 0 {
		t.Fatal("chosen avatar was not decoded outside Paint")
	}

	harness.clickID(harness.ui.ids.profileSave)
	command := harness.lastCommand()
	if command.Kind != domain.SetProfile || command.Name != "新昵称" || command.Value != avatar {
		t.Fatalf("profile command = %+v", command)
	}
	if harness.ui.profile.open {
		t.Fatal("profile editor stayed open after saving")
	}
}

func TestStateAvatarsAreDecodedOnWake(t *testing.T) {
	selfAvatar := writeExtraTestPNG(t, "我的头像.png")
	peerAvatar := writeExtraTestPNG(t, "联系人头像.png")
	harness := newShellTestHarness(t, shellTestState(t))

	state := shellTestState(t)
	state.Self.Avatar = selfAvatar
	state.Peers[0].Avatar = peerAvatar
	harness.publish(state)
	harness.waitFor(func() bool {
		for _, path := range []string{selfAvatar, peerAvatar} {
			bitmap := harness.ui.avatarBitmap(path)
			if bitmap.Width <= 1 || bitmap.Height <= 1 {
				return false
			}
		}
		return true
	})

	for _, path := range []string{selfAvatar, peerAvatar} {
		if bitmap := harness.ui.avatarBitmap(path); bitmap.Width <= 0 || bitmap.Height <= 0 {
			t.Fatalf("avatar %q was not decoded on wake", path)
		}
	}
}

func TestEmojiPickerInsertsUnicodeTextWithoutSending(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.ids.chatEditor)
	harness.clickID(harness.ui.ids.chatEmoji)
	if !harness.ui.emojiOpen || !harness.hasHit(harness.ui.emojiIDs["grin"]) {
		t.Fatal("emoji button did not open the emoji picker")
	}
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.emojiIDs["grin"])
	if got := harness.ui.chatEditor.String(); got != "😀" {
		t.Fatalf("editor after emoji insertion = %q", got)
	}
	if harness.ui.emojiOpen {
		t.Fatal("emoji picker stayed open after selection")
	}
	if cmd := harness.lastCommand(); cmd.Kind != domain.RememberEmoji || cmd.Text != "😀" {
		t.Fatalf("recent emoji was not persisted: %+v", cmd)
	}
	harness.clickID(harness.ui.ids.chatEmoji)
	if !harness.hasText("最近使用") || !harness.hasHit(harness.ui.emojiRecentIDs["grin"]) {
		t.Fatal("recent emoji did not appear when reopening the picker")
	}

	for _, command := range harness.backend.commandLog() {
		if command.Kind == domain.SendText {
			t.Fatalf("emoji selection sent the message: %+v", command)
		}
	}
}

func TestMentionAutocompleteSupportsTypingKeyboardSelectionAndEscape(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.ids.chatEditor)
	if err := harness.driver.Type("@A"); err != nil {
		t.Fatal(err)
	}
	if !harness.ui.mention.open || len(harness.ui.mention.candidates) != 1 ||
		harness.ui.mention.candidates[0].name != "Alice" {
		t.Fatalf("filtered mention candidates = %+v", harness.ui.mention.candidates)
	}
	requireShellLayout(t, harness.snapshot())

	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := harness.ui.chatEditor.String(); got != "@Alice " {
		t.Fatalf("editor after mention selection = %q", got)
	}

	if err := harness.driver.Type("@"); err != nil {
		t.Fatal(err)
	}
	if !harness.ui.mention.open || len(harness.ui.mention.candidates) < 2 {
		t.Fatalf("mention candidates = %+v", harness.ui.mention.candidates)
	}
	if err := harness.driver.Key(ui.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if harness.ui.mention.selected != 1 {
		t.Fatalf("selected mention = %d, want 1", harness.ui.mention.selected)
	}
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := harness.ui.chatEditor.String(); got != "@Alice @Bob " {
		t.Fatalf("editor after keyboard mention = %q", got)
	}

	if err := harness.driver.Type("@"); err != nil {
		t.Fatal(err)
	}
	if !harness.ui.mention.open {
		t.Fatal("typing @ did not reopen mention suggestions")
	}
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if harness.ui.mention.open {
		t.Fatal("Escape did not close mention suggestions")
	}
	if got := harness.ui.chatEditor.String(); got != "@Alice @Bob @" {
		t.Fatalf("Escape changed the editor text: %q", got)
	}

	if err := harness.driver.Type("A"); err != nil {
		t.Fatal(err)
	}
	if !harness.ui.mention.open {
		t.Fatal("typing a mention query did not reopen suggestions")
	}
	if err := harness.driver.Key(ui.KeyEnter, ui.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if got := harness.ui.chatEditor.String(); got != "@Alice @Bob @A\n" {
		t.Fatalf("Shift+Enter selected a mention instead of inserting a line break: %q", got)
	}
	for _, command := range harness.backend.commandLog() {
		if command.Kind == domain.SendText {
			t.Fatalf("Shift+Enter sent a message while mentions were open: %+v", command)
		}
	}

	if err := harness.driver.Type("@B"); err != nil {
		t.Fatal(err)
	}
	beforeComposition := harness.ui.chatEditor.String()
	if err := harness.driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := harness.ui.chatEditor.String(); got != beforeComposition+"ni" {
		t.Fatalf("Enter during IME composition selected a mention: %q", got)
	}
}

func TestGroupNicknameEditAndRenderedNames(t *testing.T) {
	state := shellTestState(t)
	state.GroupNicknames = []domain.GroupNickname{
		{GroupID: "group-1", PeerID: "self", Name: "我的群名"},
		{GroupID: "group-1", PeerID: "peer-a", Name: "艾小A"},
		{GroupID: "group-1", PeerID: "peer-b", Name: "小B"},
	}
	state.Messages[0].Nickname = "快照昵称"
	state.Messages = append(state.Messages, domain.Message{
		ID: "message-group-nickname", ConversationID: "conv-group", SenderID: "peer-b",
		Kind: "text", Text: "群昵称消息", CreatedAt: time.Now(), Status: "delivered",
	})
	harness := newShellTestHarness(t, state)

	if !harness.hasText("快照昵称") {
		t.Fatal("Message.Nickname was not rendered for a group sender")
	}
	if !harness.hasText("小B") {
		t.Fatal("State.GroupNicknames was not rendered for a group sender")
	}

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	if !harness.hasText("艾小A") || !harness.hasText("我的群名") {
		t.Fatal("member list did not use group nicknames")
	}
	harness.clickID(harness.ui.ids.memberStartNickname)
	harness.clickID(harness.ui.ids.memberNickname)
	if err := harness.driver.SelectText(0, utf8.RuneCountInString(harness.ui.nicknameField.Text())); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Type("项目群昵称"); err != nil {
		t.Fatal(err)
	}
	harness.clickID(harness.ui.ids.memberSaveNickname)
	command := harness.lastCommand()
	if command.Kind != domain.SetGroupNickname || command.GroupID != "group-1" ||
		command.Value != "项目群昵称" {
		t.Fatalf("group nickname command = %+v", command)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestInvitationFilteringWithdrawPendingAndReinviteRejected(t *testing.T) {
	state := shellTestState(t)
	for index := range state.Peers {
		if state.Peers[index].ID == "peer-d" {
			state.Peers[index].Online = true
		}
	}
	state.Invitations = append(state.Invitations,
		domain.Invitation{
			ID: "invite-self", GroupID: "group-3", GroupName: "可见邀请", FromID: "peer-a",
			FromName: "Alice", Status: "pending", ToID: "self", CreatedAt: time.Now(),
		},
		domain.Invitation{
			ID: "invite-other", GroupID: "group-4", GroupName: "他人邀请", FromID: "peer-b",
			FromName: "Bob", Status: "pending", ToID: "peer-a", CreatedAt: time.Now(),
		},
		domain.Invitation{
			ID: "invite-pending", GroupID: "group-1", GroupName: "产品讨论组", FromID: "self",
			Status: "pending", ToID: "peer-c", CreatedAt: time.Now(),
		},
		domain.Invitation{
			ID: "invite-rejected", GroupID: "group-1", GroupName: "产品讨论组", FromID: "self",
			Status: "rejected", ToID: "peer-d", CreatedAt: time.Now(),
		},
	)
	harness := newShellTestHarness(t, state)

	if !harness.hasHit(harness.ui.inviteAcceptIDs["invite-1"]) ||
		!harness.hasHit(harness.ui.inviteAcceptIDs["invite-self"]) {
		t.Fatal("received pending invitations addressed to self were not shown")
	}
	for _, hidden := range []string{"invite-other", "invite-pending", "invite-rejected"} {
		if harness.hasHit(harness.ui.inviteAcceptIDs[hidden]) {
			t.Fatalf("outgoing or foreign invitation %q was shown as received", hidden)
		}
	}

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	withdrawID := harness.ui.inviteWithdrawIDs[groupInviteKey("group-1", "peer-c")]
	if !harness.hasHit(withdrawID) {
		t.Fatal("owner cannot withdraw a pending invitation")
	}
	harness.clickID(withdrawID)
	command := harness.lastCommand()
	if command.Kind != domain.WithdrawInvite || command.GroupID != "group-1" ||
		command.PeerID != "peer-c" || command.InvitationID != "invite-pending" {
		t.Fatalf("withdraw command = %+v", command)
	}

	reinviteID := harness.ui.inviteReinviteIDs[groupInviteKey("group-1", "peer-d")]
	if !harness.hasHit(reinviteID) {
		t.Fatal("owner cannot re-invite a contact who rejected an invitation")
	}
	harness.clickID(reinviteID)
	command = harness.lastCommand()
	if command.Kind != domain.InviteMembers || command.GroupID != "group-1" ||
		len(command.IDs) != 1 || command.IDs[0] != "peer-d" {
		t.Fatalf("re-invite command = %+v", command)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestOwnershipInvitationsUseExplicitConsentAndStayOutOfMemberInvites(t *testing.T) {
	for _, action := range []string{"accept", "reject"} {
		t.Run("incoming "+action, func(t *testing.T) {
			state := shellTestState(t)
			state.Invitations = []domain.Invitation{{
				ID: "ownership-in", GroupID: "group-2", GroupName: "工程同步",
				FromID: "peer-b", FromName: "Bob", ToID: "self", Kind: "ownership",
				Status: "pending", CreatedAt: time.Now(),
			}}
			state.Groups = append(state.Groups, domain.Group{
				ID: "group-2", Name: "工程同步", OwnerID: "peer-b",
				Members: []string{"self", "peer-a", "peer-b"},
			})
			harness := newShellTestHarness(t, state)
			if !harness.hasText("提议将群主转让给你") || !harness.hasText("接受转让") {
				t.Fatal("ownership invitation did not explain the ownership transfer")
			}
			if harness.hasText("邀请你加入") {
				t.Fatal("ownership invitation was presented as a join invitation")
			}
			id := harness.ui.inviteAcceptIDs["ownership-in"]
			want := domain.AcceptInvite
			if action == "reject" {
				id = harness.ui.inviteRejectIDs["ownership-in"]
				want = domain.RejectInvite
			}
			harness.clickID(id)
			command := harness.lastCommand()
			if command.Kind != want || command.InvitationID != "ownership-in" {
				t.Fatalf("ownership %s command = %+v", action, command)
			}
		})
	}

	t.Run("outgoing", func(t *testing.T) {
		state := shellTestState(t)
		state.Groups[0].Pending = nil
		state.Invitations = []domain.Invitation{
			{
				ID: "ownership-out", GroupID: "group-1", GroupName: "产品讨论组",
				FromID: "self", ToID: "peer-a", Kind: "ownership", Status: "pending",
				CreatedAt: time.Now(),
			},
			{
				ID: "ownership-rejected", GroupID: "group-1", GroupName: "产品讨论组",
				FromID: "self", ToID: "peer-b", Kind: "ownership", Status: "rejected",
				CreatedAt: time.Now().Add(-time.Minute),
			},
		}
		harness := newShellTestHarness(t, state)
		harness.clickID(harness.ui.ids.chatHeaderMembers)
		harness.settle()

		if !harness.hasText("群主转让给 Alice") ||
			!harness.hasText("等待 Alice 接受群主转让") ||
			!harness.hasText("Bob 已拒绝群主转让") {
			t.Fatal("outgoing ownership invitations were not shown separately with their state")
		}
		if harness.hasText("待确认邀请") || harness.hasText("已拒绝邀请") {
			t.Fatal("ownership invitations leaked into member invitation counts")
		}
		for _, peerID := range []string{"peer-a", "peer-b"} {
			key := groupInviteKey("group-1", peerID)
			if harness.hasHit(harness.ui.inviteWithdrawIDs[key]) ||
				harness.hasHit(harness.ui.inviteReinviteIDs[key]) {
				t.Fatalf("ownership invitation for %s exposed member-invite actions", peerID)
			}
		}
	})
}

func TestJoinInvitationListsCurrentMembers(t *testing.T) {
	state := shellTestState(t)
	state.Groups = append(state.Groups, domain.Group{
		ID: "group-2", Name: "工程同步", OwnerID: "peer-b",
		Members: []string{"peer-a", "peer-b"},
		Pending: []string{"self"},
	})
	state.Invitations = []domain.Invitation{{
		ID: "join-members", GroupID: "group-2", GroupName: "工程同步",
		FromID: "peer-b", FromName: "Bob", ToID: "self", Kind: "join",
		Status: "pending", CreatedAt: time.Now(),
	}}
	harness := newShellTestHarness(t, state)
	if !harness.hasText("当前成员（2）：Alice、Bob") {
		t.Fatal("join invitation did not expose the group's current members")
	}
}

func TestFailedInvitationsOfferRetryOnlineAndOffline(t *testing.T) {
	state := shellTestState(t)
	state.Peers = append(state.Peers, domain.Peer{
		ID: "peer-e", Name: "Eve", ContactGroup: "工程团队", Online: true,
	})
	state.Groups[0].Pending = []string{"peer-c", "peer-e"}
	state.Invitations = []domain.Invitation{
		{
			ID: "failed-c", GroupID: "group-1", GroupName: "产品讨论组",
			FromID: "self", ToID: "peer-c", Kind: "join", Status: "failed",
			CreatedAt: time.Now(),
		},
		{
			ID: "failed-e", GroupID: "group-1", GroupName: "产品讨论组",
			FromID: "self", ToID: "peer-e", Kind: "join", Status: "failed",
			CreatedAt: time.Now(),
		},
	}
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()

	if !harness.hasText("发送失败的邀请") || harness.hasText("待确认邀请") {
		t.Fatal("failed invitations were still counted as pending")
	}
	offlineID := harness.ui.inviteReinviteIDs[groupInviteKey("group-1", "peer-c")]
	onlineID := harness.ui.inviteReinviteIDs[groupInviteKey("group-1", "peer-e")]
	offline, ok := harness.findHit(offlineID)
	if !ok || !offline.Disabled || !strings.Contains(offline.Label, "离线") {
		t.Fatalf("offline failed invitation retry = %+v", offline)
	}
	online, ok := harness.findHit(onlineID)
	if !ok || online.Disabled || online.Label != "重试" {
		t.Fatalf("online failed invitation retry = %+v", online)
	}
	for _, peerID := range []string{"peer-c", "peer-e"} {
		if harness.hasHit(harness.ui.inviteWithdrawIDs[groupInviteKey("group-1", peerID)]) {
			t.Fatalf("failed invitation for %s still offered withdraw", peerID)
		}
	}

	harness.clickID(onlineID)
	command := harness.lastCommand()
	if command.Kind != domain.InviteMembers || command.GroupID != "group-1" ||
		len(command.IDs) != 1 || command.IDs[0] != "peer-e" {
		t.Fatalf("retry command = %+v", command)
	}
}

func TestConfiguredApplicationShortcutReplacesHardcodedDefault(t *testing.T) {
	state := shellTestState(t)
	for index := range state.Settings.Shortcuts {
		if state.Settings.Shortcuts[index].Action == shortcutActionOpenSettings {
			state.Settings.Shortcuts[index].Keys = "Ctrl+Shift+,"
		}
	}
	harness := newShellTestHarness(t, state)

	handled, err := harness.driver.ShortcutChord(ui.ModifierControl, ',')
	if err != nil {
		t.Fatal(err)
	}
	if handled || harness.ui.section == sectionSettings {
		t.Fatal("the replaced Ctrl+, default still opened settings")
	}
	handled, err = harness.driver.ShortcutChord(ui.ModifierControl|ui.ModifierShift, ',')
	if err != nil {
		t.Fatal(err)
	}
	if !handled || harness.ui.section != sectionSettings {
		t.Fatal("configured Ctrl+Shift+, did not open settings")
	}
}

func TestNewInteractionOverlaysRemainCleanAtMinimumWindow(t *testing.T) {
	harness := newShellTestHarnessAtSize(t, shellTestState(t), 1000, 700)

	harness.clickID(harness.ui.ids.profileAvatar)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.chatEmoji)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.chatEditor)
	if err := harness.driver.Type("@"); err != nil {
		t.Fatal(err)
	}
	harness.settle()
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
}
