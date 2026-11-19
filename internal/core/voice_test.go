package core

import (
	"bytes"
	"testing"
	"time"

	"goim/internal/domain"
)

func waitVoiceCall(t *testing.T, n *Node, check func(domain.VoiceCall) bool) domain.VoiceCall {
	t.Helper()
	var call domain.VoiceCall
	eventually(t, "voice call state", func() bool {
		call = n.Snapshot().VoiceCall
		return check(call)
	})
	return call
}

func TestVoiceCallNegotiatesLocalControlsAndTwoWayAudio(t *testing.T) {
	a, b := testNode(t, "呼叫方"), testNode(t, "接收方")
	connectNodes(t, a, b)
	bID := b.Snapshot().Self.ID
	if peer := a.Snapshot().Peer(bID); !peer.VoiceSupported {
		t.Fatal("connected peer did not advertise voice support")
	}
	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	outgoing := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Direction == domain.VoiceCallOutgoing && call.Status == domain.VoiceCallRinging
	})
	incoming := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.Direction == domain.VoiceCallIncoming && call.Status == domain.VoiceCallRinging &&
			call.ID == outgoing.ID && call.PeerID == a.Snapshot().Self.ID
	})
	if err := a.SendVoiceAudio(outgoing.ID, []byte{1}); err == nil {
		t.Fatal("caller audio was accepted before the call connected")
	}
	if err := b.SendVoiceAudio(incoming.ID, []byte{1}); err == nil {
		t.Fatal("callee audio was accepted before the call connected")
	}

	if err := a.execute(domain.Command{Kind: domain.SetVoiceMuted, CallID: outgoing.ID, Bool: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.execute(domain.Command{Kind: domain.SetVoiceSpeakerMuted, CallID: incoming.ID, Bool: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.execute(domain.Command{Kind: domain.AcceptVoiceCall, CallID: incoming.ID}); err != nil {
		t.Fatal(err)
	}
	activeA := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallActive && !call.ConnectedAt.IsZero()
	})
	activeB := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallActive && !call.ConnectedAt.IsZero()
	})
	if !activeA.Muted || activeA.SpeakerMuted {
		t.Fatalf("caller local controls were overwritten: %+v", activeA)
	}
	if activeB.Muted || !activeB.SpeakerMuted {
		t.Fatalf("callee local controls were overwritten: %+v", activeB)
	}

	fromA := []byte{0x11, 0x22, 0x33}
	fromB := []byte{0x44, 0x55}
	if err := a.SendVoiceAudio(outgoing.ID, fromA); err != nil {
		t.Fatal(err)
	}
	if err := b.SendVoiceAudio(outgoing.ID, fromB); err != nil {
		t.Fatal(err)
	}
	audioA, audioB := a.VoiceAudio(outgoing.ID), b.VoiceAudio(outgoing.ID)
	if audioA == nil || audioB == nil {
		t.Fatal("active calls did not expose their receive streams")
	}
	waitFrame := func(name string, stream <-chan []byte, want []byte) {
		t.Helper()
		select {
		case frame := <-stream:
			if !bytes.Equal(frame, want) {
				t.Fatalf("%s frame = %x, want %x", name, frame, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for %s audio", name)
		}
	}
	waitFrame("callee", audioB, fromA)
	waitFrame("caller", audioA, fromB)

	if err := a.execute(domain.Command{Kind: domain.EndVoiceCall, CallID: outgoing.ID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded && call.Reason == "ended"
	})
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded && call.Reason == "ended"
	})
}

func TestVoiceCallRejectTimeoutAndCapability(t *testing.T) {
	a, b := testNode(t, "呼叫方"), testNode(t, "接收方")
	connectNodes(t, a, b)
	bID := b.Snapshot().Self.ID
	a.change(func(state *domain.State) {
		for i := range state.Peers {
			if state.Peers[i].ID == bID {
				state.Peers[i].VoiceSupported = false
			}
		}
	})
	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err == nil {
		t.Fatal("call to a client without voice support was allowed")
	}
	a.change(func(state *domain.State) {
		for i := range state.Peers {
			if state.Peers[i].ID == bID {
				state.Peers[i].VoiceSupported = true
			}
		}
	})
	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	outgoing := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Direction == domain.VoiceCallOutgoing && call.Status == domain.VoiceCallRinging
	})
	incoming := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Direction == domain.VoiceCallIncoming
	})
	if err := b.execute(domain.Command{Kind: domain.RejectVoiceCall, CallID: incoming.ID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded && call.Reason == "declined"
	})

	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	second := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Status == domain.VoiceCallRinging && call.ID != outgoing.ID
	})
	a.expireVoice(second.ID)
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == second.ID && call.Status == domain.VoiceCallEnded && call.Reason == "no_answer"
	})
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == second.ID && call.Status == domain.VoiceCallEnded
	})
}

