package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"goim/internal/domain"
	"goim/internal/printing"
)

const printCannotCancel = "当前打印阶段无法取消，请查看最新打印状态"

func printStatusForPeer(status string, peer domain.Peer) string {
	if status == domain.PrintStatusCancelled && !peer.PrintCancelSupported {
		return domain.PrintStatusFailed
	}
	return status
}

func (n *Node) cancelPrintJob(id string) error {
	s := n.snapshot(false)
	job := s.PrintJob(id)
	if job.ID == "" {
		return errors.New("找不到打印任务")
	}
	if job.PrinterOwnerID == s.Self.ID {
		return n.cancelOwnedPrint(s.Self.ID, id, "")
	}
	if job.SenderID != s.Self.ID {
		return errors.New("只能取消自己发送的打印任务")
	}
	var denied error
	err := n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, id)
		if i < 0 || !p.State.PrintJobs[i].CanCancel() {
			denied = errors.New(printCannotCancel)
			return nil
		}
		j := &p.State.PrintJobs[i]
		if !p.State.Peer(j.PrinterOwnerID).PrintCancelSupported {
			denied = errors.New("对方版本不支持远程取消打印")
			return nil
		}
		j.CancelRequest, j.CancelError = newID(), ""
		return []pending{{PeerID: j.PrinterOwnerID, Envelope: envelope{ID: j.CancelRequest, Type: "print_cancel", PrintJobID: id}}}
	})
	return errors.Join(err, denied)
}

func (n *Node) cancelOwnedPrint(actor, id, request string) error {
	if !validPrintID(id) || request != "" && !validPrintID(request) {
		return errors.New("无效的打印取消请求")
	}
	n.printCancelMu.Lock()
	defer n.printCancelMu.Unlock()
	s := n.snapshot(false)
	job := s.PrintJob(id)
	local := actor == s.Self.ID
	if !local && !validPrintID(request) {
		return errors.New("无效的打印取消请求")
	}
	if job.ID == "" || job.PrinterOwnerID != s.Self.ID || !local && job.SenderID != actor {
		if local {
			return errors.New("找不到可取消的打印任务")
		}
		// Return an explicit, non-disclosing result even when the owner has
		// already cleared its records. Otherwise the sender waits indefinitely.
		return n.durable(func(*persisted) []pending {
			return []pending{{PeerID: actor, Envelope: envelope{Type: "print_cancel_result", PrintJobID: id, CancelRequest: request, Error: "未找到你可以取消的打印任务"}}}
		})
	}
	var problem string
	switch {
	case job.Status == domain.PrintStatusCancelled: // Replay the authoritative result.
	case !job.CanCancel():
		problem = printCannotCancel
	case job.Status == domain.PrintStatusQueued:
		backend, ok := n.printBackend.(interface {
			Cancel(context.Context, printing.Receipt) error
		})
		if !ok || job.LocalJobID == "" {
			problem = "系统打印队列不支持取消此任务"
			break
		}
		receipt := printing.Receipt{Printer: job.PrinterDevice, JobID: job.LocalJobID}
		if err := backend.Cancel(n.ctx, receipt); err != nil {
			problem = fmt.Sprintf("系统打印队列拒绝取消：%.300s", err)
		}
	}
	err := n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, id)
		if i < 0 {
			return nil
		}
		j := &p.State.PrintJobs[i]
		if j.Terminal() && j.Status != domain.PrintStatusCancelled && problem == "" {
			problem = printCannotCancel
		}
		if problem != "" {
			j.CancelError = problem
			if local {
				return nil
			}
			return []pending{{PeerID: actor, Envelope: envelope{Type: "print_cancel_result", PrintJobID: id, CancelRequest: request, Error: problem}}}
		}
		if j.Status != domain.PrintStatusCancelled {
			j.Error = "打印任务已取消"
			if j.Status == domain.PrintStatusQueued {
				j.Error = "系统队列已接受取消，已输出的页面无法撤回"
			}
		}
		j.Status, j.CancelError, j.UpdatedAt = domain.PrintStatusCancelled, "", time.Now()
		n.cancelPrintTransferState(p, *j)
		return []pending{{PeerID: j.SenderID, Envelope: envelope{Type: "print_status", PrintJobID: id, PrintStatus: printStatusForPeer(j.Status, p.State.Peer(j.SenderID)), Error: j.Error}}}
	})
	if err != nil {
		return err
	}
	if problem == "" {
		n.removePrintFile(id)
	} else if local {
		return errors.New(problem)
	}
	return nil
}

func (n *Node) cancelPrintTransferState(p *persisted, job domain.PrintJob) {
	i := transferIndex(&p.State, job.TransferID)
	if i < 0 {
		return
	}
	t := &p.State.Transfers[i]
	if t.PrintJobID != job.ID || terminalTransfer(t.Status) {
		return
	}
	t.Status, t.Speed, t.UpdatedAt = "cancelled", 0, time.Now()
	if t.Direction == "send" {
		t.Revoked = true
	}
	p.TransferTombstones[t.PeerID+":"+t.ID] = "cancelled"
	p.queuePartialCleanup(*t)
	if stop := n.transfers[t.ID]; stop != nil {
		stop()
	}
}

func (n *Node) receivePrintCancelResult(peer string, e envelope) error {
	if !validPrintID(e.PrintJobID) || !validPrintID(e.CancelRequest) || e.Error == "" || len(e.Error) > 2048 {
		return errors.New("无效的打印取消结果")
	}
	return n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, e.PrintJobID)
		if i < 0 {
			return nil
		}
		j := &p.State.PrintJobs[i]
		if j.PrinterOwnerID == peer && j.SenderID == p.State.Self.ID && j.CancelRequest == e.CancelRequest {
			j.CancelRequest, j.CancelError = "", e.Error
		}
		return nil
	})
}
