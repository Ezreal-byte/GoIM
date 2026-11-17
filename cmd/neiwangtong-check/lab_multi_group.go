package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"goim/internal/core"
	"goim/internal/domain"
)

func verifyMultiGroup(ctx context.Context, opt options) report {
	r := report{Mode: "sandbox", Target: "10.77.0.2:9011, 10.77.0.3:9011"}
	ctx, cancel := context.WithTimeout(ctx, opt.timeout)
	defer cancel()
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	runID := hex.EncodeToString(nonce[:])
	name := "GoIM QA " + runID[:8]
	lab := newLabChecks(opt, name, runID)
	data := filepath.Join(opt.labDir, "goim-multi-profile")
	var node *core.Node
	defer func() {
		if node != nil {
			_ = node.Close()
		}
	}()
	var first, second domain.Peer
	if !r.run("group_discovers_two_originals", func() (string, error) {
		var err error
		node, err = core.New(core.Options{DataDir: data, Name: name, DownloadDir: filepath.Join(data, "downloads"), Listen: ":0", DisableIPMSG: true})
		if err != nil {
			return "", err
		}
		_, err = waitCoreState(ctx, node, 16*time.Second, func(s domain.State) bool {
			for _, p := range s.Peers {
				if !p.Online || p.Transport != domain.TransportNeiwangtong || p.Version != opt.expectedVersion {
					continue
				}
				addr, _ := netip.ParseAddrPort(p.Address)
				switch {
				case addr.Addr() == opt.target.Addr() && p.Name == opt.expectedName:
					first = p
				case addr.Addr().String() == "10.77.0.3" && p.Name == "GOIM-NWT-2":
					second = p
				}
			}
			return first.ID != "" && second.ID != "" && first.ID != second.ID
		})
		if err == nil {
			r.ReferenceVersion = opt.expectedVersion
		}
		return "GoIM discovered both named stock clients without manual addresses", err
	}) {
		return r
	}
	var room string
	if !r.run("group_starts_with_first_original", func() (string, error) {
		var err error
		room, err = lab.checkGoIMGroup(ctx, node, first.ID)
		return "first original opened the group and replied", err
	}) {
		return r
	}
	peer := filepath.Join(opt.labDir, "peer")
	display, displayErr := os.ReadFile(filepath.Join(peer, "display"))
	authority, authorityErr := os.ReadFile(filepath.Join(peer, "xauthority"))
	roomName := node.Snapshot().Group(room).Name
	conversation := "group:" + room
	secondDriver := func(action string, args ...string) error {
		if displayErr != nil || authorityErr != nil {
			return fmt.Errorf("second original's private display did not start (%v, %v)", displayErr, authorityErr)
		}
		return lab.driveWithEnv(ctx, []string{"DISPLAY=unix" + strings.TrimSpace(string(display)), "XAUTHORITY=" + strings.TrimSpace(string(authority)),
			"WINEPREFIX=" + filepath.Join(peer, "wine"), "HOME=" + filepath.Join(peer, "home")}, action, roomName, args...)
	}
	if !r.run("group_adds_second_original", func() (string, error) {
		node.Submit(domain.Command{Kind: domain.InviteMembers, GroupID: room, IDs: []string{second.ID}})
		if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
			return slices.Contains(s.Group(room).Members, second.ID)
		}); err != nil {
			return "", err
		}
		if err := secondDriver("group-open"); err != nil {
			return "", err
		}
		return "the new member opened the existing group in the second original", nil
	}) {
		return r
	}
	exchanged := r.run("group_message_reaches_all_three_clients", func() (string, error) {
		fromGoIM := "群聊三方 GoIM & " + runID
		node.Submit(domain.Command{Kind: domain.SendText, ConversationID: conversation, Text: fromGoIM})
		if err := lab.driveFor(ctx, "group-check", roomName, fromGoIM); err != nil {
			return "", err
		}
		if err := secondDriver("group-check", fromGoIM); err != nil {
			return "", err
		}
		fromFirst, fromSecond := "群聊三方 原版甲 & "+runID, "群聊三方 原版乙 & "+runID
		if err := lab.driveFor(ctx, "group-reply", roomName, fromFirst); err != nil {
			return "", err
		}
		if err := secondDriver("group-check", fromFirst); err != nil {
			return "", err
		}
		if err := secondDriver("group-reply", fromSecond); err != nil {
			return "", err
		}
		if err := lab.driveFor(ctx, "group-check", roomName, fromSecond); err != nil {
			return "", err
		}
		_, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
			messages := s.ChatMessages(conversation)
			match := func(peer, text string) bool {
				return slices.ContainsFunc(messages, func(m domain.Message) bool { return m.SenderID == peer && m.Text == text })
			}
			return match(first.ID, fromFirst) && match(second.ID, fromSecond)
		})
		return "both originals saw GoIM's message and the other original's exact reply; GoIM received both replies under the same group", err
	})
	renamed := exchanged && r.run("goim_group_rename_in_original", func() (string, error) {
		return lab.checkGoIMGroupRename(ctx, node, room, first.ID)
	})
	r.Passed = renamed && r.run("original_member_leaves_goim_group", func() (string, error) {
		name := node.Snapshot().Group(room).Name
		if err := lab.driveFor(ctx, "group-exit", name); err != nil {
			return "", err
		}
		if _, err := waitCoreState(ctx, node, 6*time.Second, func(s domain.State) bool {
			g := s.Group(room)
			return !slices.Contains(g.Members, first.ID) && slices.Contains(g.Members, second.ID) && !g.Dissolved
		}); err != nil {
			return "", err
		}
		return "the original left the group and GoIM kept the other original member", nil
	})
	if r.Passed {
		r.Passed = r.run("goim_owner_removes_original_member", func() (string, error) {
			roomName = node.Snapshot().Group(room).Name
			if err := secondDriver("group-open"); err != nil {
				return "", err
			}
			node.Submit(domain.Command{Kind: domain.RemoveMember, GroupID: room, PeerID: second.ID})
			if _, err := waitCoreState(ctx, node, 6*time.Second, func(s domain.State) bool {
				g := s.Group(room)
				return !slices.Contains(g.Members, second.ID) && !g.Dissolved
			}); err != nil {
				return "", err
			}
			return "the original closed its group after GoIM removed it", secondDriver("group-await-closed")
		})
	}
	return r
}

