package core

import (
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
	"goim/internal/printing"
)

const (
	maxPrinterShares     = 256
	maxPrinterName       = 80
	maxPendingPrintBytes = 512 << 20
)

func validPrintID(id string) bool {
	if id == "" || id != strings.ToLower(id) {
		return false
	}
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 16
}

func validPrintText(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	return !strings.ContainsAny(value, "\r\n\x00")
}

func sanitizedPrinterShare(share domain.PrinterShare) domain.PrinterShare {
	share.DeviceName = ""
	share.Capabilities = printing.NormalizeCapabilities(share.Capabilities)
	return share
}

func publicPrintJob(job domain.PrintJob) domain.PrintJob {
	job.PrinterDevice = ""
	job.LocalJobID = ""
	job.SubmitKey = ""
	job.Error = ""
	return job
}

func printerShareIndex(s *domain.State, id string) int {
	for i := range s.Printers {
		if s.Printers[i].ID == id {
			return i
		}
	}
	return -1
}

func printJobIndex(s *domain.State, id string) int {
	for i := range s.PrintJobs {
		if s.PrintJobs[i].ID == id {
			return i
		}
	}
	return -1
}

func printJobForTransferIndex(s *domain.State, transferID string) int {
	for i := range s.PrintJobs {
		if s.PrintJobs[i].TransferID == transferID {
			return i
		}
	}
	return -1
}

func (n *Node) refreshPrinters() {
	n.mu.Lock()
	if n.closing || n.printersLoading {
		n.mu.Unlock()
		return
	}
	n.printersLoading = true
	n.printError = ""
	n.mu.Unlock()
	n.signal()

	n.async(func() {
		printers, err := n.printBackend.List(n.ctx)
		if n.ctx.Err() != nil {
			return
		}
		if err != nil {
			n.mu.Lock()
			n.printersLoading = false
			n.printError = err.Error()
			n.mu.Unlock()
			n.signal()
			return
		}
		local := make([]domain.LocalPrinter, 0, len(printers))
		for _, printer := range printers {
			if !validPrintText(printer.ID, 512) || !validPrintText(printer.Name, maxPrinterName) {
				continue
			}
			local = append(local, domain.LocalPrinter{
				ID: printer.ID, Name: printer.Name, Available: printer.Available,
				Default: printer.Default, Status: strings.TrimSpace(printer.Status),
				Capabilities: printing.NormalizeCapabilities(printer.Capabilities),
			})
		}
		sort.SliceStable(local, func(i, j int) bool {
			if local[i].Available != local[j].Available {
				return local[i].Available
			}
			if local[i].Default != local[j].Default {
				return local[i].Default
			}
			return local[i].Name < local[j].Name
		})
		n.mu.Lock()
		n.localPrinters = local
		n.printersLoading = false
		n.printError = ""
		n.mu.Unlock()
		n.signal()
		n.syncLocalPrinterShares(local)
	})
}

func (n *Node) syncLocalPrinterShares(printers []domain.LocalPrinter) {
	byID := make(map[string]domain.LocalPrinter, len(printers))
	for _, printer := range printers {
		byID[printer.ID] = printer
	}
	changed := false
	err := n.durable(func(p *persisted) []pending {
		for i := range p.State.Printers {
			share := &p.State.Printers[i]
			if share.OwnerID != p.State.Self.ID || share.DeviceName == "" {
				continue
			}
			printer, ok := byID[share.DeviceName]
			available := ok && printer.Available
			capabilities := share.Capabilities
			if ok {
				capabilities = printer.Capabilities
			}
			if share.Available == available && samePrintCapabilities(share.Capabilities, capabilities) {
				continue
			}
			share.Available = available
			share.Capabilities = capabilities
			changed = true
		}
		return nil
	})
	if err == nil && changed {
		n.broadcastPrinterCatalog()
	}
}

func samePrintCapabilities(a, b domain.PrintCapabilities) bool {
	return slices.Equal(a.Papers, b.Papers) && slices.Equal(a.Orientations, b.Orientations) &&
		slices.Equal(a.ColorModes, b.ColorModes) && slices.Equal(a.DuplexModes, b.DuplexModes) &&
		a.SupportsPageRange == b.SupportsPageRange
}

