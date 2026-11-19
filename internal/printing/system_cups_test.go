//go:build linux || darwin

package printing

import (
	"testing"

	"goim/internal/domain"
)

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
