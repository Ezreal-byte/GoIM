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

const (
	sampleSize = 96<<10 + 17
	largeSize  = 1<<32 + 65553
	largeTask  = 103
)

type labMessage struct {
	peer, body string
}

type labOffer struct {
	peer string
	file neiwangtong.XMLSendFile
}

type labImage struct {
	peer string
	ref  neiwangtong.ChatImageRef
}

type labAvatar struct {
	peer     string
	revision uint64
	png      []byte
}

type labDirectory struct {
	peer  string
	offer neiwangtong.FolderOffer
}

type labDirectoryResult struct {
	peer, task string
	done       int64
	err        error
}

type labRoster struct {
	peer   string
	roster neiwangtong.GroupRoster
}

type labRoomMessage struct {
	peer    string
	message neiwangtong.GroupMessage
}

type labProgress struct {
	peer        string
	task        uint64
	done, total int64
	complete    bool
	err         error
}

type labOperation struct {
	kind uint32
	task uint64
	op   uint32
}

type labCancellation struct {
	peer string
	task uint64
}

type labSendGate struct {
	task            uint64
	started, resume chan struct{}
	waiting         atomic.Bool
}

type labDirectoryGate struct {
	task    string
	started chan int64
	resume  chan struct{}
	waiting atomic.Bool
}

type labChecks struct {
	opt         options
	qaName      string
	runID       string
	messages    chan labMessage
	offers      chan labOffer
	progress    chan labProgress
	ops         chan labOperation
	cancelled   chan labCancellation
	imageRefs   chan labImage
	avatars     chan labAvatar
	folders     chan labDirectory
	folderDone  chan labDirectoryResult
	rosters     chan labRoster
	groupTexts  chan labRoomMessage
	peerEvents  chan neiwangtong.Peer
	gate        atomic.Pointer[labSendGate]
	folderGate  atomic.Pointer[labDirectoryGate]
	textSeen    atomic.Uint32
	profileSeen atomic.Uint32
	largeRecv   atomic.Int64
}

func newLabChecks(opt options, qaName, runID string) *labChecks {
	return &labChecks{opt: opt, qaName: qaName, runID: runID,
		messages: make(chan labMessage, 8), offers: make(chan labOffer, 8),
		progress: make(chan labProgress, 16), ops: make(chan labOperation, 32),
		cancelled: make(chan labCancellation, 8), imageRefs: make(chan labImage, 8), avatars: make(chan labAvatar, 8),
		folders: make(chan labDirectory, 8), folderDone: make(chan labDirectoryResult, 4),
		rosters: make(chan labRoster, 4), groupTexts: make(chan labRoomMessage, 8), peerEvents: make(chan neiwangtong.Peer, 16)}
}

