package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkGroupFileResume(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	owner := l.runID + "0000000000000000"
	group := neiwangtong.GroupRoster{
		ID: l.runID + l.runID, Name: "GoIM QA 群聊 " + l.runID, OwnerID: owner,
		InfoVersion: 1, MembersVersion: 1,
		Members: []neiwangtong.GroupMember{{ID: owner, Name: l.qaName}, {ID: peer, Name: l.opt.expectedName}},
	}
	if err := service.SendGroupRoster(peer, group); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-open", group.Name); err != nil {
		return "", err
	}
	name := "原版群文件-" + l.runID + ".bin"
	folder := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	if err := os.MkdirAll(folder, 0700); err != nil {
		return "", err
	}
	const size = 512 << 10
	digest, err := makeSample(filepath.Join(folder, name), size)
	if err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-file-send", group.Name, "C:\\goim-qa\\"+name); err != nil {
		return "", err
	}
	var ref neiwangtong.GroupFileRef
	if _, err := wait(ctx, 6*time.Second, l.groupTexts, func(event labRoomMessage) bool {
		if event.peer != peer || event.message.GroupID != group.ID {
			return false
		}
		for _, part := range event.message.Message.Parts {
			if part.File != nil && !part.File.Folder && part.File.Size == size &&
				filepath.Base(strings.ReplaceAll(part.File.Name, "\\", "/")) == name {
				ref = *part.File
				return true
			}
		}
		return false
	}); err != nil {
		return "", fmt.Errorf("the original did not send the group attachment: %w", err)
	}
	first, err := service.RequestGroupFile(ctx, peer, ref.ID, ref.Size)
	if err != nil {
		return "", err
	}
	partial := filepath.Join(l.opt.labDir, "group-resume.part")
	dropped, disconnect := context.WithCancel(ctx)
	defer disconnect()
	err = first.Resume(dropped, partial, func(_, _ int64) { disconnect() })
	info, statErr := os.Stat(partial)
	if !errors.Is(err, context.Canceled) || statErr != nil {
		return "", fmt.Errorf("the original group stream did not stop after writing a prefix: %w", errors.Join(err, statErr))
	}
	offset := info.Size()
	if offset <= 0 || offset >= size {
		return "", fmt.Errorf("expected an incomplete group file, got %d bytes", offset)
	}
	retry, err := service.RequestGroupFile(ctx, peer, ref.ID, ref.Size)
	if err != nil {
		return "", err
	}
	if first.ModTime <= 0 || retry.ModTime != first.ModTime {
		return "", errors.New("the original did not report the same group file revision on retry")
	}
	if err := retry.Resume(ctx, partial, nil); err != nil {
		return "", fmt.Errorf("the original group file did not resume at %d bytes: %w", offset, err)
	}
	if _, err := verifySample(partial, size, digest); err != nil {
		return "", err
	}
	return fmt.Sprintf("the original resumed the same group invitation from %d bytes; %d bytes, sha256=%x", offset, size, digest), nil
}