func (n *Node) sharePrinter(c domain.Command) error {
	queue := strings.TrimSpace(c.PrinterID)
	if queue == "" {
		return errors.New("请选择本机打印机")
	}
	s := n.snapshot(false)
	var local domain.LocalPrinter
	found := false
	for _, printer := range s.LocalPrinters {
		if printer.ID == queue {
			local, found = printer, true
			break
		}
	}
	if !found {
		return errors.New("本机打印机列表已变化，请刷新后重试")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = strings.TrimSpace(s.Self.Name) + "-" + local.Name
	}
	if !validPrintText(name, maxPrinterName) {
		return fmt.Errorf("共享名称需为 1 至 %d 个字符且不能换行", maxPrinterName)
	}
	capabilities := printing.NormalizeCapabilities(local.Capabilities)
	updatePassword := c.Setting == "password" || c.Password != ""
	var next credential
	if c.Password != "" {
		var err error
		next, err = newCredential(c.Password)
		if err != nil {
			return err
		}
	}
	now := time.Now()
	err := n.durable(func(p *persisted) []pending {
		state := &p.State
		state.Notice, state.Error = "打印机已共享", ""
		for i := range state.Printers {
			share := &state.Printers[i]
			if share.OwnerID == state.Self.ID && share.DeviceName == queue {
				share.Name = name
				share.OwnerName = state.Self.Name
				share.Available = local.Available
				share.Capabilities = capabilities
				if updatePassword {
					setPrinterCredential(p, share, next)
				}
				return nil
			}
		}
		state.Printers = append(state.Printers, domain.PrinterShare{
			ID: newID(), OwnerID: state.Self.ID, OwnerName: state.Self.Name,
			Name: name, DeviceName: queue, Available: local.Available,
			Capabilities: capabilities, CreatedAt: now,
		})
		setPrinterCredential(p, &state.Printers[len(state.Printers)-1], next)
		return nil
	})
	if err != nil {
		return err
	}
	n.broadcastPrinterCatalog()
	return nil
}

func (n *Node) unsharePrinter(c domain.Command) error {
	id := strings.TrimSpace(c.PrinterID)
	removed := false
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		i := printerShareIndex(s, id)
		if i < 0 || s.Printers[i].OwnerID != s.Self.ID {
			return nil
		}
		setPrinterCredential(p, &s.Printers[i], credential{})
		delete(p.PrintCredentials, id)
		s.Printers = append(s.Printers[:i], s.Printers[i+1:]...)
		s.Notice, s.Error = "已停止共享打印机", ""
		removed = true
		return nil
	})
	if err != nil {
		return err
	}
	if !removed {
		return errors.New("找不到要停止共享的打印机")
	}
	n.broadcastPrinterCatalog()
	return nil
}

