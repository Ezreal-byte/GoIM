package core

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"goim/internal/domain"
	"goim/internal/systemauth"
)

func TestManualConnectionPublishesSuccessAndFailure(t *testing.T) {
	sender, receiver := testNode(t, "Sender"), testNode(t, "Receiver")
	if err := sender.execute(domain.Command{Kind: domain.Connect, Address: receiver.Address(), Transport: domain.TransportGoIM}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "manual connection success", func() bool {
		feedback := sender.Snapshot().ConnectionFeedback
		return feedback.Sequence >= 2 && !feedback.Pending
	})
	feedback := sender.Snapshot().ConnectionFeedback
	if feedback.Problem != "" || feedback.Address != receiver.Address() || !sender.Snapshot().Peer(receiver.Snapshot().Self.ID).Online {
		t.Fatalf("manual connection success = %+v", feedback)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	if err := sender.execute(domain.Command{Kind: domain.Connect, Address: address, Transport: domain.TransportGoIM}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "manual connection refusal", func() bool {
		result := sender.Snapshot().ConnectionFeedback
		return result.Sequence >= feedback.Sequence+2 && !result.Pending
	})
	if result := sender.Snapshot().ConnectionFeedback; result.Address != address || !strings.Contains(result.Problem, "端口拒绝连接") {
		t.Fatalf("manual connection failure = %+v", result)
	}
}

func TestManualConnectionClassifiesNetworkProblems(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "连接超时"},
		{syscall.ECONNREFUSED, "端口拒绝连接"},
		{syscall.ENETUNREACH, "无法到达目标网络"},
		{errors.New("TLS handshake failed"), "TLS handshake failed"},
	} {
		if message := manualConnectionProblem(test.err); !strings.Contains(message, test.want) {
			t.Fatalf("%v: %q", test.err, message)
		}
	}
}

func TestUserDataResetWaitsForExitAndPreservesUserFiles(t *testing.T) {
	directory := t.TempDir()
	node := testNodeAt(t, "Private User", directory)
	identity := node.Snapshot().Self.ID
	if err := node.execute(domain.Command{Kind: domain.SetLockPassword, Text: "private-passphrase"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"avatars/private.png", "chat-images/private.png", "print-jobs/private.pdf", "cache/.localsend-tls-interrupted", "downloads/keep.txt", "unrelated/keep.txt"} {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep or clear"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := node.execute(domain.Command{Kind: domain.ResetUserData}); err != nil {
		t.Fatal(err)
	}
	if !node.Snapshot().DataResetPending || !node.Snapshot().Settings.HasLockPassword {
		t.Fatal("reset was not scheduled or destroyed live state before shutdown")
	}
	if other, err := New(Options{DataDir: directory, Listen: "127.0.0.1:0"}); err == nil {
		_ = other.Close()
		t.Fatal("another process could open the directory during reset")
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"goim.db", "identity.key", "avatars", "chat-images", "print-jobs", "cache", resetMarker} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("private data %q remained: %v", name, err)
		}
	}
	for _, name := range []string{"downloads/keep.txt", "unrelated/keep.txt"} {
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name))); err != nil {
			t.Fatalf("user file %q was removed: %v", name, err)
		}
	}
	reopened := testNodeAt(t, "New User", directory)
	if reopened.Snapshot().Self.ID == identity || reopened.Snapshot().Settings.HasLockPassword || len(reopened.Snapshot().Peers) != 0 {
		t.Fatal("reset retained the old identity or password")
	}
}

func TestUserDataResetRecoversAfterInterruptedExit(t *testing.T) {
	directory := t.TempDir()
	node := testNodeAt(t, "Old User", directory)
	identity := node.Snapshot().Self.ID
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	if err := requestDataReset(directory); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "New User", directory)
	if reopened.Snapshot().Self.ID == identity {
		t.Fatal("pending reset was not recovered on restart")
	}
}

func TestUnsupportedSystemUnlockCannotBeEnabled(t *testing.T) {
	if systemauth.Supported() {
		t.Skip("this platform supports system unlock")
	}
	node := testNode(t, "User")
	if err := node.execute(domain.Command{Kind: domain.SetLockPassword, Text: "secret-0123"}); err != nil {
		t.Fatal(err)
	}
	if err := node.execute(domain.Command{Kind: domain.SetSystemUnlock, Bool: true, Password: "secret-0123"}); err == nil || node.Snapshot().Settings.SystemUnlock {
		t.Fatal("unsupported system unlock was enabled")
	}
}

func TestUserDataResetPreservesActiveOlderDatabase(t *testing.T) {
	directory := t.TempDir()
	database, err := bolt.Open(filepath.Join(directory, "goim.db"), 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := requestDataReset(directory); err != nil {
		t.Fatal(err)
	}
	if err := completePendingDataReset(directory); err == nil {
		t.Fatal("reset removed a database still held by an older application")
	}
	if _, err := os.Stat(filepath.Join(directory, "goim.db")); err != nil {
		t.Fatal("active database was removed")
	}
}

func TestUserDataResetRejectsInvalidMarker(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, resetMarker), []byte("incomplete"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(directory, "identity.key")
	if err := os.WriteFile(identity, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := completePendingDataReset(directory); err == nil {
		t.Fatal("invalid reset marker was accepted")
	}
	if content, err := os.ReadFile(identity); err != nil || string(content) != "preserve" {
		t.Fatal("invalid reset deleted identity data")
	}
}
