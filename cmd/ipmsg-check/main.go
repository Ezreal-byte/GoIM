// Command ipmsg-check verifies the production backend against an original
// IP Messenger in the private network created by scripts/ipmsg-lab/run.sh.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/core"
	"goim/internal/domain"
)

type check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}
type report struct {
	Reference string  `json:"reference"`
	Passed    bool    `json:"passed"`
	Checks    []check `json:"checks"`
}

func wait(ctx context.Context, node *core.Node, accept func(domain.State) bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		if accept(node.Snapshot()) {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return errors.New("GoIM did not reach the expected interoperability state")
		}
	}
}

func main() {
	dir := flag.String("lab-dir", "", "isolated lab directory")
	driver := flag.String("driver", "", "private reference command-line runner")
	flag.Parse()
	r := report{Reference: "original IP Messenger"}
	run := func(name string, action func() (string, error)) bool {
		detail, err := action()
		if err != nil {
			detail = err.Error()
		}
		r.Checks = append(r.Checks, check{Name: name, Passed: err == nil, Detail: detail})
		return err == nil
	}
	r.Passed = verify(*dir, *driver, run)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
	if !r.Passed {
		os.Exit(1)
	}
}

func verify(dir, driver string, run func(string, func() (string, error)) bool) bool {
	netns, err := os.Readlink("/proc/self/ns/net")
	initial, _ := os.Readlink("/proc/1/ns/net")
	if !run("isolated_lab", func() (string, error) {
		if err != nil || netns == initial || os.Getenv("GOIM_IPMSG_SANDBOX") != "1" ||
			!strings.HasPrefix(filepath.Clean(dir), "/tmp/goim-ipmsg.") || driver == "" {
			return "", errors.New("this checker requires the isolated lab launcher")
		}
		return "private network namespace with a disposable data directory", nil
	}) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false
	}
	token := hex.EncodeToString(nonce[:])
	options := core.Options{DataDir: filepath.Join(dir, "goim"), Name: "GoIM QA " + token,
		Listen: ":0", DownloadDir: filepath.Join(dir, "downloads")}
	node, err := core.New(options)
	if !run("production_goim", func() (string, error) { return "started the real GoIM backend", err }) {
		return false
	}
	defer node.Close()
	var peer string
	if !run("broadcast_discovery", func() (string, error) {
		err := wait(ctx, node, func(s domain.State) bool {
			for _, p := range s.Peers {
				if p.Transport == domain.TransportIPMSG && p.Address == "10.78.0.2:2425" && p.Online {
					peer = p.ID
					return true
				}
			}
			return false
		})
		return "GoIM discovered the original without a manual address", err
	}) {
		return false
	}
	command := func(ctx context.Context, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		call := exec.CommandContext(ctx, driver, append([]string{"ipcmd"}, args...)...)
		out, err := call.CombinedOutput()
		// ipcmd returns a result count (such as two listed devices), not
		// just zero on success. Failure is -1; timeout uses 124 or higher.
		var exited *exec.ExitError
		if errors.As(err, &exited) && exited.ExitCode() > 0 && exited.ExitCode() < 124 {
			err = nil
		}
		if err != nil {
			return nil, fmt.Errorf("original command-line operation failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return out, nil
	}
	if !run("original_discovers_goim", func() (string, error) {
		out, err := command(ctx, "list")
		if err == nil && !bytes.Contains(out, []byte("10.78.0.1")) {
			err = errors.New("the original did not list the GoIM address")
		}
		return "original ipcmd listed the GoIM device", err
	}) {
		return false
	}
	node.Submit(domain.Command{Kind: domain.StartChat, PeerID: peer})
	var chat string
	if err := wait(ctx, node, func(s domain.State) bool {
		for _, c := range s.Conversations {
			if c.Kind == "direct" && c.PeerID == peer {
				chat = c.ID
				return true
			}
		}
		return false
	}); err != nil {
		return false
	}
	if !run("goim_to_original_with_receipt", func() (string, error) {
		listen, stop := context.WithTimeout(ctx, 12*time.Second)
		defer stop()
		type received struct {
			body []byte
			err  error
		}
		result := make(chan received, 1)
		go func() { body, err := command(listen, "recv"); result <- received{body, err} }()
		time.Sleep(250 * time.Millisecond)
		body := "GoIM QA " + token + " 中文 & <IPMSG>"
		node.Submit(domain.Command{Kind: domain.SendText, ConversationID: chat, Text: body})
		if err := wait(listen, node, func(s domain.State) bool {
			for _, m := range s.ChatMessages(chat) {
				if m.Text == body && m.Status == "sent" {
					return true
				}
			}
			return false
		}); err != nil {
			return "", err
		}
		select {
		case got := <-result:
			if got.err != nil {
				return "", got.err
			}
			if !bytes.Contains(got.body, []byte(body)) {
				return "", errors.New("the original did not return the exact Chinese message")
			}
		case <-listen.Done():
			return "", errors.New("the original did not return its received message")
		}
		return "the original returned the exact Chinese text and acknowledged its packet", nil
	}) {
		return false
	}
	if !run("original_to_goim", func() (string, error) {
		body := "IPMSG QA " + token + " 原版中文回复"
		if _, err := command(ctx, "send", body); err != nil {
			return "", err
		}
		err := wait(ctx, node, func(s domain.State) bool {
			for _, m := range s.ChatMessages(chat) {
				if m.SenderID == peer && m.Text == body {
					return true
				}
			}
			return false
		})
		return "the original sent a Chinese reply into the same GoIM conversation", err
	}) {
		return false
	}
	files := verifyFiles(ctx, node, peer, chat, dir, token, command, run)
	presence := verifyPresence(ctx, node, peer, command, run)
	offline := verifyOffline(ctx, node, options, peer, chat, token, command, run)
	return files && presence && offline
}