func (n *Node) printFile(c domain.Command) error {
	if len(c.Paths) != 1 || strings.TrimSpace(c.Paths[0]) == "" {
		return errors.New("请选择一个打印文件")
	}
	path, err := printing.ValidateFile(c.Paths[0])
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	s := n.snapshot(false)
	share := s.Printer(c.PrinterID)
	if share.ID == "" {
		return errors.New("共享打印机已不存在")
	}
	if !share.Available {
		return errors.New("共享打印机当前不可用")
	}
	if share.OwnerID == s.Self.ID {
		return errors.New("请选择局域网内其他成员共享的打印机")
	}
	owner := s.Peer(share.OwnerID)
	if !owner.PrintSupported || owner.Transport != domain.TransportGoIM {
		return errors.New("共享人的 GoIM 版本不支持打印共享")
	}
	if !owner.Online {
		return errors.New("共享打印机的主人当前不在线")
	}
	n.mu.Lock()
	access := n.data.PrintAccess[share.ID]
	n.mu.Unlock()
	if share.PasswordRequired && (access.OwnerID != share.OwnerID || access.Revision != share.AuthRevision || access.Token == "") {
		return errors.New("请先验证打印密码；密码变更后需要重新验证")
	}
	options, err := printing.NormalizeOptions(c.PrintOptions, printing.NormalizeCapabilities(share.Capabilities))
	if err != nil {
		return err
	}
	hash, err := fileHashContext(n.ctx, path)
	if err != nil {
		return err
	}
	now := time.Now()
	f := domain.File{
		ID: newID(), Name: filepath.Base(path), Size: info.Size(), Hash: hash,
		MIME: mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), SenderID: s.Self.ID,
		ConversationID: directID(s.Self.ID, owner.ID), SourcePath: path, CreatedAt: now,
	}
	if !validFile(f) {
		return errors.New("文件名不适合跨平台打印")
	}
	transferID, jobID, messageID := newID(), newID(), newID()
	job := domain.PrintJob{
		ID: jobID, MessageID: messageID, TransferID: transferID, FileID: f.ID,
		ConversationID: f.ConversationID, PrinterID: share.ID, PrinterName: share.Name,
		PrinterOwnerID: share.OwnerID, PrinterOwnerName: share.OwnerName,
		SenderID: s.Self.ID, SenderName: s.Self.Name, FileName: f.Name, FileSize: f.Size,
		MIME: f.MIME, Options: options, Status: domain.PrintStatusSending, CreatedAt: now, UpdatedAt: now,
	}
	var denied error
	err = n.durable(func(p *persisted) []pending {
		state := &p.State
		target := state.Printer(share.ID)
		peer := state.Peer(target.OwnerID)
		if target.ID == "" || target.OwnerID == state.Self.ID || !target.Available || !peer.Online || !peer.PrintSupported {
			denied = errors.New("共享打印机已不可用")
			return nil
		}
		if target.PasswordRequired && (access.OwnerID != target.OwnerID || access.Revision != target.AuthRevision) {
			denied = errors.New("打印密码已变更，请重新验证")
			return nil
		}
		conversation := ensureDirect(state, peer)
		f.ConversationID, job.ConversationID = conversation, conversation
		if fileIndex(state, f.ID) >= 0 || printJobIndex(state, job.ID) >= 0 {
			denied = errors.New("打印任务身份冲突")
			return nil
		}
		state.Files = append(state.Files, f)
		message := domain.Message{
			ID: messageID, ConversationID: conversation, SenderID: state.Self.ID,
			Kind: "print", Text: "打印 " + f.Name, FileID: f.ID, PrintJobID: job.ID,
			CreatedAt: now, Status: "sent",
		}
		addMessageState(state, message)
		offer := appendFileOffer(state, peer.ID, conversation, f, transferID, false)
		if i := transferIndex(state, transferID); i >= 0 {
			state.Transfers[i].PrintJobID = job.ID
		}
		state.PrintJobs = append(state.PrintJobs, job)
		offer.Envelope.PrintJob = publicPrintJob(job)
		if target.PasswordRequired {
			offer.Envelope.PrintToken = access.Token
		}
		return []pending{offer}
	})
	if err != nil {
		return err
	}
	return denied
}

func (n *Node) receivePrinterCatalog(peer string, e envelope) error {
	n.mu.Lock()
	remote := n.data.State.Peer(peer)
	n.mu.Unlock()
	if !remote.PrintSupported || remote.Transport != domain.TransportGoIM {
		return errors.New("对端不支持打印共享")
	}
	shares := make([]domain.PrinterShare, 0, min(len(e.Printers), maxPrinterShares))
	seen := make(map[string]bool, len(e.Printers))
	for _, share := range e.Printers {
		if len(shares) >= maxPrinterShares {
			break
		}
		share = sanitizedPrinterShare(share)
		if !validPrintID(share.ID) || seen[share.ID] || !validPrintText(share.Name, maxPrinterName) {
			continue
		}
		if share.PasswordRequired && (len(share.AuthSalt) != 16 || !validPrintID(share.AuthRevision)) {
			continue
		}
		if !share.PasswordRequired {
			share.AuthSalt = nil
		}
		seen[share.ID] = true
		share.OwnerID = peer
		share.OwnerName = remote.Name
		share.CreatedAt = time.Now()
		shares = append(shares, share)
	}
	return n.durable(func(p *persisted) []pending {
		if e.PrinterRevision < p.PrinterCatalogRevisions[peer] {
			return nil
		}
		p.PrinterCatalogRevisions[peer] = e.PrinterRevision
		s := &p.State
		s.Printers = slices.DeleteFunc(s.Printers, func(share domain.PrinterShare) bool {
			return share.OwnerID == peer
		})
		for _, share := range shares {
			if printerShareIndex(s, share.ID) < 0 {
				s.Printers = append(s.Printers, share)
			}
		}
		for id, access := range p.PrintAccess {
			if access.OwnerID == peer && (s.Printer(id).OwnerID != peer || s.Printer(id).AuthRevision != access.Revision) {
				delete(p.PrintAccess, id)
			}
		}
		sort.SliceStable(s.Printers, func(i, j int) bool {
			if s.Printers[i].Available != s.Printers[j].Available {
				return s.Printers[i].Available
			}
			if s.Printers[i].OwnerName != s.Printers[j].OwnerName {
				return s.Printers[i].OwnerName < s.Printers[j].OwnerName
			}
			return s.Printers[i].Name < s.Printers[j].Name
		})
		return nil
	})
}

