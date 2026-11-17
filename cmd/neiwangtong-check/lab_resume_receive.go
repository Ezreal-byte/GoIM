package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkStockReceiveResume(ctx context.Context, service *neiwangtong.Service, peer string, cfg neiwangtong.Config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	const task, selector, size = 106, 3516, 512 << 10
	const block = 32 << 10
	name := "GoIM断点-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, name)
	digest, err := makeSample(source, size)
	if err != nil {
		return "", err
	}
	paused := &labSendGate{task: task, started: make(chan struct{}), resume: make(chan struct{})}
	l.gate.Store(paused)
	release := func() {
		if l.gate.CompareAndSwap(paused, nil) {
			close(paused.resume)
		}
	}
	defer release()
	modified := time.Now().Unix()
	if err := service.SendFileOfferWithIDs(peer, task, selector, name, size, modified, source); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "accept-file"); err != nil {
		return "", err
	}
	match := func(p labProgress) bool { return p.peer == peer && p.task == task }
	first, err := wait(ctx, 8*time.Second, l.progress, match)
	if err != nil || first.done != block || first.err != nil {
		return "", fmt.Errorf("expected the initial %d bytes from GoIM, saw %d: %v", block, first.done, errors.Join(err, first.err))
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-paused.started:
	}
	dest := l.stockDownloadPath(name)
	if err := waitSandboxFile(ctx, 5*time.Second, func() (bool, error) {
		stat, err := os.Stat(dest)
		return err == nil && stat.Size() == block, ignoreAbsent(err)
	}); err != nil {
		return "", fmt.Errorf("original did not write the first block to its download: %w", err)
	}
	// Closing the sender closes its real TCP connection while the next
	// block is still held at the gate. Nothing writes the partial for stock.
	if err := service.Close(); err != nil {
		return "", err
	}
	release()
	service, err = neiwangtong.Start(cfg)
	if err != nil {
		return "", err
	}
	defer service.Close()
	var got neiwangtong.Peer
	for attempt := 0; attempt < 3; attempt++ {
		got, err = service.ProbePeer(ctx, l.opt.target, int(neiwangtong.ControlPort), int(neiwangtong.AvatarPort))
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	if err != nil || got.ID != peer || got.Name != l.opt.expectedName || got.Version != l.opt.expectedVersion {
		return "", fmt.Errorf("could not reconnect to the same reference client: %v", err)
	}
	if err := service.SendFileOfferWithIDs(peer, task, selector, name, size, modified, source); err != nil {
		return "", err
	}
	restarted, err := wait(ctx, 5*time.Second, l.progress, match)
	if err != nil && ctx.Err() == nil {
		if driveErr := l.drive(ctx, "accept-file"); driveErr != nil {
			return "", driveErr
		}
		restarted, err = wait(ctx, 10*time.Second, l.progress, match)
	}
	if err != nil {
		return "", fmt.Errorf("reference did not resume after the sender reconnected: %w", err)
	}
	if restarted.err != nil {
		return "", restarted.err
	}
	if offset := restarted.done - block; offset != first.done {
		return "", fmt.Errorf("reference resumed at %d; expected retained offset %d", offset, first.done)
	}
	if _, err := wait(ctx, 10*time.Second, l.progress, func(p labProgress) bool {
		return match(p) && p.complete && p.done == size
	}); err != nil {
		return "", fmt.Errorf("resumed transfer never completed: %w", err)
	}
	if err := waitSandboxFile(ctx, 5*time.Second, func() (bool, error) {
		_, err := verifySample(dest, size, digest)
		return err == nil, nil
	}); err != nil {
		return "", fmt.Errorf("resumed reference download did not match: %w", err)
	}
	return fmt.Sprintf("reference requested offset %d after the sender disconnected and restarted; %d bytes, sha256=%x", first.done, size, digest), nil
}

func ignoreAbsent(err error) error {
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func waitSandboxFile(ctx context.Context, timeout time.Duration, check func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		if found, err := check(); found || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
