package main

import (
	"context"
	"errors"
	"fmt"

	"goim/internal/core"
	"goim/internal/domain"
)

func verifyPresence(ctx context.Context, node *core.Node, peer string,
	command func(context.Context, ...string) ([]byte, error), run func(string, func() (string, error)) bool) bool {
	return run("original_presence", func() (string, error) {
		for _, step := range []struct {
			action string
			mode   domain.Presence
		}{{"away", domain.PresenceAway}, {"available", domain.PresenceOnline}} {
			if _, err := command(ctx, step.action); err != nil {
				return "", err
			}
			if err := wait(ctx, node, func(s domain.State) bool {
				p := s.Peer(peer)
				return p.Online && p.Presence == step.mode
			}); err != nil {
				return "", fmt.Errorf("the original did not reach %s: %w", step.mode, err)
			}
		}
		return "the same original contact reported Away and Online in order", nil
	})
}

func verifyOffline(ctx context.Context, node *core.Node, options core.Options, peer, chat, token string,
	command func(context.Context, ...string) ([]byte, error), run func(string, func() (string, error)) bool) bool {
	body := "IPMSG离线补投-" + token
	pending := func(s domain.State) bool {
		if p := s.Peer(peer); p.ID != peer || p.Online {
			return false
		}
		for _, msg := range s.ChatMessages(chat) {
			if msg.Text == body && msg.Status == "pending" {
				return true
			}
		}
		return false
	}
	if !run("queue_while_original_is_closed", func() (string, error) {
		if _, err := command(ctx, "stop"); err != nil {
			return "", err
		}
		if err := wait(ctx, node, func(s domain.State) bool { return !s.Peer(peer).Online }); err != nil {
			return "", fmt.Errorf("original exit not observed: %w", err)
		}
		node.Submit(domain.Command{Kind: domain.SendText, ConversationID: chat, Text: body})
		return "the original exited and GoIM saved a queued message", wait(ctx, node, pending)
	}) {
		return false
	}
	return run("offline_text_after_both_restart", func() (string, error) {
		if err := node.Close(); err != nil {
			return "", err
		}
		reopened, err := core.New(options)
		if err != nil {
			return "", err
		}
		defer reopened.Close()
		if !pending(reopened.Snapshot()) {
			return "", errors.New("the same conversation did not retain its queued message across restart")
		}
		if _, err := command(ctx, "start"); err != nil {
			return "", err
		}
		if err := wait(ctx, reopened, func(s domain.State) bool {
			if !s.Peer(peer).Online {
				return false
			}
			count := 0
			for _, msg := range s.ChatMessages(chat) {
				if msg.Text == body && msg.Status == "sent" {
					count++
				}
			}
			return count == 1
		}); err != nil {
			state := reopened.Snapshot()
			for _, msg := range state.ChatMessages(chat) {
				if msg.Text == body {
					return "", fmt.Errorf("original reconnect did not confirm the queued message (online=%t, status=%s): %w",
						state.Peer(peer).Online, msg.Status, err)
				}
			}
			return "", err
		}
		return "the re-opened original was discovered automatically and confirmed the queued Chinese message", nil
	})
}
