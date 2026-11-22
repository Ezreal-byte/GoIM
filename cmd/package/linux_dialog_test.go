package main

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLinuxDEBBundlesFileDialogFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("DEB assembly requires Linux")
	}
	for _, tool := range []string{"bash", "dpkg-deb", "shasum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is unavailable", tool)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "packaging", "unix.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		t.Run(architecture, func(t *testing.T) {
			output := t.TempDir()
			name := "GoIM_0.1.14-dialog-test_linux_" + architecture
			for _, entry := range []string{
				"GoIM", "goim-snip", "goim-media", "LICENSE", "README.md", "THIRD_PARTY_NOTICES.md", "build-info.json",
				"licenses/fixture.txt", "share/applications/dev.goim.desktop.desktop", "share/polkit-1/actions/dev.goim.desktop.policy",
				"lib/goim/file-dialog/bin/zenity", "lib/goim/file-dialog/xclip", "lib/goim/file-dialog/lib/libgtk-3.so.0",
			} {
				path := filepath.Join(output, name, filepath.FromSlash(entry))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(output, name, "build-info.json"), []byte(`{"update_public_key":""}`), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(output, name, "lib/goim/file-dialog/architecture"), []byte(architecture), 0644); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", script, "0.1.14-dialog-test", "linux", architecture, output)
			if result, err := command.CombinedOutput(); err != nil {
				t.Fatalf("assemble DEB: %v\n%s", err, result)
			}
			archive := filepath.Join(output, name+".deb")
			result, err := exec.Command("dpkg-deb", "--field", archive, "Depends").Output()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, dependency := range strings.Split(string(result), ",") {
				if strings.TrimSpace(dependency) == "zenity | kdialog" {
					found = true
				}
			}
			if found {
				t.Fatalf("offline installation must not require an external dialog package; Depends=%s", result)
			}
			extracted := filepath.Join(t.TempDir(), "extract")
			if result, err := exec.Command("dpkg-deb", "--extract", archive, extracted).CombinedOutput(); err != nil {
				t.Fatalf("extract: %v\n%s", err, result)
			}
			helper, err := elf.Open(filepath.Join(extracted, "usr/lib/goim/deb-update"))
			if err != nil {
				t.Fatal(err)
			}
			defer helper.Close()
			if want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[architecture]; helper.Machine != want {
				t.Fatalf("DEB helper architecture %v, want %v", helper.Machine, want)
			}
			for _, file := range []string{"bin/zenity", "xclip", "lib/libgtk-3.so.0", "architecture"} {
				if _, err := os.Stat(filepath.Join(extracted, "usr/lib/goim/file-dialog", file)); err != nil {
					t.Fatal(err)
				}
			}
			control := filepath.Join(t.TempDir(), "control")
			if result, err := exec.Command("dpkg-deb", "--control", archive, control).CombinedOutput(); err != nil {
				t.Fatalf("extract control: %v\n%s", err, result)
			}
			preinst := filepath.Join(control, "preinst")
			info, err := os.Stat(preinst)
			if err != nil || info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("DEB preinst must be executable: %v", err)
			}
			if result, err := exec.Command("sh", "-n", preinst).CombinedOutput(); err != nil {
				t.Fatalf("invalid preinst: %v\n%s", err, result)
			}
		})
	}
}

func TestLinuxInstallerBlocksRunningGoIM(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux installer process fixture")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "packaging", "linux", "preinst"))
	if err != nil {
		t.Fatal(err)
	}
	for _, running := range []bool{false, true} {
		directory := t.TempDir()
		process := filepath.Join(directory, "proc", "1234")
		if err := os.MkdirAll(process, 0o700); err != nil {
			t.Fatal(err)
		}
		target := "/usr/bin/unrelated"
		if running {
			target = "/usr/bin/GoIM (deleted)"
		}
		if err := os.Symlink(target, filepath.Join(process, "exe")); err != nil {
			t.Fatal(err)
		}
		fixture := strings.Replace(string(source), "/proc/[0-9]*/exe", filepath.Join(directory, "proc")+"/[0-9]*/exe", 1)
		path := filepath.Join(directory, "preinst")
		if err := os.WriteFile(path, []byte(fixture), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"install", "upgrade", "abort-upgrade"} {
			result, err := exec.Command("sh", path, action).CombinedOutput()
			blocked := running && action != "abort-upgrade"
			if blocked != (err != nil) || blocked && !strings.Contains(string(result), "GoIM is running") {
				t.Fatalf("running=%v action=%s: %v\n%s", running, action, err, result)
			}
		}
	}
}