func (l *labChecks) attach(cfg *neiwangtong.Config) {
	notifyPeer := cfg.Callbacks.Peer
	cfg.Callbacks.Peer = func(peer neiwangtong.Peer) {
		if notifyPeer != nil {
			notifyPeer(peer)
		}
		publish(l.peerEvents, peer)
	}
	notifyAvatar := cfg.Callbacks.AvatarUpdated
	cfg.Callbacks.AvatarUpdated = func(peer string, revision uint64, png []byte) {
		if notifyAvatar != nil {
			notifyAvatar(peer, revision, png)
		}
		publish(l.avatars, labAvatar{peer, revision, png})
	}
	cfg.TraceEvent = func(kind uint32, payload []byte) {
		switch kind {
		case neiwangtong.CmdSendMsg:
			l.textSeen.Add(1)
		case neiwangtong.CmdChangeInfo:
			l.profileSeen.Add(1)
		case neiwangtong.CmdOperateRecvFile:
			if task, op, err := neiwangtong.ParseOperateRecvFile(payload); err == nil {
				publish(l.ops, labOperation{kind, task, op})
			}
		case neiwangtong.CmdOperateSendFile:
			if task, op, err := neiwangtong.ParseOperateSendFile(payload); err == nil {
				publish(l.ops, labOperation{kind, task, op})
			}
		case neiwangtong.CmdProgressRecvFile:
			if l.opt.labLargeFile {
				progress, err := neiwangtong.ParseProgressRecvFile(payload)
				if err == nil && progress.TaskID == largeTask {
					l.largeRecv.Store(progress.RecvdSize)
				}
			}
		}
	}
	cfg.Callbacks.Message = func(peer string, message neiwangtong.DirectMessage) error {
		for _, part := range message.Parts {
			if part.Image != nil {
				publish(l.imageRefs, labImage{peer, *part.Image})
			}
		}
		return nil
	}
	cfg.Callbacks.Text = func(peer string, _ uint64, body string) {
		publish(l.messages, labMessage{peer, body})
	}
	cfg.Callbacks.GroupRoster = func(peer string, roster neiwangtong.GroupRoster) {
		publish(l.rosters, labRoster{peer, roster})
	}
	cfg.Callbacks.GroupText = func(peer string, message neiwangtong.GroupMessage) error {
		publish(l.groupTexts, labRoomMessage{peer, message})
		return nil
	}
	cfg.Callbacks.Files = func(peer string, offer neiwangtong.XMLSendFile) {
		publish(l.offers, labOffer{peer, offer})
	}
	cfg.Callbacks.Folders = func(peer string, offer neiwangtong.FolderOffer) {
		publish(l.folders, labDirectory{peer, offer})
	}
	cfg.Callbacks.FolderProgress = func(peer, task string, done, total int64, complete bool, err error) {
		if complete || err != nil {
			publish(l.folderDone, labDirectoryResult{peer, task, done, err})
		} else if gate := l.folderGate.Load(); gate != nil && gate.task == task && done > 0 && done < total &&
			gate.waiting.CompareAndSwap(false, true) {
			gate.started <- done
			if gate.resume != nil {
				<-gate.resume
			}
		}
	}
	cfg.Callbacks.FileCancelled = func(peer string, task uint64) {
		publish(l.cancelled, labCancellation{peer, task})
	}
	cfg.Callbacks.FileProgress = func(peer string, task uint64, done, total int64, complete bool, err error) {
		publish(l.progress, labProgress{peer, task, done, total, complete, err})
		if gate := l.gate.Load(); gate != nil && gate.task == task && done > 0 && done < total && err == nil &&
			gate.waiting.CompareAndSwap(false, true) {
			close(gate.started)
			<-gate.resume
		}
	}
}

func (l *labChecks) drive(ctx context.Context, action string, args ...string) error {
	return l.driveFor(ctx, action, l.qaName, args...)
}

func (l *labChecks) driveFor(ctx context.Context, action, title string, args ...string) error {
	return l.driveWithEnv(ctx, nil, action, title, args...)
}

func (l *labChecks) driveWithEnv(ctx context.Context, env []string, action, title string, args ...string) error {
	cmd := exec.CommandContext(ctx, l.opt.labDriver, append([]string{action, title}, args...)...)
	if len(env) != 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
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
	if l.opt.labAvatarUpdate {
		out := r.run("goim_avatar_update_in_original", func() (string, error) {
			return l.checkAvatarToOriginal(ctx, service)
		})
		in := r.run("original_avatar_change_and_reset", func() (string, error) {
			return l.checkAvatarFromOriginal(ctx, reference.ID)
		})
		return text && out && in
	}
	if l.opt.labOffline || l.opt.labResume || l.opt.labDirectory || l.opt.labFolderResend {
		return text
	}
	if l.opt.labLargeFile {
		if l.opt.labDirection == "from-reference" {
			return r.run("large_file_from_reference", func() (string, error) {
				return l.receiveFile(ctx, service, reference.ID, largeSize)
			}) && text
		}
		return r.run("large_file_to_reference", func() (string, error) {
			return l.sendFile(ctx, service, reference.ID, largeSize, largeTask, 3513)
		}) && text
	}
	// Offer a file before downloading one so the original has not already
	// opened a file channel with the test peer.
	sent := r.run("file_to_reference", func() (string, error) {
		return l.sendFile(ctx, service, reference.ID, sampleSize, 101, 3511)
	})
	received := r.run("file_from_reference", func() (string, error) {
		return l.receiveFile(ctx, service, reference.ID, sampleSize)
	})
	rejected := r.run("reference_declines_file", func() (string, error) {
		return l.stockDecline(ctx, service, reference.ID)
	})
	cancelled := r.run("reference_cancels_file", func() (string, error) {
		return l.stockCancel(ctx, reference.ID)
	})
	stopped := r.run("reference_cancels_receiving_file", func() (string, error) {
		return l.stockStopReceiving(ctx, service, reference.ID)
	})
	withdrawn := r.run("goim_cancels_file", func() (string, error) {
		return l.withdrawOffer(ctx, service, reference.ID)
	})
	declined := r.run("goim_declines_then_receives_new_offer", func() (string, error) {
		return l.declineAndReceive(ctx, service, reference.ID)
	})
	images := l.checkImages(ctx, r, service, reference.ID)
	resumed := r.run("reference_file_resume", func() (string, error) {
		return l.checkResume(ctx, service, reference.ID)
	})
	dirs := l.checkDirectories(ctx, r, service, reference.ID)
	groups := l.checkGroup(ctx, r, service, reference.ID)
	presence := r.run("native_presence_from_original", func() (string, error) {
		return l.checkOriginalPresence(ctx, reference.ID)
	})
	return text && sent && received && rejected && cancelled && stopped && withdrawn && declined && images && resumed && dirs && groups && presence
}

