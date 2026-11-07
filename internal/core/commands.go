package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
)

func (n *Node) execute(c domain.Command) error {
	n.mu.Lock()
	locked := n.data.State.Locked
	n.mu.Unlock()
	if locked && c.Kind != domain.UnlockApp && c.Kind != domain.ReportError {
		return nil
	}
	switch c.Kind {
	case domain.Connect:
		if strings.TrimSpace(c.Address) == "" {
			return errors.New("请输入 IP 或 IP:端口")
		}
		n.Connect(c.Address)
	case domain.StartChat:
		s := n.snapshot(false)
		if c.ConversationID != "" {
			conversation := s.Conversation(c.ConversationID)
			g := s.Group(conversation.GroupID)
			if conversation.Kind != "group" || g.ID == "" || g.Left || g.Dissolved {
				return errors.New("群聊已不可用")
			}
			return n.change(func(s *domain.State) {
				if i := convIndex(s, c.ConversationID); i >= 0 {
					s.Conversations[i].Hidden = false
				}
			})
		}
		p := s.Peer(c.PeerID)
		if p.ID == s.Self.ID || peerIndex(&s, p.ID) < 0 {
			return errors.New("联系人不存在")
		}
		return n.change(func(s *domain.State) {
			id := ensureDirect(s, p)
			if i := convIndex(s, id); i >= 0 {
				s.Conversations[i].Hidden = false
			}
		})
	case domain.SendText:
		return n.sendText(c.ConversationID, c.Text)
	case domain.SendFiles:
		n.async(func() {
			for _, path := range c.Paths {
				if err := n.sendFile(c.ConversationID, path, false, ""); err != nil {
					n.report(err)
				}
			}
		})
	case domain.CreateGroup:
		return n.createGroup(c.Name, c.IDs)
	case domain.InviteMembers:
		return n.inviteMembers(c.GroupID, c.IDs)
	case domain.AcceptInvite:
		return n.answerInvite(c.InvitationID, true)
	case domain.RejectInvite:
		return n.answerInvite(c.InvitationID, false)
	case domain.RenameGroup, domain.RemoveMember, domain.LeaveGroup, domain.TransferOwner, domain.DissolveGroup, domain.WithdrawInvite:
		return n.manageGroup(c)
	case domain.SetGroupNickname:
		return n.setGroupNickname(c.GroupID, c.Value)
	case domain.AcceptTransfer:
		path := ""
		if len(c.Paths) > 0 {
			path = c.Paths[0]
		}
		n.startReceive(c.TransferID, path)
	case domain.RetryTransfer:
		s := n.snapshot(false)
		i := transferIndex(&s, c.TransferID)
		if i < 0 {
			return errors.New("找不到传输任务")
		}
		t := s.Transfers[i]
		return n.retryTransfer(t)
	case domain.RejectTransfer, domain.CancelTransfer:
		s := n.snapshot(false)
		i := transferIndex(&s, c.TransferID)
		if i < 0 {
			return errors.New("找不到传输任务")
		}
		t := s.Transfers[i]
		if t.Status == "completed" {
			return nil
		}
		n.mu.Lock()
		if cancel := n.transfers[t.ID]; cancel != nil {
			cancel()
		}
		n.mu.Unlock()
		status := "cancelled"
		if c.Kind == domain.RejectTransfer {
			status = "rejected"
		}
		return n.durable(func(p *persisted) []pending {
			if i := transferIndex(&p.State, t.ID); i >= 0 {
				v := &p.State.Transfers[i]
				if terminalTransfer(v.Status) {
					return nil
				}
				v.Status = status
				v.Speed = 0
				v.UpdatedAt = time.Now()
				if v.Direction == "send" {
					v.Revoked = true
				}
			}
			p.TransferTombstones[t.PeerID+":"+t.ID] = status
			for id, d := range p.Outbox {
				if d.PeerID == t.PeerID && (d.Envelope.Type == "file_offer" || d.Envelope.Type == "file_retry") && d.Envelope.TransferID == t.ID {
					delete(p.Outbox, id)
				}
			}
			return []pending{{PeerID: t.PeerID, Envelope: envelope{Type: "file_status", TransferID: t.ID, Status: status}}}
		})
	case domain.SetSetting:
		return n.setting(c)
	case domain.SetProfile:
		return n.setProfilePixels(c.Name, c.Value, c.AvatarBGRA, c.AvatarSize)
	case domain.SetLockPassword, domain.LockApp, domain.UnlockApp:
		err := n.updateLock(c)
		if c.Request == 0 {
			return err
		}
		result := domain.LockResult{Request: c.Request}
		if err != nil {
			result.Error = err.Error()
		}
		n.mu.Lock()
		n.lockResult = result
		n.mu.Unlock()
		n.signal()
		return nil
	case domain.SetSharePassword:
		return n.updateSharePassword(c.FileID, c.Text)
	case domain.DeleteConversation:
		return n.deleteConversation(c.ConversationID)
	case domain.SetContactGroup, domain.CreateContactGroup, domain.RenameContactGroup, domain.DeleteContactGroup:
		return n.updateContactGroups(c)
	case domain.TogglePin, domain.ToggleMute, domain.MarkRead, domain.SaveDraft:
		n.change(func(s *domain.State) {
			if i := convIndex(s, c.ConversationID); i >= 0 {
				v := &s.Conversations[i]
				switch c.Kind {
				case domain.TogglePin:
					v.Pinned = !v.Pinned
				case domain.ToggleMute:
					v.Muted = !v.Muted
				case domain.MarkRead:
					v.Unread = 0
				case domain.SaveDraft:
					v.Draft = c.Text
				}
			}
		})
	case domain.ShareFiles:
		n.async(func() {
			for _, path := range c.Paths {
				if err := n.shareFile(path, c.Text); err != nil {
					n.report(err)
				}
			}
			n.broadcastCatalog()
		})
	case domain.RemoveShare:
		if err := n.durable(func(p *persisted) []pending {
			s := &p.State
			if i := fileIndex(s, c.FileID); i < 0 || s.Files[i].SenderID != s.Self.ID {
				return nil
			} else {
				s.Files[i].Shared = false
			}
			delete(p.BrowserShares, c.FileID)
			return n.revokeShareGrants(p, c.FileID, "分享已撤回")
		}); err != nil {
			return err
		}
		n.broadcastCatalog()
	case domain.RefreshShares:
		s := n.snapshot(false)
		for _, p := range s.Peers {
			if p.Online {
				_ = n.send(p.ID, envelope{Type: "catalog_request"})
			}
		}
	case domain.GetShared:
		s := n.snapshot(false)
		f := s.File(c.FileID)
		if f.ID == "" || !f.Shared {
			return errors.New("分享已失效")
		}
		if f.SenderID == s.Self.ID {
			return errors.New("这是你自己的文件")
		}
		n.reliable(f.SenderID, envelope{Type: "get_shared", File: domain.File{ID: f.ID}})
	case domain.ClearError:
		n.change(func(s *domain.State) { s.Error = ""; s.Notice = "" })
	case domain.ReportError:
		n.change(func(s *domain.State) { s.Error = c.Text })
	default:
		return fmt.Errorf("未知操作 %s", c.Kind)
	}
	return nil
}

