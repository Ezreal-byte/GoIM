package neiwangtong

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderResumeRestartsWhenManifestChanges(t *testing.T) {
	source := t.TempDir()
	name := "资料.bin"
	path := filepath.Join(source, name)
	data := bytes.Repeat([]byte("first directory revision"), 6000)
	stage := t.TempDir()
	target := directoryTarget{path: filepath.Join(stage, "files"), manifest: filepath.Join(stage, "manifest")}

	exchange := func(ctx context.Context, revision time.Time, progress func(int64)) (error, error) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, revision, revision); err != nil {
			t.Fatal(err)
		}
		tree, err := InspectDirectory(source)
		if err != nil {
			t.Fatal(err)
		}
		const task = "1234567890abcdef1234567890abcdef"
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		sent := make(chan error, 1)
		go func() {
			defer server.Close()
			sent <- sendDirectory(context.Background(), server, task, tree, nil)
		}()
		init, err := requireFolderFrame(client, 1, 684)
		if err != nil {
			t.Fatal(err)
		}
		received := receiveDirectory(ctx, client, init, tree.offer(task), target, progress)
		_ = client.Close()
		return received, <-sent
	}

	revision := time.Now().Add(-time.Minute).Truncate(time.Second)
	first, cancel := context.WithCancel(context.Background())
	defer cancel()
	received, sent := exchange(first, revision, func(int64) { cancel() })
	if !errors.Is(received, context.Canceled) || sent == nil {
		t.Fatalf("expected an interruption, got receiver=%v sender=%v", received, sent)
	}
	partial := filepath.Join(target.path, name)
	const retained = folderBlockSize - 1
	if err := os.Truncate(partial, retained); err != nil {
		t.Fatal(err)
	}
	var firstProgress int64
	trackProgress := func(done int64) {
		if firstProgress == 0 {
			firstProgress = done
		}
	}
	received, sent = exchange(context.Background(), revision, trackProgress)
	if err := errors.Join(received, sent); err != nil || firstProgress != retained {
		t.Fatalf("did not resume the matching manifest (first progress %d): %v", firstProgress, err)
	}
	if got, err := os.ReadFile(partial); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("resumed file did not match: %v", err)
	}

	data[0] ^= 0xff
	firstProgress = 0
	received, sent = exchange(context.Background(), revision.Add(2*time.Second), trackProgress)
	if err := errors.Join(received, sent); err != nil || firstProgress != folderBlockSize {
		t.Fatalf("did not restart from zero with the changed manifest (first progress %d): %v", firstProgress, err)
	}
	if got, err := os.ReadFile(partial); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file retained data from the old revision: %v", err)
	}
}
