package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkGroup(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	driveErr := l.drive(ctx, "probe-group")
	event, err := wait(ctx, 4*time.Second, l.rosters, func(e labRoster) bool {
		return e.peer == peer && e.roster.OwnerID == peer &&
			slices.ContainsFunc(e.roster.Members, func(m neiwangtong.GroupMember) bool { return m.ID == l.runID+"0000000000000000" })
	})
	if err != nil {
		return "", errors.Join(driveErr, err)
	}
	body := "群聊回复-" + l.runID
	if err := l.driveFor(ctx, "group-reply", event.roster.Name, body); err != nil {
		return "", err
	}
	if _, err := wait(ctx, 4*time.Second, l.groupTexts, func(got labRoomMessage) bool {
		return got.peer == peer && got.message.GroupID == event.roster.ID && got.message.Message.Text() == body
	}); err != nil {
		return "", fmt.Errorf("no matching group text from the stock client: %w", err)
	}
	reply := "GoIM 群聊 & 中文 " + l.runID
	if err := service.SendGroupText(peer, event.roster.ID, 7798, l.runID+l.runID, reply); err != nil {
		return "", fmt.Errorf("GoIM group reply transport: %w", err)
	}
	if err := l.driveFor(ctx, "group-check", event.roster.Name, body); err != nil {
		return "", fmt.Errorf("could not read the known original message from its transcript: %w", err)
	}
	if err := l.driveFor(ctx, "group-check", event.roster.Name, reply); err != nil {
		return "", fmt.Errorf("the original transcript did not contain the GoIM reply: %w", err)
	}
	return "original roster and inbound group text matched; the original transcript contained the exact GoIM group reply", nil
}
