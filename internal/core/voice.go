package core

import (
	"errors"
	"fmt"
	"time"
	"unicode"

	"goim/internal/domain"
)

const (
	voiceRingTimeout = 45 * time.Second
	voiceEndedLinger = 3 * time.Second
	voiceAudioQueue  = 64
)

type voiceSession struct {
	call       domain.VoiceCall
	audio      chan []byte
	ringTimer  *time.Timer
	clearTimer *time.Timer
}

func validVoiceAction(action string) bool {
	switch action {
	case "invite", "ringing", "accept", "reject", "busy", "end":
		return true
	default:
		return false
	}
}

func validVoiceID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func voiceReason(reason string) string {
	switch reason {
	case "declined", "busy", "no_answer", "cancelled", "ended", "disconnected", "unavailable", "incompatible":
		return reason
	default:
		return "ended"
	}
}

func (n *Node) voicePeerLocked(id string) domain.Peer {
	if i := peerIndex(&n.data.State, id); i >= 0 {
		return n.data.State.Peers[i]
	}
	return domain.Peer{}
}

func (n *Node) newVoiceSessionLocked(peer domain.Peer, direction domain.VoiceCallDirection, id string) *voiceSession {
	now := time.Now()
	n.voice = &voiceSession{
		call: domain.VoiceCall{
			ID:         id,
			PeerID:     peer.ID,
			PeerName:   peer.Name,
			PeerAvatar: peer.Avatar,
			Direction:  direction,
			Status:     domain.VoiceCallRinging,
			StartedAt:  now,
		},
		audio: make(chan []byte, voiceAudioQueue),
	}
	n.voice.ringTimer = time.AfterFunc(voiceRingTimeout, func() { n.expireVoice(id) })
	return n.voice
}

func (n *Node) startVoiceCall(peerID string) error {
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return errors.New("应用正在关闭")
	}
	if n.voice != nil && n.voice.call.Live() {
		n.mu.Unlock()
		return errors.New("当前已有语音通话")
	}
	if n.voice != nil {
		n.closeVoiceLocked()
	}
	peer := n.voicePeerLocked(peerID)
	if peer.ID == "" || peer.ID == n.data.State.Self.ID {
		n.mu.Unlock()
		return errors.New("联系人不存在")
	}
	if peer.Transport != domain.TransportGoIM || n.connections[peerID] == nil {
		n.mu.Unlock()
		return errors.New("对方当前不在线，无法发起语音通话")
	}
	if !peer.VoiceSupported {
		n.mu.Unlock()
		return errors.New("对方客户端版本不支持语音通话")
	}
	call := n.newVoiceSessionLocked(peer, domain.VoiceCallOutgoing, newID())
	id := call.call.ID
	n.mu.Unlock()
	n.signal()

	err := n.sendVoiceSignal(peerID, domain.VoiceSignal{
		CallID: id, Action: "invite", Codec: domain.VoiceCodecPCMU,
		SampleRate: domain.VoiceSampleRate,
	})
	if err != nil {
		n.finishVoice(id, "unavailable", false)
		return err
	}
	return nil
}

func (n *Node) acceptVoiceCall(callID string) error {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID {
		n.mu.Unlock()
		return errors.New("语音邀请已失效")
	}
	call := &n.voice.call
	if call.Status == domain.VoiceCallActive {
		n.mu.Unlock()
		return nil
	}
	if call.Status != domain.VoiceCallRinging || call.Direction != domain.VoiceCallIncoming {
		n.mu.Unlock()
		return errors.New("无法接听此语音邀请")
	}
	call.Status = domain.VoiceCallActive
	call.ConnectedAt = time.Now()
	if n.voice.ringTimer != nil {
		n.voice.ringTimer.Stop()
		n.voice.ringTimer = nil
	}
	peer, signal := call.PeerID, domain.VoiceSignal{
		CallID: callID, Action: "accept", Muted: call.Muted, SpeakerMuted: call.SpeakerMuted,
	}
	n.mu.Unlock()
	n.signal()
	if err := n.sendVoiceSignal(peer, signal); err != nil {
		n.finishVoice(callID, "disconnected", true)
		return err
	}
	return nil
}

func (n *Node) rejectVoiceCall(callID string) error {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || n.voice.call.Status != domain.VoiceCallRinging ||
		n.voice.call.Direction != domain.VoiceCallIncoming {
		n.mu.Unlock()
		return nil
	}
	peer := n.voice.call.PeerID
	n.mu.Unlock()
	n.finishVoice(callID, "declined", true)
	_ = n.sendVoiceSignal(peer, domain.VoiceSignal{CallID: callID, Action: "reject", Reason: "declined"})
	return nil
}

func (n *Node) endVoiceCall(callID string) error {
	reason := "ended"
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || !n.voice.call.Live() {
		n.mu.Unlock()
		return nil
	}
	if n.voice.call.Status == domain.VoiceCallRinging {
		reason = "cancelled"
	}
	peer := n.voice.call.PeerID
	n.mu.Unlock()
	n.finishVoice(callID, reason, true)
	_ = n.sendVoiceSignal(peer, domain.VoiceSignal{CallID: callID, Action: "end", Reason: reason})
	return nil
}

