package selfupdate

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/tools/appbundle"
)

func nativeExeSuffix() string { return "" }

func prepareNativeUpgrade(t *testing.T, root, binary string) (string, string) {
	t.Helper()
	build := func(version string) string {
		bundle := filepath.Join(root, version, "GoIM.app")
		if _, err := appbundle.Assemble(appbundle.Options{Dest: bundle, Executable: binary,
			Info: appbundle.Info{Name: "GoIM Update Fixture", ID: AppID, Version: version, Executable: "GoIM", MinSystem: "13.0"}}); err != nil {
			t.Fatal(err)
		}
		mac := filepath.Join(bundle, "Contents", "MacOS")
		resources := filepath.Join(bundle, "Contents", "Resources")
		if err := os.MkdirAll(resources, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(resources, "fixture-version"), []byte(version), 0600); err != nil {
			t.Fatal(err)
		}
		payload, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mac, "goim-snip"), payload, 0755); err != nil {
			t.Fatal(err)
		}
		if b, err := exec.Command("codesign", "--force", "--sign", "-", filepath.Join(mac, "goim-snip")).CombinedOutput(); err != nil {
			t.Fatalf("sign helper: %v\n%s", err, b)
		}
		if b, err := exec.Command("codesign", "--force", "--sign", "-", bundle).CombinedOutput(); err != nil {
			t.Fatalf("sign: %v\n%s", err, b)
		}
		return bundle
	}
	old, next := build(nativeOld), build(nativeNew)
	archive := filepath.Join(root, "GoIM.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(file)
	err = z.AddFS(os.DirFS(filepath.Dir(next)))
	zipErr, closeErr := z.Close(), file.Close()
	if err != nil || zipErr != nil || closeErr != nil {
		t.Fatalf("zip fixture: %v %v %v", err, zipErr, closeErr)
	}
	return filepath.Join(old, "Contents", "MacOS", "GoIM"), archive
}
