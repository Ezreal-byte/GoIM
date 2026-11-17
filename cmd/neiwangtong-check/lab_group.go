package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkGroup(ctx context.Context, r *report, service *neiwangtong.Service, peer string) bool {
	rosterCtx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	driveErr := l.drive(rosterCtx, "probe-group")
	event, err := wait(rosterCtx, 4*time.Second, l.rosters, func(e labRoster) bool {
		return e.peer == peer && e.roster.OwnerID == peer &&
			slices.ContainsFunc(e.roster.Members, func(m neiwangtong.GroupMember) bool { return m.ID == l.runID+"0000000000000000" })
	})
	if err != nil {
		return r.run("native_group_roster", func() (string, error) { return "", errors.Join(driveErr, err) })
	}

	text := r.run("native_group_text", func() (string, error) {
		return l.checkGroupText(ctx, service, peer, event.roster)
	})
	passed := text
	for index, format := range []string{"png", "jpeg", "gif"} {
		received := r.run("native_group_"+format+"_from_reference", func() (string, error) {
			return l.receiveGroupImage(ctx, service, peer, event.roster, format)
		})
		sent := r.run("native_group_"+format+"_to_reference", func() (string, error) {
			return l.sendGroupImage(ctx, service, peer, event.roster, format, uint64(index))
		})
		passed = passed && received && sent
	}
	return passed && r.run("native_group_exit_to_reference", func() (string, error) {
		if err := service.SendGroupExit(peer, event.roster.ID); err != nil {
			return "", err
		}
		return "the original group transcript reported that the GoIM participant left",
			l.driveFor(ctx, "group-exit-seen", event.roster.Name)
	})
}

func (l *labChecks) checkGroupText(ctx context.Context, service *neiwangtong.Service, peer string, room neiwangtong.GroupRoster) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	body := "群聊回复-" + l.runID
	if err := l.driveFor(ctx, "group-reply", room.Name, body); err != nil {
		return "", err
	}
	if _, err := wait(ctx, 4*time.Second, l.groupTexts, func(got labRoomMessage) bool {
		return got.peer == peer && got.message.GroupID == room.ID && got.message.Message.Text() == body
	}); err != nil {
		return "", fmt.Errorf("no matching group text from the stock client: %w", err)
	}
	reply := "GoIM 群聊 & 中文 " + l.runID
	if err := service.SendGroupText(peer, room.ID, 7798, l.runID+l.runID, reply); err != nil {
		return "", fmt.Errorf("GoIM group reply transport: %w", err)
	}
	if err := l.driveFor(ctx, "group-check", room.Name, body); err != nil {
		return "", fmt.Errorf("could not read the known original message from its transcript: %w", err)
	}
	if err := l.driveFor(ctx, "group-check", room.Name, reply); err != nil {
		return "", fmt.Errorf("the original transcript did not contain the GoIM reply: %w", err)
	}
	return "original roster and inbound group text matched; the original transcript contained the exact GoIM group reply", nil
}

func (l *labChecks) receiveGroupImage(ctx context.Context, service *neiwangtong.Service, peer string, room neiwangtong.GroupRoster, format string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := "原版群图-" + l.runID + "." + imageFixtureExtension(format)
	expected, err := writeFixtureImage(filepath.Join(dir, name), format, 65, 27, color.RGBA{R: 214, A: 255})
	if err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-image", room.Name, `C:\goim-qa\`+name); err != nil {
		return "", err
	}
	var ref neiwangtong.ChatImageRef
	if _, err := wait(ctx, 5*time.Second, l.groupTexts, func(got labRoomMessage) bool {
		if got.peer != peer || got.message.GroupID != room.ID {
			return false
		}
		for _, part := range got.message.Message.Parts {
			if part.Image != nil {
				ref = *part.Image
				return true
			}
		}
		return false
	}); err != nil {
		return "", fmt.Errorf("original did not send an inline image reference in the group: %w", err)
	}
	data, err := service.FetchChatImage(ctx, peer, ref)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(data, expected) {
		return "", errors.New("image from the original group did not match the fixture")
	}
	return fmt.Sprintf("%s image, %d bytes, sha256=%x", format, len(data), sha256.Sum256(data)), nil
}

func (l *labChecks) sendGroupImage(ctx context.Context, service *neiwangtong.Service, peer string, room neiwangtong.GroupRoster, format string, index uint64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	path := filepath.Join(l.opt.labDir, "GoIM群图-"+l.runID+"."+imageFixtureExtension(format))
	data, err := writeFixtureImage(path, format, 51, 35, color.RGBA{G: 206, A: 255})
	if err != nil {
		return "", err
	}
	task, selector := 1249+index, 601249+index
	if err := service.SendGroupImage(peer, room.ID, 7799+index, fmt.Sprintf("%s%016x", l.runID, 100+index), task, selector,
		int64(len(data)), md5.Sum(data), path); err != nil {
		return "", err
	}
	if _, err := wait(ctx, 8*time.Second, l.progress, func(p labProgress) bool {
		return p.peer == peer && p.task == task && p.complete && p.done == int64(len(data))
	}); err != nil {
		return "", fmt.Errorf("the original did not download the group image: %w", err)
	}
	cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache")
	if err := waitForCachedImage(ctx, cache, data); err != nil {
		return "", fmt.Errorf("the original did not save a matching group image: %w", err)
	}
	return fmt.Sprintf("reference cached %s, %d bytes, sha256=%x", format, len(data), sha256.Sum256(data)), nil
}
