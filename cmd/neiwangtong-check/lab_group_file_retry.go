package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkGroupFileRetry(ctx context.Context, service *neiwangtong.Service, peer string, cfg neiwangtong.Config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	group, err := l.groupAttachmentRoom(ctx, service, peer)
	if err != nil {
		return "", err
	}
	const size, block, task, selector = 512 << 10, 32 << 10, 8155, 8156
	name := "GoIM群文件重试-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, name)
	digest, err := makeSample(source, size)
	if err != nil {
		return "", err
	}
	ref := neiwangtong.GroupFileRef{ID: l.runID + "0000000000000034", Name: name, Size: size}
	gate := &labSendGate{task: task, started: make(chan struct{}), resume: make(chan struct{})}
	l.gate.Store(gate)
	release := func() {
		if l.gate.CompareAndSwap(gate, nil) {
			close(gate.resume)
		}
	}
	defer release()
	if err := service.SendGroupFile(peer, group.ID, 7878, l.runID+"0000000000000056", ref, task, selector, source); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-file-receive", group.Name); err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-gate.started:
	}
	cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "recv")
	if err := waitGroupCacheFile(ctx, cache, func(path, filename string) bool {
		if filename != name {
			return false
		}
		info, err := os.Stat(path)
		return err == nil && info.Size() == block
	}); err != nil {
		return "", fmt.Errorf("the original did not save its first group file block: %w", err)
	}
	match := func(p labProgress) bool { return p.task == task && p.peer == peer }
	if err := service.Close(); err != nil {
		return "", err
	}
	release()
	progress := make(chan labProgress, 16)
	cfg.Callbacks.FileProgress = func(peer string, task uint64, done, total int64, complete bool, err error) {
		publish(progress, labProgress{peer, task, done, total, complete, err})
	}
	reopened, err := neiwangtong.Start(cfg)
	if err != nil {
		return "", err
	}
	defer reopened.Close()
	reconnected, err := reopened.ProbePeer(ctx, l.opt.target, int(neiwangtong.ControlPort), int(neiwangtong.AvatarPort))
	if err != nil || reconnected.ID != peer {
		return "", fmt.Errorf("could not reconnect to the same original: %v", err)
	}
	if err := reopened.RememberGroupFile(peer, group.ID, ref, task, selector, source); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-file-receive", group.Name); err != nil {
		return "", err
	}
	first, err := wait(ctx, 10*time.Second, progress, match)
	if err != nil {
		return "", fmt.Errorf("the original did not reopen the same group offer: %w", err)
	}
	if first.err != nil || first.done < block {
		return "", fmt.Errorf("the retry did not start a new data stream at a valid offset: %v", first.err)
	}
	if _, err := wait(ctx, 10*time.Second, progress, func(p labProgress) bool { return match(p) && p.complete }); err != nil {
		return "", fmt.Errorf("the retry did not finish sending: %w", err)
	}
	// The original can save a restarted transfer under a numbered name
	// when its first attempt has already created a file in the cache.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if err := waitGroupCacheFile(ctx, cache, func(path, filename string) bool {
		if !strings.HasPrefix(filename, stem) {
			return false
		}
		_, err := verifySample(path, size, digest)
		return err == nil
	}); err != nil {
		return "", fmt.Errorf("the retried file at offset %d did not match on the original: %w", first.done-block, err)
	}
	return fmt.Sprintf("the original saved %d bytes, reopened the same invitation at offset %d, and saved the complete file; sha256=%x",
		block, first.done-block, digest), nil
}
