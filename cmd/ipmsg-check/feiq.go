// The Windows original is driven through its desktop UI by scripts/feiq-lab.
// Only observations from the production backend are returned here.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"goim/internal/core"
	"goim/internal/domain"
)

type feiqPlan struct {
	Name         string
	OutgoingText string
	IncomingText string
	OutgoingFile string
	IncomingFile string
	ReceivePath  string
	OutgoingHash string
	IncomingHash string
}

func runWindowsFeiq(dir string) error {
	if runtime.GOOS != "windows" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" || os.Getenv("GOIM_FEIQ_LAB") != "1" || dir == "" {
		return fmt.Errorf("requires the disposable Windows FeiQ runner")
	}
	var token [6]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(token[:])
	plan := feiqPlan{Name: "GoIM-QA-" + id, OutgoingText: "项目交接 出站 " + id, IncomingText: "飞秋回复 入站 " + id,
		OutgoingFile: filepath.Join(dir, "goim-出站-"+id+".bin"), IncomingFile: filepath.Join(dir, "飞秋-入站-"+id+".bin"),
		ReceivePath: filepath.Join(dir, "received", "飞秋-入站-"+id+".bin")}
	for _, path := range []string{plan.OutgoingFile, plan.IncomingFile} {
		data := make([]byte, 98321)
		if _, err := rand.Read(data); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		if path == plan.OutgoingFile {
			plan.OutgoingHash = digest
		} else {
			plan.IncomingHash = digest
		}
	}
	write := func(name string, obj any) error {
		data, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name), data, 0600)
	}
	if err := write("plan.json", plan); err != nil {
		return err
	}
	checks := map[string]bool{"unicast_discovery": false, "outgoing_text_receipt": false, "incoming_text_exact": false, "outgoing_file_served": false, "incoming_file_sha256": false}
	defer func() { _ = write("backend.json", checks) }()
	node, err := core.New(core.Options{DataDir: filepath.Join(dir, "goim"), Name: plan.Name, Listen: "127.0.0.1:0",
		DownloadDir: filepath.Dir(plan.ReceivePath), DisableDiscovery: true, DisableCompatibility: true})
	if err != nil {
		return err
	}
	defer node.Close()
	result := make(chan error, 1)
	node.Submit(domain.Command{Kind: domain.SetCompatibility, Transport: domain.TransportIPMSG, Compatibility: domain.CompatibilityConfig{Enabled: true, Port: 2426}, OnResult: func(err error) { result <- err }})
	select {
	case err = <-result:
		if err != nil {
			return err
		}
	case <-time.After(8 * time.Second):
		return fmt.Errorf("IPMSG service did not start")
	}
	peer, chat, offered, accepted := "", "", false, false
	defer func() {
		state := node.Snapshot()
		messages := make([]map[string]string, 0)
		for _, m := range state.Messages {
			messages = append(messages, map[string]string{"kind": m.Kind, "text": m.Text, "status": m.Status, "sender": m.SenderID})
		}
		transfers := make([]map[string]any, 0)
		for _, t := range state.Transfers {
			transfers = append(transfers, map[string]any{"name": t.Name, "direction": t.Direction, "status": t.Status, "bytes": t.Done, "error": t.Error})
		}
		_ = write("observations.json", map[string]any{"reference_peer": peer, "messages": messages, "transfers": transfers})
	}()
	timeout := time.After(105 * time.Second)
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	var nextProbe time.Time
	ready := false
	for {
		select {
		case <-timeout:
			return fmt.Errorf("original client acceptance timed out: %v", checks)
		case <-tick.C:
			state := node.Snapshot()
			if peer == "" {
				for _, p := range state.Peers {
					endpoint, e := netip.ParseAddrPort(p.Address)
					if p.Transport == domain.TransportIPMSG && p.Online && e == nil && endpoint.Addr().Unmap().IsLoopback() && endpoint.Port() == 2425 {
						peer = p.ID
						checks["unicast_discovery"] = true
						node.Submit(domain.Command{Kind: domain.StartChat, PeerID: peer})
						break
					}
				}
				if peer == "" && !time.Now().Before(nextProbe) {
					nextProbe = time.Now().Add(5 * time.Second)
					node.Submit(domain.Command{Kind: domain.Connect, Transport: domain.TransportIPMSG, Address: "127.0.0.1:2425"})
				}
				continue
			}
			if chat == "" {
				for _, c := range state.Conversations {
					if c.Kind == "direct" && c.PeerID == peer {
						chat = c.ID
						break
					}
				}
				if chat == "" {
					continue
				}
			}
			if !offered {
				offered = true
				node.Submit(domain.Command{Kind: domain.SendText, ConversationID: chat, Text: plan.OutgoingText})
				node.Submit(domain.Command{Kind: domain.SendFiles, ConversationID: chat, Paths: []string{plan.OutgoingFile}})
			}
			for _, m := range state.ChatMessages(chat) {
				if m.SenderID == state.Self.ID && m.Text == plan.OutgoingText && m.Status == "sent" {
					checks["outgoing_text_receipt"] = true
				}
				if m.SenderID == peer && m.Text == plan.IncomingText {
					checks["incoming_text_exact"] = true
				}
			}
			for _, t := range state.Transfers {
				if t.PeerID == peer && t.Direction == "send" && t.ConversationID == chat && t.Name == filepath.Base(plan.OutgoingFile) && t.Status == "completed" && t.Done == 98321 {
					checks["outgoing_file_served"] = true
				}
				if t.PeerID != peer || t.ConversationID != chat || t.Direction != "receive" || t.Name != filepath.Base(plan.IncomingFile) || t.Size != 98321 {
					continue
				}
				if !accepted && t.Status == "offered" {
					accepted = true
					node.Submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: t.ID, Paths: []string{plan.ReceivePath}})
				}
				if t.Status == "completed" {
					data, e := os.ReadFile(plan.ReceivePath)
					checks["incoming_file_sha256"] = e == nil && fmt.Sprintf("%x", sha256.Sum256(data)) == plan.IncomingHash
				}
			}
			if checks["incoming_file_sha256"] && checks["outgoing_file_served"] && checks["incoming_text_exact"] && checks["outgoing_text_receipt"] {
				// Keep serving until the original has finished saving the outgoing file.
				if !ready {
					if err := os.WriteFile(filepath.Join(dir, "backend-complete"), nil, 0600); err != nil {
						return err
					}
					ready = true
				}
				if _, e := os.Stat(filepath.Join(dir, "ui-finished")); e == nil {
					return nil
				}
			}
		}
	}
}
