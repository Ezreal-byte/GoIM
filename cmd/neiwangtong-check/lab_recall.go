package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"goim/internal/core"
	"goim/internal/domain"
)

func verifyCoreChat(ctx context.Context, opt options) report {
	r := report{Mode: "sandbox", Target: opt.target.String()}
	ctx, cancel := context.WithTimeout(ctx, opt.timeout)
	defer cancel()
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	runID := hex.EncodeToString(nonce[:])
	profile := "GoIM QA " + runID[:8]
	name := profile
	lab := newLabChecks(opt, profile, runID)
	label, driver, reply := "text", "chat-recall-", "reply"
	if opt.labGroupRecall || opt.labGroupFile || opt.labGroupFolder {
		name = "GoIM QA 群聊 " + runID
		label, driver, reply = "group_text", "group-recall-", "group-reply"
	}
	data := filepath.Join(opt.labDir, "goim-recall")
	node, err := core.New(core.Options{DataDir: data, Name: profile, DownloadDir: filepath.Join(data, "downloads"), Listen: ":0", DisableIPMSG: true})
	if !r.run("starts_goim", func() (string, error) { return "started the production GoIM backend", err }) {
		return r
	}
	defer func() { _ = node.Close() }()

	var peer, chat string
	if !r.run("opens_stock_chat", func() (string, error) {
		_, err := waitCoreState(ctx, node, 16*time.Second, func(s domain.State) bool {
			for _, p := range s.Peers {
				address, _ := netip.ParseAddrPort(p.Address)
				if address.Addr() == opt.target.Addr() && p.Name == opt.expectedName &&
					p.Online && p.Version == opt.expectedVersion && p.SupportsNeiwangtongRecall() {
					peer, r.ReferenceVersion = p.ID, p.Version
					return true
				}
			}
			return false
		})
		if err != nil {
			return "", err
		}
		if opt.labGroupRecall || opt.labGroupFile || opt.labGroupFolder {
			node.Submit(domain.Command{Kind: domain.CreateGroup, Name: name, IDs: []string{peer}})
			if _, err := waitCoreState(ctx, node, 4*time.Second, func(s domain.State) bool {
				for _, group := range s.Groups {
					if group.Name == name && group.Transport == domain.TransportNeiwangtong && slices.Contains(group.Members, peer) {
						chat = "group:" + group.ID
						return true
					}
				}
				return false
			}); err != nil {
				return "", err
			}
			return "the original opened a GoIM-created group", lab.driveFor(ctx, "group-open", name)
		}
		node.Submit(domain.Command{Kind: domain.StartChat, PeerID: peer})
		_, err = waitCoreState(ctx, node, 4*time.Second, func(s domain.State) bool {
			for _, c := range s.Conversations {
				if c.Kind == "direct" && c.PeerID == peer && c.Opened {
					chat = c.ID
					return true
				}
			}
			return false
		})
		return "opened a discovered stock contact", err
	}) {
		return r
	}
	if opt.labGroupFile {
		r.Passed = lab.checkGroupFiles(ctx, &r, &node, peer, chat, name)
		return r
	}
	if opt.labGroupFolder {
		outbound := r.run("group_folder_to_original", func() (string, error) {
			return lab.checkGroupFolderToOriginal(ctx, node, peer, chat, name)
		})
		inbound := r.run("group_folder_from_original", func() (string, error) {
			return lab.checkGroupFolder(ctx, node, peer, chat, name)
		})
		r.Passed = outbound && inbound
		return r
	}
	waitMessage := func(match func(domain.Message) bool) (domain.Message, error) {
		var result domain.Message
		_, err := waitCoreState(ctx, node, 6*time.Second, func(s domain.State) bool {
			for _, m := range s.ChatMessages(chat) {
				if match(m) {
					result = m
					return true
				}
			}
			return false
		})
		return result, err
	}
	send := func(text string) (domain.Message, error) {
		node.Submit(domain.Command{Kind: domain.SendText, ConversationID: chat, Text: text})
		return waitMessage(func(m domain.Message) bool { return m.Text == text && m.Status == "sent" })
	}
	if opt.labEmoticons {
		control := "GoIM 表情 " + runID
		in := r.run("stock_emoticon_received", func() (string, error) {
			if _, err := send(control); err != nil {
				return "", err
			}
			if err := lab.drive(ctx, "chat-emoticon"); err != nil {
				return "", err
			}
			_, err := waitMessage(func(m domain.Message) bool { return m.SenderID == peer && strings.TrimSpace(m.Text) == "🙂" })
			if err != nil {
				var symbols []rune
				for _, m := range node.Snapshot().ChatMessages(chat) {
					if m.SenderID == peer {
						for _, value := range m.Text {
							if value > 0xffff {
								symbols = append(symbols, value)
							}
						}
					}
				}
				return "", fmt.Errorf("%w; received stock emoji code points: %U", err, symbols)
			}
			return "GoIM interpreted the stock 1251 smile as its own emoji", err
		})
		if !in {
			return r
		}
		out := r.run("stock_emoticon_in_mixed_message", func() (string, error) {
			if _, err := send(control + " 前🙂后"); err != nil {
				return "", err
			}
			return "the original acknowledged and recorded the mixed native text and emoticon",
				lab.drive(ctx, "chat-emoticon-check", runID)
		})
		r.Passed = in && out
		return r
	}
	control := "原文保留 " + runID
	if !r.run(label+"_recall_from_original", func() (string, error) {
		if _, err := send(control); err != nil {
			return "", err
		}
		stock := "原版待撤回 " + runID
		if err := lab.driveFor(ctx, reply, name, stock); err != nil {
			return "", err
		}
		original, err := waitMessage(func(m domain.Message) bool { return m.SenderID == peer && m.Text == stock })
		if err != nil {
			return "", err
		}
		if err := lab.driveFor(ctx, driver+"stock", name, runID); err != nil {
			return "", err
		}
		if _, err := waitMessage(func(m domain.Message) bool { return m.ID == original.ID && m.Recalled && m.Text == "" }); err != nil {
			return "", err
		}
		_, err = waitMessage(func(m domain.Message) bool { return m.Text == control && !m.Recalled })
		return "stock recall replaced exactly its own GoIM chat entry; the control message remained", err
	}) {
		return r
	}
	r.Passed = r.run(label+"_recall_to_original", func() (string, error) {
		target, err := send("GoIM 待撤回 " + runID)
		if err != nil {
			return "", err
		}
		if err := lab.driveFor(ctx, driver+"check", name, runID); err != nil {
			return "", err
		}
		node.Submit(domain.Command{Kind: domain.RecallMessage, ConversationID: chat, MessageID: target.ID})
		if _, err := waitMessage(func(m domain.Message) bool { return m.ID == target.ID && m.Recalled && m.Text == "" }); err != nil {
			return "", err
		}
		return "the original transcript kept the control message and removed the exact recalled body", lab.driveFor(ctx, driver+"gone", name, runID)
	})
	if r.Passed && opt.labGroupRecall {
		room := strings.TrimPrefix(chat, "group:")
		r.Passed = r.run("goim_owner_dismisses_group", func() (string, error) {
			node.Submit(domain.Command{Kind: domain.DissolveGroup, GroupID: room})
			if _, err := waitCoreState(ctx, node, 6*time.Second, func(s domain.State) bool {
				return s.Group(room).Dissolved && len(s.ChatMessages(chat)) > 0
			}); err != nil {
				return "", err
			}
			return "the original closed the dismissed group; GoIM kept its history", lab.driveFor(ctx, "group-await-closed", name)
		})
	}
	return r
}