func (l *labChecks) checkOriginalPresence(ctx context.Context, peerID string) (string, error) {
	for _, check := range []struct {
		name   string
		status uint32
	}{{"busy", neiwangtong.StatusBusy}, {"away", neiwangtong.StatusAway}, {"online", neiwangtong.StatusOnline}} {
		if err := l.drive(ctx, "stock-presence", check.name); err != nil {
			return "", err
		}
		if _, err := wait(ctx, 5*time.Second, l.peerEvents, func(p neiwangtong.Peer) bool {
			return p.ID == peerID && p.Online && p.Status == check.status
		}); err != nil {
			return "", fmt.Errorf("the original did not publish its %s status: %w", check.name, err)
		}
	}
	return "the same original contact reported Busy, Away, and Online in order", nil
}

func (l *labChecks) stockDecline(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	name := "拒收-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, name)
	if _, err := makeSample(source, sampleSize); err != nil {
		return "", err
	}
	const taskID, fileID = 102, 3512
	if err := service.SendFileOfferWithIDs(peer, taskID, fileID, name, sampleSize, time.Now().Unix(), source); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "cancel-file"); err != nil {
		return "", err
	}
	if err := l.expectFileEnd(ctx, peer, taskID, neiwangtong.ErrFileRejected); err != nil {
		return "", err
	}
	return "GoIM reports refusal for the offered task", l.checkNoStockDownload(name)
}

