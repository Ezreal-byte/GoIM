package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestNativeFileResumesLegacyPrefixOnDestinationVolume(t *testing.T) {
	n := testNode(t, "receiver")
	id := newID()
	task := domain.Transfer{ID: id, FileID: id, Direction: "receive", Status: "failed", LegacyTransport: transportNWT, Size: 6}
	if err := n.change(func(s *domain.State) { s.Transfers = append(s.Transfers, task) }); err != nil {
		t.Fatal(err)
	}
	old := n.nativePartialPath(id)
	if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if volume := os.Getenv("GOIM_QA_RECEIVE_VOLUME"); volume != "" {
		if runtime.GOOS == "windows" && strings.EqualFold(filepath.VolumeName(n.dir), filepath.VolumeName(volume)) {
			t.Fatal("select a different volume to exercise cross-volume prefix copying")
		}
		var err error
		parent, err = os.MkdirTemp(volume, "goim-stage-test-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(parent) })
	}
	destination := filepath.Join(parent, "receive.iso")
	stage, offset, err := n.prepareNativeFileStage(t.Context(), task, destination)
	if err != nil || filepath.Dir(stage) != filepath.Clean(parent) || offset != 3 {
		t.Fatalf("stage %q offset %d: %v", stage, offset, err)
	}
	if data, err := os.ReadFile(stage); err != nil || string(data) != "abc" {
		t.Fatalf("legacy prefix: %q, %v", data, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy prefix still occupies space", err)
	}
	task = n.Snapshot().Transfers[0]
	retry, offset, err := n.prepareNativeFileStage(t.Context(), task, destination)
	if err != nil || retry != stage || offset != 3 {
		t.Fatalf("retry stage %q offset %d: %v", retry, offset, err)
	}
	if err := os.WriteFile(stage, []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	// Capture the file identity from an open handle. On Windows os.Stat
	// can defer looking up the identity until after the old name is gone.
	openStage, err := os.Open(stage)
	if err != nil {
		t.Fatal(err)
	}
	before, err := openStage.Stat()
	closeErr := openStage.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	// A name conflict racing the final move must leave both files available.
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.publishNativeFileStage(t.Context(), task, stage, destination); err == nil {
		t.Fatal("overwrote existing destination")
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "existing" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	destination = filepath.Join(parent, "save-as.iso")
	stage, offset, err = n.prepareNativeFileStage(t.Context(), task, destination)
	if err != nil || offset != task.Size {
		t.Fatalf("conflict lost complete prefix: %d, %v", offset, err)
	}
	if err := n.publishNativeFileStage(t.Context(), task, stage, destination); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(destination)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("publishing did not rename the original stage in place", err)
	}
	if got := n.Snapshot().Transfers[0]; got.Status != "completed" || got.FileStage != "" || got.Done != task.Size {
		t.Fatalf("published transfer: %+v", got)
	}
}

func TestInterruptedNativeStageCleanup(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		n := testNode(t, "receiver")
		destination := filepath.Join(t.TempDir(), "file.iso")
		task := domain.Transfer{ID: newID(), Direction: "receive", Status: "offered", LegacyTransport: transportNWT}
		if err := n.change(func(s *domain.State) { s.Transfers = append(s.Transfers, task) }); err != nil {
			t.Fatal(err)
		}
		stage, _, err := n.prepareNativeFileStage(t.Context(), task, destination)
		if err != nil {
			t.Fatal(err)
		}
		if cancel {
			err = n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: task.ID})
		} else {
			n.setTransfer(task.ID, "failed", 0, 0, "interrupted")
			err = n.clearTransferPartial(task.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) || n.Snapshot().Transfers[0].FileStage != "" {
			t.Fatal("interrupted file was not cleared", err)
		}
	}
}