func TestVoiceCallEndsWhenPeerDisconnects(t *testing.T) {
	a, b := testNode(t, "呼叫方"), testNode(t, "接收方")
	connectNodes(t, a, b)
	bID := b.Snapshot().Self.ID
	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	outgoing := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Status == domain.VoiceCallRinging
	})
	incoming := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallRinging
	})
	if err := b.execute(domain.Command{Kind: domain.AcceptVoiceCall, CallID: incoming.ID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallActive })
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded && call.Reason == "disconnected"
	})
	if a.VoiceAudio(outgoing.ID) != nil {
		t.Fatal("ended call still exposes an audio stream")
	}
}

func TestVoiceCallsEndAndAreDeclinedWhileAppIsLocked(t *testing.T) {
	a, b := testNode(t, "呼叫方"), testNode(t, "接收方")
	connectNodes(t, a, b)
	bID := b.Snapshot().Self.ID
	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	outgoing := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Direction == domain.VoiceCallOutgoing && call.Status == domain.VoiceCallRinging
	})
	incoming := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Direction == domain.VoiceCallIncoming
	})
	if err := b.execute(domain.Command{Kind: domain.AcceptVoiceCall, CallID: incoming.ID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallActive })
	if err := a.execute(domain.Command{Kind: domain.SetLockPassword, Text: "secret-0123"}); err != nil {
		t.Fatal(err)
	}
	if err := a.execute(domain.Command{Kind: domain.LockApp}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded
	})
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded && call.Reason == "ended"
	})

	aID := a.Snapshot().Self.ID
	if err := b.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: aID}); err != nil {
		t.Fatal(err)
	}
	second := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID != outgoing.ID && call.Status == domain.VoiceCallRinging
	})
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == second.ID && call.Status == domain.VoiceCallEnded && call.Reason == "unavailable"
	})
	if call := a.Snapshot().VoiceCall; call.ID == second.ID {
		t.Fatalf("locked app accepted an incoming call: %+v", call)
	}
}

func TestVoiceCallReportsPeerBusyReasonAndSettingsPersist(t *testing.T) {
	directory := t.TempDir()
	a := testNodeAt(t, "呼叫方", directory)
	b := testNodeAt(t, "接收方", t.TempDir())
	connectNodes(t, a, b)
	bID := b.Snapshot().Self.ID
	if err := a.execute(domain.Command{Kind: domain.StartVoiceCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	outgoing := waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Direction == domain.VoiceCallOutgoing && call.Status == domain.VoiceCallRinging
	})
	_ = waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallRinging
	})
	if err := a.receiveVoiceSignal(bID, domain.VoiceSignal{
		CallID: outgoing.ID, Action: "busy", Reason: "incompatible",
	}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.ID == outgoing.ID && call.Status == domain.VoiceCallEnded && call.Reason == "incompatible"
	})

	for _, setting := range []struct {
		name, value string
	}{
		{"voice_microphone", "mic:studio"},
		{"voice_speaker", "speaker:room"},
		{"voice_microphone_volume", "35"},
		{"voice_speaker_volume", "72"},
	} {
		if err := a.execute(domain.Command{Kind: domain.SetSetting, Setting: setting.name, Value: setting.value}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "呼叫方", directory)
	settings := reopened.Snapshot().Settings
	if settings.VoiceMicrophone != "mic:studio" || settings.VoiceSpeaker != "speaker:room" {
		t.Fatalf("voice devices were not persisted: %+v", settings)
	}
	if settings.VoiceMicrophoneVolume != 35 || settings.VoiceSpeakerVolume != 72 {
		t.Fatalf("voice volumes were not persisted: %+v", settings)
	}
}
