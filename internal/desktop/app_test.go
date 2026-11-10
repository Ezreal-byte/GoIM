package desktop

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type shellTestBackend struct {
	mu       sync.Mutex
	state    domain.State
	commands []domain.Command
	updates  chan struct{}
}

func newShellTestBackend(state domain.State) *shellTestBackend {
	return &shellTestBackend{
		state:   cloneShellState(state),
		updates: make(chan struct{}, 16),
	}
}

func cloneShellState(state domain.State) domain.State {
	clone := state
	clone.Peers = slices.Clone(state.Peers)
	clone.Conversations = slices.Clone(state.Conversations)
	clone.Messages = slices.Clone(state.Messages)
	clone.Groups = slices.Clone(state.Groups)
	clone.Invitations = slices.Clone(state.Invitations)
	clone.Files = slices.Clone(state.Files)
	clone.Transfers = slices.Clone(state.Transfers)
	clone.GroupNicknames = slices.Clone(state.GroupNicknames)
	clone.Settings.Shortcuts = slices.Clone(state.Settings.Shortcuts)
	for index := range clone.Groups {
		clone.Groups[index].Members = slices.Clone(clone.Groups[index].Members)
		clone.Groups[index].Pending = slices.Clone(clone.Groups[index].Pending)
		clone.Groups[index].Ownership = slices.Clone(clone.Groups[index].Ownership)
	}
	return clone
}

func (backend *shellTestBackend) Snapshot() domain.State {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return cloneShellState(backend.state)
}

func (backend *shellTestBackend) Submit(command domain.Command) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.commands = append(backend.commands, command)
}

func (backend *shellTestBackend) Updates() <-chan struct{} {
	return backend.updates
}

func (backend *shellTestBackend) setState(state domain.State) {
	backend.mu.Lock()
	backend.state = cloneShellState(state)
	backend.mu.Unlock()
	backend.updates <- struct{}{}
}

func (backend *shellTestBackend) commandLog() []domain.Command {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return slices.Clone(backend.commands)
}

func hasCommandKind(commands []domain.Command, kind domain.CommandKind) bool {
	for _, command := range commands {
		if command.Kind == kind {
			return true
		}
	}
	return false
}

type shellTestHarness struct {
	t       *testing.T
	ui      *UI
	backend *shellTestBackend
	driver  *app.Driver
}

func newShellTestHarness(t *testing.T, state domain.State) *shellTestHarness {
	return newShellTestHarnessAtSize(t, state, 1440, 960)
}

func newShellTestHarnessAtSize(t *testing.T, state domain.State, width, height int) *shellTestHarness {
	t.Helper()
	backend := newShellTestBackend(state)
	shell := New(backend)
	config := shell.Config()
	config.Width, config.Height = width, height
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	harness := &shellTestHarness{t: t, ui: shell, backend: backend, driver: driver}
	// Run wakes once after OpenWindow binds the invalidator. Model that same
	// startup lifecycle so results completed before binding are collected.
	shell.invalidator.Invalidate()
	t.Cleanup(shell.Close)
	t.Cleanup(driver.Close)
	return harness
}

func shellTestState(t *testing.T) domain.State {
	t.Helper()
	now := time.Now()
	settings := domain.DefaultSettings(t.TempDir())
	settings.SendKey = "Enter"
	return domain.State{
		Self: domain.Peer{ID: "self", Name: "我", Online: true},
		Peers: []domain.Peer{
			{ID: "peer-a", Name: "Alice", Address: "192.168.1.20:7331", ContactGroup: "产品与设计", Online: true, LastSeen: now},
			{ID: "peer-b", Name: "Bob", Address: "192.168.1.21:7331", ContactGroup: "产品与设计", Online: true, LastSeen: now},
			{ID: "peer-c", Name: "Carol", ContactGroup: "工程团队", Online: false, LastSeen: now.Add(-time.Hour)},
			{ID: "peer-d", Name: "Dave", Online: false, LastSeen: now.Add(-2 * time.Hour)},
		},
		Conversations: []domain.Conversation{
			{ID: "conv-group", Kind: "group", GroupID: "group-1", UpdatedAt: now, Unread: 2, Preview: "Alice：看一下最新预览"},
			{ID: "conv-alice", Kind: "direct", PeerID: "peer-a", Title: "Alice", UpdatedAt: now.Add(-time.Minute), Preview: "晚点确认"},
		},
		Messages: []domain.Message{
			{ID: "message-1", ConversationID: "conv-group", SenderID: "peer-a", Kind: "text", Text: "看一下最新预览。", CreatedAt: now.Add(-3 * time.Minute), Status: "delivered"},
			{ID: "message-2", ConversationID: "conv-group", SenderID: "self", Kind: "text", Text: "收到，我来核对交互细节。", CreatedAt: now.Add(-2 * time.Minute), Status: "sent"},
			{ID: "message-3", ConversationID: "conv-group", SenderID: "self", Kind: "file", Text: "设计素材", FileID: "file-1", CreatedAt: now.Add(-time.Minute), Status: "sent"},
			{ID: "message-4", ConversationID: "conv-alice", SenderID: "peer-a", Kind: "text", Text: "晚点确认。", CreatedAt: now.Add(-time.Minute), Status: "delivered"},
		},
		Groups: []domain.Group{
			{ID: "group-1", Name: "产品讨论组", OwnerID: "self", Members: []string{"self", "peer-a", "peer-b"}, Pending: []string{"peer-c"}},
		},
		Invitations: []domain.Invitation{
			{ID: "invite-1", GroupID: "group-2", GroupName: "工程同步", FromID: "peer-b", FromName: "Bob", Status: "pending", CreatedAt: now.Add(-30 * time.Second)},
		},
		Files: []domain.File{
			{ID: "file-1", Name: "设计素材.zip", Size: 41_943_040, MIME: "application/zip", SenderID: "self", ConversationID: "conv-group", CreatedAt: now.Add(-time.Minute)},
		},
		Transfers: []domain.Transfer{
			{ID: "transfer-1", FileID: "file-1", ConversationID: "conv-group", PeerID: "peer-a", Direction: "send", Status: "transferring", Name: "设计素材.zip", Size: 41_943_040, Done: 30_000_000, Speed: 12_400_000, UpdatedAt: now},
		},
		Settings:      settings,
		ListenAddress: "192.168.1.10:7331",
		Version:       "test",
		Revision:      1,
	}
}

func shellMinimalState(t *testing.T) domain.State {
	t.Helper()
	return domain.State{
		Self:     domain.Peer{ID: "self", Name: "我", Online: true},
		Settings: domain.DefaultSettings(t.TempDir()),
	}
}

func (harness *shellTestHarness) snapshot() app.Snapshot {
	harness.t.Helper()
	return harness.driver.Snapshot()
}

func (harness *shellTestHarness) findHit(id ui.ID) (ui.Hit, bool) {
	harness.t.Helper()
	for _, hit := range harness.snapshot().Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}

func (harness *shellTestHarness) clickID(id ui.ID) {
	harness.t.Helper()
	hit, ok := harness.findHit(id)
	if !ok {
		harness.t.Fatalf("control %v is not on screen", id)
	}
	if err := harness.driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *shellTestHarness) typeInto(id ui.ID, value string) {
	harness.t.Helper()
	harness.clickID(id)
	if err := harness.driver.Type(value); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *shellTestHarness) hasText(value string) bool {
	harness.t.Helper()
	for _, text := range harness.snapshot().Texts {
		if strings.Contains(text.Value, value) {
			return true
		}
	}
	return false
}

func (harness *shellTestHarness) hasHit(id ui.ID) bool {
	harness.t.Helper()
	_, ok := harness.findHit(id)
	return ok
}

