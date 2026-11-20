package desktop

import (
	"context"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type systemCallInvitation struct {
	callID, name, avatar, accept, decline, body string
	locked, private                             bool
	cancel                                      context.CancelFunc
	failed                                      bool
}

func (r *desktopRuntime) cancelSystemInvitation() {
	if notice := r.systemInvitation; notice != nil {
		r.systemInvitation = nil
		notice.cancel()
	}
}

// Wayland owns popup placement and foreground activation. Give the compositor
// the call actions. Also keep them in GoIM when its notifications are silenced.
func (r *desktopRuntime) syncSystemInvitation(state domain.State, locked bool) bool {
	call := state.VoiceCall
	if !incomingVoiceInvitation(call) || !state.Settings.DesktopNotifications || !useSystemCallInvitation() || r.application == nil {
		r.cancelSystemInvitation()
		return false
	}
	r.ui.inlineVoiceID = call.ID
	private := locked || state.Settings.HideNotificationText
	name, path := voicePeerPresentation(state)
	if private {
		name, path = "GoIM", ""
	}
	accept := i18n.T("接听")
	if locked {
		accept = i18n.T("解锁后接听")
	}
	decline, body := i18n.T("拒绝"), i18n.T("收到一个语音通话邀请")
	if n := r.systemInvitation; n != nil && n.callID == call.ID && n.name == name && n.avatar == path &&
		n.private == private && n.locked == locked && n.accept == accept && n.decline == decline {
		return !n.failed
	}
	r.cancelSystemInvitation()
	ctx, cancel := context.WithCancel(context.Background())
	n := &systemCallInvitation{callID: call.ID, name: name, avatar: path, locked: locked, private: private,
		accept: accept, decline: decline, body: body, cancel: cancel}
	r.systemInvitation = n
	go func() {
		action, err := waitSystemCallAction(ctx, n)
		if ctx.Err() != nil {
			return
		}
		r.application.Post(func() {
			if r.systemInvitation != n {
				return
			}
			if err != nil {
				n.failed = true
				r.ui.invalidator.Invalidate()
				return
			}
			switch action {
			case "accept":
				if n.locked && r.ui.unlockForVoice != nil {
					r.ui.unlockForVoice()
					return
				}
				r.ui.respondToInvitation(n.callID, true)
			case "reject":
				r.ui.respondToInvitation(n.callID, false)
			}
		})
	}()
	return true
}
