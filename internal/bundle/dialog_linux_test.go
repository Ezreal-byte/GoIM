package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDialogDirectorySupportsRelocatedTarAndDEB(t *testing.T) {
	for _, installed := range []bool{false, true} {
		root := filepath.Join(t.TempDir(), "离线 安装 & runtime")
		bin := root
		if installed {
			bin = filepath.Join(root, "bin")
		}
		want := filepath.Join(root, "lib", "goim", "file-dialog", "bin")
		if err := os.MkdirAll(want, 0755); err != nil {
			t.Fatal(err)
		}
		helper := filepath.Join(want, "zenity")
		if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
		if got := dialogDirectory(filepath.Join(bin, "GoIM")); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if err := os.Chmod(helper, 0644); err != nil {
			t.Fatal(err)
		}
		if got := dialogDirectory(filepath.Join(bin, "GoIM")); got != "" {
			t.Fatalf("non executable helper: %q", got)
		}
	}
}
