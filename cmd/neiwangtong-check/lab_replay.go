package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkResume(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	phase, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	name := "续传-" + l.runID + ".bin"
	source := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa", name)
	digest, err := makeSample(source, sampleSize)
	if err != nil {
		return "", err
	}
	offer, err := l.stockOffer(phase, peer, name)
	if err != nil {
		return "", fmt.Errorf("offer for resume: %w", err)
	}
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	const position = 32768
	dest := filepath.Join(l.opt.labDir, "resume.part")
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	_, copyErr := io.CopyN(out, in, position)
	if err := out.Close(); err != nil {
		return "", err
	}
	if copyErr != nil {
		return "", copyErr
	}
	if err := service.ResumeFile(phase, peer, offer.TaskID, offer.FileID, offer.Size, dest, nil); err != nil {
		return "", err
	}
	result, err := verifySample(dest, sampleSize, digest)
	return fmt.Sprintf("original resumed at %d; %s", position, result), err
}

func (l *labChecks) checkOffline(ctx context.Context, r *report, first *neiwangtong.Service, peer string, cfg neiwangtong.Config) bool {
	phase, cancel := context.WithTimeout(ctx, 42*time.Second)
	defer cancel()
	body := "离线补投-" + l.runID
	name := "离线文件-" + l.runID + ".bin"
	root := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	var digest [32]byte
	err := os.MkdirAll(root, 0700)
	if err == nil {
		digest, err = makeSample(filepath.Join(root, name), sampleSize)
	}
	stopped := r.run("reference_queue_while_offline", func() (string, error) {
		if err != nil {
			return "", err
		}
		if err := first.SendQuit(peer); err != nil {
			return "", fmt.Errorf("notify original before disconnecting: %w", err)
		}
		if err := first.Close(); err != nil {
			return "", err
		}
		time.Sleep(500 * time.Millisecond)
		if err := l.drive(phase, "queue-text", body); err != nil {
			return "", fmt.Errorf("compose offline text with original: %w", err)
		}
		if err := l.drive(phase, "send-file", "C:\\goim-qa\\"+name); err != nil {
			return "", fmt.Errorf("compose offline file with original: %w", err)
		}
		return "reference sent while GoIM had no listeners", nil
	})
	if !stopped {
		return false
	}
	newMessages := make(chan neiwangtong.DirectMessage, 16)
	cfg.Callbacks.Message = func(_ string, m neiwangtong.DirectMessage) error {
		publish(newMessages, m)
		return nil
	}
	var service *neiwangtong.Service
	ready := r.run("offline_reconnect", func() (string, error) {
		var err error
		service, err = neiwangtong.Start(cfg)
		if err != nil {
			return "", err
		}
		for attempt := 0; attempt < 3; attempt++ {
			err = service.Probe(phase, l.opt.target, int(neiwangtong.ControlPort), int(neiwangtong.AvatarPort))
			if err == nil || phase.Err() != nil {
				break
			}
		}
		return "same stable client identity reconnected", err
	})
	if service != nil {
		defer service.Close()
	}
	if !ready {
		return false
	}
	msg := r.run("offline_text_from_reference", func() (string, error) {
		event, err := wait(phase, l.opt.timeout, newMessages, func(m neiwangtong.DirectMessage) bool { return m.Text() == body })
		if err != nil {
			return "", err
		}
		if event.OfflineAt.IsZero() {
			return "", errors.New("reference omitted the original offline-message timestamp")
		}
		return fmt.Sprintf("matching offline message, original timestamp: %s", event.OfflineAt), nil
	})
	file := r.run("offline_file_from_reference", func() (string, error) {
		event, err := wait(phase, l.opt.timeout, l.offers, func(got labOffer) bool {
			return got.peer == peer && got.file.Name == name && got.file.Size == sampleSize
		})
		if err != nil {
			return "", err
		}
		dest := filepath.Join(l.opt.labDir, "offline.bin")
		if err := service.AcceptFile(phase, peer, event.file.TaskID, event.file.FileID, event.file.Size, dest, nil); err != nil {
			return "", err
		}
		result, err := verifySample(dest, sampleSize, digest)
		return fmt.Sprintf("offline flag %d; %s", event.file.Offline, result), err
	})
	outbound := r.run("offline_marked_text_to_reference", func() (string, error) {
		err := service.SendTextConfirmed(phase, peer, 7799, l.runID+l.runID, "GoIM补投的消息-"+l.runID, time.Now().Add(-time.Minute))
		return "reference application acknowledged a message with its queued timestamp", err
	})
	return msg && file && outbound
}
