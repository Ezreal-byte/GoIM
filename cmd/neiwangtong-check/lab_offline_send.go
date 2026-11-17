package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"goim/internal/core"
	"goim/internal/domain"
)

func verifyOfflineSend(ctx context.Context, opt options) report {
	r := report{Mode: "sandbox", Target: opt.target.String()}
	ctx, cancel := context.WithTimeout(ctx, opt.timeout)
	defer cancel()
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	runID := hex.EncodeToString(nonce[:])
	name := "GoIM QA " + runID[:8]
	lab := newLabChecks(opt, name, runID)
	data := filepath.Join(opt.labDir, "goim-offline-profile")
	newNode := func() (*core.Node, error) {
		return core.New(core.Options{DataDir: data, Name: name, DownloadDir: filepath.Join(data, "downloads"),
			Listen: net.JoinHostPort(opt.bind.String(), "0"), DisableDiscovery: true, DisableIPMSG: true})
	}
	var node *core.Node
	defer func() {
		if node != nil {
			_ = node.Close()
		}
	}()
	var peer domain.Peer
	var self, conversation string
	connect := func() {
		node.Submit(domain.Command{Kind: domain.Connect, Transport: domain.TransportNeiwangtong, Address: opt.target.String()})
	}
	if !r.run("core_session_before_original_exits", func() (string, error) {
		var err error
		node, err = newNode()
		if err != nil {
			return "", err
		}
		connect()
		s, err := waitCoreState(ctx, node, 10*time.Second, func(s domain.State) bool {
			for _, p := range s.Peers {
				addr, err := netip.ParseAddrPort(p.Address)
				if err == nil && addr.Addr() == opt.target.Addr() && p.Transport == domain.TransportNeiwangtong && p.Online {
					peer = p
					return true
				}
			}
			return false
		})
		if err != nil {
			return "", err
		}
		if peer.Name != opt.expectedName || peer.Version != opt.expectedVersion {
			return "", errors.New("unexpected stock peer name or version")
		}
		r.ReferenceVersion, self = peer.Version, s.Self.ID
		node.Submit(domain.Command{Kind: domain.StartChat, PeerID: peer.ID})
		_, err = waitCoreState(ctx, node, 3*time.Second, func(s domain.State) bool {
			for _, c := range s.Conversations {
				if c.PeerID == peer.ID && c.Kind == "direct" && c.Opened {
					conversation = c.ID
					return true
				}
			}
			return false
		})
		return "GoIM created the chat with the named original client", err
	}) {
		return r
	}
	body := "GoIM 离线补投 & 中文 " + runID
	filename := "GoIM离线文件-" + runID + ".bin"
	source := filepath.Join(opt.labDir, filename)
	var digest [32]byte
	queued := func(s domain.State) bool {
		if s.Self.ID != self || s.Peer(peer.ID).Online {
			return false
		}
		var message, file bool
		for _, m := range s.ChatMessages(conversation) {
			message = message || (m.Text == body && m.Status == "pending")
		}
		for _, t := range s.Transfers {
			file = file || (t.Name == filename && t.PeerID == peer.ID && t.Direction == "send" && t.RemotePacket == "")
		}
		return message && file
	}
	reopen := func() error {
		if err := node.Close(); err != nil {
			return err
		}
		var err error
		node, err = newNode()
		return err
	}
	if !r.run("queue_and_reopen_goim_while_original_is_closed", func() (string, error) {
		if err := requestReferenceState(ctx, opt.labDir, "stop", "stopped"); err != nil {
			return "", err
		}
		if err := reopen(); err != nil {
			return "", err
		}
		var err error
		if digest, err = makeSample(source, sampleSize); err != nil {
			return "", err
		}
		node.Submit(domain.Command{Kind: domain.SendText, ConversationID: conversation, Text: body})
		node.Submit(domain.Command{Kind: domain.SendFiles, ConversationID: conversation, Paths: []string{source}})
		if _, err := waitCoreState(ctx, node, 5*time.Second, queued); err != nil {
			return "", err
		}
		if err := reopen(); err != nil {
			return "", err
		}
		if !queued(node.Snapshot()) {
			return "", errors.New("GoIM's pending text and file were not recovered from its profile")
		}
		return "GoIM recovered its pending text and unsent file offer while the original was still closed", nil
	}) {
		return r
	}
	if !r.run("goim_offline_text_acknowledged_by_original", func() (string, error) {
		if err := requestReferenceState(ctx, opt.labDir, "start", "started"); err != nil {
			return "", err
		}
		connect()
		if _, err := waitCoreState(ctx, node, 15*time.Second, func(s domain.State) bool {
			return s.Peer(peer.ID).Online && s.Peer(peer.ID).Version == peer.Version
		}); err != nil {
			return "", err
		}
		if _, err := waitCoreState(ctx, node, 8*time.Second, func(s domain.State) bool {
			for _, m := range s.ChatMessages(conversation) {
				if m.Text == body && m.Status == "sent" {
					return true
				}
			}
			return false
		}); err != nil {
			return "", err
		}
		return "the original acknowledged the replayed text", nil
	}) {
		return r
	}
	saved := r.run("goim_offline_file_saved_by_original", func() (string, error) {
		if err := lab.drive(ctx, "accept-file"); err != nil {
			return "", err
		}
		var detail string
		err := waitSandboxFile(ctx, 8*time.Second, func() (bool, error) {
			value, err := verifySample(lab.stockDownloadPath(filename), sampleSize, digest)
			detail = value
			return err == nil, nil
		})
		return detail, err
	})
	visible := r.run("goim_offline_text_in_original_transcript", func() (string, error) {
		control := "GoIM 离线补投验收（在线校验） " + runID
		node.Submit(domain.Command{Kind: domain.SendText, ConversationID: conversation, Text: control})
		if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
			for _, m := range s.ChatMessages(conversation) {
				if m.Text == control && m.Status == "sent" {
					return true
				}
			}
			return false
		}); err != nil {
			return "", err
		}
		if err := lab.drive(ctx, "chat-check", control); err != nil {
			return "", fmt.Errorf("could not verify the online control message in the original transcript: %w", err)
		}
		if err := lab.drive(ctx, "chat-check", body); err != nil {
			return "", fmt.Errorf("the original transcript showed the online control message but not the replay: %w", err)
		}
		return "the original transcript contained both the replay and a fresh control message", nil
	})
	r.Passed = saved && visible
	return r
}

func requestReferenceState(ctx context.Context, dir, action, result string) error {
	if err := os.WriteFile(filepath.Join(dir, "reference-"+action+".request"), nil, 0600); err != nil {
		return err
	}
	if err := waitSandboxFile(ctx, 12*time.Second, func() (bool, error) {
		_, err := os.Stat(filepath.Join(dir, "reference-"+result))
		return err == nil, ignoreAbsent(err)
	}); err != nil {
		return fmt.Errorf("reference did not become %s: %w", result, err)
	}
	return nil
}

func waitCoreState(ctx context.Context, node *core.Node, timeout time.Duration, match func(domain.State) bool) (domain.State, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		state := node.Snapshot()
		if match(state) {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, fmt.Errorf("GoIM did not reach the expected state (%s): %w", state.Error, ctx.Err())
		case <-node.Updates():
		}
	}
}