func (n *Node) sendPrinterCatalog(peer string) {
	s := n.snapshot(false)
	if !s.Peer(peer).PrintSupported {
		return
	}
	list := make([]domain.PrinterShare, 0, len(s.Printers))
	for _, share := range s.Printers {
		if share.OwnerID == s.Self.ID {
			list = append(list, sanitizedPrinterShare(share))
		}
	}
	_ = n.send(peer, envelope{Type: "printer_catalog", Printers: list, PrinterRevision: s.Revision})
}

func (n *Node) broadcastPrinterCatalog() {
	s := n.snapshot(false)
	for _, peer := range s.Peers {
		if peer.Online && peer.PrintSupported {
			n.sendPrinterCatalog(peer.ID)
		}
	}
}

func (n *Node) receivePrintOffer(peer string, p *persisted, f domain.File, e envelope) (domain.PrintJob, bool, error) {
	incoming := e.PrintJob
	if !validPrintID(incoming.ID) || !validPrintID(incoming.MessageID) || incoming.TransferID != e.TransferID ||
		incoming.FileID != f.ID || incoming.SenderID != peer || incoming.PrinterOwnerID != p.State.Self.ID ||
		incoming.FileName != f.Name || incoming.FileSize != f.Size {
		return domain.PrintJob{}, false, errors.New("无效打印任务")
	}
	if f.Size <= 0 || f.Size > printing.MaxFileSize {
		return domain.PrintJob{}, false, fmt.Errorf("打印文件不能超过 %d MB", printing.MaxFileSize>>20)
	}
	i := printerShareIndex(&p.State, incoming.PrinterID)
	if i < 0 || p.State.Printers[i].OwnerID != p.State.Self.ID || p.State.Printers[i].DeviceName == "" {
		return domain.PrintJob{}, false, errors.New("打印机已停止共享")
	}
	share := p.State.Printers[i]
	if !validPrinterAccess(p, share, peer, e.PrintToken) {
		return domain.PrintJob{}, false, errPrinterAuthorization
	}
	if !share.Available {
		return domain.PrintJob{}, false, errors.New("共享打印机当前不可用")
	}
	options, err := printing.NormalizeOptions(incoming.Options, printing.NormalizeCapabilities(share.Capabilities))
	if err != nil {
		return domain.PrintJob{}, false, err
	}
	now := time.Now()
	job := domain.PrintJob{
		ID: incoming.ID, MessageID: incoming.MessageID, TransferID: incoming.TransferID, FileID: f.ID,
		ConversationID: directID(p.State.Self.ID, peer), PrinterID: share.ID, PrinterName: share.Name,
		PrinterDevice: share.DeviceName, PrinterOwnerID: p.State.Self.ID, PrinterOwnerName: p.State.Self.Name,
		SenderID: peer, SenderName: p.State.Peer(peer).Name, FileName: f.Name, FileSize: f.Size,
		MIME: f.MIME, Options: options, Status: domain.PrintStatusReceiving, CreatedAt: now, UpdatedAt: now,
	}
	return job, true, nil
}

func (n *Node) receivePrintStatus(peer string, e envelope) error {
	if !validPrintID(e.PrintJobID) {
		return errors.New("无效打印状态")
	}
	switch e.PrintStatus {
	case domain.PrintStatusQueued, domain.PrintStatusCompleted, domain.PrintStatusFailed:
	default:
		return errors.New("无效打印状态")
	}
	errText := strings.TrimSpace(e.Error)
	if len(errText) > 2048 {
		errText = errText[:2048]
	}
	return n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, e.PrintJobID)
		if i < 0 {
			return nil
		}
		job := &p.State.PrintJobs[i]
		if job.PrinterOwnerID != peer || job.SenderID != p.State.Self.ID || job.Status == e.PrintStatus {
			return nil
		}
		if job.Status == domain.PrintStatusCompleted || job.Status == domain.PrintStatusFailed {
			return nil
		}
		if e.PrintStatus == domain.PrintStatusQueued &&
			job.Status != domain.PrintStatusSending && job.Status != domain.PrintStatusReceiving {
			return nil
		}
		job.Status, job.Error, job.UpdatedAt = e.PrintStatus, errText, time.Now()
		return nil
	})
}

