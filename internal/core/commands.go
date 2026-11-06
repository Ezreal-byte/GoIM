package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"goim/internal/domain"
)

func (n *Node) execute(c domain.Command) error {
	switch c.Kind {
	case domain.Connect:
		if strings.TrimSpace(c.Address) == "" {
			return errors.New("请输入 IP 或 IP:端口")
		}
		n.Connect(c.Address)
	case domain.StartChat:
		s := n.Snapshot()
		p := s.Peer(c.PeerID)
		if p.ID == s.Self.ID || peerIndex(&s, p.ID) < 0 {
			return errors.New("联系人不存在")
		}
		n.change(func(s *domain.State) { ensureDirect(s, p) })
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
		s := n.Snapshot()
		i := transferIndex(&s, c.TransferID)
		if i < 0 {
			return errors.New("找不到传输任务")
		}
		t := s.Transfers[i]
		return n.retryTransfer(t)
	case domain.RejectTransfer, domain.CancelTransfer:
		s := n.Snapshot()
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
		return n.setProfile(c.Name, c.Value)
	case domain.SetContactGroup:
		n.change(func(s *domain.State) {
			if i := peerIndex(s, c.PeerID); i >= 0 {
				s.Peers[i].ContactGroup = strings.TrimSpace(c.Name)
			}
		})
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
				if err := n.sendFile("", path, true, ""); err != nil {
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
			var out []pending
			for i := range s.Transfers {
				t := &s.Transfers[i]
				if t.FileID != c.FileID || !t.FromShare || t.Direction != "send" || terminalTransfer(t.Status) {
					continue
				}
				t.Revoked = true
				t.Status = "cancelled"
				t.Speed = 0
				t.Error = "分享已撤回"
				if cancel := n.transfers[t.ID]; cancel != nil {
					cancel()
				}
				p.TransferTombstones[t.PeerID+":"+t.ID] = "cancelled"
				for id, d := range p.Outbox {
					if d.Envelope.Type == "file_offer" && d.Envelope.TransferID == t.ID {
						delete(p.Outbox, id)
					}
				}
				out = append(out, pending{PeerID: t.PeerID, Envelope: envelope{Type: "file_status", TransferID: t.ID, Status: "cancelled"}})
			}
			return out
		}); err != nil {
			return err
		}
		n.broadcastCatalog()
	case domain.RefreshShares:
		s := n.Snapshot()
		for _, p := range s.Peers {
			if p.Online {
				_ = n.send(p.ID, envelope{Type: "catalog_request"})
			}
		}
	case domain.GetShared:
		s := n.Snapshot()
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
func (n *Node) sendText(convID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) > 256<<10 {
		return errors.New("消息过长")
	}
	s := n.Snapshot()
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
		return without(g.Members, s.Self.ID), nil
	}
	return []string{c.PeerID}, nil
}
func (n *Node) setting(c domain.Command) error {
	s := n.Snapshot()
	settings := s.Settings
	switch c.Setting {
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
	s := n.Snapshot()
	e := n.profileEnvelope()
	for _, p := range s.Peers {
		if p.Online {
			n.reliable(p.ID, e)
		}
	}
}