func (l *labChecks) checkGoIMGroup(ctx context.Context, node *core.Node, peerID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	name := "GoIM QA 群聊 " + l.runID
	var room, conversation string
	node.Submit(domain.Command{Kind: domain.CreateGroup, Name: name, IDs: []string{peerID}})
	if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
		for _, group := range s.Groups {
			if group.Name == name && group.Transport == domain.TransportNeiwangtong && group.OwnerID == s.Self.ID &&
				slices.Contains(group.Members, peerID) && len(group.Pending) == 0 {
				room = group.ID
				break
			}
		}
		for _, chat := range s.Conversations {
			if room != "" && chat.GroupID == room {
				conversation = chat.ID
				return true
			}
		}
		return false
	}); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-open", name); err != nil {
		return "", err
	}
	body := "GoIM 群聊建立 & " + l.runID
	node.Submit(domain.Command{Kind: domain.SendText, ConversationID: conversation, Text: body})
	if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
		return slices.ContainsFunc(s.ChatMessages(conversation), func(m domain.Message) bool {
			return m.Text == body && m.Status == "sent"
		})
	}); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-check", name, body); err != nil {
		return "", err
	}
	reply := "原版回复到 GoIM 群聊 & " + l.runID
	if err := l.driveFor(ctx, "group-reply", name, reply); err != nil {
		return "", err
	}
	if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
		return slices.ContainsFunc(s.ChatMessages(conversation), func(m domain.Message) bool {
			return m.SenderID == peerID && m.Text == reply
		})
	}); err != nil {
		return "", err
	}
	return room, nil
}

func (l *labChecks) checkGoIMGroupRename(ctx context.Context, node *core.Node, room, peerID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	name := node.Snapshot().Group(room).Name
	if err := l.driveFor(ctx, "group-close", name); err != nil {
		return "", err
	}
	updated := name + " 新名"
	node.Submit(domain.Command{Kind: domain.RenameGroup, GroupID: room, Name: updated})
	if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool { return s.Group(room).Name == updated }); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-open", updated); err != nil {
		return "", err
	}
	reply := "原版回复改名后的群聊 & " + l.runID
	if err := l.driveFor(ctx, "group-reply", updated, reply); err != nil {
		return "", err
	}
	_, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
		return slices.ContainsFunc(s.ChatMessages("group:"+room), func(m domain.Message) bool {
			return m.SenderID == peerID && m.Text == reply
		})
	})
	return "the original opened the renamed group and GoIM received a reply in the existing conversation", err
}
