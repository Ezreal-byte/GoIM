package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
	case domain.RenameGroup, domain.RemoveMember, domain.LeaveGroup, domain.TransferOwner, domain.DissolveGroup:
		return n.manageGroup(c)
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
		if t.Direction == "receive" {
			n.startReceive(t.ID, t.Path)
		} else {
			f := s.File(t.FileID)
			if f.SourcePath == "" {
				return errors.New("源文件不可用")
			}
			n.offerFile(t.PeerID, t.ConversationID, f, t.ID)
		}
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
		n.setTransfer(t.ID, status, t.Done, 0, "")
		n.reliable(t.PeerID, envelope{Type: "file_status", TransferID: t.ID, Status: status})
	case domain.SetSetting:
		return n.setting(c)
	case domain.SetProfile:
		name := strings.TrimSpace(c.Name)
		if name == "" || utf8.RuneCountInString(name) > 40 {
			return errors.New("昵称需为 1 至 40 个字符")
		}
		n.change(func(s *domain.State) {
			s.Settings.Nickname = name
			s.Self.Name = name
			if c.Value != "" {
				s.Settings.Avatar = c.Value
				s.Self.Avatar = c.Value
			}
		})
		n.broadcastProfile()
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
		n.change(func(s *domain.State) {
			if i := fileIndex(s, c.FileID); i >= 0 && s.Files[i].SenderID == s.Self.ID {
				s.Files[i].Shared = false
			}
		})
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
	n.change(func(s *domain.State) {
		s.Messages = append(s.Messages, m)
		if i := convIndex(s, convID); i >= 0 {
			s.Conversations[i].Draft = ""
			s.Conversations[i].Preview = text
			s.Conversations[i].UpdatedAt = m.CreatedAt
		}
	})
	for _, id := range targets {
		n.reliable(id, envelope{Type: "message", Message: m})
	}
	return nil
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
		for _, t := range s.Transfers {
			if t.Status == "transferring" || t.Status == "verifying" {
				return errors.New("请等待进行中的传输结束后再切换网络")
			}
		}
		settings.Network = c.Value
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
	n.mu.Lock()
	var connections []*peerConn
	for _, pc := range n.connections {
		connections = append(connections, pc)
	}
	n.mu.Unlock()
	for _, pc := range connections {
		pc.conn.Close()
	}
}
