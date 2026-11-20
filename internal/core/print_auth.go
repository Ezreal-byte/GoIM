package core

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"slices"
	"time"

	"goim/internal/domain"
)

var errPrinterAuthorization = errors.New("打印密码已变更或尚未验证，请重新验证")

type printerGrant struct {
	Revision  string
	TokenHash []byte
}

type printerAccess struct {
	OwnerID  string
	Revision string
	Token    string
}

type printerAuthMessage struct {
	PrinterID string
	Revision  string
	RequestID string
	Proof     string
	Token     string
}

type printerAuthWaiter struct {
	peer  string
	reply chan envelope
}

func printerPasswordProof(key []byte, printer, peer, revision, request string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("GoIM printer authorization\x00" + printer + "\x00" + peer + "\x00" + revision + "\x00" + request))
	return hex.EncodeToString(mac.Sum(nil))
}

func (n *Node) setPrinterPinned(c domain.Command) error {
	if n.snapshot(false).Printer(c.PrinterID).ID == "" {
		return errors.New("共享打印机已不存在")
	}
	return n.change(func(s *domain.State) {
		s.Settings.PinnedPrinters = slices.DeleteFunc(s.Settings.PinnedPrinters, func(id string) bool { return id == c.PrinterID })
		if c.Bool {
			s.Settings.PinnedPrinters = append(s.Settings.PinnedPrinters, c.PrinterID)
		}
	})
}

func (n *Node) authenticatePrinter(c domain.Command) {
	n.mu.Lock()
	if n.printAuthResults == nil {
		n.printAuthResults = make(map[string]domain.PrinterAuthResult)
	}
	if n.printAuthActive[c.PrinterID] {
		n.printAuthResults[c.PrinterID] = domain.PrinterAuthResult{Request: c.Request, Error: "正在验证打印密码，请稍后重试"}
		n.data.State.Revision++
		n.mu.Unlock()
		n.signal()
		return
	}
	if n.printAuthActive == nil {
		n.printAuthActive = make(map[string]bool)
	}
	n.printAuthActive[c.PrinterID] = true
	n.printAuthResults[c.PrinterID] = domain.PrinterAuthResult{Request: c.Request, Pending: true}
	n.data.State.Revision++
	n.mu.Unlock()
	n.signal()
	n.async(func() {
		err := n.requestPrinterAccess(c.PrinterID, c.Password)
		result := domain.PrinterAuthResult{Request: c.Request}
		if err != nil {
			result.Error = err.Error()
		}
		n.mu.Lock()
		delete(n.printAuthActive, c.PrinterID)
		if current := n.printAuthResults[c.PrinterID]; current.Request == c.Request && current.Pending {
			n.printAuthResults[c.PrinterID] = result
			n.data.State.Revision++
		}
		n.mu.Unlock()
		n.signal()
	})
}

