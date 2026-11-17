package selfupdate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/vivaui/viva/tools/winpkg"
	"golang.org/x/sys/windows/registry"
)

func nativeExeSuffix() string { return ".exe" }

func prepareNativeUpgrade(t *testing.T, root, binary string) (string, string) {
	t.Helper()
	keyName := "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\" + AppID
	installed, err := registry.OpenKey(registry.CURRENT_USER, keyName, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err == nil {
		_ = installed.Close()
		t.Fatal("existing GoIM installation; the fixture requires a clean user")
	}
	if !errors.Is(err, registry.ErrNotExist) {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "GoIM update fixture")
	build := func(version string) string {
		source := filepath.Join(root, version)
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		var extra []winpkg.ExtraFile
		for name, value := range map[string]string{"fixture-version": version, "goim-snip.exe": "companion " + version} {
			path := filepath.Join(source, name)
			if err := os.WriteFile(path, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
			extra = append(extra, winpkg.ExtraFile{Source: path, Destination: name})
		}
		artifact := filepath.Join(source, "setup.exe")
		_, err := winpkg.Build(winpkg.Script{Name: "GoIM Update Fixture", Version: version, Publisher: "GoIM Test",
			ID: AppID, Scope: "user", Arch: runtime.GOARCH, Executable: "GoIM.exe", Program: binary, Output: artifact, Files: extra})
		if err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	oldSetup, newSetup := build(nativeOld), build(nativeNew)
	uninstall := filepath.Join(dir, "Uninstall.exe")
	t.Cleanup(func() {
		if !regularFile(uninstall) {
			return
		}
		nativeCommand(t, exec.Command(uninstall, "/S"))
		// The uninstaller's launcher can exit while its cleanup copy is still
		// working. Wait before t.TempDir removes the fixture.
		deadline := time.Now().Add(10 * time.Second)
		for {
			key, err := registry.OpenKey(registry.CURRENT_USER, keyName, registry.QUERY_VALUE|registry.WOW64_64KEY)
			if err == nil {
				_ = key.Close()
			}
			if errors.Is(err, registry.ErrNotExist) && !regularFile(uninstall) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("fixture uninstall did not remove its files and registration")
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	install := exec.Command(oldSetup)
	// NSIS takes the rest of the command line after the final /D= literally.
	install.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(oldSetup) + " /S /D=" + dir}
	nativeCommand(t, install)
	return filepath.Join(dir, "GoIM.exe"), newSetup
}
