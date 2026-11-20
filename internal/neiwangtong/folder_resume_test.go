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
	const other = "资料续.bin"
	if err := os.WriteFile(filepath.Join(source, other), []byte("second file"), 0600); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	target := directoryTarget{path: filepath.Join(stage, "files"), manifest: filepath.Join(stage, "manifest")}

	var uploaded []int64
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
		uploaded = uploaded[:0]
		go func() {
			defer server.Close()
			sent <- sendDirectory(context.Background(), server, task, tree, func(done int64) {
				uploaded = append(uploaded, done)
			})
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
	received, sent := exchange(first, revision, func(done int64) {
		if done > 0 {
			cancel()
		}
	})
	if !errors.Is(received, context.Canceled) || sent == nil {
		t.Fatalf("expected an interruption, got receiver=%v sender=%v", received, sent)
	}
	partial := filepath.Join(target.path, name)
	const retained = folderBlockSize - 1
	if err := os.Truncate(partial, retained); err != nil {
		t.Fatal(err)
	}
	var samples []int64
	trackProgress := func(done int64) {
		samples = append(samples, done)
	}
	received, sent = exchange(context.Background(), revision, trackProgress)
	if err := errors.Join(received, sent); err != nil || len(samples) < 2 || samples[0] != retained || samples[1]-samples[0] != folderBlockSize {
		t.Fatalf("resume must report the saved prefix before the first network block: samples=%v err=%v", samples, err)
	}
	if got, err := os.ReadFile(partial); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("resumed file did not match: %v", err)
	}

	// A prefix in a later file must enter the initial baseline too. Reaching
	// that file must not make the network rate jump on either side.
	if err := os.Truncate(partial, 10); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(target.path, other), 5); err != nil {
		t.Fatal(err)
	}
	samples = nil
	received, sent = exchange(context.Background(), revision, trackProgress)
	if err := errors.Join(received, sent); err != nil || len(samples) < 2 || len(uploaded) == 0 ||
		samples[0] != 15 || samples[1] != 15+folderBlockSize || uploaded[0] != samples[1] {
		t.Fatalf("multi-file resume mixed disk bytes with network progress: receiver=%v sender=%v err=%v", samples, uploaded, err)
	}

	data[0] ^= 0xff
	samples = nil
	received, sent = exchange(context.Background(), revision.Add(2*time.Second), trackProgress)
	if err := errors.Join(received, sent); err != nil || len(samples) < 2 || samples[0] != 0 || samples[1] != folderBlockSize {
		t.Fatalf("changed manifest must restart at zero: samples=%v err=%v", samples, err)
	}
	if got, err := os.ReadFile(partial); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file retained data from the old revision: %v", err)
	}
}
