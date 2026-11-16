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

const logoAssets = "internal/brand/assets"

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
	version := flag.String("version", "0.1.0", "release version")
	commit := flag.String("commit", "", "source commit (defaults to git HEAD or working-tree)")
	framework := flag.String("framework", "", "override Viva source (defaults to the project pin)")
	source := flag.String("source", "", "build source: local or github-actions (defaults from CI)")
	buildTime := flag.String("build-time", "", "UTC build time in RFC3339 (defaults to now)")
	ciURL := flag.String("ci-url", "", "CI run URL (defaults from GitHub Actions)")
	out := flag.String("out", "dist", "artifact directory")
	stamp := flag.String("stamp-windows", "", "apply the Windows icon and metadata to an existing GoIM executable")
	flag.Parse()
	platformVersion, _, _ := strings.Cut(*version, "-")
	if *stamp != "" {
		return stampWindowsExecutable(*stamp, platformVersion)
	}
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
	sourceCommit := strings.TrimSpace(*commit)
	if sourceCommit == "" {
		sourceCommit = localCommit()
	}
	for _, s := range []string{*version, sourceCommit} {
		if strings.ContainsAny(s, " /\\\"'\n\r") {
			return fmt.Errorf("invalid build identifier")
		}
	}
	facts := packageBuildFactsFromEnvironment(os.Getenv, time.Now().UTC())
	facts.commitTime = localCommitTime(sourceCommit)
	if value := strings.TrimSpace(*source); value != "" {
		facts.source = value
	}
	if value := strings.TrimSpace(*buildTime); value != "" {
		facts.buildTime = value
	}
	if value := strings.TrimSpace(*ciURL); value != "" {
		facts.ciURL = value
	}
	if _, err := time.Parse(time.RFC3339, facts.buildTime); err != nil {
		return fmt.Errorf("invalid build time")
	}
	if facts.source != "local" && facts.source != "github-actions" {
		return fmt.Errorf("invalid build source")
	}
	if !validFrameworkValue(*framework) {
		return fmt.Errorf("invalid framework revision")
	}
	for _, value := range []string{*framework, facts.source, facts.buildTime, facts.commitTime, facts.ciRef, facts.ciRunID, facts.ciURL} {
		if !validLinkerValue(value) {
			return fmt.Errorf("invalid build metadata")
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
	ld := packageLinkerFlags(*version, sourceCommit, *framework, facts, runtime.GOOS == "windows")
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
		if err := stampWindowsExecutable(executable, platformVersion); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(logoAssets, "goim.ico"), filepath.Join(staging, "GoIM.ico")); err != nil {
			return err
		}
	case "darwin":
		bundle := filepath.Join(staging, "GoIM.app")
		_, err := appbundle.Assemble(appbundle.Options{Dest: bundle, Executable: executable, Icon: filepath.Join(logoAssets, "goim.icns"), Info: appbundle.Info{Name: "GoIM", ID: "dev.goim.desktop", Version: platformVersion, Executable: "GoIM", MinSystem: "13.0", Category: "public.app-category.social-networking", Copyright: "2026 Ezreal-byte"}})
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
	case "linux":
		if err := copyFile(filepath.Join(logoAssets, "goim.desktop"), filepath.Join(staging, "share", "applications", "dev.goim.desktop.desktop")); err != nil {
			return err
		}
		for _, size := range []int{16, 24, 32, 48, 64, 128, 256, 512} {
			source := filepath.Join(logoAssets, fmt.Sprintf("goim-%d.png", size))
			dest := filepath.Join(staging, "share", "icons", "hicolor", fmt.Sprintf("%dx%d", size, size), "apps", "goim.png")
			if err := copyFile(source, dest); err != nil {
				return err
			}
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
	info := map[string]string{
		"version": *version, "commit": sourceCommit, "commit_time": facts.commitTime,
		"viva": *framework, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"source": facts.source, "source_ref": facts.ciRef, "run_id": facts.ciRunID,
		"build_time": facts.buildTime, "ci_url": facts.ciURL,
		"desktop_validation": "Windows loopback and offscreen tests; native shortcut lifecycle tests; physical multi-device desktop validation pending",
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
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

type packageBuildFacts struct {
	source     string
	ciRef      string
	ciRunID    string
	ciURL      string
	buildTime  string
	commitTime string
}

func packageBuildFactsFromEnvironment(getenv func(string) string, now time.Time) packageBuildFacts {
	facts := packageBuildFacts{
		source:    "local",
		buildTime: now.UTC().Format(time.RFC3339),
	}
	if getenv("GITHUB_ACTIONS") != "true" {
		return facts
	}
	facts.source = "github-actions"
	facts.ciRef = firstValue(getenv("GITHUB_REF"), getenv("GITHUB_REF_NAME"))
	facts.ciRunID = strings.TrimSpace(getenv("GITHUB_RUN_ID"))
	server := firstValue(getenv("GITHUB_SERVER_URL"), "https://github.com")
	repository := strings.TrimSpace(getenv("GITHUB_REPOSITORY"))
	if repository != "" && facts.ciRunID != "" {
		facts.ciURL = strings.TrimRight(server, "/") + "/" + repository + "/actions/runs/" + facts.ciRunID
	}
	return facts
}

func packageLinkerFlags(version, commit, framework string, facts packageBuildFacts, windowsGUI bool) string {
	parts := []string{
		"-s", "-w", "-buildid=",
		"-X", "main.version=" + version,
		"-X", "main.commit=" + commit,
		"-X", "goim/internal/buildinfo.Version=" + version,
		"-X", "goim/internal/buildinfo.Commit=" + commit,
		"-X", "goim/internal/buildinfo.CommitTime=" + facts.commitTime,
		"-X", "goim/internal/buildinfo.Framework=" + framework,
		"-X", "goim/internal/buildinfo.Source=" + facts.source,
		"-X", "goim/internal/buildinfo.BuildTime=" + facts.buildTime,
		"-X", "goim/internal/buildinfo.CIRef=" + facts.ciRef,
		"-X", "goim/internal/buildinfo.CIRunID=" + facts.ciRunID,
		"-X", "goim/internal/buildinfo.CIURL=" + facts.ciURL,
	}
	if windowsGUI {
		parts = append(parts, "-H=windowsgui")
	}
	return strings.Join(parts, " ")
}

func validLinkerValue(value string) bool {
	return !strings.ContainsAny(value, " \t\r\n\"'\\")
}

func validFrameworkValue(value string) bool {
	return value != "" && validLinkerValue(value) && !strings.ContainsAny(value, "/")
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func localCommit() string {
	revision, err := gitOutput("rev-parse", "HEAD")
	if err != nil || revision == "" {
		return "working-tree"
	}
	if status, err := gitOutput("status", "--porcelain"); err == nil && status != "" {
		revision += "-dirty"
	}
	return revision
}

func localCommitTime(revision string) string {
	revision = strings.TrimSuffix(strings.TrimSpace(revision), "-dirty")
	if !hexRevision(revision) {
		return ""
	}
	value, err := gitOutput("show", "-s", "--format=%cI", revision)
	if err != nil {
		return ""
	}
	stamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}
	return stamp.UTC().Format(time.RFC3339)
}

func hexRevision(value string) bool {
	if len(value) < 7 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			if digit < 'a' || digit > 'f' {
				if digit < 'A' || digit > 'F' {
					return false
				}
			}
		}
	}
	return true
}

func gitOutput(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output()
	return strings.TrimSpace(string(output)), err
}

func stampWindowsExecutable(path, version string) error {
	ico, _, err := winpkg.LoadIcon(filepath.Join(logoAssets, "goim.ico"))
	if err != nil {
		return err
	}
	executable, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	executable, err = winres.Patch(executable, ico, winres.Info{Name: "GoIM", Description: "GoIM LAN Messenger", Version: version, Publisher: "Ezreal-byte", File: "GoIM.exe"})
	if err != nil {
		return err
	}
	return os.WriteFile(path, executable, 0755)
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
