package desktop

import (
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
	select {
	case <-harness.driver.Wakes():
	case <-time.After(2 * time.Second):
		harness.t.Fatal("backend update did not wake the shell")
	}
	if err := harness.driver.Pump(); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *shellTestHarness) settle() {
	harness.t.Helper()
	if err := harness.driver.Advance(600 * time.Millisecond); err != nil {
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
	if !harness.hasText("桌面通知") {
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

	if err := harness.driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if len(harness.backend.commandLog()) != 0 {
		t.Fatalf("Enter during IME composition submitted commands: %+v", harness.backend.commandLog())
	}
	if err := harness.driver.Type("你"); err != nil {
		t.Fatal(err)
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
	if len(harness.backend.commandLog()) != 0 {
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
	if len(enterHarness.backend.commandLog()) != 0 {
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
	harness := newShellTestHarness(t, shellTestState(t))
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

func TestImageMessageOpensPreviewDetail(t *testing.T) {
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
	harness := newShellTestHarness(t, state)

	if !harness.hasHit(harness.ui.fileButtonIDs["file-image"]) {
		t.Fatal("image message did not expose a preview target")
	}
	harness.clickID(harness.ui.fileButtonIDs["file-image"])
	harness.settle()
	if harness.ui.drawer != "files" || harness.ui.activeFileID != "file-image" {
		t.Fatal("clicking an image did not open its file detail")
	}
	if !harness.hasText("文件详情") {
		t.Fatal("image detail preview was not shown")
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

func TestIncomingMessagesDoNotShowDeliveryStatus(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	if harness.hasText("已送达") {
		t.Fatal("incoming messages displayed a delivery status")
	}
	if !harness.hasText("已发送") {
		t.Fatal("outgoing messages did not display their status")
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
}