func (l *labChecks) stockCancel(ctx context.Context, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	name := "原版取消-" + l.runID + ".bin"
	path := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa", name)
	if _, err := makeSample(path, sampleSize); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "send-file", `C:\goim-qa\`+name); err != nil {
		return "", err
	}
	offer, err := wait(ctx, l.opt.timeout, l.offers, func(e labOffer) bool { return e.peer == peer && e.file.Name == name })
	if err != nil {
		return "", err
	}
	if err := l.drive(ctx, "cancel-file"); err != nil {
		return "", err
	}
	if _, err := wait(ctx, l.opt.timeout, l.cancelled, func(got labCancellation) bool {
		return got.peer == peer && got.task == offer.file.TaskID
	}); err != nil {
		return "", err
	}
	return "GoIM sees the original sender withdraw this incoming offer", nil
}

func (l *labChecks) stockStopReceiving(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	const taskID, fileID, size = 104, 3514, 256 << 10
	name := "中途取消-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, name)
	digest, err := makeSample(source, size)
	if err != nil {
		return "", err
	}
	gate := &labSendGate{task: taskID, started: make(chan struct{}), resume: make(chan struct{})}
	l.gate.Store(gate)
	defer func() { l.gate.Store(nil); close(gate.resume) }()
	if err := service.SendFileOfferWithIDs(peer, taskID, fileID, name, size, time.Now().Unix(), source); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "accept-file"); err != nil {
		return "", err
	}
	select {
	case <-gate.started:
	case <-ctx.Done():
		return "", fmt.Errorf("reference did not start receiving the file: %w", ctx.Err())
	}
	if err := l.drive(ctx, "cancel-file"); err != nil {
		return "", err
	}
	event, err := wait(ctx, l.opt.timeout, l.ops, func(e labOperation) bool { return e.task == taskID && e.op != 2 })
	if err != nil {
		return "", fmt.Errorf("no receive control after the first delivered block: %w", err)
	}
	if err := l.expectFileEnd(ctx, peer, taskID, neiwangtong.ErrFileCancelled); err != nil {
		return "", fmt.Errorf("reference command %d operation %d: %w", event.kind, event.op, err)
	}
	dest := l.stockDownloadPath(name)
	if _, err := verifySample(dest, size, digest); err == nil {
		return "", errors.New("a cancelled upload produced the complete file")
	}
	if info, err := os.Lstat(dest); err == nil {
		return fmt.Sprintf("GoIM stops the matching upload; reference retains an incomplete file (%d bytes on disk)", info.Size()), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return "GoIM stops the matching upload; reference has no destination file", nil
}

func (l *labChecks) expectFileEnd(ctx context.Context, peer string, task uint64, reason error) error {
	event, err := wait(ctx, l.opt.timeout, l.progress, func(p labProgress) bool {
		return p.peer == peer && p.task == task && (p.err != nil || p.complete)
	})
	if err != nil {
		return err
	}
	if !errors.Is(event.err, reason) {
		return fmt.Errorf("wanted %v, got %v (completed=%t)", reason, event.err, event.complete)
	}
	return nil
}

func (l *labChecks) withdrawOffer(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	const taskID, fileID = 105, 3515
	name := "撤销-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, name)
	if _, err := makeSample(source, sampleSize); err != nil {
		return "", err
	}
	if err := service.SendFileOfferWithIDs(peer, taskID, fileID, name, sampleSize, time.Now().Unix(), source); err != nil {
		return "", err
	}
	if err := service.CancelSend(peer, taskID); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "accept-file"); err != nil {
		return "", err
	}
	_, err := wait(ctx, 1200*time.Millisecond, l.ops, func(e labOperation) bool {
		return e.kind == neiwangtong.CmdOperateRecvFile && e.task == taskID && e.op == 2
	})
	if err == nil {
		return "", errors.New("the reference still accepted the withdrawn offer")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "withdrawn offer can no longer be accepted by the reference", l.checkNoStockDownload(name)
}

func (l *labChecks) declineAndReceive(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	name := "重新发送-" + l.runID + ".bin"
	dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	digest, err := makeSample(filepath.Join(dir, name), sampleSize)
	if err != nil {
		return "", err
	}
	offer, err := l.stockOffer(ctx, peer, name)
	if err != nil {
		return "", err
	}
	if err := service.StopReceiving(peer, offer.TaskID); err != nil {
		return "", err
	}
	again, err := l.stockOffer(ctx, peer, name)
	if err != nil {
		return "", err
	}
	if again.TaskID == offer.TaskID || again.Size != sampleSize {
		return "", errors.New("the original did not make a fresh offer after refusal")
	}
	dest := filepath.Join(l.opt.labDir, name)
	defer os.Remove(dest)
	if err := service.AcceptFile(ctx, peer, again.TaskID, again.FileID, again.Size, dest, nil); err != nil {
		return "", err
	}
	return verifySample(dest, sampleSize, digest)
}

func (l *labChecks) stockOffer(ctx context.Context, peer, name string) (neiwangtong.XMLSendFile, error) {
	if err := l.drive(ctx, "send-file", `C:\goim-qa\`+name); err != nil {
		return neiwangtong.XMLSendFile{}, err
	}
	event, err := wait(ctx, l.opt.timeout, l.offers, func(got labOffer) bool {
		return got.peer == peer && got.file.Name == name
	})
	return event.file, err
}

func (l *labChecks) stockDownloadPath(name string) string {
	return filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "recv", l.qaName, name)
}

func (l *labChecks) checkNoStockDownload(name string) error {
	if _, err := os.Lstat(l.stockDownloadPath(name)); !os.IsNotExist(err) {
		return fmt.Errorf("reference still has the cancelled destination: %v", err)
	}
	return nil
}

func (l *labChecks) receiveFile(ctx context.Context, service *neiwangtong.Service, peer string, size int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.fileTimeout(size))
	defer cancel()
	name := fmt.Sprintf("原版-%s-%d.bin", l.runID, size)
	dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	digest, err := makeSample(filepath.Join(dir, name), size)
	if err != nil {
		return "", err
	}
	offer, err := l.stockOffer(ctx, peer, name)
	if err != nil {
		return "", fmt.Errorf("no reference file offer with the expected name: %w", err)
	}
	if offer.Size != size {
		return "", fmt.Errorf("reference offered %d bytes, expected %d", offer.Size, size)
	}
	dest := filepath.Join(l.opt.labDir, name)
	defer os.Remove(dest)
	if err := service.AcceptFile(ctx, peer, offer.TaskID, offer.FileID, offer.Size, dest, nil); err != nil {
		return "", err
	}
	return verifySample(dest, size, digest)
}

func (l *labChecks) sendFile(ctx context.Context, service *neiwangtong.Service, peer string, size int64, taskID, fileID uint64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.fileTimeout(size))
	defer cancel()
	name := fmt.Sprintf("GoIM-样例-%s-%d.bin", l.runID, size)
	source := filepath.Join(l.opt.labDir, name)
	digest, err := makeSample(source, size)
	if err != nil {
		return "", err
	}
	if err := service.SendFileOfferWithIDs(peer, taskID, fileID, name, size, time.Now().Unix(), source); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "accept-file"); err != nil {
		return "", err
	}
	event, err := l.waitForFileSend(ctx, peer, taskID, size)
	if err == nil && event.err != nil {
		err = fmt.Errorf("stream stopped after %d of %d bytes: %w", event.done, size, event.err)
	}
	if err != nil {
		if l.opt.labLargeFile && taskID == largeTask {
			onDisk := int64(-1)
			if info, statErr := os.Stat(l.stockDownloadPath(name)); statErr == nil {
				onDisk = info.Size()
			}
			err = fmt.Errorf("%w (stock output length %d; last stock progress %d)", err, onDisk, l.largeRecv.Load())
		}
		return "", err
	}
	if event.done != size || event.total != size {
		return "", errors.New("unexpected completed byte count")
	}
	dest := l.stockDownloadPath(name)
	defer os.Remove(dest)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		detail, err := verifySample(dest, size, digest)
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

func (l *labChecks) fileTimeout(size int64) time.Duration {
	if l.opt.labLargeFile && size >= largeSize {
		return 18 * time.Minute
	}
	return l.opt.timeout
}

func (l *labChecks) waitForFileSend(ctx context.Context, peer string, taskID uint64, size int64) (labProgress, error) {
	idle := l.opt.timeout
	var streamed, logged int64
	large := l.opt.labLargeFile && size >= largeSize
	if large {
		idle = time.Minute
	}
	for {
		event, err := wait(ctx, idle, l.progress, func(p labProgress) bool {
			return p.peer == peer && p.task == taskID && (p.done > streamed || p.complete || p.err != nil)
		})
		if err != nil {
			return labProgress{}, fmt.Errorf("no completion for the offered file (%d of %d bytes reported): %w", streamed, size, err)
		}
		if event.err != nil || event.complete {
			return event, nil
		}
		streamed = event.done
		if large && streamed-logged >= 512<<20 {
			fmt.Fprintf(os.Stderr, "Large file send to original: %d of %d bytes reported\n", streamed, size)
			logged = streamed
		}
	}
}

func makeSample(path string, size int64) ([32]byte, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		return [32]byte{}, err
	}
	if size == 0 {
		return sha256.Sum256(nil), nil
	}
	// Distinct data around the 4 GiB boundary catches truncation, wraparound
	// and repeated blocks without filling the source file with random data.
	offsets := []int64{0, size - 4096}
	if size >= largeSize {
		offsets = append(offsets, 1<<32-2048)
	}
	var block [4096]byte
	for _, offset := range offsets {
		if _, err := rand.Read(block[:]); err != nil {
			return [32]byte{}, err
		}
		if _, err := f.WriteAt(block[:], offset); err != nil {
			return [32]byte{}, err
		}
	}
	return sampleDigest(f, size)
}

func sampleDigest(f *os.File, size int64) ([32]byte, error) {
	h := sha256.New()
	if _, err := io.CopyN(h, f, size); err != nil {
		return [32]byte{}, err
	}
	var digest [32]byte
	h.Sum(digest[:0])
	return digest, nil
}

func verifySample(path string, size int64, expected [32]byte) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() != size {
		return "", fmt.Errorf("file size is %d, expected %d", info.Size(), size)
	}
	got, err := sampleDigest(f, size)
	if err != nil {
		return "", err
	}
	if got != expected {
		return "", errors.New("file SHA-256 does not match")
	}
	return fmt.Sprintf("%d bytes, sha256=%x", size, expected), nil
}
