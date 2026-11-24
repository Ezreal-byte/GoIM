package localsend

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestLargeFileOverFourGiB(t *testing.T) {
	if os.Getenv("GOIM_LOCALSEND_LARGE_CASE") == "" {
		t.Skip("requires local space for two files over 4 GiB")
	}
	const bytes = 4<<30 + 23
	file := filepath.Join(t.TempDir(), "large.dat")
	h, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Truncate(bytes); err != nil {
		t.Fatal(err)
	}
	if _, err = h.WriteAt([]byte("end"), bytes-3); err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	a := newFixture(t, "source", false, false)
	b := newFixture(t, "destination", false, false)
	id := addPeer(t, a, b, false)
	if err := a.Send([]string{id}, []string{file}, "", "", false); err != nil {
		t.Fatal(err)
	}
	offer := b.waitOffer(t)
	if err := b.Answer(offer.ID, []string{offer.Files[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	// Stream completion can take longer than the normal short-fixture deadline.
	for {
		row := b.newest("completed")
		if row.ID != "" {
			break
		}
		select {
		case <-b.ctx.Done():
			t.Fatal("service stopped before the transfer completed")
		case <-time.After(200 * time.Millisecond):
		}
		if row = b.newest("failed"); row.ID != "" {
			t.Fatal(row.Error)
		}
	}
	check := func(name string) (int64, string) {
		t.Helper()
		input, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		hash := sha256.New()
		count, err := io.Copy(hash, input)
		if err != nil {
			t.Fatal(err)
		}
		return count, string(hash.Sum(nil))
	}
	n, expected := check(file)
	got, actual := check(filepath.Join(b.destination, "large.dat"))
	if got != n || got != bytes || actual != expected {
		t.Fatalf("mismatched large file: %d vs %d", got, n)
	}
	waitFor(t, func() (domain.Transfer, bool) { r := a.newest("completed"); return r, r.Done == bytes })
}
