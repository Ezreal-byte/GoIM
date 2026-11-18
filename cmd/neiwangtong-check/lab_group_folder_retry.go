package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkGroupFolderRetry(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	group, err := l.groupAttachmentRoom(ctx, service, peer)
	if err != nil {
		return "", err
	}
	const name = "GoIMQA目录"
	source := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "nwtlab", "Desktop", name)
	files, err := makeDirectorySample(source)
	if err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "send-folder", group.Name, "C:\\users\\nwtlab\\Desktop\\"+name); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-reply", group.Name, ""); err != nil {
		return "", err
	}
	ref, err := l.waitGroupAttachment(ctx, peer, group.ID, name, true)
	if err != nil {
		return "", err
	}
	partial, err := os.MkdirTemp(l.opt.labDir, "interrupted-group-folder-")
	if err != nil {
		return "", err
	}
	first, disconnect := context.WithCancel(ctx)
	defer disconnect()
	var offset int64
	_, err = service.ReceiveGroupDirectory(first, peer, ref.ID, name, partial, func(done, _ int64) {
		if done > 0 {
			offset = done
			disconnect()
		}
	})
	if err == nil || offset <= 0 || offset >= 2*sampleSize {
		return "", fmt.Errorf("the first folder attempt did not stop with an incomplete copy (%d bytes): %v", offset, err)
	}
	retry, err := os.MkdirTemp(l.opt.labDir, "retried-group-folder-")
	if err != nil {
		return "", err
	}
	total, err := service.ReceiveGroupDirectory(ctx, peer, ref.ID, name, retry, nil)
	if err != nil {
		return "", fmt.Errorf("the original did not reopen the folder after %d bytes: %w", offset, err)
	}
	if total != 2*sampleSize {
		return "", fmt.Errorf("retry returned an unexpected folder size %d", total)
	}
	if err := verifyDirectorySample(retry, files); err != nil {
		return "", err
	}
	return fmt.Sprintf("the original reopened the same group folder after %d bytes; fresh copy preserved paths, empty entries and every SHA-256", offset), nil
}
