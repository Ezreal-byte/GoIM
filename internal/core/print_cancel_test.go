package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goim/internal/domain"
	"goim/internal/printing"
)

type cancellableTestPrinter struct {
	printTestBackend
	fail     bool
	receipts []printing.Receipt
}

func (b *cancellableTestPrinter) Cancel(_ context.Context, r printing.Receipt) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.receipts = append(b.receipts, r)
	if b.fail {
		return errors.New("queue refused")
	}
	return nil
}

func TestRemotePrintCancellationTracksOwnerDecision(t *testing.T) {
	dev := &cancellableTestPrinter{printTestBackend: printTestBackend{state: printing.StateQueued, printers: []printing.Printer{{ID: "queue", Name: "Office", Available: true, Capabilities: printing.DefaultCapabilities()}}}, fail: true}
	a := testPrintNode(t, "Sender", t.TempDir(), &printTestBackend{})
	b := testPrintNode(t, "Owner", t.TempDir(), dev)
	connectNodes(t, a, b)
	b.execute(domain.Command{Kind: domain.RefreshPrinters})
	eventually(t, "printer inventory", func() bool { return len(b.Snapshot().LocalPrinters) == 1 })
	if err := b.execute(domain.Command{Kind: domain.SharePrinter, PrinterID: "queue", Name: "Office"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "printer shared", func() bool { return len(a.Snapshot().Printers) == 1 })
	path := filepath.Join(t.TempDir(), "example.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7 test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.execute(domain.Command{Kind: domain.PrintFile, PrinterID: a.Snapshot().Printers[0].ID, Paths: []string{path}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "sender sees OS receipt", func() bool {
		s := a.Snapshot()
		return len(s.PrintJobs) > 0 && s.PrintJobs[0].Status == domain.PrintStatusQueued
	})
	id := a.Snapshot().PrintJobs[0].ID
	cancel := domain.Command{Kind: domain.CancelPrintJob, PrintJobID: id}
	if err := a.execute(cancel); err != nil {
		t.Fatal(err)
	}
	eventually(t, "queue refusal reaches sender", func() bool {
		j := a.Snapshot().PrintJob(id)
		return j.Status == domain.PrintStatusQueued && j.CancelRequest == "" && strings.Contains(j.CancelError, "queue refused")
	})
	dev.mu.Lock()
	dev.fail = false
	dev.mu.Unlock()
	if err := a.execute(cancel); err != nil {
		t.Fatal(err)
	}
	eventually(t, "both peers record cancellation", func() bool {
		return a.Snapshot().PrintJob(id).Status == domain.PrintStatusCancelled && b.Snapshot().PrintJob(id).Status == domain.PrintStatusCancelled
	})
	if err := a.execute(cancel); err == nil {
		t.Fatal("cancelled job accepted a new request")
	}
	b.finishPrintJob(id, domain.PrintStatusCompleted, "")
	if b.Snapshot().PrintJob(id).Status != domain.PrintStatusCancelled {
		t.Fatal("late poll reversed cancellation")
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if len(dev.receipts) != 2 || dev.receipts[1] != (printing.Receipt{Printer: "queue", JobID: "job-1"}) {
		t.Fatal(dev.receipts)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("sender source was removed", err)
	}
}

func TestPrintCancelGuardsSubmissionAndOwnership(t *testing.T) {
	n := testPrintNode(t, "Owner", t.TempDir(), &printTestBackend{})
	id, sender, transfer := newID(), newID(), newID()
	if err := n.durable(func(p *persisted) []pending {
		p.State.PrintJobs = append(p.State.PrintJobs, domain.PrintJob{ID: id, SenderID: sender, PrinterOwnerID: p.State.Self.ID, TransferID: transfer, Status: domain.PrintStatusReceiving})
		p.State.Transfers = append(p.State.Transfers, domain.Transfer{ID: transfer, PeerID: sender, PrintJobID: id, Status: "transferring", Direction: "receive"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.cancelOwnedPrint(newID(), id, newID()); err != nil {
		t.Fatal(err)
	}
	if n.Snapshot().PrintJob(id).Status != domain.PrintStatusReceiving {
		t.Fatal("unrelated peer cancelled job")
	}
	if err := n.cancelPrintJob(id); err != nil {
		t.Fatal(err)
	}
	if err := n.beginPrintSubmission(id); err == nil {
		t.Fatal("cancelled job crossed spool boundary")
	}
	s := n.Snapshot()
	if s.Transfers[transferIndex(&s, transfer)].Status != "cancelled" {
		t.Fatal("print file still transferring")
	}
}