func (n *Node) sendPrintStatuses(peer string) {
	s := n.snapshot(false)
	for _, job := range s.PrintJobs {
		if job.PrinterOwnerID != s.Self.ID || job.SenderID != peer {
			continue
		}
		switch job.Status {
		case domain.PrintStatusQueued, domain.PrintStatusCompleted, domain.PrintStatusFailed:
			_ = n.send(peer, envelope{
				Type: "print_status", PrintJobID: job.ID, PrintStatus: job.Status, Error: job.Error,
			})
		}
	}
}

func (n *Node) resumePrintJobs() {
	s := n.snapshot(false)
	for _, job := range s.PrintJobs {
		if job.PrinterOwnerID != s.Self.ID {
			continue
		}
		i := transferIndex(&s, job.TransferID)
		if i < 0 {
			continue
		}
		transfer := s.Transfers[i]
		switch {
		case job.Status == domain.PrintStatusQueued && job.LocalJobID != "":
			n.startPrintWorker(job.TransferID, true)
		case job.Status == domain.PrintStatusSubmitting:
			n.finishPrintJob(job.ID, domain.PrintStatusFailed, "系统打印队列提交结果未知；为避免重复出纸，请确认后重新发起")
		case transfer.Status == "completed" && job.Status == domain.PrintStatusReceiving:
			n.startPrintSubmission(job.TransferID)
		}
	}
}

func (n *Node) startPrintSubmission(transferID string) {
	n.startPrintWorker(transferID, false)
}

func (n *Node) startPrintIfReady(transferID string) {
	s := n.snapshot(false)
	i := printJobForTransferIndex(&s, transferID)
	ti := transferIndex(&s, transferID)
	if i < 0 || ti < 0 || s.Transfers[ti].Status != "completed" ||
		s.PrintJobs[i].PrinterOwnerID != s.Self.ID || s.PrintJobs[i].Terminal() {
		return
	}
	n.startPrintSubmission(transferID)
}

func (n *Node) startPrintWorker(transferID string, pollOnly bool) {
	n.mu.Lock()
	if n.closing || n.printPolls[transferID] {
		n.mu.Unlock()
		return
	}
	n.printPolls[transferID] = true
	n.mu.Unlock()
	if !n.async(func() {
		defer func() {
			n.mu.Lock()
			delete(n.printPolls, transferID)
			n.mu.Unlock()
		}()
		n.runPrintWorker(transferID, pollOnly)
	}) {
		n.mu.Lock()
		delete(n.printPolls, transferID)
		n.mu.Unlock()
	}
}

func (n *Node) runPrintWorker(transferID string, pollOnly bool) {
	started := time.Now()
	for {
		if n.ctx.Err() != nil {
			return
		}
		s := n.snapshot(false)
		i := printJobForTransferIndex(&s, transferID)
		if i < 0 {
			return
		}
		job := s.PrintJobs[i]
		if job.Terminal() {
			return
		}
		if job.LocalJobID != "" {
			state, err := n.printBackend.Status(n.ctx, printing.Receipt{Printer: job.PrinterDevice, JobID: job.LocalJobID})
			if err == nil {
				switch state {
				case printing.StateCompleted, printing.StateFailed:
					status := domain.PrintStatusCompleted
					message := ""
					if state == printing.StateFailed {
						status, message = domain.PrintStatusFailed, "系统打印队列报告任务失败"
					}
					n.finishPrintJob(job.ID, status, message)
					return
				}
			}
			if time.Since(started) >= n.printPollLimit {
				n.finishPrintJob(job.ID, domain.PrintStatusFailed, "无法在限定时间内确认系统打印队列状态")
				return
			}
			if !n.waitPrintPoll() {
				return
			}
			continue
		}
		if pollOnly {
			return
		}
		pollOnly = true
		ti := transferIndex(&s, transferID)
		if ti < 0 || s.Transfers[ti].Status != "completed" {
			return
		}
		share := s.Printer(job.PrinterID)
		if share.ID == "" || share.OwnerID != s.Self.ID || share.DeviceName == "" || !share.Available {
			n.finishPrintJob(job.ID, domain.PrintStatusFailed, "打印机已停止共享")
			return
		}
		path, err := printing.ValidateFile(s.Transfers[ti].Path)
		if err != nil {
			n.finishPrintJob(job.ID, domain.PrintStatusFailed, err.Error())
			return
		}
		if err := n.beginPrintSubmission(job.ID); err != nil {
			return
		}
		select {
		case n.printSubmitTokens <- struct{}{}:
		case <-n.ctx.Done():
			return
		}
		receipt, err := n.printBackend.Submit(n.ctx, share.DeviceName, path, job.Options)
		<-n.printSubmitTokens
		if err != nil {
			n.finishPrintJob(job.ID, domain.PrintStatusFailed, err.Error())
			return
		}
		if receipt.JobID == "" {
			n.finishPrintJob(job.ID, domain.PrintStatusFailed, "系统打印队列未返回任务编号")
			return
		}
		if err := n.queuePrintJob(job.ID, receipt.JobID); err != nil {
			return
		}
	}
}

