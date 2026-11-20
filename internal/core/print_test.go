package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/printing"
)

type printTestBackend struct {
	mu        sync.Mutex
	printers  []printing.Printer
	state     printing.State
	submitErr error
	submitted []printTestSubmission
}

type printTestSubmission struct {
	printer string
	path    string
	options domain.PrintOptions
}

func (b *printTestBackend) List(context.Context) ([]printing.Printer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]printing.Printer(nil), b.printers...), nil
}

func (b *printTestBackend) Submit(_ context.Context, printer, path string, options domain.PrintOptions) (printing.Receipt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.submitErr != nil {
		return printing.Receipt{}, b.submitErr
	}
	b.submitted = append(b.submitted, printTestSubmission{printer: printer, path: path, options: options})
	return printing.Receipt{Printer: printer, JobID: "job-1"}, nil
}

func (b *printTestBackend) Status(context.Context, printing.Receipt) (printing.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, nil
}

func (b *printTestBackend) setState(state printing.State) {
	b.mu.Lock()
	b.state = state
	b.mu.Unlock()
}

func (b *printTestBackend) submissions() []printTestSubmission {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]printTestSubmission(nil), b.submitted...)
}

func testPrintNode(t *testing.T, name, dir string, backend printing.Backend) *Node {
	t.Helper()
	n, err := New(Options{
		DataDir: dir, Listen: "127.0.0.1:0", Name: name,
		DisableDiscovery: true, DisableCompatibility: true, Version: "test",
		PrintBackend: backend,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func TestPrinterCatalogRefreshKeepsSharedPrinters(t *testing.T) {
	n := testPrintNode(t, "Receiver", t.TempDir(), &printTestBackend{})
	peer := domain.Peer{ID: newID(), Name: "Owner", Transport: domain.TransportGoIM, PrintSupported: true}
	if err := n.durable(func(p *persisted) []pending {
		p.State.Peers = append(p.State.Peers, peer)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	share := domain.PrinterShare{ID: newID(), Name: "Office Printer", Available: true}
	for _, revision := range []uint64{1, 1, 2} {
		if err := n.receivePrinterCatalog(peer.ID, envelope{
			Printers: []domain.PrinterShare{share}, PrinterRevision: revision,
		}); err != nil {
			t.Fatal(err)
		}
		printers := n.Snapshot().Printers
		if len(printers) != 1 || printers[0].ID != share.ID {
			t.Fatalf("printer catalog after revision %d = %+v", revision, printers)
		}
	}
}

func TestPrinterSharingAutoSubmitsAndPropagatesQueueStatus(t *testing.T) {
	backend := &printTestBackend{
		printers: []printing.Printer{{
			ID: "office-queue", Name: "Office Laser", Available: true, Default: true,
			Capabilities: printing.DefaultCapabilities(),
		}},
		state: printing.StateQueued,
	}
	sender := testPrintNode(t, "Alice", t.TempDir(), &printTestBackend{})
	ownerDir := t.TempDir()
	owner := testPrintNode(t, "Chen", ownerDir, backend)
	connectNodes(t, sender, owner)

	owner.execute(domain.Command{Kind: domain.RefreshPrinters})
	eventually(t, "local printer inventory", func() bool { return len(owner.Snapshot().LocalPrinters) == 1 })
	if err := owner.execute(domain.Command{Kind: domain.SharePrinter, PrinterID: "office-queue", Name: "Chen-Office Laser"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "remote printer catalog", func() bool { return len(sender.Snapshot().Printers) == 1 })
	share := sender.Snapshot().Printers[0]
	if share.Name != "Chen-Office Laser" || share.OwnerID != owner.Snapshot().Self.ID || share.DeviceName != "" {
		t.Fatalf("remote share was not sanitized correctly: %+v", share)
	}

	source := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.7 test payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sender.execute(domain.Command{
		Kind: domain.PrintFile, PrinterID: share.ID, Paths: []string{source},
		PrintOptions: domain.PrintOptions{
			Paper: "A4", Copies: 2, Orientation: domain.PrintOrientationPortrait,
			PageRange: "1-2", Color: domain.PrintColorMonochrome, Duplex: domain.PrintDuplexLongEdge,
		},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "receiver completed verified publication", func() bool {
		s := owner.Snapshot()
		return len(s.Transfers) == 1 && s.Transfers[0].Status == "completed"
	})
	eventually(t, "receiver submitted to the OS queue", func() bool {
		s := owner.Snapshot()
		return len(s.PrintJobs) == 1 && s.PrintJobs[0].Status == domain.PrintStatusQueued
	})
	eventually(t, "sender received the queued receipt", func() bool {
		s := sender.Snapshot()
		return len(s.PrintJobs) == 1 && s.PrintJobs[0].Status == domain.PrintStatusQueued
	})

	submissions := backend.submissions()
	if len(submissions) != 1 {
		t.Fatalf("OS submissions = %d, want 1", len(submissions))
	}
	if submissions[0].printer != "office-queue" || submissions[0].options.Copies != 2 ||
		submissions[0].options.PageRange != "1-2" || submissions[0].options.Duplex != domain.PrintDuplexLongEdge {
		t.Fatalf("submitted options = %+v", submissions[0])
	}
	if _, err := os.Stat(submissions[0].path); err != nil {
		t.Fatalf("submitted file is unavailable: %v", err)
	}
	for name, node := range map[string]*Node{"sender": sender, "owner": owner} {
		s := node.Snapshot()
		prints := 0
		for _, message := range s.Messages {
			if message.Kind == "print" {
				prints++
				if message.PrintJobID != s.PrintJobs[0].ID {
					t.Fatalf("%s print message does not reference its job", name)
				}
			}
		}
		if prints != 1 || len(s.PrintJobs) != 1 {
			t.Fatalf("%s print history = %d messages, %d jobs", name, prints, len(s.PrintJobs))
		}
	}

	backend.setState(printing.StateCompleted)
	eventually(t, "terminal status reaches both peers", func() bool {
		ownerJob := owner.Snapshot().PrintJob(owner.Snapshot().PrintJobs[0].ID)
		senderJob := sender.Snapshot().PrintJob(sender.Snapshot().PrintJobs[0].ID)
		return ownerJob.Status == domain.PrintStatusCompleted && senderJob.Status == domain.PrintStatusCompleted
	})

	jobID := owner.Snapshot().PrintJobs[0].ID
	if err := owner.durable(func(p *persisted) []pending {
		i := printJobIndex(&p.State, jobID)
		if i < 0 {
			t.Fatal("print job disappeared before restart recovery")
		}
		p.State.PrintJobs[i].Status = domain.PrintStatusSubmitting
		p.State.PrintJobs[i].LocalJobID = ""
		p.State.PrintJobs[i].UpdatedAt = time.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	owner.resumePrintJobs()
	eventually(t, "unknown submission becomes terminal without resubmitting", func() bool {
		return owner.Snapshot().PrintJob(jobID).Status == domain.PrintStatusFailed
	})
	if submissions := backend.submissions(); len(submissions) != 1 {
		t.Fatalf("restart recovery resubmitted an uncertain job: %+v", submissions)
	}
}

func TestPrintSubmissionFailureAndUnshareRemoveRemotePrinter(t *testing.T) {
	backend := &printTestBackend{
		printers: []printing.Printer{{
			ID: "queue", Name: "Queue", Available: true, Capabilities: printing.DefaultCapabilities(),
		}},
		state:     printing.StateFailed,
		submitErr: context.Canceled,
	}
	sender := testPrintNode(t, "Bob", t.TempDir(), &printTestBackend{})
	owner := testPrintNode(t, "Lin", t.TempDir(), backend)
	connectNodes(t, sender, owner)
	owner.execute(domain.Command{Kind: domain.RefreshPrinters})
	eventually(t, "printer inventory", func() bool { return len(owner.Snapshot().LocalPrinters) == 1 })
	if err := owner.execute(domain.Command{Kind: domain.SharePrinter, PrinterID: "queue", Name: "Lin-Queue"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "remote printer", func() bool { return len(sender.Snapshot().Printers) == 1 })
	share := sender.Snapshot().Printers[0]

	source := filepath.Join(t.TempDir(), "page.txt")
	if err := os.WriteFile(source, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sender.execute(domain.Command{Kind: domain.PrintFile, PrinterID: share.ID, Paths: []string{source}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "failure reaches sender", func() bool {
		s := sender.Snapshot()
		return len(s.PrintJobs) == 1 && s.PrintJobs[0].Status == domain.PrintStatusFailed
	})

	ownerShare := owner.Snapshot().Printers[0]
	if err := owner.execute(domain.Command{Kind: domain.UnsharePrinter, PrinterID: ownerShare.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "printer removed from peer catalog", func() bool { return len(sender.Snapshot().Printers) == 0 })
}

func TestStalePrintOfferIsRejectedAndFailsSenderJob(t *testing.T) {
	for _, reason := range []string{"unshared", "password-changed", "cache-full", "receive-directory"} {
		t.Run(reason, func(t *testing.T) {
			backend := &printTestBackend{
				printers: []printing.Printer{{
					ID: "stale-queue", Name: "Stale Queue", Available: true, Default: true,
					Capabilities: printing.DefaultCapabilities(),
				}},
			}
			sender := testPrintNode(t, "Sender", t.TempDir(), &printTestBackend{})
			owner := testPrintNode(t, "Owner", t.TempDir(), backend)
			observer := testPrintNode(t, "Observer", t.TempDir(), &printTestBackend{})
			connectNodes(t, sender, owner)
			connectNodes(t, sender, observer)

			owner.execute(domain.Command{Kind: domain.RefreshPrinters})
			eventually(t, "local inventory", func() bool { return len(owner.Snapshot().LocalPrinters) == 1 })
			if err := owner.execute(domain.Command{Kind: domain.SharePrinter, PrinterID: "stale-queue", Name: "Owner-Stale"}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "sender catalog", func() bool { return len(sender.Snapshot().Printers) == 1 })
			share := sender.Snapshot().Printers[0]
			if reason == "unshared" {
				owner.execute(domain.Command{Kind: domain.UnsharePrinter, PrinterID: owner.Snapshot().Printers[0].ID})
			} else if reason == "password-changed" {
				if err := owner.sharePrinter(domain.Command{PrinterID: "stale-queue", Setting: "password", Password: "new-print-password"}); err != nil {
					t.Fatal(err)
				}
			} else if reason == "receive-directory" {
				if err := os.WriteFile(filepath.Join(owner.dir, "print-jobs"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := owner.change(func(s *domain.State) {
				s.Transfers = append(s.Transfers, domain.Transfer{
					ID: newID(), PrintJobID: newID(), Direction: "receive", Status: "waiting", Size: maxPendingPrintBytes,
				})
			}); err != nil {
				t.Fatal(err)
			}

			fileID, transferID, jobID, messageID := newID(), newID(), newID(), newID()
			now := time.Now()
			senderID := sender.Snapshot().Self.ID
			file := domain.File{
				ID: fileID, Name: "stale.pdf", Size: 16, Hash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				MIME: "application/pdf", SenderID: senderID,
				ConversationID: directID(senderID, owner.Snapshot().Self.ID), CreatedAt: now,
			}
			job := domain.PrintJob{
				ID: jobID, MessageID: messageID, TransferID: transferID, FileID: fileID,
				ConversationID: file.ConversationID, PrinterID: share.ID, PrinterName: share.Name,
				PrinterOwnerID: owner.Snapshot().Self.ID, PrinterOwnerName: owner.Snapshot().Self.Name,
				SenderID: senderID, SenderName: sender.Snapshot().Self.Name,
				FileName: file.Name, FileSize: file.Size, MIME: file.MIME,
				Options: domain.PrintOptions{Paper: "A4", Copies: 1, Orientation: domain.PrintOrientationPortrait,
					Color: domain.PrintColorMonochrome, Duplex: domain.PrintDuplexSimplex},
				Status: domain.PrintStatusSending, CreatedAt: now, UpdatedAt: now,
			}
			transfer := domain.Transfer{
				ID: transferID, FileID: fileID, ConversationID: file.ConversationID,
				PeerID: owner.Snapshot().Self.ID, Direction: "send", Status: "offered",
				Name: file.Name, Size: file.Size, UpdatedAt: now, PrintJobID: jobID,
			}
			if err := sender.durable(func(p *persisted) []pending {
				p.State.Files = append(p.State.Files, file)
				p.State.PrintJobs = append(p.State.PrintJobs, job)
				p.State.Transfers = append(p.State.Transfers, transfer)
				addMessageState(&p.State, domain.Message{
					ID: messageID, ConversationID: file.ConversationID, SenderID: senderID,
					Kind: "print", Text: "打印 " + file.Name, FileID: fileID, PrintJobID: jobID, CreatedAt: now,
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			if err := owner.receiveOffer(sender.Snapshot().Self.ID, envelope{
				Type: "file_offer", File: publicFile(file), TransferID: transferID, PrintJob: publicPrintJob(job),
			}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "sender learns the print offer was rejected", func() bool {
				s := sender.Snapshot()
				return s.PrintJob(jobID).Status == domain.PrintStatusFailed &&
					s.Transfers[0].Status == "rejected"
			})
			existing := 0
			if reason == "cache-full" {
				existing = 1
			}
			if len(owner.Snapshot().Transfers) != existing || len(owner.Snapshot().Files) != 0 || len(backend.submissions()) != 0 {
				t.Fatal("rejected offer received or printed a file")
			}
		})
	}
}

func TestPrinterCatalogRejectsCrossOwnerIDCollision(t *testing.T) {
	sender := testPrintNode(t, "Sender", t.TempDir(), &printTestBackend{})
	owner := testPrintNode(t, "Owner", t.TempDir(), &printTestBackend{})
	other := testPrintNode(t, "Other", t.TempDir(), &printTestBackend{})
	connectNodes(t, sender, owner)
	connectNodes(t, sender, other)

	collision := domain.PrinterShare{
		ID: "00112233445566778899aabbccddeeff", Name: "Collision",
		Available: true, Capabilities: printing.DefaultCapabilities(),
	}
	printerRevision := sender.Snapshot().Revision + 1
	if err := sender.receivePrinterCatalog(owner.Snapshot().Self.ID, envelope{
		Printers: []domain.PrinterShare{collision}, PrinterRevision: printerRevision,
	}); err != nil {
		t.Fatal(err)
	}
	if err := sender.receivePrinterCatalog(other.Snapshot().Self.ID, envelope{
		Printers: []domain.PrinterShare{collision}, PrinterRevision: printerRevision + 1,
	}); err != nil {
		t.Fatal(err)
	}
	s := sender.Snapshot()
	if len(s.Printers) != 1 || s.Printers[0].OwnerID != owner.Snapshot().Self.ID {
		t.Fatalf("printer catalog accepted a cross-owner ID collision: %+v", s.Printers)
	}
}
