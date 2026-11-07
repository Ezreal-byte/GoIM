package core

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
)

type credential struct {
	Salt []byte
	Hash []byte
}

const passwordRounds = 600_000

func (p credential) set() bool { return len(p.Salt) == 16 && len(p.Hash) == 32 }

func newCredential(password string) (credential, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 6 || len(password) > 256 {
		return credential{}, errors.New("密码至少 6 个字符，且不能超过 256 字节")
	}
	p := credential{Salt: make([]byte, 16)}
	rand.Read(p.Salt)
	hash, err := pbkdf2.Key(sha256.New, password, p.Salt, passwordRounds, 32)
	p.Hash = hash
	return p, err
}

func (p credential) matches(password string) bool {
	if !p.set() || len(password) == 0 || len(password) > 256 {
		return false
	}
	hash, err := pbkdf2.Key(sha256.New, password, p.Salt, passwordRounds, 32)
	return err == nil && subtle.ConstantTimeCompare(hash, p.Hash) == 1
}

func (n *Node) updateLock(c domain.Command) error {
	n.mu.Lock()
	old, locked := n.data.LockCredential, n.data.State.Locked
	remaining := time.Until(n.unlockAfter)
	n.mu.Unlock()
	switch c.Kind {
	case domain.SetLockPassword:
		if remaining > 0 && old.set() {
			return errors.New("请稍等片刻后再试")
		}
		if old.set() && !old.matches(c.Password) {
			n.delayUnlock()
			return errors.New("当前密码不正确")
		}
		next, err := newCredential(c.Text)
		if err != nil {
			return err
		}
		return n.durable(func(p *persisted) []pending {
			p.LockCredential = next
			p.State.Settings.HasLockPassword = true
			p.State.Locked = c.Bool
			p.State.Error, p.State.Notice = "", "锁定密码已保存"
			return nil
		})
	case domain.LockApp:
		if !old.set() {
			return errors.New("请先设置锁定密码")
		}
		return n.change(func(s *domain.State) {
			s.Locked = true
			s.Error, s.Notice = "", ""
		})
	case domain.UnlockApp:
		if !locked {
			return nil
		}
		if remaining > 0 {
			return errors.New("请稍等片刻后再试")
		}
		if !old.matches(c.Password) {
			n.delayUnlock()
			return errors.New("密码不正确")
		}
		return n.change(func(s *domain.State) {
			s.Locked = false
			s.Error, s.Notice = "", ""
		})
	}
	return nil
}

func (n *Node) delayUnlock() {
	n.mu.Lock()
	n.unlockAfter = time.Now().Add(time.Second)
	n.mu.Unlock()
}

func (n *Node) deleteConversation(id string) error {
	return n.durable(func(p *persisted) []pending {
		s := &p.State
		i := convIndex(s, id)
		if i < 0 {
			return nil
		}
		c := &s.Conversations[i]
		c.Hidden, c.Pinned, c.Unread, c.Draft, c.Preview = true, false, 0, "", ""
		messages := s.Messages[:0]
		for _, m := range s.Messages {
			if m.ConversationID != id {
				messages = append(messages, m)
			}
		}
		clear(s.Messages[len(messages):])
		s.Messages = messages
		s.Notice = "会话记录已删除"
		return nil
	})
}
