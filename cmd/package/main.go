// Command package builds and archives the current native target. Run on the
// matching OS and architecture; macOS uses the system compiler and ad-hoc sign.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vivaui/viva/tools/appbundle"
	"github.com/vivaui/viva/tools/winpkg"
	"github.com/vivaui/viva/tools/winres"
)

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if runtime.GOOS == "darwin" {
		c.Env = append(os.Environ(), "CGO_ENABLED=1", "CGO_CFLAGS=-mmacosx-version-min=13.0", "CGO_LDFLAGS=-mmacosx-version-min=13.0")
	}
	return c.Run()
}
func main() {
	if err := build(); err != nil {
		log.Fatal(err)
	}
}
func build() error {
	version := flag.String("version", "0.1.0-alpha.1", "release version")
	commit := flag.String("commit", "working-tree", "source commit")
	framework := flag.String("framework", "", "override Viva source (defaults to the project pin)")
	out := flag.String("out", "dist", "artifact directory")
	flag.Parse()
	if *framework == "" {
		raw, err := os.ReadFile("framework/viva-commit.txt")
		if err != nil {
			return fmt.Errorf("read Viva pin: %w", err)
		}
		pin := strings.TrimSpace(string(raw))
		if id, err := hex.DecodeString(pin); err != nil || len(id) != 20 {
			return fmt.Errorf("invalid Viva pin")
		}
		*framework = pin + "+patch"
	}
	for _, s := range []string{*version, *commit} {
		if strings.ContainsAny(s, " /\\\"'\n\r") {
			return fmt.Errorf("invalid build identifier")
		}
	}
	name := "GoIM_" + *version + "_" + runtime.GOOS + "_" + runtime.GOARCH
	staging := filepath.Join(*out, name)
	if err := os.MkdirAll(staging, 0755); err != nil {
		return err
	}
	executable := filepath.Join(staging, "GoIM")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	ld := "-s -w -buildid= -X main.version=" + *version + " -X main.commit=" + *commit
	if runtime.GOOS == "windows" {
		ld += " -H=windowsgui"
	}
	if err := run("go", "build", "-trimpath", "-ldflags", ld, "-o", executable, "./cmd/goim"); err != nil {
		return err
	}
	if err := verifyArchitecture(executable); err != nil {
		return err
	}
	companion := filepath.Join(staging, "goim-snip")
	companionFlags := "-s -w -buildid="
	if runtime.GOOS == "windows" {
		companion += ".exe"
		companionFlags += " -H=windowsgui"
	}
	if err := run("go", "build", "-trimpath", "-ldflags", companionFlags, "-o", companion, "./cmd/goim-snip"); err != nil {
		return err
	}
	if err := verifyArchitecture(companion); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		ico, _, err := winpkg.LoadIcon("internal/desktop/assets/icon.png")
		if err != nil {
			return err
		}
		b, err := os.ReadFile(executable)
		if err != nil {
			return err
		}
		b, err = winres.Patch(b, ico, winres.Info{Name: "GoIM", Description: "GoIM LAN Messenger", Version: "0.1.0", Publisher: "Ezreal-byte", File: "GoIM.exe"})
		if err != nil {
			return err
		}
		if err = os.WriteFile(executable, b, 0755); err != nil {
			return err
		}
	case "darwin":
		bundle := filepath.Join(staging, "GoIM.app")
		_, err := appbundle.Assemble(appbundle.Options{Dest: bundle, Executable: executable, Icon: "internal/desktop/assets/icon.png", Info: appbundle.Info{Name: "GoIM", ID: "dev.goim.desktop", Version: "0.1.0", Executable: "GoIM", MinSystem: "13.0", Category: "public.app-category.social-networking", Copyright: "2026 Ezreal-byte"}})
		if err != nil {
			return err
		}
		if err := os.Rename(companion, filepath.Join(bundle, "Contents", "MacOS", "goim-snip")); err != nil {
			return err
		}
		if err := run("codesign", "--force", "--sign", "-", filepath.Join(bundle, "Contents", "MacOS", "goim-snip")); err != nil {
			return err
		}
		if err = run("codesign", "--force", "--sign", "-", bundle); err != nil {
			return err
		}
		if err = run("codesign", "--verify", "--deep", "--strict", bundle); err != nil {
			return err
		}
		if err = os.Remove(executable); err != nil {
			return err
		}
	}
	for _, p := range []string{"README.md", "LICENSE", "THIRD_PARTY_NOTICES.md"} {
		if err := copyFile(p, filepath.Join(staging, p)); err != nil {
			return err
		}
	}
	if err := filepath.WalkDir("licenses", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return copyFile(path, filepath.Join(staging, path))
	}); err != nil {
		return err
	}
	info := map[string]string{"version": *version, "commit": *commit, "viva": *framework, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "desktop_validation": "Windows loopback and offscreen tests; native shortcut lifecycle tests; physical multi-device desktop validation pending"}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(filepath.Join(staging, "build-info.json"), b, 0644); err != nil {
		return err
	}
	ext := ".zip"
	if runtime.GOOS == "linux" {
		ext = ".tar.gz"
	}
	archive := filepath.Join(*out, name+ext)
	if err := pack(staging, archive, runtime.GOOS == "linux"); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil)) + "  " + filepath.Base(archive) + "\n"
	if err = os.WriteFile(archive+".sha256", []byte(sum), 0644); err != nil {
		return err
	}
	fmt.Print(sum)
	return nil
}

func verifyArchitecture(path string) error {
	valid := false
	switch runtime.GOOS {
	case "windows":
		f, err := pe.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		valid = runtime.GOARCH == "amd64" && f.Machine == pe.IMAGE_FILE_MACHINE_AMD64 || runtime.GOARCH == "arm64" && f.Machine == pe.IMAGE_FILE_MACHINE_ARM64
	case "linux":
		f, err := elf.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		valid = runtime.GOARCH == "amd64" && f.Machine == elf.EM_X86_64 || runtime.GOARCH == "arm64" && f.Machine == elf.EM_AARCH64
	case "darwin":
		f, err := macho.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		valid = runtime.GOARCH == "amd64" && f.Cpu == macho.CpuAmd64 || runtime.GOARCH == "arm64" && f.Cpu == macho.CpuArm64
	}
	if !valid {
		return fmt.Errorf("binary architecture does not match native target %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return nil
}
func copyFile(src, dest string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0644)
}
func pack(root, dest string, tgz bool) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	var z *zip.Writer
	var tw *tar.Writer
	var gz *gzip.Writer
	if tgz {
		gz = gzip.NewWriter(f)
		tw = tar.NewWriter(gz)
	} else {
		z = zip.NewWriter(f)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		var w io.Writer
		if tgz {
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = rel
			hdr.ModTime = time.Unix(0, 0)
			if err = tw.WriteHeader(hdr); err != nil {
				return err
			}
			w = tw
		} else {
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = rel
			hdr.Method = zip.Deflate
			hdr.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
			w, err = z.CreateHeader(hdr)
			if err != nil {
				return err
			}
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, input)
		input.Close()
		return err
	})
	if err != nil {
		return err
	}
	if tgz {
		if err = tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	}
	return z.Close()
}
