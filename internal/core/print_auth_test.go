package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goim/internal/domain"
	"goim/internal/printing"
)

func TestPrinterPasswordAuthorizationPersistsAndRevokes(t *testing.T) {
	backend := &printTestBackend{printers: []printing.Printer{{ID: "queue", Name: "Office", Available: true,
		Capabilities: printing.DefaultCapabilities()}}, state: printing.StateCompleted}
	ownerDir, senderDir := t.TempDir(), t.TempDir()
	owner := testPrintNode(t, "Owner", ownerDir, backend)
	sender := testPrintNode(t, "Sender", senderDir, &printTestBackend{})
	connectNodes(t, sender, owner)
	owner.refreshPrinters()
	eventually(t, "printer inventory", func() bool { return len(owner.Snapshot().LocalPrinters) == 1 })
	if err := owner.sharePrinter(domain.Command{PrinterID: "queue", Password: "printer-secret"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "protected catalog", func() bool { return len(sender.Snapshot().Printers) == 1 })
	share := sender.Snapshot().Printers[0]
	if !share.PasswordRequired || len(share.AuthSalt) != 16 || !validPrintID(share.AuthRevision) {
		t.Fatalf("missing protection: %+v", share)
	}
	source := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.7 test payload"), 0600); err != nil {
		t.Fatal(err)
	}
	command := domain.Command{Kind: domain.PrintFile, PrinterID: share.ID, Paths: []string{source},
		PrintOptions: domain.PrintOptions{Paper: "A4", Copies: 1, Orientation: domain.PrintOrientationPortrait, Color: domain.PrintColorMonochrome, Duplex: domain.PrintDuplexSimplex}}
	if err := sender.printFile(command); err == nil {
		t.Fatal("unverified print was accepted")
	}
	if len(sender.Snapshot().PrintJobs) != 0 || len(backend.submissions()) != 0 {
		t.Fatal("unauthorized print created work")
	}
	if err := sender.requestPrinterAccess(share.ID, "wrong-password"); err == nil {
		t.Fatal("wrong password was accepted")
	}
	owner.mu.Lock()
	clear(owner.printAuthAfter)
	owner.mu.Unlock()
	file := domain.File{ID: newID(), Name: "guard.pdf", Size: 12}
	transfer := newID()
	request := envelope{TransferID: transfer, PrintJob: domain.PrintJob{ID: newID(), MessageID: newID(), TransferID: transfer,
		FileID: file.ID, FileName: file.Name, FileSize: file.Size, SenderID: sender.Snapshot().Self.ID,
		PrinterID: share.ID, PrinterOwnerID: owner.Snapshot().Self.ID,
		Options: command.PrintOptions}}
	owner.mu.Lock()
	_, autoReceive, denied := owner.receivePrintOffer(sender.Snapshot().Self.ID, &owner.data, file, request)
	owner.mu.Unlock()
	if denied == nil || autoReceive {
		t.Fatal("receiver accepted a forged offer without a token")
	}
	if err := sender.requestPrinterAccess(share.ID, "printer-secret"); err != nil {
		t.Fatal(err)
	}
	sender.mu.Lock()
	token := sender.data.PrintAccess[share.ID].Token
	sender.mu.Unlock()
	owner.mu.Lock()
	if !validPrinterAccess(&owner.data, share, sender.Snapshot().Self.ID, token) {
		owner.mu.Unlock()
		t.Fatal("grant rejected")
	}
	if validPrinterAccess(&owner.data, share, "another-identity", token) {
		owner.mu.Unlock()
		t.Fatal("grant not bound to identity")
	}
	owner.mu.Unlock()
	if err := sender.setPrinterPinned(domain.Command{PrinterID: share.ID, Bool: true}); err != nil {
		t.Fatal(err)
	}
	if err := sender.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner = testPrintNode(t, "Owner", ownerDir, backend)
	sender = testPrintNode(t, "Sender", senderDir, &printTestBackend{})
	connectNodes(t, sender, owner)
	owner.refreshPrinters()
	eventually(t, "reopened inventory", func() bool { return len(owner.Snapshot().LocalPrinters) == 1 })
	eventually(t, "persistent authorization", func() bool { return sender.Snapshot().PrinterAuthorized[share.ID] == share.AuthRevision })
	if len(sender.Snapshot().Settings.PinnedPrinters) != 1 {
		t.Fatal("printer pin was not persisted")
	}
	if err := sender.printFile(command); err != nil {
		t.Fatal(err)
	}
	eventually(t, "authenticated OS submission", func() bool { return len(backend.submissions()) == 1 })
	if err := owner.sharePrinter(domain.Command{PrinterID: "queue", Password: "changed-password", Setting: "password"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "new password revision", func() bool { return sender.Snapshot().Printer(share.ID).AuthRevision != share.AuthRevision })
	if sender.Snapshot().PrinterAuthorized[share.ID] != "" {
		t.Fatal("old authorization still visible")
	}
	if err := sender.printFile(command); err == nil {
		t.Fatal("old authorization survived password rotation")
	}
	owner.mu.Lock()
	current := owner.data.State.Printer(share.ID)
	valid := validPrinterAccess(&owner.data, current, sender.Snapshot().Self.ID, token)
	owner.mu.Unlock()
	if valid {
		t.Fatal("receiver accepted revoked token")
	}
	if err := sender.requestPrinterAccess(share.ID, "changed-password"); err != nil {
		t.Fatal(err)
	}
	if err := sender.printFile(command); err != nil {
		t.Fatal(err)
	}
	eventually(t, "second authenticated OS submission", func() bool { return len(backend.submissions()) == 2 })
	for _, node := range []*Node{sender, owner} {
		encoded, _ := json.Marshal(node.Snapshot())
		if strings.Contains(string(encoded), "changed-password") || strings.Contains(string(encoded), token) {
			t.Fatal("secret leaked in public snapshot")
		}
	}
	if err := owner.sharePrinter(domain.Command{PrinterID: "queue", Setting: "password"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "password removed", func() bool { return !sender.Snapshot().Printer(share.ID).PasswordRequired })
	if err := sender.printFile(command); err != nil {
		t.Fatal(err)
	}
}

func TestPrinterAuthProofBindsPrinterIdentityRevisionAndRequest(t *testing.T) {
	key := []byte("test-only-key")
	proof := printerPasswordProof(key, "printer", "peer", "revision", "request")
	for _, changed := range []string{
		printerPasswordProof(key, "other", "peer", "revision", "request"),
		printerPasswordProof(key, "printer", "other", "revision", "request"),
		printerPasswordProof(key, "printer", "peer", "other", "request"),
		printerPasswordProof(key, "printer", "peer", "revision", "other"),
	} {
		if proof == changed {
			t.Fatal("authorization proof was reusable outside its scope")
		}
	}
}

func TestPrinterAuthenticationReportsErrorsAndRevisions(t *testing.T) {
	n := testPrintNode(t, "Sender", t.TempDir(), &printTestBackend{})
	before := n.Snapshot().Revision
	n.authenticatePrinter(domain.Command{PrinterID: "missing", Password: "anything", Request: 12})
	eventually(t, "authorization error", func() bool {
		result := n.Snapshot().PrinterAuth["missing"]
		return result.Request == 12 && !result.Pending && result.Error != ""
	})
	if n.Snapshot().Revision <= before {
		t.Fatal("authorization completion did not wake dependent windows")
	}
}

func TestPrinterConcurrentAuthenticationReturnsFeedback(t *testing.T) {
	n := testPrintNode(t, "Sender", t.TempDir(), &printTestBackend{})
	n.mu.Lock()
	n.printAuthActive = map[string]bool{"printer": true}
	n.mu.Unlock()
	n.authenticatePrinter(domain.Command{PrinterID: "printer", Request: 24})
	result := n.Snapshot().PrinterAuth["printer"]
	if result.Request != 24 || result.Pending || result.Error == "" {
		t.Fatalf("duplicate request has no feedback: %+v", result)
	}
}
