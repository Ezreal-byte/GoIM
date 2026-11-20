package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"goim/internal/domain"
	"goim/internal/ipmsg"
)

func legacyOfferIDs(task domain.Transfer) (uint64, uint32, error) {
	number, err := strconv.ParseUint(task.RemotePacket, 16, 64)
	if err != nil || number == 0 {
		return 0, 0, errors.New("兼容文件邀请标识无效")
	}
	id, err := parseHexUint32(task.RemoteFile)
	return number, id, err
}

var errLegacySourceChanged = errors.New("源文件已变化或不可用，请重新发送")

func verifiedLegacyOffer(ctx context.Context, service *ipmsg.Service, peer string, t domain.Transfer, f domain.File) (ipmsg.FileOffer, error) {
	if err := ctx.Err(); err != nil {
		return ipmsg.FileOffer{}, err
	}
	number, id, err := legacyOfferIDs(t)
	if err != nil {
		return ipmsg.FileOffer{}, err
	}
	source, err := os.Open(f.SourcePath)
	if err != nil {
		return ipmsg.FileOffer{}, errLegacySourceChanged
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return ipmsg.FileOffer{}, errLegacySourceChanged
	}
	offer := ipmsg.FileOffer{Packet: number, ID: id, TransferID: t.ID, Name: f.Name, Path: f.SourcePath, Size: f.Size, Source: info, Digest: f.Hash}
	if service != nil && service.VerifiedFile(peer, offer) {
		return offer, nil
	}
	digest, err := fileReaderHashContext(ctx, source)
	if ctx.Err() != nil {
		return ipmsg.FileOffer{}, ctx.Err()
	}
	if err != nil || digest != f.Hash {
		return ipmsg.FileOffer{}, errLegacySourceChanged
	}
	return offer, nil
}

func (n *Node) sendLegacyOffer(service *ipmsg.Service, wire string, value pending) error {
	s := n.snapshot(false)
	i := transferIndex(&s, value.Envelope.TransferID)
	if i < 0 {
		return nil
	}
	t := s.Transfers[i]
	if t.Direction != "send" || t.Revoked || terminalTransfer(t.Status) {
		return nil
	}
	// One file per invitation. Sequence is allocated with the durable outbox
	// entry and keeps file invitations and text packets in the same namespace.
	if t.RemotePacket == "" {
		t.RemotePacket, t.RemoteFile = fmt.Sprintf("%x", max(value.Sequence, 1)), "0"
		prepared := false
		if err := n.change(func(s *domain.State) {
			i := transferIndex(s, t.ID)
			if i < 0 {
				return
			}
			current := &s.Transfers[i]
			if current.Revoked || terminalTransfer(current.Status) {
				return
			}
			current.LegacyTransport, current.RemotePeerID = legacyIPMSG, wire
			current.RemotePacket, current.RemoteFile = t.RemotePacket, t.RemoteFile
			prepared = true
		}); err != nil || !prepared {
			return err
		}
	}
	offer, err := verifiedLegacyOffer(n.ctx, service, wire, t, s.File(t.FileID))
	if err != nil {
		return err
	}
	return service.SendFileOffer(n.ctx, wire, offer, "")
}

func (n *Node) restoreIPMSGOffers(peer string) {
	n.mu.Lock()
	service := n.legacy
	wire := wireCompatibilityID(&n.data, peer, legacyIPMSG)
	type outgoing struct {
		task domain.Transfer
		file domain.File
	}
	var tasks []outgoing
	if service != nil && wire != "" {
		awaiting := make(map[string]bool)
		for _, p := range n.data.Outbox {
			if p.PeerID == peer && p.Envelope.Type == "file_offer" {
				awaiting[p.Envelope.TransferID] = true
			}
		}
		for _, t := range n.data.State.Transfers {
			if t.PeerID != peer || t.RemotePeerID != wire || t.Direction != "send" || t.LegacyTransport != legacyIPMSG || t.Revoked ||
				terminalTransfer(t.Status) || t.RemotePacket == "" || awaiting[t.ID] || t.Error == errLegacySourceChanged.Error() {
				continue
			}
			tasks = append(tasks, outgoing{t, n.data.State.File(t.FileID)})
		}
	}
	n.mu.Unlock()
	for _, item := range tasks {
		t := item.task
		offer, err := verifiedLegacyOffer(n.ctx, service, wire, t, item.file)
		if err == nil {
			err = service.RememberFile(wire, offer)
		}
		if err != nil && !errors.Is(err, ipmsg.ErrPeerOffline) && !errors.Is(err, context.Canceled) {
			n.setTransfer(t.ID, "failed", t.Done, 0, err.Error())
		}
	}
}

// Reopen the original inbound invitation. IPMSG downloads are receiver-initiated.
func (n *Node) retryLegacyReceive(t domain.Transfer) error {
	if _, _, err := legacyOfferIDs(t); err != nil {
		return errors.New("原文件邀请已失效，请让对方重新发送")
	}
	var denied error
	err := n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, t.ID)
		if i < 0 {
			denied = errors.New("找不到传输任务")
			return nil
		}
		task := &p.State.Transfers[i]
		if task.Status != "cancelled" && task.Status != "rejected" && task.Status != "retry_requested" {
			denied = errors.New("文件任务状态已变化")
			return nil
		}
		if n.transfers[t.ID] != nil {
			denied = errors.New("上次接收正在结束，请稍后重试")
			return nil
		}
		if task.Revoked || p.fileMessageRecalled(*task) || !conversationAllows(&p.State, task.ConversationID, task.PeerID) {
			denied = errors.New("原文件邀请已失效，请让对方重新发送")
			return nil
		}
		delete(p.TransferTombstones, task.PeerID+":"+task.ID)
		task.Status, task.Error, task.UpdatedAt = "waiting", "", time.Now()
		return nil
	})
	if err != nil || denied != nil {
		return errors.Join(err, denied)
	}
	n.startReceive(t.ID, t.Path)
	return nil
}
