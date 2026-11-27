package core

import (
	"bytes"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestVideoCallLifecycleCameraAndFrames(t *testing.T) {
	a, b := testNode(t, "呼叫方"), testNode(t, "接收方")
	connectNodes(t, a, b)
	aID, bID := a.Snapshot().Self.ID, b.Snapshot().Self.ID

	if peer := a.Snapshot().Peer(bID); !peer.VideoSupported {
		t.Fatal("expected connected peer to advertise VideoSupported")
	}

	if err := a.execute(domain.Command{Kind: domain.StartVideoCall, PeerID: bID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool {
		return call.Status == domain.VoiceCallRinging && call.Video && call.Direction == domain.VoiceCallOutgoing
	})
	incoming := waitVoiceCall(t, b, func(call domain.VoiceCall) bool {
		return call.Status == domain.VoiceCallRinging && call.Video && call.Direction == domain.VoiceCallIncoming
	})

	callID := incoming.ID
	if err := b.execute(domain.Command{Kind: domain.AcceptVoiceCall, CallID: callID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallActive })
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallActive })

	// Camera state sync
	if err := a.execute(domain.Command{Kind: domain.SetVideoCameraOff, CallID: callID, Bool: true}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool { return call.PeerCameraOff })
	if !a.Snapshot().VoiceCall.CameraOff {
		t.Fatal("expected local CameraOff to be true")
	}

	// Bidirectional video frame transport
	rxB := b.VideoFrames(callID)
	if rxB == nil {
		t.Fatal("expected non-nil video receive channel on active video call")
	}
	sampleFrame := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3, 4, 0xFF, 0xD9}
	if err := a.SendVideoFrame(callID, sampleFrame); err != nil {
		t.Fatalf("SendVideoFrame: %v", err)
	}
	select {
	case got := <-rxB:
		if !bytes.Equal(got, sampleFrame) {
			t.Fatalf("video frame mismatch: got %x, want %x", got, sampleFrame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for video frame")
	}

	// End call and check history
	if err := b.execute(domain.Command{Kind: domain.EndVoiceCall, CallID: callID}); err != nil {
		t.Fatal(err)
	}
	waitVoiceCall(t, a, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallEnded })
	waitVoiceCall(t, b, func(call domain.VoiceCall) bool { return call.Status == domain.VoiceCallEnded })

	msgs := a.Snapshot().ChatMessages(directID(aID, bID))
	var found bool
	for _, m := range msgs {
		if m.Voice != nil && m.Voice.Video {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected video call history record in %+v", msgs)
	}
}