func (n *Node) endVoiceBeforeLock() {
	n.mu.Lock()
	callID := ""
	if n.voice != nil && n.voice.call.Live() {
		callID = n.voice.call.ID
	}
	n.mu.Unlock()
	if callID != "" {
		_ = n.endVoiceCall(callID)
	}
}

func (n *Node) setVoiceMuted(callID string, muted bool) error {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || !n.voice.call.Live() {
		n.mu.Unlock()
		return nil
	}
	n.voice.call.Muted = muted
	n.mu.Unlock()
	n.signal()
	return nil
}

func (n *Node) setVoiceSpeakerMuted(callID string, muted bool) error {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || !n.voice.call.Live() {
		n.mu.Unlock()
		return nil
	}
	n.voice.call.SpeakerMuted = muted
	n.mu.Unlock()
	n.signal()
	return nil
}

func (n *Node) receiveVoiceSignal(peer string, signal domain.VoiceSignal) error {
	if !validVoiceAction(signal.Action) || !validVoiceID(signal.CallID) {
		return nil
	}
	signal.Reason = voiceReason(signal.Reason)
	var (
		replies []domain.VoiceSignal
		changed bool
	)
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return nil
	}
	p := n.voicePeerLocked(peer)
	known := p.ID != "" && p.Online && p.Transport == domain.TransportGoIM && p.VoiceSupported
	current := n.voice
	if signal.Action == "invite" {
		if signal.Codec != domain.VoiceCodecPCMU || signal.SampleRate != domain.VoiceSampleRate {
			replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "busy", Reason: "incompatible"})
		} else if n.data.State.Locked {
			replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "busy", Reason: "unavailable"})
		} else if !known {
			replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "busy", Reason: "unavailable"})
		} else if current != nil && current.call.Live() {
			cur := current.call
			switch {
			case cur.ID == signal.CallID && cur.PeerID == peer:
				replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "ringing"})
			case cur.PeerID == peer && cur.Direction == domain.VoiceCallOutgoing &&
				cur.Status == domain.VoiceCallRinging && n.data.State.Self.ID > peer:
				n.closeVoiceLocked()
				n.newVoiceSessionLocked(p, domain.VoiceCallIncoming, signal.CallID)
				changed = true
				replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "ringing"})
			default:
				replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "busy", Reason: "busy"})
			}
		} else {
			if current != nil {
				n.closeVoiceLocked()
			}
			if n.voicePeerLocked(peer).ID == "" {
				replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "busy", Reason: "unavailable"})
			} else {
				n.newVoiceSessionLocked(p, domain.VoiceCallIncoming, signal.CallID)
				changed = true
				replies = append(replies, domain.VoiceSignal{CallID: signal.CallID, Action: "ringing"})
			}
		}
	} else if current != nil && current.call.ID == signal.CallID && current.call.PeerID == peer {
		call := &current.call
		switch signal.Action {
		case "accept":
			if call.Status == domain.VoiceCallRinging && call.Direction == domain.VoiceCallOutgoing {
				call.Status = domain.VoiceCallActive
				call.ConnectedAt = time.Now()
				if current.ringTimer != nil {
					current.ringTimer.Stop()
					current.ringTimer = nil
				}
				changed = true
			}
		case "reject":
			if call.Live() {
				n.endVoiceLocked(voiceReason(signal.Reason))
				changed = true
			}
		case "busy":
			if call.Status == domain.VoiceCallRinging {
				reason := signal.Reason
				if reason == "ended" {
					reason = "busy"
				}
				n.endVoiceLocked(reason)
				changed = true
			}
		case "end":
			if call.Live() {
				n.endVoiceLocked(voiceReason(signal.Reason))
				changed = true
			}
		}
	}
	n.mu.Unlock()
	if changed {
		n.signal()
	}
	for _, reply := range replies {
		_ = n.sendVoiceSignal(peer, reply)
	}
	return nil
}

func (n *Node) sendVoiceSignal(peer string, signal domain.VoiceSignal) error {
	return n.send(peer, envelope{Type: "voice_signal", Voice: signal})
}

func (n *Node) finishVoice(callID, reason string, notifyUI bool) {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || !n.voice.call.Live() {
		n.mu.Unlock()
		return
	}
	n.endVoiceLocked(voiceReason(reason))
	n.mu.Unlock()
	if notifyUI {
		n.signal()
	}
}

func (n *Node) expireVoice(callID string) {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || n.voice.call.Status != domain.VoiceCallRinging {
		n.mu.Unlock()
		return
	}
	peer := n.voice.call.PeerID
	n.endVoiceLocked("no_answer")
	n.mu.Unlock()
	n.signal()
	_ = n.sendVoiceSignal(peer, domain.VoiceSignal{CallID: callID, Action: "end", Reason: "no_answer"})
}