func (n *Node) waitPrintPoll() bool {
	timer := time.NewTimer(n.printPollDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-n.ctx.Done():
		return false
	}
}

func (n *Node) setPrintJobStatus(id, status, problem string) error {
	return n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, id)
		if i < 0 || p.State.PrintJobs[i].Terminal() {
			return nil
		}
		p.State.PrintJobs[i].Status = status
		p.State.PrintJobs[i].Error = problem
		p.State.PrintJobs[i].UpdatedAt = time.Now()
		return nil
	})
}

func (n *Node) beginPrintSubmission(id string) error {
	return n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, id)
		if i < 0 || p.State.PrintJobs[i].Terminal() {
			return nil
		}
		job := &p.State.PrintJobs[i]
		job.Status, job.Error, job.UpdatedAt = domain.PrintStatusSubmitting, "", time.Now()
		if job.SubmitKey == "" {
			job.SubmitKey = job.ID
		}
		return nil
	})
}

func (n *Node) queuePrintJob(id, localID string) error {
	s := n.snapshot(false)
	job := s.PrintJob(id)
	if job.ID == "" || job.Terminal() {
		return nil
	}
	err := n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, id)
		if i < 0 || p.State.PrintJobs[i].Terminal() {
			return nil
		}
		job := &p.State.PrintJobs[i]
		job.LocalJobID, job.Status, job.Error, job.UpdatedAt = localID, domain.PrintStatusQueued, "", time.Now()
		return []pending{{PeerID: job.SenderID, Envelope: envelope{
			Type: "print_status", PrintJobID: job.ID, PrintStatus: job.Status, Error: job.Error,
		}}}
	})
	return err
}

func (n *Node) finishPrintJob(id, status, problem string) {
	s := n.snapshot(false)
	job := s.PrintJob(id)
	if job.ID == "" || job.Terminal() {
		return
	}
	_ = n.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, id)
		if i < 0 || p.State.PrintJobs[i].Terminal() {
			return nil
		}
		job := &p.State.PrintJobs[i]
		job.Status, job.Error, job.UpdatedAt = status, problem, time.Now()
		return []pending{{PeerID: job.SenderID, Envelope: envelope{
			Type: "print_status", PrintJobID: job.ID, PrintStatus: status, Error: problem,
		}}}
	})
	n.removePrintFile(id)
}

func (n *Node) removePrintFile(id string) {
	s := n.snapshot(false)
	job := s.PrintJob(id)
	if job.ID == "" || job.PrinterOwnerID != s.Self.ID {
		return
	}
	if i := transferIndex(&s, job.TransferID); i >= 0 && s.Transfers[i].Path != "" {
		path := s.Transfers[i].Path
		_ = os.Remove(path)
		_ = os.Remove(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".part"))
	}
}

func (n *Node) failPrintTransfer(transferID, problem string) {
	s := n.snapshot(false)
	i := printJobForTransferIndex(&s, transferID)
	if i < 0 || s.PrintJobs[i].Terminal() {
		return
	}
	n.finishPrintJob(s.PrintJobs[i].ID, domain.PrintStatusFailed, problem)
}

func printReceivePath(dataDir string, job domain.PrintJob) string {
	ext := strings.ToLower(filepath.Ext(job.FileName))
	if len(ext) > 16 || strings.IndexFunc(ext, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.')
	}) >= 0 {
		ext = ""
	}
	return filepath.Join(dataDir, "print-jobs", job.ID+ext)
}

func ensurePrintDirectory(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0700)
}