func (n *Node) requestPrinterAccess(id, password string) error {
	s := n.snapshot(false)
	share := s.Printer(id)
	if share.ID == "" || !share.Available || !s.Peer(share.OwnerID).Online {
		return errors.New("共享打印机当前不可用")
	}
	if !share.PasswordRequired {
		return nil
	}
	if len(password) == 0 || len(password) > 256 || len(share.AuthSalt) != 16 || !validPrintID(share.AuthRevision) {
		return errors.New("请输入有效的打印密码")
	}
	key, err := pbkdf2.Key(sha256.New, password, share.AuthSalt, passwordRounds, 32)
	if err != nil {
		return err
	}
	request := newID()
	auth := printerAuthMessage{PrinterID: id, Revision: share.AuthRevision, RequestID: request,
		Proof: printerPasswordProof(key, id, s.Self.ID, share.AuthRevision, request)}
	clear(key)
	reply := make(chan envelope, 1)
	n.mu.Lock()
	if n.printAuthWaiters == nil {
		n.printAuthWaiters = make(map[string]printerAuthWaiter)
	}
	n.printAuthWaiters[request] = printerAuthWaiter{peer: share.OwnerID, reply: reply}
	n.mu.Unlock()
	defer func() { n.mu.Lock(); delete(n.printAuthWaiters, request); n.mu.Unlock() }()
	if err := n.send(share.OwnerID, envelope{Type: "printer_auth", PrintAuth: auth}); err != nil {
		return errors.New("无法连接打印机共享人，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return errors.New("打印密码验证超时，请稍后重试")
	case response := <-reply:
		if len(response.Error) > 512 {
			return errors.New("打印密码验证结果无效")
		}
		if response.Error != "" {
			return errors.New(response.Error)
		}
		access := response.PrintAuth
		if access.PrinterID != id || access.Revision != share.AuthRevision || !validPrintID(access.Token) {
			return errors.New("打印密码验证结果无效")
		}
		var changed bool
		err := n.durable(func(p *persisted) []pending {
			current := p.State.Printer(id)
			if current.OwnerID != share.OwnerID || current.AuthRevision != share.AuthRevision {
				changed = true
				return nil
			}
			if p.PrintAccess == nil {
				p.PrintAccess = make(map[string]printerAccess)
			}
			p.PrintAccess[id] = printerAccess{OwnerID: share.OwnerID, Revision: share.AuthRevision, Token: access.Token}
			return nil
		})
		if changed {
			return errors.New("打印密码已变更，请重新验证")
		}
		return err
	}
}

func (n *Node) receivePrinterAuthResult(peer string, e envelope) {
	n.mu.Lock()
	waiter, ok := n.printAuthWaiters[e.PrintAuth.RequestID]
	n.mu.Unlock()
	if ok && waiter.peer == peer {
		select {
		case waiter.reply <- e:
		default:
		}
	}
}

func (n *Node) receivePrinterAuth(peer string, e envelope) error {
	auth := e.PrintAuth
	if !validPrintID(auth.RequestID) || !validPrintID(auth.PrinterID) || !validPrintID(auth.Revision) || len(auth.Proof) != 64 {
		return errors.New("无效打印验证请求")
	}
	response := envelope{Type: "printer_auth_result", PrintAuth: printerAuthMessage{
		PrinterID: auth.PrinterID, Revision: auth.Revision, RequestID: auth.RequestID}}
	n.mu.Lock()
	if n.printAuthAfter == nil {
		n.printAuthAfter = make(map[string]time.Time)
	}
	throttled := time.Now().Before(n.printAuthAfter[peer])
	n.printAuthAfter[peer] = time.Now().Add(time.Second)
	n.mu.Unlock()
	if throttled {
		response.Error = "验证过于频繁，请稍等片刻后再试"
		return n.send(peer, response)
	}
	token := newID()
	tokenHash := sha256.Sum256([]byte(token))
	err := n.durable(func(p *persisted) []pending {
		share := p.State.Printer(auth.PrinterID)
		secret := p.PrintCredentials[share.ID]
		if share.OwnerID != p.State.Self.ID || !share.PasswordRequired || share.AuthRevision != auth.Revision || !secret.set() {
			response.Error = "打印密码或共享状态已变更，请刷新后重试"
			return nil
		}
		expected := printerPasswordProof(secret.Hash, share.ID, peer, share.AuthRevision, auth.RequestID)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(auth.Proof)) != 1 {
			response.Error = "打印密码不正确，请重新输入"
			return nil
		}
		if p.PrintGrants == nil {
			p.PrintGrants = make(map[string]printerGrant)
		}
		p.PrintGrants[share.ID+":"+peer] = printerGrant{Revision: share.AuthRevision, TokenHash: tokenHash[:]}
		response.PrintAuth.Token = token
		return nil
	})
	if err != nil {
		response.Error = "无法保存打印授权，请稍后重试"
		response.PrintAuth.Token = ""
	}
	return n.send(peer, response)
}

func validPrinterAccess(p *persisted, share domain.PrinterShare, peer, token string) bool {
	if !share.PasswordRequired {
		return true
	}
	grant := p.PrintGrants[share.ID+":"+peer]
	if grant.Revision != share.AuthRevision || !validPrintID(token) {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(hash[:], grant.TokenHash) == 1
}

func setPrinterCredential(p *persisted, share *domain.PrinterShare, next credential) {
	if p.PrintCredentials == nil {
		p.PrintCredentials = make(map[string]credential)
	}
	delete(p.PrintCredentials, share.ID)
	share.PasswordRequired = next.set()
	share.AuthRevision = newID()
	share.AuthSalt = nil
	if next.set() {
		p.PrintCredentials[share.ID] = next
		share.AuthSalt = append([]byte(nil), next.Salt...)
	}
	for key := range p.PrintGrants {
		if len(key) > len(share.ID) && key[:len(share.ID)+1] == share.ID+":" {
			delete(p.PrintGrants, key)
		}
	}
}