func (n *Node) endVoiceLocked(reason string) {
	if n.voice == nil || !n.voice.call.Live() {
		return
	}
	if n.voice.ringTimer != nil {
		n.voice.ringTimer.Stop()
		n.voice.ringTimer = nil
	}
	if n.voice.audio != nil {
		close(n.voice.audio)
		n.voice.audio = nil
	}
	n.voice.call.Status = domain.VoiceCallEnded
	n.voice.call.Reason = voiceReason(reason)
	n.recordVoiceHistoryLocked(n.voice.call)
	callID := n.voice.call.ID
	n.voice.clearTimer = time.AfterFunc(voiceEndedLinger, func() { n.clearEndedVoice(callID) })
}

func (n *Node) recordVoiceHistoryLocked(call domain.VoiceCall) {
	messageID := "voice:" + call.ID
	for _, message := range n.data.State.Messages {
		if message.ID == messageID {
			return
		}
	}
	state := &n.data.State
	conversationID := directID(state.Self.ID, call.PeerID)
	ensureDirect(state, state.Peer(call.PeerID))
	detail := map[string]string{
		"declined": "通话已拒绝", "busy": "对方忙线", "no_answer": "无人接听",
		"cancelled": "通话已取消", "disconnected": "连接已断开", "unavailable": "对方不可用",
		"incompatible": "客户端不兼容", "ended": "通话已结束",
	}[call.Reason]
	if !call.ConnectedAt.IsZero() {
		seconds := max(int(time.Since(call.ConnectedAt).Seconds()), 0)
		detail = fmt.Sprintf("通话时长 %02d:%02d", seconds/60, seconds%60)
	}
	addMessageState(state, domain.Message{
		ID: messageID, ConversationID: conversationID, SenderID: state.Self.ID,
		Kind: "system", Text: "语音通话 · " + detail, CreatedAt: time.Now(), Status: "delivered",
	})
	state.Revision++
	if err := saveStore(n.db, &n.data); err != nil {
		state.Error = "保存通话记录失败：" + err.Error()
	}
}

func (n *Node) clearEndedVoice(callID string) {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || n.voice.call.Status != domain.VoiceCallEnded {
		n.mu.Unlock()
		return
	}
	n.closeVoiceLocked()
	n.mu.Unlock()
	n.signal()
}

func (n *Node) closeVoiceLocked() {
	if n.voice == nil {
		return
	}
	if n.voice.ringTimer != nil {
		n.voice.ringTimer.Stop()
		n.voice.ringTimer = nil
	}
	if n.voice.clearTimer != nil {
		n.voice.clearTimer.Stop()
		n.voice.clearTimer = nil
	}
	if n.voice.audio != nil {
		close(n.voice.audio)
		n.voice.audio = nil
	}
	n.voice = nil
}

func (n *Node) voicePeerDisconnected(peer string) {
	n.mu.Lock()
	if n.voice == nil || n.voice.call.PeerID != peer || !n.voice.call.Live() {
		n.mu.Unlock()
		return
	}
	n.endVoiceLocked("disconnected")
	n.mu.Unlock()
	n.signal()
}

// VoiceAudio returns the live encoded receive stream for callID. It returns nil
// outside an active call. A closed channel means the call has ended.
func (n *Node) VoiceAudio(callID string) <-chan []byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.voice == nil || n.voice.call.ID != callID || n.voice.call.Status != domain.VoiceCallActive {
		return nil
	}
	return n.voice.audio
}

// SendVoiceAudio forwards one encoded media frame. Audio is deliberately not
// persisted or acknowledged as a chat message; TCP already orders and delivers
// it within the authenticated peer connection.
func (n *Node) SendVoiceAudio(callID string, encoded []byte) error {
	if !validVoiceID(callID) || len(encoded) == 0 || len(encoded) > domain.VoiceMaxEncodedSize {
		return errors.New("无效的语音数据")
	}
	n.mu.Lock()
	if n.voice == nil || n.voice.call.ID != callID || n.voice.call.Status != domain.VoiceCallActive {
		n.mu.Unlock()
		return errors.New("语音通话未接通")
	}
	peer := n.voice.call.PeerID
	n.mu.Unlock()
	payload := append([]byte(nil), encoded...)
	return n.send(peer, envelope{Type: "voice_audio", CallID: callID, Audio: payload})
}

func (n *Node) receiveVoiceAudio(peer string, e envelope) {
	if len(e.Audio) == 0 || len(e.Audio) > domain.VoiceMaxEncodedSize {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.voice == nil || n.voice.call.Status != domain.VoiceCallActive ||
		n.voice.call.ID != e.CallID || n.voice.call.PeerID != peer || n.voice.audio == nil {
		return
	}
	payload := append([]byte(nil), e.Audio...)
	select {
	case n.voice.audio <- payload:
	default:
		// Keep latency bounded: discard the oldest frame rather than growing a
		// lagging playback queue.
		select {
		case <-n.voice.audio:
		default:
		}
		select {
		case n.voice.audio <- payload:
		default:
		}
	}
}
