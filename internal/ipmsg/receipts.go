package ipmsg

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// ErrReceiptTimeout leaves delivery uncertain: the receiver may have accepted
// the message while its receipt was lost. A later attempt must reuse the number.
var ErrReceiptTimeout = errors.New("未收到飞鸽／飞秋的送达确认")
var ErrPeerOffline = errors.New("飞鸽／飞秋联系人已离线")

type messageReceiptKey struct {
	peer   string
	packet uint64
}

type messageReceipt struct {
	transferID string
	address    netip.AddrPort
	done       chan struct{}
}

// SendTextConfirmed waits for IPMSG_RECVMSG, not a read notification. All
// retries carry the same packet number and bytes so the peer can deduplicate.
func (s *Service) SendTextConfirmed(ctx context.Context, peer string, number uint64, text string) error {
	return s.sendConfirmed(ctx, peer, number, text, "", "")
}

func (s *Service) sendConfirmed(ctx context.Context, peer string, number uint64, text, attachments, transferID string) error {
	if number == 0 {
		return errors.New("invalid IPMSG message number")
	}
	state := s.peer(peer)
	if state == nil {
		return ErrPeerOffline
	}
	data, err := s.messagePacket(state, number, text, attachments)
	if err != nil {
		return err
	}
	address := state.addr.AddrPort()
	address = netip.AddrPortFrom(address.Addr().Unmap(), address.Port())
	key := messageReceiptKey{peer, number}
	receipt := messageReceipt{transferID: transferID, address: address, done: make(chan struct{}, 1)}
	s.mu.Lock()
	if s.ctx.Err() != nil || len(s.receipts) >= 1024 {
		s.mu.Unlock()
		return errors.New("IPMSG message confirmation unavailable")
	}
	if _, exists := s.receipts[key]; exists {
		s.mu.Unlock()
		return errors.New("IPMSG message is already awaiting confirmation")
	}
	s.receipts[key] = receipt
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.receipts, key)
		s.mu.Unlock()
	}()
	for range 3 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if transferID != "" && s.cfg.Callbacks.AllowFile != nil && !s.cfg.Callbacks.AllowFile(transferID) {
			return context.Canceled
		}
		if err := s.sendToLogin(state, data); err != nil {
			return err
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-receipt.done:
			timer.Stop()
			return nil
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-s.ctx.Done():
			timer.Stop()
			return s.ctx.Err()
		}
	}
	return ErrReceiptTimeout
}

func (s *Service) receiveMessageReceipt(message packet, from *net.UDPAddr) {
	value, _, _ := strings.Cut(message.Extra, "\x00")
	number, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || number == 0 {
		return
	}
	key := messageReceiptKey{peerID(message.Host, message.Sender), number}
	address := from.AddrPort()
	address = netip.AddrPortFrom(address.Addr().Unmap(), address.Port())
	s.mu.Lock()
	receipt, known := s.receipts[key]
	s.mu.Unlock()
	if known && receipt.address == address {
		select {
		case receipt.done <- struct{}{}:
		default:
		}
	}
}

// A valid TCP request acknowledges only the file invitation that registered it.
func (s *Service) confirmFileRequest(number uint64, entry servingFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, found := s.receipts[messageReceiptKey{entry.peerID, number}]
	if found && receipt.transferID == entry.transferID && entry.recipient.Equal(net.IP(receipt.address.Addr().AsSlice())) {
		select {
		case receipt.done <- struct{}{}:
		default:
		}
	}
}