func (harness *shellTestHarness) publish(state domain.State) {
	harness.t.Helper()
	harness.backend.setState(state)
	// Media and backend results share a coalescing wake source. A queued wake
	// can predate this publication; observe the revision, not the next token.
	harness.waitFor(func() bool { return harness.ui.state.Revision == state.Revision })
}

func (harness *shellTestHarness) settle() {
	harness.t.Helper()
	if err := harness.driver.Advance(600 * time.Millisecond); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *shellTestHarness) waitFor(predicate func() bool) {
	harness.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !predicate() {
		select {
		case <-harness.driver.Wakes():
			if err := harness.driver.Pump(); err != nil {
				harness.t.Fatal(err)
			}
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(deadline) {
			harness.t.Fatal("timed out waiting for background media")
		}
	}
}

func (harness *shellTestHarness) wake() {
	harness.t.Helper()
	harness.ui.invalidator.Invalidate()
	if err := harness.driver.Pump(); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *shellTestHarness) lastCommand() domain.Command {
	harness.t.Helper()
	commands := harness.backend.commandLog()
	if len(commands) == 0 {
		harness.t.Fatal("no backend command was submitted")
	}
	return commands[len(commands)-1]
}

func requireShellLayout(t *testing.T, snapshot app.Snapshot) {
	t.Helper()
	for _, problem := range vivaheadless.CheckLayout(snapshot) {
		if problem.Kind == vivaheadless.LayoutClipped {
			continue
		}
		t.Errorf("layout %s at %+v for %q: %s", problem.Kind, problem.Bounds, problem.Text, problem.Detail)
	}
}

func TestShellLayoutKeepsRailSidebarAndChatGeometry(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	snapshot := harness.snapshot()

	nav, ok := harness.findHit(harness.ui.ids.navMessages)
	if !ok {
		t.Fatal("messages navigation is missing")
	}
	if nav.Bounds.X < 0 || nav.Bounds.X+nav.Bounds.Width > railWidth+0.01 {
		t.Fatalf("navigation rail is wider than %.0fpx: %+v", railWidth, nav.Bounds)
	}
	search, ok := harness.findHit(harness.ui.ids.sidebarSearch)
	if !ok {
		t.Fatal("sidebar search is missing")
	}
	if search.Bounds.X < railWidth || search.Bounds.X+search.Bounds.Width > railWidth+sidebarWidth {
		t.Fatalf("sidebar search is outside the %.0fpx sidebar: %+v", sidebarWidth, search.Bounds)
	}
	header, ok := harness.findHit(harness.ui.ids.chatHeaderFile)
	if !ok {
		t.Fatal("chat header file control is missing")
	}
	if header.Bounds.X < railWidth+sidebarWidth {
		t.Fatalf("chat header starts inside the sidebar: %+v", header.Bounds)
	}
	if harness.ui.isDark() {
		t.Fatal("default theme is not light")
	}
	if selected, ok := harness.findHit(harness.ui.convRowIDs["conv-group"]); !ok || !selected.Selected {
		t.Fatal("selected conversation row does not expose its selected state")
	}
	requireShellLayout(t, snapshot)
}

func TestMinimumWindowLayoutsRemainClean(t *testing.T) {
	harness := newShellTestHarnessAtSize(t, shellTestState(t), 1000, 700)
	requireShellLayout(t, harness.snapshot())

	harness.clickID(harness.ui.ids.chatHeaderSearch)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.sidebarCreate)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if harness.ui.confirm.open {
		harness.clickID(harness.ui.ids.confirmRun)
		harness.settle()
	}

	harness.clickID(harness.ui.ids.navSettings)
	harness.settle()
	requireShellLayout(t, harness.snapshot())
}

