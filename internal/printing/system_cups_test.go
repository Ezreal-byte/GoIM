//go:build linux || darwin

package printing

import (
	"context"
	"errors"
	"slices"
	"testing"

	"goim/internal/domain"
)

func TestCUPSCancelUsesOnlyTheReturnedQueueJob(t *testing.T) {
	previous := run
	defer func() { run = previous }()
	var calls int
	run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if name != "cancel" || !slices.Equal(args, []string{"office-east-42"}) {
			t.Fatal(name, args)
		}
		return []byte("job already completed"), errors.New("exit status 1")
	}
	backend := System{}
	for _, id := range []string{"-a", "other-42", "office-east-0", "office-east-42 "} {
		if err := backend.Cancel(context.Background(), Receipt{Printer: "office-east", JobID: id}); err == nil {
			t.Fatal(id)
		}
	}
	if calls != 0 {
		t.Fatal("invalid receipt reached CUPS")
	}
	if err := backend.Cancel(context.Background(), Receipt{Printer: "office-east", JobID: "office-east-42"}); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestCUPSParseCapabilities(t *testing.T) {
	got := cupsCapabilities(`
PageSize/Page Size: *A4 A3 Letter Legal
sides/Two-Sided: *one-sided two-sided-long-edge two-sided-short-edge
ColorModel/Color Mode: *Gray RGB
`, DefaultCapabilities())
	if len(got.Papers) != 4 || got.Papers[0] != "A4" || got.Papers[3] != "Legal" {
		t.Fatalf("papers = %v", got.Papers)
	}
	if len(got.ColorModes) != 2 || got.ColorModes[0] != domain.PrintColorMonochrome {
		t.Fatalf("colors = %v", got.ColorModes)
	}
	if len(got.DuplexModes) != 3 || got.DuplexModes[1] != domain.PrintDuplexLongEdge {
		t.Fatalf("duplex = %v", got.DuplexModes)
	}
}

func TestCUPSCompletedStateDistinguishesCanceledJobs(t *testing.T) {
	output := `
printer-1-42 user 1024 completed
printer-1-43 user 1024 cancelled
printer-1-44 user 1024 aborted
`
	if got := cupsCompletedState(output, "printer-1-42"); got != StateCompleted {
		t.Fatalf("completed job state = %v", got)
	}
	if got := cupsCompletedState(output, "printer-1-43"); got != StateFailed {
		t.Fatalf("cancelled job state = %v", got)
	}
	if got := cupsCompletedState(output, "printer-1-44"); got != StateFailed {
		t.Fatalf("aborted job state = %v", got)
	}
	if got := cupsCompletedState(output, "printer-1-45"); got != StateUnknown {
		t.Fatalf("missing job state = %v", got)
	}
}
