package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
)

func TestContactDetailsAndAvatarMenuKeepCurrentChat(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].ClientType = domain.ClientNeiwangtong
	state.Peers[0].Transport = domain.TransportNeiwangtong
	state.Peers[0].Signature = "欢迎交流"
	state.Peers = append(state.Peers, domain.Peer{
		ID: "legacy-announcement", Name: "Alice legacy", ClientType: domain.ClientNeiwangtong,
		Transport: domain.TransportIPMSG, Online: true,
	})
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navContacts)
	if !h.hasText("选择联系人或群聊") || h.hasText("Alice legacy") {
		t.Fatal("contacts did not show the selection prompt or hide the legacy announcement")
	}
	requireShellLayout(t, h.snapshot())
	before := len(h.backend.commandLog())
	h.clickID(h.ui.peerRowIDs[state.Peers[0].ID])
	if _, editorVisible := h.chatEditorHit(); h.ui.section != sectionContacts || !h.hasText("联系人资料") ||
		!h.hasText("内网通") || !h.hasText("欢迎交流") || !h.hasText("设备信息") || h.hasText("关联兼容连接") || editorVisible {
		t.Fatal("contact selection did not display the profile without manual routing")
	}
	for _, c := range h.backend.commandLog()[before:] {
		if c.Kind == domain.StartChat || c.Kind == domain.MarkRead {
			t.Fatalf("viewing a profile changed the conversation: %+v", c)
		}
	}
	requireShellLayout(t, h.snapshot())
	if h.hasHit(h.ui.ids.contactCall) {
		t.Fatal("voice action is visible for a contact without voice support")
	}
	h.clickID(h.ui.ids.contactTalk)
	if h.ui.section != sectionMessages || h.ui.selected != "conv-alice" {
		t.Fatalf("send-message action opened %s / %s", h.ui.section, h.ui.selected)
	}
	h.rightClickID(h.ui.ids.chatHeaderAvatar)
	h.clickID(h.ui.ids.shellMenuProfile)
	if h.ui.memberProfile != state.Peers[0].ID || h.ui.selected != "conv-alice" || !h.hasText("内网通") || !h.hasText("设备信息") {
		t.Fatal("avatar menu did not open the shared profile without leaving chat")
	}
	requireShellLayout(t, h.snapshot())
}

func TestContactActionsBelowProfile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		online bool
		font   int
	}{
		{"wide-online", 1440, true, 14},
		{"narrow-offline", 780, false, 14},
		{"large-type", 1000, true, domain.InterfaceFontSizeMaximum},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.InterfaceFontSize = tc.font
			peer := &state.Peers[0]
			peer.Transport, peer.VoiceSupported, peer.Online = domain.TransportGoIM, true, tc.online
			h := newShellTestHarnessAtSize(t, state, tc.width, 960)
			h.clickID(h.ui.ids.navContacts)
			h.clickID(h.ui.peerRowIDs[peer.ID])
			call, hasCall := h.findHit(h.ui.ids.contactCall)
			talk, hasTalk := h.findHit(h.ui.ids.contactTalk)
			if !hasCall || !hasTalk || call.Disabled != !tc.online || talk.Disabled ||
				call.Label != "语音通话" || talk.Label != "发消息" || call.Hint != call.Label || talk.Hint != talk.Label {
				t.Fatalf("unexpected profile actions: call=%+v talk=%+v", call, talk)
			}
			if call.Bounds.Y != talk.Bounds.Y || call.Bounds.Right() >= talk.Bounds.X {
				t.Fatalf("profile actions should sit side by side: %v %v", call.Bounds, talk.Bounds)
			}
			if !h.hasText(call.Label) || !h.hasText(talk.Label) {
				t.Fatal("contact action labels must be visible without hovering")
			}
			var deviceBottom float32
			for _, text := range h.snapshot().Texts {
				if text.Value == peer.Address {
					deviceBottom = text.Bounds.Y + text.Bounds.Height
				}
			}
			if deviceBottom == 0 || call.Bounds.Y <= deviceBottom {
				t.Fatalf("buttons at %v should follow the device details ending at %v", call.Bounds.Y, deviceBottom)
			}
			if tc.online {
				h.clickHit(call)
				if cmd := h.lastCommand(); cmd.Kind != domain.StartVoiceCall || cmd.PeerID != peer.ID {
					t.Fatalf("voice button submitted %+v", cmd)
				}
			}
		})
	}
}

func BenchmarkContactDetailsPaint(b *testing.B) {
	state := domain.State{
		Self:     domain.Peer{ID: "self", Name: "我", Online: true},
		Settings: domain.DefaultSettings(b.TempDir()),
		Revision: 1,
		Peers: []domain.Peer{{
			ID: "reference", Name: "研发联系人", Address: "192.0.2.21:9012",
			Online: true, Presence: domain.PresenceAway, ClientType: domain.ClientNeiwangtong,
			Transport: domain.TransportNeiwangtong, Signature: "有问题随时留言", Version: "3.4.3055",
			Details: domain.ProfileDetails{Company: "研究所", Department: "产品与设计", Phone: "010-5550100", Email: "design@example.com"},
		}},
	}
	shell := New(newShellTestBackend(state))
	defer shell.Close()
	shell.section, shell.contactPeer = sectionContacts, "reference"
	config := shell.Config()
	config.Width, config.Height = 1200, 800
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	shell.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		shell.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}
