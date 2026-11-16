package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"goim/internal/neiwangtong"
)

const sampleSize = 96<<10 + 17

type labMessage struct {
	peer, body string
}

type labOffer struct {
	peer string
	file neiwangtong.XMLSendFile
}

type labProgress struct {
	peer        string
	task        uint64
	done, total int64
	complete    bool
	err         error
}

type labChecks struct {
	opt      options
	qaName   string
	runID    string
	messages chan labMessage
	offers   chan labOffer
	progress chan labProgress
	textSeen atomic.Uint32
}

func newLabChecks(opt options, qaName, runID string) *labChecks {
	return &labChecks{opt: opt, qaName: qaName, runID: runID,
		messages: make(chan labMessage, 8), offers: make(chan labOffer, 8), progress: make(chan labProgress, 16)}
}

func (l *labChecks) attach(cfg *neiwangtong.Config) {
	cfg.TraceEvent = func(kind uint32, _ []byte) {
		if kind == neiwangtong.CmdSendMsg {
			l.textSeen.Add(1)
		}
	}
	cfg.Callbacks.Text = func(peer string, _ uint64, body string) {
		publish(l.messages, labMessage{peer, body})
	}
	cfg.Callbacks.Files = func(peer string, offer neiwangtong.XMLSendFile) {
		publish(l.offers, labOffer{peer, offer})
	}
	cfg.Callbacks.FileProgress = func(peer string, task uint64, done, total int64, complete bool, err error) {
		publish(l.progress, labProgress{peer, task, done, total, complete, err})
	}
}

func (l *labChecks) drive(ctx context.Context, action string, args ...string) error {
	cmd := exec.CommandContext(ctx, l.opt.labDriver, append([]string{action, l.qaName}, args...)...)
	cmd.WaitDelay = time.Second
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("reference driver %s: %w: %s", action, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *labChecks) run(ctx context.Context, r *report, service *neiwangtong.Service, reference neiwangtong.Peer) bool {
	text := r.run("text_from_reference", func() (string, error) {
		phase, cancel := context.WithTimeout(ctx, l.opt.timeout)
		defer cancel()
		body := "原版回复 & " + l.runID
		if err := l.drive(phase, "reply", body); err != nil {
			return "", err
		}
		got, err := wait(phase, l.opt.timeout, l.messages, func(got labMessage) bool { return got.peer == reference.ID })
		if err != nil {
			return "", fmt.Errorf("no matching text from the reference client (received %d native text commands): %w", l.textSeen.Load(), err)
		}
		if got.body != body {
			return "", fmt.Errorf("reference reply content did not match (%d bytes, expected %d)", len(got.body), len(body))
		}
		return "matching peer and UTF-8 contents", nil
	})
	// Offer a file before downloading one so the original has not already
	// opened a file channel with the test peer.
	sent := r.run("file_to_reference", func() (string, error) {
		return l.sendFile(ctx, service, reference.ID)
	})
	received := r.run("file_from_reference", func() (string, error) {
		return l.receiveFile(ctx, service, reference.ID)
	})
	return text && received && sent
}

func (l *labChecks) receiveFile(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	name := "原版-" + l.runID + ".bin"
	dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	digest, err := makeSample(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	if err := l.drive(ctx, "send-file", `C:\goim-qa\`+name); err != nil {
		return "", err
	}
	event, err := wait(ctx, l.opt.timeout, l.offers, func(got labOffer) bool {
		return got.peer == peer && got.file.Name == name && got.file.Size == sampleSize
	})
	if err != nil {
		return "", fmt.Errorf("no reference file offer with the expected name and size: %w", err)
	}
	dest := filepath.Join(l.opt.labDir, "received.bin")
	if err := service.AcceptFile(ctx, peer, event.file.TaskID, event.file.FileID, event.file.Size, dest, nil); err != nil {
		return "", err
	}
	return verifySample(dest, digest)
}

func (l *labChecks) sendFile(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	name := "GoIM-样例-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, name)
	digest, err := makeSample(source)
	if err != nil {
		return "", err
	}
	const taskID, fileID = 101, 3511
	if err := service.SendFileOfferWithIDs(peer, taskID, fileID, name, sampleSize, time.Now().Unix(), source); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "accept-file"); err != nil {
		return "", err
	}
	event, err := wait(ctx, l.opt.timeout, l.progress, func(p labProgress) bool {
		return p.peer == peer && p.task == taskID && (p.complete || p.err != nil)
	})
	if err != nil {
		return "", fmt.Errorf("no completion for the offered file: %w", err)
	}
	if event.err != nil {
		return "", event.err
	}
	if event.done != sampleSize || event.total != sampleSize {
		return "", errors.New("unexpected completed byte count")
	}
	dest := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "recv", l.qaName, name)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		detail, err := verifySample(dest, digest)
		if err == nil {
			return detail, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("reference did not save the expected file: %w", err)
		case <-tick.C:
		}
	}
}

func makeSample(path string) ([32]byte, error) {
	body := make([]byte, sampleSize)
	_, _ = rand.Read(body)
	return sha256.Sum256(body), os.WriteFile(path, body, 0600)
}

func verifySample(path string, expected [32]byte) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, sampleSize+1))
	if err != nil {
		return "", err
	}
	if len(body) != sampleSize || sha256.Sum256(body) != expected {
		return "", errors.New("file size or SHA-256 does not match")
	}
	return fmt.Sprintf("%d bytes, sha256=%x", len(body), expected), nil
}
