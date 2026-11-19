package printing

import (
	"os"
	"path/filepath"
	"testing"

	"goim/internal/domain"
)

func TestNormalizeOptionsUsesCapabilitiesAndValidatesRanges(t *testing.T) {
	capabilities := domain.PrintCapabilities{
		Papers: []string{"A4"}, Orientations: []string{domain.PrintOrientationPortrait},
		ColorModes: []string{domain.PrintColorMonochrome}, DuplexModes: []string{domain.PrintDuplexSimplex},
		SupportsPageRange: true,
	}
	got, err := NormalizeOptions(domain.PrintOptions{PageRange: "1-3, 5"}, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if got.Copies != 1 || got.Paper != "A4" || got.PageRange != "1-3,5" ||
		got.Orientation != domain.PrintOrientationPortrait || got.Duplex != domain.PrintDuplexSimplex {
		t.Fatalf("normalized options = %#v", got)
	}
	for _, invalid := range []string{"0-2", "4-2", "one", "1,,2"} {
		if _, err := NormalizeOptions(domain.PrintOptions{PageRange: invalid}, capabilities); err == nil {
			t.Fatalf("accepted invalid page range %q", invalid)
		}
	}
	if _, err := NormalizeOptions(domain.PrintOptions{Paper: "A3"}, capabilities); err == nil {
		t.Fatal("accepted paper outside capabilities")
	}
	if _, err := NormalizeOptions(domain.PrintOptions{Paper: "Legal"}, DefaultCapabilities()); err != nil {
		t.Fatalf("rejected Legal paper: %v", err)
	}
	if _, err := NormalizeOptions(domain.PrintOptions{PageRange: "1-201"}, capabilities); err == nil {
		t.Fatal("accepted more than 200 selected pages")
	}
}

func TestValidateFileRejectsDirectoriesAndEmptyFiles(t *testing.T) {
	if _, err := ValidateFile(t.TempDir()); err == nil {
		t.Fatal("accepted a directory")
	}
	path := filepath.Join(t.TempDir(), "empty.pdf")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(path); err == nil {
		t.Fatal("accepted an empty file")
	}
}