func TestSettingsRoundTripPreservesSelectedConversationAndDraft(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.typeInto(harness.ui.ids.chatEditor, "保留的草稿")
	if got := harness.ui.chatEditor.String(); got != "保留的草稿" {
		t.Fatalf("editor = %q", got)
	}

	harness.clickID(harness.ui.ids.navSettings)
	if harness.ui.section != sectionSettings {
		t.Fatalf("section = %q, want settings", harness.ui.section)
	}
	if !harness.hasText("自定义背景") {
		t.Fatal("settings content did not replace contacts and chat")
	}

	harness.clickID(harness.ui.ids.navTheme)
	if !harness.ui.isDark() {
		t.Fatal("appearance toggle did not apply while settings was open")
	}
	if harness.ui.selected != "conv-group" || harness.ui.chatEditor.String() != "保留的草稿" {
		t.Fatal("appearance toggle cleared conversation state")
	}
	if command := harness.lastCommand(); command.Kind != domain.SetSetting || command.Setting != "theme" || command.Value != "dark" {
		t.Fatalf("theme command = %+v", command)
	}

	harness.clickID(harness.ui.ids.navMessages)
	if harness.ui.section != sectionMessages {
		t.Fatalf("section = %q, want messages", harness.ui.section)
	}
	if harness.ui.selected != "conv-group" {
		t.Fatalf("selected conversation = %q, want conv-group", harness.ui.selected)
	}
	if got := harness.ui.chatEditor.String(); got != "保留的草稿" {
		t.Fatalf("returned editor = %q", got)
	}
	foundDraft := false
	for _, command := range harness.backend.commandLog() {
		if command.Kind == domain.SaveDraft && command.ConversationID == "conv-group" && command.Text == "保留的草稿" {
			foundDraft = true
		}
	}
	if !foundDraft {
		t.Fatal("leaving the conversation did not save its draft")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestContactOnlineFilterAndSearchUsePeerIdentities(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.ids.navContacts)
	if harness.ui.section != sectionContacts {
		t.Fatalf("section = %q", harness.ui.section)
	}

	harness.clickID(harness.ui.ids.filterOnline)
	if !harness.hasHit(harness.ui.peerRowIDs["peer-a"]) || !harness.hasHit(harness.ui.peerRowIDs["peer-b"]) {
		t.Fatal("online contacts are missing")
	}
	if harness.hasHit(harness.ui.peerRowIDs["peer-c"]) || harness.hasHit(harness.ui.peerRowIDs["peer-d"]) {
		t.Fatal("offline contacts remained visible under the online filter")
	}

	harness.typeInto(harness.ui.ids.sidebarSearch, "Ali")
	if !harness.hasHit(harness.ui.peerRowIDs["peer-a"]) {
		t.Fatal("matching contact is missing")
	}
	if harness.hasHit(harness.ui.peerRowIDs["peer-b"]) {
		t.Fatal("non-matching online contact remained visible")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestDirectChatHidesGroupMemberControl(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.peerRowIDs["peer-a"])
	harness.settle()
	if harness.ui.selected != "conv-alice" {
		t.Fatalf("selected conversation = %q, want conv-alice", harness.ui.selected)
	}
	if harness.hasHit(harness.ui.ids.chatHeaderMembers) {
		t.Fatal("direct chat exposed the group-member control")
	}
	if !harness.hasHit(harness.ui.ids.chatEditor) {
		t.Fatal("direct chat did not expose its message editor")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestCreateGroupKeepsSelectionAcrossSearchAndSubmitsCommand(t *testing.T) {
	state := shellMinimalState(t)
	state.Peers = []domain.Peer{
		{ID: "peer-a", Name: "Alice", ContactGroup: "产品与设计", Online: true},
		{ID: "peer-b", Name: "Bob", ContactGroup: "产品与设计", Online: true},
	}
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.sidebarCreate)
	if !harness.ui.compose.open {
		t.Fatal("create-group dialog did not open")
	}

	harness.clickID(harness.ui.groupSelectRows["peer-a"])
	if !harness.ui.compose.selected["peer-a"] {
		t.Fatal("Alice was not selected")
	}
	harness.typeInto(harness.ui.ids.composeSearch, "Bob")
	if !harness.ui.compose.selected["peer-a"] {
		t.Fatal("changing the search cleared an earlier selection")
	}
	harness.clickID(harness.ui.groupSelectRows["peer-b"])
	if len(harness.ui.selectedPeersForCompose()) != 2 {
		t.Fatalf("selected peers = %+v", harness.ui.selectedPeersForCompose())
	}

	harness.clickID(harness.ui.ids.composeCreate)
	command := harness.lastCommand()
	if command.Kind != domain.CreateGroup || command.Name != "Alice、Bob" {
		t.Fatalf("create-group command = %+v", command)
	}
	if !slices.Equal(command.IDs, []string{"peer-a", "peer-b"}) {
		t.Fatalf("create-group IDs = %v", command.IDs)
	}
	if !harness.ui.compose.pending {
		t.Fatal("create-group dialog did not display pending submission")
	}

	state.Groups = append(state.Groups, domain.Group{
		ID: "group-new", Name: "Alice、Bob", OwnerID: "self",
		Members: []string{"self"}, Epoch: 1,
	})
	state.Conversations = append(state.Conversations, domain.Conversation{
		ID: "conv-new", Kind: "group", GroupID: "group-new", Title: "Alice、Bob",
		UpdatedAt: time.Now(),
	})
	state.Revision++
	harness.publish(state)
	if harness.ui.compose.open || harness.ui.compose.pending {
		t.Fatal("create-group dialog did not close after the backend created the group")
	}
	if harness.ui.selected != "conv-new" {
		t.Fatalf("selected conversation = %q after group creation, want conv-new", harness.ui.selected)
	}
}

func TestChatIMEConfirmationDoesNotSendAndEnterSendsText(t *testing.T) {
	state := shellTestState(t)
	state.Conversations = state.Conversations[:1]
	state.Messages = state.Messages[:1]
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.chatEditor)

	// The Driver commits the current candidate on Return, like the native IME.
	if err := harness.driver.Compose("你"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if commands := harness.backend.commandLog(); hasCommandKind(commands, domain.SendText) {
		t.Fatalf("Enter during IME composition submitted commands: %+v", harness.backend.commandLog())
	}
	if got := harness.ui.chatEditor.String(); got != "你" {
		t.Fatalf("committed editor text = %q", got)
	}
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	command := harness.lastCommand()
	if command.Kind != domain.SendText || command.ConversationID != "conv-group" || command.Text != "你" {
		t.Fatalf("send command = %+v", command)
	}
	if got := harness.ui.chatEditor.String(); got != "" {
		t.Fatalf("editor was not cleared after send: %q", got)
	}
}

func TestChatSendKeyModes(t *testing.T) {
	state := shellTestState(t)
	state.Conversations = state.Conversations[:1]
	state.Messages = state.Messages[:1]
	state.Settings.SendKey = "Ctrl+Enter"
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.chatEditor)

	if err := harness.driver.Type("第一行"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if commands := harness.backend.commandLog(); hasCommandKind(commands, domain.SendText) {
		t.Fatalf("Enter sent while Ctrl+Enter mode was active: %+v", harness.backend.commandLog())
	}
	if err := harness.driver.Type("第二行"); err != nil {
		t.Fatal(err)
	}
	if got := harness.ui.chatEditor.String(); got != "第一行\n第二行" {
		t.Fatalf("Ctrl+Enter mode editor = %q", got)
	}
	if err := harness.driver.Key(ui.KeyEnter, ui.ModifierControl); err != nil {
		t.Fatal(err)
	}
	command := harness.lastCommand()
	if command.Kind != domain.SendText || command.Text != "第一行\n第二行" {
		t.Fatalf("Ctrl+Enter send command = %+v", command)
	}

	enterState := shellTestState(t)
	enterState.Conversations = enterState.Conversations[:1]
	enterState.Messages = enterState.Messages[:1]
	enterState.Settings.SendKey = "Enter"
	enterHarness := newShellTestHarness(t, enterState)
	enterHarness.clickID(enterHarness.ui.ids.chatEditor)
	if err := enterHarness.driver.Type("第一行"); err != nil {
		t.Fatal(err)
	}
	if err := enterHarness.driver.Key(ui.KeyEnter, ui.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if commands := enterHarness.backend.commandLog(); hasCommandKind(commands, domain.SendText) {
		t.Fatalf("Shift+Enter sent a message: %+v", enterHarness.backend.commandLog())
	}
	if err := enterHarness.driver.Type("第二行"); err != nil {
		t.Fatal(err)
	}
	if got := enterHarness.ui.chatEditor.String(); got != "第一行\n第二行" {
		t.Fatalf("Shift+Enter editor = %q", got)
	}
}

func TestManualConnectDialogSubmitsAddress(t *testing.T) {
	harness := newShellTestHarness(t, shellMinimalState(t))
	harness.clickID(harness.ui.ids.sidebarConnect)
	if !harness.ui.connectOpen {
		t.Fatal("manual-connect dialog did not open")
	}
	harness.typeInto(harness.ui.ids.connectField, "192.168.1.44:7331")
	harness.clickID(harness.ui.ids.connectSubmit)
	command := harness.lastCommand()
	if command.Kind != domain.Connect || command.Address != "192.168.1.44:7331" {
		t.Fatalf("connect command = %+v", command)
	}
	if harness.ui.connectOpen {
		t.Fatal("manual-connect dialog stayed open after submission")
	}
}

func TestGroupMemberInviteDrawerIsInteractive(t *testing.T) {
	state := shellTestState(t)
	state.Peers = append(state.Peers, domain.Peer{
		ID: "peer-e", Name: "Eve", ContactGroup: "产品与设计", Online: true,
	})
	harness := newShellTestHarness(t, state)

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	if harness.ui.drawer != "members" || !harness.hasText("群聊信息") {
		t.Fatal("member drawer did not open")
	}
	harness.clickID(harness.ui.ids.memberInvite)
	harness.settle()
	if !harness.ui.compose.open || harness.ui.compose.mode != "invite" {
		t.Fatal("member invite did not reuse the group-member picker")
	}
	if harness.hasHit(harness.ui.groupSelectRows["peer-a"]) || harness.hasHit(harness.ui.groupSelectRows["peer-b"]) {
		t.Fatal("existing group members were offered as invite candidates")
	}
	harness.clickID(harness.ui.groupSelectRows["peer-e"])
	harness.clickID(harness.ui.ids.composeCreate)
	command := harness.lastCommand()
	if command.Kind != domain.InviteMembers || command.GroupID != "group-1" || !slices.Equal(command.IDs, []string{"peer-e"}) {
		t.Fatalf("invite command = %+v", command)
	}
}

func TestMemberMenuUsesItsOwnScopeAndRemovesOfflineMember(t *testing.T) {
	state := shellTestState(t)
	state.Groups[0].Members = append(state.Groups[0].Members, "peer-d")
	harness := newShellTestHarness(t, state)

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	harness.clickID(harness.ui.memberMenuIDs["peer-d"])
	harness.settle()
	if harness.ui.memberMenu != "peer-d" {
		t.Fatalf("member menu did not open for peer-d: %q", harness.ui.memberMenu)
	}

	remove, ok := harness.findHit(harness.ui.memberRemoveIDs["peer-d"])
	if !ok {
		for _, hit := range harness.snapshot().Hits {
			if hit.Scope == harness.ui.ids.memberMenuScope {
				t.Logf("menu hit id=%v role=%v bounds=%+v label=%q", hit.ID, hit.Role, hit.Bounds, hit.Label)
			}
		}
		t.Fatal("offline members cannot be removed by the owner")
	}
	if remove.Scope != harness.ui.ids.memberMenuScope {
		t.Fatalf("member menu item scope = %v, want %v", remove.Scope, harness.ui.ids.memberMenuScope)
	}
	if harness.hasHit(harness.ui.memberTransferIDs["peer-d"]) {
		t.Fatal("offline members were offered as ownership-transfer targets")
	}
	harness.clickID(harness.ui.memberRemoveIDs["peer-d"])
	harness.settle()
	harness.clickID(harness.ui.ids.confirmRun)
	command := harness.lastCommand()
	if command.Kind != domain.RemoveMember || command.GroupID != "group-1" || command.PeerID != "peer-d" {
		t.Fatalf("remove-member command = %+v", command)
	}
}

func TestDirtyGroupNameCloseRequiresConfirmation(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	harness.clickID(harness.ui.ids.memberStartRename)
	harness.clickID(harness.ui.ids.memberRename)
	if err := harness.driver.Type("新群名"); err != nil {
		t.Fatal(err)
	}

	harness.clickID(harness.ui.ids.memberClose)
	harness.settle()
	if !harness.ui.confirm.open {
		t.Fatal("closing an edited group name did not ask for confirmation")
	}
	harness.clickID(harness.ui.ids.confirmCancel)
	harness.settle()
	if harness.ui.drawer != "members" || !harness.ui.renameOpen {
		t.Fatal("cancelling the discard confirmation closed the editor")
	}

	harness.clickID(harness.ui.ids.memberClose)
	harness.settle()
	harness.clickID(harness.ui.ids.confirmRun)
	harness.settle()
	if harness.ui.drawer != "" || harness.ui.renameOpen {
		t.Fatal("confirming the discard did not close the member drawer")
	}
}

func TestMemberProfileStartsDirectChat(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	harness.clickID(harness.ui.memberMenuIDs["peer-b"])
	harness.settle()
	harness.clickID(harness.ui.memberInfoIDs["peer-b"])
	harness.settle()
	if harness.ui.memberProfile != "peer-b" || !harness.hasText("成员资料") {
		t.Fatal("member profile did not open from the member menu")
	}
	harness.clickID(harness.ui.ids.memberProfileTalk)
	command := harness.lastCommand()
	if command.Kind != domain.StartChat || command.PeerID != "peer-b" {
		t.Fatalf("profile message command = %+v", command)
	}
	if harness.ui.section != sectionMessages || harness.ui.drawer != "" || harness.ui.memberProfile != "" {
		t.Fatal("profile message action did not return to the message list")
	}
}

func TestChatFilesListAndDetailAreInteractive(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	harness.clickID(harness.ui.ids.chatHeaderFile)
	harness.settle()
	if harness.ui.drawer != "files" || !harness.hasText("聊天文件") {
		t.Fatal("chat files drawer did not open")
	}
	harness.clickID(harness.ui.fileRowIDs["file-1"])
	harness.settle()
	if harness.ui.activeFileID != "file-1" || !harness.hasText("文件详情") {
		t.Fatal("file detail did not open from the file list")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestImageMessageOpensImageViewer(t *testing.T) {
	state := shellTestState(t)
	path := filepath.Join(t.TempDir(), "预览.png")
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
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
	state.Files = append(state.Files, domain.File{
		ID: "file-image", Name: "预览.png", Size: 128, MIME: "image/png",
		SenderID: "peer-a", ConversationID: "conv-group", SourcePath: path, CreatedAt: time.Now(),
	})
	state.Messages = append(state.Messages, domain.Message{
		ID: "message-image", ConversationID: "conv-group", SenderID: "peer-a",
		Kind: "image", FileID: "file-image", CreatedAt: time.Now(), Status: "delivered",
	})
	harness := newChatSurfaceHarness(t, state, 1440, 960)
	harness.waitFor(func() bool { return bitmapReady(harness.ui.fileBitmap(path)) })

	if !harness.hasHit(harness.ui.fileButtonIDs["file-image"]) {
		t.Fatal("image message did not expose a preview target")
	}
	harness.clickID(harness.ui.fileButtonIDs["file-image"])
	harness.settle()
	if !harness.ui.imageViewerOpen() {
		t.Fatalf("clicking an image did not open the image viewer (open %v, selected %q, section %q)",
			harness.ui.chatSurface().viewer.open, harness.ui.selected, harness.ui.section)
	}
	if !harness.hasText("1 / 1") || !harness.hasText("预览.png") {
		t.Fatal("image viewer did not show the counter and filename")
	}
	if !harness.hasHit(harness.ui.chatSurface().ids.viewerClose) {
		t.Fatal("image viewer did not expose its close control")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestFileDropSendsPathsToTheVisibleConversation(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	path := filepath.Join(t.TempDir(), "说明.txt")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	anchor, ok := harness.findHit(harness.ui.ids.chatHeaderFile)
	if !ok {
		t.Fatal("chat header is missing")
	}
	x, y := anchor.Bounds.X+anchor.Bounds.Width/2, anchor.Bounds.Y+anchor.Bounds.Height/2
	accepted, err := harness.driver.Drop(ui.FileDropEvent{Phase: ui.FileDropOver, X: x, Y: y})
	if err != nil {
		t.Fatal(err)
	}
	if !accepted || !harness.ui.dragging {
		t.Fatal("dragging files over the chat was not accepted")
	}
	accepted, err = harness.driver.Drop(ui.FileDropEvent{
		Phase: ui.FileDropDrop, X: x, Y: y, Paths: []string{path},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Fatal("dropping files on the chat was not accepted")
	}
	command := harness.lastCommand()
	if command.Kind != domain.SendFiles || command.ConversationID != "conv-group" ||
		!slices.Equal(command.Paths, []string{path}) {
		t.Fatalf("file-drop command = %+v", command)
	}
}

func TestMessagesDoNotShowSuccessfulDeliveryStatus(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	if harness.hasText("已送达") {
		t.Fatal("incoming messages displayed a delivery status")
	}
	if harness.hasText("已发送") {
		t.Fatal("outgoing messages displayed a sent status")
	}
}

func TestDissolvedGroupKeepsHistoryButCannotSend(t *testing.T) {
	state := shellTestState(t)
	harness := newShellTestHarness(t, state)
	state.Groups[0].Dissolved = true
	state.Revision++
	harness.publish(state)

	if !harness.hasText("群聊已解散") {
		t.Fatal("dissolved group did not explain its read-only state")
	}
	if !harness.hasText("看一下最新预览") {
		t.Fatal("dissolved group lost its local message history")
	}
	if harness.hasHit(harness.ui.ids.chatEditor) || harness.hasHit(harness.ui.ids.chatSend) {
		t.Fatal("dissolved group still exposed a message editor")
	}
	anchor, ok := harness.findHit(harness.ui.ids.chatHeaderFile)
	if !ok {
		t.Fatal("chat header is missing")
	}
	accepted, err := harness.driver.Drop(ui.FileDropEvent{
		Phase: ui.FileDropDrop,
		X:     anchor.Bounds.X + anchor.Bounds.Width/2,
		Y:     anchor.Bounds.Y + anchor.Bounds.Height/2,
		Paths: []string{filepath.Join(t.TempDir(), "closed.txt")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("dissolved group accepted a file drop")
	}
}

func TestBackendUpdateRefreshesOnWake(t *testing.T) {
	state := shellTestState(t)
	harness := newShellTestHarness(t, state)
	if harness.hasText("新消息已经到达") {
		t.Fatal("unexpected text before the backend update")
	}

	state.Messages = append(state.Messages, domain.Message{
		ID: "message-new", ConversationID: "conv-group", SenderID: "peer-a",
		Kind: "text", Text: "新消息已经到达。", CreatedAt: time.Now(), Status: "delivered",
	})
	state.Revision++
	harness.publish(state)
	if !harness.hasText("新消息已经到达") {
		t.Fatal("OnWake snapshot did not reach the painted timeline")
	}
}

func TestDirtyGroupRenameIsScopedAndDiscardedOnNavigation(t *testing.T) {
	state := shellTestState(t)
	state.Groups = append(state.Groups, domain.Group{
		ID: "group-2", Name: "工程同步组", OwnerID: "self",
		Members: []string{"self", "peer-b"},
	})
	state.Conversations = append(state.Conversations, domain.Conversation{
		ID: "conv-group-2", Kind: "group", GroupID: "group-2",
		UpdatedAt: state.Conversations[0].UpdatedAt.Add(-time.Second),
	})
	harness := newShellTestHarness(t, state)

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	harness.clickID(harness.ui.ids.memberStartRename)
	harness.clickID(harness.ui.ids.memberRename)
	if err := harness.driver.Type("A 的未保存群名"); err != nil {
		t.Fatal(err)
	}

	harness.ui.setSection(sectionSettings)
	harness.ui.invalidator.Invalidate()
	waitForShellWake(t, harness)
	if !harness.ui.confirm.open || harness.ui.section != sectionContacts ||
		harness.ui.selected != "conv-group" || !harness.ui.renameOpen {
		t.Fatalf("switching to settings lost the dirty-name guard: confirm=%v section=%q selected=%q rename=%v dirty=%v field=%q",
			harness.ui.confirm.open, harness.ui.section, harness.ui.selected,
			harness.ui.renameOpen, harness.ui.groupRenameDirty(), harness.ui.renameField.Text())
	}
	harness.clickID(harness.ui.ids.confirmCancel)
	harness.settle()

	harness.clickID(harness.ui.ids.memberInvite)
	harness.settle()
	if !harness.ui.confirm.open || harness.ui.compose.open {
		t.Fatal("opening the invite picker discarded a dirty group name without confirmation")
	}
	harness.clickID(harness.ui.ids.confirmCancel)
	harness.settle()

	harness.ui.selectConversation("conv-group-2")
	harness.ui.invalidator.Invalidate()
	waitForShellWake(t, harness)
	if !harness.ui.confirm.open || harness.ui.selected != "conv-group" {
		t.Fatal("navigating to another group bypassed the dirty-name confirmation")
	}
	harness.clickID(harness.ui.ids.confirmRun)
	harness.settle()
	if harness.ui.selected != "conv-group-2" || harness.ui.drawer != "" || harness.ui.renameOpen {
		t.Fatal("confirmed navigation did not reset the old group's rename state")
	}

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	harness.clickID(harness.ui.ids.memberStartRename)
	if got := harness.ui.renameField.Text(); got != "工程同步组" {
		t.Fatalf("group B rename field = %q, want its own name", got)
	}
	harness.clickID(harness.ui.ids.memberSaveRename)
	for _, command := range harness.backend.commandLog() {
		if command.Kind == domain.RenameGroup && command.GroupID == "group-2" {
			t.Fatalf("saving an unchanged group B submitted a stale rename: %+v", command)
		}
	}
}

func TestNonOwnerMemberMenuOpensProfileAndDirectChat(t *testing.T) {
	state := shellTestState(t)
	state.Groups[0].OwnerID = "peer-a"
	harness := newShellTestHarness(t, state)

	harness.clickID(harness.ui.ids.chatHeaderMembers)
	harness.settle()
	harness.clickID(harness.ui.memberMenuIDs["peer-b"])
	harness.settle()
	if harness.ui.memberMenu != "peer-b" || !harness.hasHit(harness.ui.memberInfoIDs["peer-b"]) {
		t.Fatal("an ordinary member could not open another member's actions")
	}
	harness.clickID(harness.ui.memberInfoIDs["peer-b"])
	harness.settle()
	if harness.ui.memberProfile != "peer-b" || !harness.hasText("成员资料") {
		t.Fatal("an ordinary member could not open a member profile")
	}
	harness.clickID(harness.ui.ids.memberProfileTalk)
	command := harness.lastCommand()
	if command.Kind != domain.StartChat || command.PeerID != "peer-b" {
		t.Fatalf("member profile direct-chat command = %+v", command)
	}
	if harness.ui.section != sectionMessages || harness.ui.drawer != "" {
		t.Fatal("member profile direct-chat action did not return to messages")
	}
}

func TestLeaveOrDissolveWarnsAndCancelsOnlyThisGroupsTransfers(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		leave bool
	}{
		{name: "leave", owner: "peer-a", leave: true},
		{name: "dissolve", owner: "self"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := shellTestState(t)
			state.Groups[0].OwnerID = test.owner
			state.Conversations[0].ID = "group:group-1"
			for index := range state.Messages {
				if state.Messages[index].ConversationID == "conv-group" {
					state.Messages[index].ConversationID = "group:group-1"
				}
			}
			state.Files[0].ConversationID = "group:group-1"
			state.Transfers[0].ConversationID = "group:group-1"
			state.Files = append(state.Files, domain.File{
				ID: "file-other", Name: "其他会话.txt", Size: 10,
				SenderID: "self", ConversationID: "conv-alice", CreatedAt: time.Now(),
			})
			state.Transfers = []domain.Transfer{
				{
					ID: "transfer-group", FileID: "file-1", ConversationID: "group:group-1",
					PeerID: "peer-b", Direction: "send", Status: "transferring",
					Name: "设计素材.zip", Size: 100, Done: 40,
				},
				{
					ID: "transfer-other", FileID: "file-other", ConversationID: "conv-alice",
					PeerID: "peer-a", Direction: "send", Status: "transferring",
					Name: "其他会话.txt", Size: 10, Done: 5,
				},
			}
			harness := newShellTestHarness(t, state)
			harness.clickID(harness.ui.ids.chatHeaderMembers)
			harness.settle()
			if test.leave {
				harness.clickID(harness.ui.ids.memberLeave)
			} else {
				harness.clickID(harness.ui.ids.memberDissolve)
			}
			harness.settle()
			if !harness.ui.confirm.open ||
				!harness.hasText("仍有 1 个未完成文件任务（Bob）") ||
				!harness.hasText("其他会话不受影响") {
				t.Fatal("group exit did not warn about active transfers")
			}
			harness.clickID(harness.ui.ids.confirmRun)
			harness.settle()

			canceled := make(map[string]bool)
			left, dissolved := false, false
			for _, command := range harness.backend.commandLog() {
				switch command.Kind {
				case domain.CancelTransfer:
					canceled[command.TransferID] = true
				case domain.LeaveGroup:
					left = true
				case domain.DissolveGroup:
					dissolved = true
				}
			}
			if !canceled["transfer-group"] || canceled["transfer-other"] {
				t.Fatalf("group exit canceled the wrong transfers: %+v", canceled)
			}
			if left != test.leave || dissolved == test.leave {
				t.Fatalf("leave/dissolve commands = leave:%v dissolve:%v", left, dissolved)
			}
		})
	}
}

func TestCreateGroupReconcilesBeforePartialInviteError(t *testing.T) {
	state := shellMinimalState(t)
	state.Peers = []domain.Peer{
		{ID: "peer-a", Name: "Alice", ContactGroup: "产品", Online: true},
		{ID: "peer-b", Name: "Bob", ContactGroup: "产品", Online: true},
	}
	harness := newShellTestHarness(t, state)

	harness.clickID(harness.ui.ids.sidebarCreate)
	harness.clickID(harness.ui.groupSelectRows["peer-a"])
	harness.clickID(harness.ui.groupSelectRows["peer-b"])
	harness.clickID(harness.ui.ids.composeCreate)

	state.Groups = append(state.Groups, domain.Group{
		ID: "group-new", Name: "Alice、Bob", OwnerID: "self",
		Members: []string{"self"}, Pending: []string{"peer-a", "peer-b"}, Epoch: 1,
	})
	state.Conversations = append(state.Conversations, domain.Conversation{
		ID: "conv-new", Kind: "group", GroupID: "group-new", Title: "Alice、Bob",
		UpdatedAt: time.Now(),
	})
	state.Error = "邀请 Alice 失败"
	state.Revision++
	harness.publish(state)

	if harness.ui.compose.open || harness.ui.compose.pending {
		t.Fatal("the create dialog stayed open after the group was created")
	}
	if harness.ui.selected != "conv-new" {
		t.Fatalf("selected conversation = %q after partial success", harness.ui.selected)
	}
	if !harness.hasText("群聊已创建；部分邀请发送失败：邀请 Alice 失败") {
		t.Fatal("the partial invite failure was not shown as a notice")
	}
	var createCount, clearCount int
	for _, command := range harness.backend.commandLog() {
		switch command.Kind {
		case domain.CreateGroup:
			createCount++
		case domain.ClearError:
			clearCount++
		}
	}
	if createCount != 1 || clearCount == 0 {
		t.Fatalf("create/clear commands = %d/%d, want one create and a cleared error", createCount, clearCount)
	}
}

func TestMultiRecipientFileDetailsUseTransferSpecificActions(t *testing.T) {
	state := shellTestState(t)
	state.Conversations[0].ID = "group:group-1"
	for index := range state.Messages {
		if state.Messages[index].ConversationID == "conv-group" {
			state.Messages[index].ConversationID = "group:group-1"
		}
	}
	state.Files[0].ConversationID = "group:group-1"
	state.Transfers = []domain.Transfer{
		{ID: "transfer-a", FileID: "file-1", ConversationID: "group:group-1", PeerID: "peer-a",
			Direction: "send", Status: "completed", Name: "设计素材.zip", Size: 100, Done: 100},
		{ID: "transfer-b", FileID: "file-1", ConversationID: "group:group-1", PeerID: "peer-b",
			Direction: "send", Status: "transferring", Name: "设计素材.zip", Size: 100, Done: 50},
		{ID: "transfer-c", FileID: "file-1", ConversationID: "group:group-1", PeerID: "peer-c",
			Direction: "send", Status: "waiting", Name: "设计素材.zip", Size: 100, Done: 0},
	}
	harness := newShellTestHarnessAtSize(t, state, 1000, 700)

	if !harness.hasText("1 人已接收") || !harness.hasText("发送给 Bob · 发送中 · 50%") {
		t.Fatal("the group file card did not aggregate progress and name the active recipient")
	}
	harness.clickID(harness.ui.ids.chatHeaderFile)
	harness.settle()
	harness.clickID(harness.ui.fileRowIDs["file-1"])
	harness.settle()

	if harness.hasHit(harness.ui.fileCancelIDs["transfer-a"]) ||
		!harness.hasHit(harness.ui.fileCancelIDs["transfer-b"]) ||
		!harness.hasHit(harness.ui.fileCancelIDs["transfer-c"]) {
		_, all := harness.findHit(harness.ui.fileCancelIDs["transfer-a"])
		_, bob := harness.findHit(harness.ui.fileCancelIDs["transfer-b"])
		_, carol := harness.findHit(harness.ui.fileCancelIDs["transfer-c"])
		t.Fatalf("file cancel visibility a=%v b=%v c=%v (active=%+v)",
			all, bob, carol, harness.ui.transfersFor["file-1"])
	}
	harness.clickID(harness.ui.fileCancelIDs["transfer-b"])
	command := harness.lastCommand()
	if command.Kind != domain.CancelTransfer || command.TransferID != "transfer-b" {
		t.Fatalf("recipient-specific cancel = %+v", command)
	}

	harness.clickID(harness.ui.ids.fileBatchCancel)
	harness.settle()
	if !harness.ui.confirm.open || !harness.hasText("2 个文件任务（Bob、Carol）") {
		t.Fatal("batch cancel did not confirm the affected recipients")
	}
	harness.clickID(harness.ui.ids.confirmRun)
	harness.settle()
	canceled := make(map[string]bool)
	for _, command := range harness.backend.commandLog() {
		if command.Kind == domain.CancelTransfer {
			canceled[command.TransferID] = true
		}
	}
	if !canceled["transfer-b"] || !canceled["transfer-c"] || canceled["transfer-a"] {
		t.Fatalf("batch canceled the wrong transfers: %+v", canceled)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestFileDropIsRejectedUnderEveryModalOverlay(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*shellTestHarness)
	}{
		{"profile", func(harness *shellTestHarness) {
			harness.clickID(harness.ui.ids.profileAvatar)
			harness.settle()
		}},
		{"compose", func(harness *shellTestHarness) {
			harness.clickID(harness.ui.ids.sidebarCreate)
			harness.settle()
		}},
		{"connect", func(harness *shellTestHarness) {
			harness.clickID(harness.ui.ids.sidebarConnect)
			harness.settle()
		}},
		{"member profile", func(harness *shellTestHarness) {
			harness.clickID(harness.ui.ids.chatHeaderMembers)
			harness.settle()
			harness.clickID(harness.ui.memberMenuIDs["peer-b"])
			harness.settle()
			harness.clickID(harness.ui.memberInfoIDs["peer-b"])
			harness.settle()
		}},
		{"confirmation", func(harness *shellTestHarness) {
			harness.clickID(harness.ui.ids.chatHeaderMembers)
			harness.settle()
			harness.clickID(harness.ui.ids.memberDissolve)
			harness.settle()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newShellTestHarness(t, shellTestState(t))
			test.setup(harness)
			if !harness.ui.modalOpen() {
				t.Fatal("test setup did not open a modal")
			}
			chat := harness.ui.lastChat
			accepted, err := harness.driver.Drop(ui.FileDropEvent{
				Phase: ui.FileDropDrop,
				X:     chat.X + chat.Width/2,
				Y:     chat.Y + chat.Height/2,
				Paths: []string{filepath.Join(t.TempDir(), "modal.txt")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if accepted || hasCommandKind(harness.backend.commandLog(), domain.SendFiles) {
				t.Fatal("a modal overlay accepted a file drop on the covered chat")
			}
		})
	}
}

func TestShortcutsRespectModalsAndWorkFromSettings(t *testing.T) {
	t.Run("dirty compose", func(t *testing.T) {
		harness := newShellTestHarness(t, shellTestState(t))
		harness.clickID(harness.ui.ids.sidebarCreate)
		harness.settle()
		harness.typeInto(harness.ui.ids.composeName, "未提交群名")
		section := harness.ui.section
		if _, err := harness.driver.ShortcutChord(ui.ModifierControl, ','); err != nil {
			t.Fatal(err)
		}
		if !harness.ui.compose.open || harness.ui.confirm.open || harness.ui.section != section {
			t.Fatal("an application shortcut bypassed the dirty compose modal")
		}
	})

	t.Run("all modal overlays", func(t *testing.T) {
		tests := []struct {
			name   string
			setup  func(*shellTestHarness)
			opened func(*shellTestHarness) bool
		}{
			{"profile", func(harness *shellTestHarness) {
				harness.clickID(harness.ui.ids.profileAvatar)
				harness.settle()
			}, func(harness *shellTestHarness) bool { return harness.ui.profile.open }},
			{"compose", func(harness *shellTestHarness) {
				harness.clickID(harness.ui.ids.sidebarCreate)
				harness.settle()
			}, func(harness *shellTestHarness) bool { return harness.ui.compose.open }},
			{"connect", func(harness *shellTestHarness) {
				harness.clickID(harness.ui.ids.sidebarConnect)
				harness.settle()
			}, func(harness *shellTestHarness) bool { return harness.ui.connectOpen }},
			{"member profile", func(harness *shellTestHarness) {
				harness.clickID(harness.ui.ids.chatHeaderMembers)
				harness.settle()
				harness.clickID(harness.ui.memberMenuIDs["peer-b"])
				harness.settle()
				harness.clickID(harness.ui.memberInfoIDs["peer-b"])
				harness.settle()
			}, func(harness *shellTestHarness) bool { return harness.ui.memberProfile != "" }},
			{"confirmation", func(harness *shellTestHarness) {
				harness.clickID(harness.ui.ids.chatHeaderMembers)
				harness.settle()
				harness.clickID(harness.ui.ids.memberDissolve)
				harness.settle()
			}, func(harness *shellTestHarness) bool { return harness.ui.confirm.open }},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				harness := newShellTestHarness(t, shellTestState(t))
				test.setup(harness)
				section, selected := harness.ui.section, harness.ui.selected
				for _, chord := range []struct {
					modifiers ui.Modifiers
					key       rune
				}{
					{ui.ModifierControl, ','},
					{ui.ModifierControl, 'k'},
					{ui.ModifierControl | ui.ModifierShift, 'n'},
				} {
					if _, err := harness.driver.ShortcutChord(chord.modifiers, chord.key); err != nil {
						t.Fatal(err)
					}
					if !test.opened(harness) || harness.ui.section != section ||
						harness.ui.selected != selected {
						t.Fatalf("shortcut %v bypassed the %s modal", chord, test.name)
					}
				}
			})
		}
	})

	t.Run("quick find and create from settings", func(t *testing.T) {
		harness := newShellTestHarness(t, shellTestState(t))
		harness.clickID(harness.ui.ids.navSettings)
		harness.settle()
		handled, err := harness.driver.ShortcutChord(ui.ModifierControl, 'k')
		if err != nil {
			t.Fatal(err)
		}
		if !handled || harness.ui.section != sectionMessages ||
			harness.snapshot().Focused != harness.ui.ids.sidebarSearch {
			t.Fatal("Ctrl+K did not leave settings and focus quick find")
		}

		harness.clickID(harness.ui.ids.navSettings)
		harness.settle()
		handled, err = harness.driver.ShortcutChord(ui.ModifierControl|ui.ModifierShift, 'n')
		if err != nil {
			t.Fatal(err)
		}
		if !handled || harness.ui.section != sectionMessages ||
			!harness.ui.compose.open || harness.ui.compose.mode != "create" {
			t.Fatal("Ctrl+Shift+N did not open group creation from settings")
		}
	})
}

func TestCtrlEnterDoesNotSendDuringIMEComposition(t *testing.T) {
	state := shellTestState(t)
	state.Settings.SendKey = "Ctrl+Enter"
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.chatEditor)
	if err := harness.driver.Type("第一行"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Key(ui.KeyEnter, ui.ModifierControl); err != nil {
		t.Fatal(err)
	}
	if hasCommandKind(harness.backend.commandLog(), domain.SendText) {
		t.Fatal("Ctrl+Enter submitted text while the IME was composing")
	}
	if err := harness.driver.Type("你"); err != nil {
		t.Fatal(err)
	}
	if got := harness.ui.chatEditor.String(); got != "第一行你" {
		t.Fatalf("committed editor text = %q", got)
	}
	if err := harness.driver.Key(ui.KeyEnter, ui.ModifierControl); err != nil {
		t.Fatal(err)
	}
	command := harness.lastCommand()
	if command.Kind != domain.SendText || command.Text != "第一行你" {
		t.Fatalf("Ctrl+Enter send command = %+v", command)
	}
}

func TestUnreadStaysWhileScrolledAwayOrSearching(t *testing.T) {
	t.Run("scrolled away", func(t *testing.T) {
		state := shellStateWithReadConversation(t)
		now := time.Now()
		for index := 0; index < 30; index++ {
			state.Messages = append(state.Messages, domain.Message{
				ID: "scroll-" + intString(index), ConversationID: "conv-group",
				SenderID: "peer-a", Kind: "text", Text: "历史消息 " + intString(index),
				CreatedAt: now.Add(-time.Duration(30-index) * time.Minute),
			})
		}
		harness := newShellTestHarness(t, state)
		chat := harness.ui.lastChat
		if err := harness.driver.Scroll(ui.ScrollEvent{
			X: chat.X + chat.Width/2, Y: chat.Y + 300, DeltaY: -600, Precise: true,
		}); err != nil {
			t.Fatal(err)
		}
		if scroll := harness.ui.chatView.ScrollState(harness.ui.ids.chatMessages); scroll == nil || scroll.AtEnd() {
			t.Fatal("the test chat did not scroll away from its live edge")
		}

		state.Conversations[0].Unread = 4
		state.Revision++
		harness.publish(state)
		if commands := markReadCommands(harness); len(commands) != 0 {
			t.Fatalf("a scrolled-away conversation was marked read: %+v", commands)
		}

		if err := harness.driver.Scroll(ui.ScrollEvent{
			X: chat.X + chat.Width/2, Y: chat.Y + 300, DeltaY: 5000, Precise: true,
		}); err != nil {
			t.Fatal(err)
		}
		if commands := markReadCommands(harness); len(commands) != 1 {
			t.Fatalf("returning to the live edge did not mark the chat read: %+v", commands)
		}
	})

	t.Run("active search", func(t *testing.T) {
		state := shellStateWithReadConversation(t)
		harness := newShellTestHarness(t, state)
		if _, err := harness.driver.ShortcutChord(ui.ModifierControl, 'f'); err != nil {
			t.Fatal(err)
		}
		if err := harness.driver.Type("核对"); err != nil {
			t.Fatal(err)
		}
		state.Conversations[0].Unread = 2
		state.Revision++
		harness.publish(state)
		if commands := markReadCommands(harness); len(commands) != 0 {
			t.Fatalf("an active conversation filter marked unseen messages read: %+v", commands)
		}
	})
}

func TestRailBadgesUpdateInSettings(t *testing.T) {
	state := shellTestState(t)
	state.Conversations[1].Unread = 3
	state.Transfers = append(state.Transfers, domain.Transfer{
		ID: "transfer-2", FileID: "file-1", ConversationID: "conv-group", PeerID: "peer-b",
		Direction: "send", Status: "verifying", Name: "设计素材.zip", Size: 100, Done: 100,
	})
	harness := newShellTestHarness(t, state)
	harness.clickID(harness.ui.ids.navSettings)
	harness.settle()

	messages, ok := harness.findHit(harness.ui.ids.navMessages)
	if !ok || !strings.Contains(messages.Label, "5 条未读") {
		t.Fatalf("messages rail label = %q, want five unread", messages.Label)
	}
	transfers, ok := harness.findHit(harness.ui.ids.navTransfers)
	if !ok || !strings.Contains(transfers.Label, "2 个文件任务未完成") {
		t.Fatalf("transfers rail label = %q, want two active transfers", transfers.Label)
	}

	state.Conversations[0].Unread = 4
	state.Transfers[1].Status = "completed"
	state.Revision++
	harness.publish(state)
	messages, _ = harness.findHit(harness.ui.ids.navMessages)
	transfers, _ = harness.findHit(harness.ui.ids.navTransfers)
	if !strings.Contains(messages.Label, "7 条未读") ||
		!strings.Contains(transfers.Label, "1 个文件任务未完成") {
		t.Fatalf("settings rail badges did not update: messages=%q transfers=%q",
			messages.Label, transfers.Label)
	}
}

func TestClipboardImagePasteReportsFailuresAndSendsSuccess(t *testing.T) {
	t.Run("driver image success", func(t *testing.T) {
		harness := newShellTestHarness(t, shellTestState(t))
		pngPath := writeExtraTestPNG(t, "clipboard.png")
		data, err := os.ReadFile(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := harness.driver.SetClipboardImage(data); err != nil {
			t.Fatal(err)
		}
		cache := t.TempDir()
		harness.ui.clipboardCacheDir = func() (string, error) { return cache, nil }
		harness.clickID(harness.ui.ids.chatEditor)
		if _, err := harness.driver.ShortcutChord(ui.ModifierControl, 'v'); err != nil {
			t.Fatal(err)
		}
		command := harness.lastCommand()
		if command.Kind != domain.SendFiles || len(command.Paths) != 1 {
			t.Fatalf("clipboard-image command = %+v", command)
		}
		if _, err := os.Stat(command.Paths[0]); err != nil {
			t.Fatalf("clipboard image was not written: %v", err)
		}
	})

	t.Run("read failure", func(t *testing.T) {
		harness := newShellTestHarness(t, shellTestState(t))
		harness.ui.clipboardImage = func() ([]byte, error) {
			return nil, errors.New("unsupported clipboard image")
		}
		harness.clickID(harness.ui.ids.chatEditor)
		if _, err := harness.driver.ShortcutChord(ui.ModifierControl, 'v'); err != nil {
			t.Fatal(err)
		}
		command := harness.lastCommand()
		if command.Kind != domain.ReportError || !strings.Contains(command.Text, "unsupported clipboard image") {
			t.Fatalf("clipboard read failure was silent: %+v", command)
		}
	})

	t.Run("cache failure", func(t *testing.T) {
		harness := newShellTestHarness(t, shellTestState(t))
		pngPath := writeExtraTestPNG(t, "clipboard-cache.png")
		data, err := os.ReadFile(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		notDirectory := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(notDirectory, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		harness.ui.clipboardImage = func() ([]byte, error) { return data, nil }
		harness.ui.clipboardCacheDir = func() (string, error) { return notDirectory, nil }
		harness.clickID(harness.ui.ids.chatEditor)
		if _, err := harness.driver.ShortcutChord(ui.ModifierControl, 'v'); err != nil {
			t.Fatal(err)
		}
		command := harness.lastCommand()
		if command.Kind != domain.ReportError || !strings.Contains(command.Text, "缓存目录") {
			t.Fatalf("clipboard cache failure was silent: %+v", command)
		}
	})
}

func TestDecodeBitmapUsesPremultipliedBGRA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "avatar.png")
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel := color.NRGBA{R: 240, G: 80, B: 12, A: 128}
	source.SetNRGBA(0, 0, pixel)
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

	bitmap, ok := decodeBitmap(path, 512)
	if !ok {
		t.Fatal("decodeBitmap rejected a valid PNG")
	}
	if bitmap.Width != 1 || bitmap.Height != 1 || bitmap.Stride != 4 || len(bitmap.Pix) != 4 {
		t.Fatalf("bitmap geometry = %dx%d stride %d pixels %d", bitmap.Width, bitmap.Height, bitmap.Stride, len(bitmap.Pix))
	}
	red, green, blue, alpha := pixel.RGBA()
	want := []byte{byte(blue >> 8), byte(green >> 8), byte(red >> 8), byte(alpha >> 8)}
	if !slices.Equal(bitmap.Pix, want) {
		t.Fatalf("bitmap pixel = %v, want premultiplied BGRA %v", bitmap.Pix, want)
	}
}

func writePNGHeader(t *testing.T, width, height uint32) string {
	t.Helper()
	chunk := func(name string, payload []byte) []byte {
		data := make([]byte, 0, len(name)+len(payload))
		data = append(data, name...)
		data = append(data, payload...)
		out := make([]byte, 8, 12+len(data))
		binary.BigEndian.PutUint32(out[:4], uint32(len(payload)))
		copy(out[4:8], name)
		out = append(out, payload...)
		var checksum [4]byte
		binary.BigEndian.PutUint32(checksum[:], crc32.ChecksumIEEE(data))
		return append(out, checksum[:]...)
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:4], width)
	binary.BigEndian.PutUint32(header[4:8], height)
	header[8] = 8 // bit depth
	header[9] = 2 // RGB
	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	content = append(content, chunk("IHDR", header)...)
	content = append(content, chunk("IEND", nil)...)
	path := filepath.Join(t.TempDir(), "oversized-header.png")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodeBitmapRejectsOversizedPNGHeaderBeforeDecode(t *testing.T) {
	path := writePNGHeader(t, 100_000, 100_000)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(file)
	file.Close()
	if err != nil {
		t.Fatalf("crafted PNG header is not decodable configuration: %v", err)
	}
	if config.Width != 100_000 || config.Height != 100_000 {
		t.Fatalf("crafted PNG header config = %dx%d", config.Width, config.Height)
	}
	if _, ok := decodeBitmap(path, 512); ok {
		t.Fatal("decodeBitmap accepted a pixel configuration above its safety limit")
	}
}

func TestDecodeBitmapRejectsOversizedCompressedInput(t *testing.T) {
	path := writeExtraTestPNG(t, "large-input.png")
	if err := os.Truncate(path, maxImageFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeBitmap(path, 512); ok {
		t.Fatal("decodeBitmap opened a file above its byte safety limit")
	}
}

func TestShellScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	backend := newShellTestBackend(shellTestState(t))
	shell := New(backend)
	session, err := vivaheadless.Open(shell.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

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
		t.Fatalf("control %v is not on screen", id)
	}
	shot := func(name string) {
		t.Helper()
		if err := session.Wait(700 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}

	shot("group-chat-light.png")
	click(shell.ids.chatHeaderSearch)
	click(shell.ids.chatSearchField)
	if err := session.Driver().Type("zip"); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.sidebarSearch)
	shot("chat-search.png")
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.profileAvatar)
	shot("profile-editor.png")
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.chatEmoji)
	shot("emoji-picker.png")
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.chatEditor)
	if err := session.Driver().Type("@"); err != nil {
		t.Fatal(err)
	}
	shot("mention-autocomplete.png")
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.chatHeaderMembers)
	shot("group-members.png")
	click(shell.memberMenuIDs["peer-a"])
	shot("member-menu.png")
	click(shell.memberInfoIDs["peer-a"])
	shot("member-profile.png")
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.chatHeaderFile)
	shot("chat-files.png")
	click(shell.fileRowIDs["file-1"])
	shot("file-detail.png")
	click(shell.ids.fileClose)
	click(shell.ids.sidebarCreate)
	shot("create-group.png")
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	click(shell.ids.navSettings)
	shot("settings.png")
	session.Close()

	darkState := shellTestState(t)
	darkState.Settings.Theme = "dark"
	darkBackend := newShellTestBackend(darkState)
	darkShell := New(darkBackend)
	darkSession, err := vivaheadless.Open(darkShell.Config(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		darkShell.Close()
		t.Fatal(err)
	}
	t.Cleanup(darkSession.Close)
	if err := darkSession.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := darkSession.Screenshot(filepath.Join(directory, "group-chat-dark.png")); err != nil {
		t.Fatal(err)
	}

	minimumBackend := newShellTestBackend(shellTestState(t))
	minimumShell := New(minimumBackend)
	minimumConfig := minimumShell.Config()
	minimumConfig.Width, minimumConfig.Height = 1000, 700
	minimumSession, err := vivaheadless.Open(minimumConfig, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		minimumShell.Close()
		t.Fatal(err)
	}
	t.Cleanup(minimumSession.Close)
	t.Cleanup(minimumShell.Close)
	clickMinimum := func(id ui.ID) {
		t.Helper()
		for _, hit := range minimumSession.Snapshot().Hits {
			if hit.ID == id {
				if err := minimumSession.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("control %v is not on screen at the minimum window size", id)
	}
	clickMinimum(minimumShell.ids.chatHeaderSearch)
	clickMinimum(minimumShell.ids.chatSearchField)
	if err := minimumSession.Driver().Type("zip"); err != nil {
		t.Fatal(err)
	}
	clickMinimum(minimumShell.ids.sidebarSearch)
	if err := minimumSession.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := minimumSession.Screenshot(filepath.Join(directory, "minimum-window-search.png")); err != nil {
		t.Fatal(err)
	}
}