func (n *Node) updateContactGroups(c domain.Command) error {
	name, old := strings.TrimSpace(c.Name), strings.TrimSpace(c.Text)
	if c.Kind == domain.DeleteContactGroup {
		old = name
		name = ""
	}
	if name == "未分组" && c.Kind == domain.SetContactGroup {
		name = ""
	}
	if utf8.RuneCountInString(name) > 30 || name == "未分组" || strings.ContainsAny(name, "\r\n\x00") ||
		name == "" && (c.Kind == domain.CreateContactGroup || c.Kind == domain.RenameContactGroup) {
		return errors.New("请输入 1 至 30 个字符的分组名称")
	}
	if (c.Kind == domain.RenameContactGroup || c.Kind == domain.DeleteContactGroup) && (old == "" || old == "未分组") {
		return errors.New("不能更改默认分组")
	}
	var validation error
	err := n.change(func(s *domain.State) {
		known := func(group string) bool {
			if contains(s.ContactGroups, group) {
				return true
			}
			for _, p := range s.Peers {
				if p.ContactGroup == group {
					return true
				}
			}
			return false
		}
		if name != "" && !known(name) && len(s.ContactGroups) >= 64 && c.Kind != domain.RenameContactGroup {
			validation = errors.New("联系人分组已达 64 个")
			return
		}
		add := func(group string) {
			if group != "" && !contains(s.ContactGroups, group) {
				s.ContactGroups = append(s.ContactGroups, group)
			}
		}
		switch c.Kind {
		case domain.SetContactGroup:
			if i := peerIndex(s, c.PeerID); i >= 0 {
				add(name)
				s.Peers[i].ContactGroup = name
			}
		case domain.CreateContactGroup:
			if known(name) {
				validation = errors.New("分组已存在")
				return
			}
			add(name)
		case domain.RenameContactGroup, domain.DeleteContactGroup:
			if !known(old) || old == name {
				return
			}
			if name != "" && known(name) {
				validation = errors.New("分组已存在")
				return
			}
			for i, v := range s.ContactGroups {
				if v == old {
					s.ContactGroups = append(s.ContactGroups[:i], s.ContactGroups[i+1:]...)
					break
				}
			}
			for i := range s.Peers {
				if s.Peers[i].ContactGroup == old {
					s.Peers[i].ContactGroup = name
				}
			}
			add(name)
		}
		s.Error, s.Notice = "", "联系人分组已更新"
	})
	if err != nil {
		return err
	}
	return validation
}
func (n *Node) sendText(convID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) > 256<<10 {
		return errors.New("消息过长")
	}
	s := n.snapshot(false)
	conv := s.Conversation(convID)
	if conv.ID == "" {
		return errors.New("请选择会话")
	}
	targets, err := recipients(s, conv)
	if err != nil {
		return err
	}
	m := domain.Message{ID: newID(), ConversationID: convID, SenderID: s.Self.ID, Kind: "text", Text: text, CreatedAt: time.Now(), Status: "pending"}
	for _, v := range s.GroupNicknames {
		if v.GroupID == conv.GroupID && v.PeerID == s.Self.ID {
			m.Nickname = v.Name
		}
	}
	return n.durable(func(p *persisted) []pending {
		addMessageState(&p.State, m)
		var deliveries []pending
		for _, id := range targets {
			deliveries = append(deliveries, pending{PeerID: id, Envelope: envelope{Type: "message", Message: m}})
		}
		return deliveries
	})
}
func recipients(s domain.State, c domain.Conversation) ([]string, error) {
	if c.Kind == "group" {
		g := s.Group(c.GroupID)
		if g.Dissolved || g.Left || !contains(g.Members, s.Self.ID) {
			return nil, errors.New("你已不在此群聊中")
		}
		peers := without(g.Members, s.Self.ID)
		if len(peers) == 0 {
			return nil, errors.New("等待成员确认邀请后再发送消息或文件")
		}
		return peers, nil
	}
	return []string{c.PeerID}, nil
}
func (n *Node) setting(c domain.Command) error {
	s := n.snapshot(false)
	settings := s.Settings
	switch c.Setting {
	case "background":
		path, err := n.importBackground(c.Value)
		if err != nil {
			return err
		}
		settings.Background = path
	case "background_shade":
		shade, err := strconv.Atoi(c.Value)
		if err != nil || shade < 0 || shade > 100 {
			return errors.New("背景深浅须在 0 到 100 之间")
		}
		settings.BackgroundShade = shade
	case "lock_on_startup":
		if c.Bool && !settings.HasLockPassword {
			return errors.New("请先设置锁定密码")
		}
		settings.LockOnStartup = c.Bool
	case "hide_notification_text":
		settings.HideNotificationText = c.Bool
	case "hide_offline_contacts":
		settings.HideOfflineContacts = c.Bool
	case "disable_auto_discovery":
		settings.DisableAutoDiscovery = c.Bool
	case "auto_accept_images":
		settings.AutoAcceptImages = c.Bool
	case "theme":
		if c.Value != "light" && c.Value != "dark" {
			return errors.New("仅支持明亮和暗黑主题")
		}
		settings.Theme = c.Value
	case "desktop_notifications":
		settings.DesktopNotifications = c.Bool
	case "sound":
		settings.Sound = c.Bool
	case "auto_accept":
		settings.AutoAccept = c.Bool
	case "close_to_tray":
		settings.CloseToTray = c.Bool
	case "download_dir":
		if c.Value == "" {
			return errors.New("请选择保存目录")
		}
		if err := os.MkdirAll(c.Value, 0700); err != nil {
			return err
		}
		f, err := os.CreateTemp(c.Value, ".goim-write-check-*")
		if err != nil {
			return errors.New("此目录不可写: " + err.Error())
		}
		path := f.Name()
		f.Close()
		_ = os.Remove(path)
		settings.DownloadDir = c.Value
	case "duplicate_policy":
		if c.Value != "rename" && c.Value != "ask" {
			return errors.New("无效的同名文件策略")
		}
		settings.DuplicatePolicy = c.Value
	case "send_key":
		if c.Value != "Enter" && c.Value != "Ctrl+Enter" {
			return errors.New("无效的发送快捷键")
		}
		settings.SendKey = c.Value
	case "network":
		if c.Value != "auto" && c.Value != "" {
			if _, err := net.InterfaceByName(c.Value); err != nil {
				return errors.New("网络接口不存在")
			}
		}
		settings.PendingNetwork = ""
		for _, t := range s.Transfers {
			if t.Status == "transferring" || t.Status == "verifying" {
				settings.PendingNetwork = c.Value
				break
			}
		}
		if settings.PendingNetwork == "" {
			settings.Network = c.Value
		}
	case "shortcuts":
		var keys []domain.Shortcut
		if err := json.Unmarshal([]byte(c.Value), &keys); err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, key := range keys {
			v := strings.ToLower(strings.TrimSpace(key.Keys))
			if v != "" {
				id := key.Scope + ":" + v
				if seen[id] {
					return errors.New("快捷键冲突，请选择其他组合")
				}
				seen[id] = true
			}
		}
		settings.Shortcuts = keys
	case "reset_shortcuts":
		settings.Shortcuts = domain.DefaultSettings(settings.DownloadDir).Shortcuts
		settings.SendKey = "Enter"
	default:
		return errors.New("未知设置项")
	}
	n.change(func(s *domain.State) { s.Settings = settings; s.Notice = "已保存"; s.Error = "" })
	return nil
}
func (n *Node) broadcastProfile() {
	s := n.snapshot(false)
	e := n.profileEnvelope()
	for _, p := range s.Peers {
		if p.Online {
			n.reliable(p.ID, e)
		}
	}
}
