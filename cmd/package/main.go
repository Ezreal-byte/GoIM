// Command package builds and archives the current native target. Run on the
// matching OS and architecture; macOS uses the system compiler and ad-hoc sign.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	binaryinfo "debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/base64"
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
	"sort"
	"strconv"
	"strings"
	"time"

	"goim/internal/buildinfo"

	"github.com/vivaui/viva/tools/appbundle"
	"github.com/vivaui/viva/tools/winpkg"
	"github.com/vivaui/viva/tools/winres"
	"github.com/vivaui/viva/update"
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
	source := flag.String("source", "", "build source: local or github-actions (defaults from CI)")
	buildTime := flag.String("build-time", "", "UTC build time in RFC3339 (defaults to now)")
	ciURL := flag.String("ci-url", "", "CI run URL (defaults from GitHub Actions)")
	out := flag.String("out", "dist", "artifact directory")
	windowsSetup := flag.Bool("windows-setup", false, "also build the Viva Windows Setup installer (requires NSIS)")
	stamp := flag.String("stamp-windows", "", "apply the Windows icon and metadata to an existing GoIM executable")
	stageCapture := flag.String("stage-shotlet", "", "build and stage the Shotlet helper beside a Windows development build")
	flag.Parse()
	platformVersion, _, _ := strings.Cut(*version, "-")
	if *stamp != "" {
		return stampWindowsExecutable(*stamp, platformVersion)
	}
	if *stageCapture != "" {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("--stage-shotlet is for Windows development builds")
		}
		return stageShotlet(*stageCapture, filepath.Join(*stageCapture, "GoIM.exe"))
	}
	if *windowsSetup && runtime.GOOS != "windows" {
		return fmt.Errorf("--windows-setup requires a native Windows build")
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
	if key := strings.TrimSpace(os.Getenv("GOIM_UPDATE_PUBLIC_KEY")); key != "" {
		parsed, err := update.ParsePublicKey(key)
		if err != nil {
			return fmt.Errorf("invalid GOIM_UPDATE_PUBLIC_KEY: %w", err)
		}
		facts.updateKey = base64.StdEncoding.EncodeToString(parsed)
	}
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
	for _, value := range []string{facts.source, facts.buildTime, facts.commitTime, facts.ciRef, facts.ciRunID, facts.ciURL} {
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
	ld := packageLinkerFlags(*version, sourceCommit, facts, runtime.GOOS == "windows")
	if err := run("go", "build", "-trimpath", "-ldflags", ld, "-o", executable, "./cmd/goim"); err != nil {
		return err
	}
	if err := verifyArchitecture(executable); err != nil {
		return err
	}
	if err := verifyPortableLinuxBinary(executable); err != nil {
		return err
	}
	linked, err := binaryinfo.ReadFile(executable)
	if err != nil {
		return fmt.Errorf("read GoIM build info: %w", err)
	}
	vivaVersion := buildinfo.Read(linked, buildinfo.Injection{}).VivaVersion
	if vivaVersion == "" {
		return fmt.Errorf("GoIM build info does not identify its Viva dependency")
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
	if err := verifyPortableLinuxBinary(companion); err != nil {
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
		if err := stageShotlet(staging, executable); err != nil {
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
		if err := stageShotlet(staging, filepath.Join(bundle, "Contents", "MacOS", "GoIM")); err != nil {
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
		if err := copyFile(filepath.Join(logoAssets, "dev.goim.desktop.policy"), filepath.Join(staging, "share", "polkit-1", "actions", "dev.goim.desktop.policy")); err != nil {
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
		"viva": vivaVersion, "go": linked.GoVersion, "os": runtime.GOOS, "arch": runtime.GOARCH,
		"source": facts.source, "source_ref": facts.ciRef, "run_id": facts.ciRunID,
		"build_time": facts.buildTime, "ci_url": facts.ciURL, "update_public_key": facts.updateKey,
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
	if err := writeChecksum(archive); err != nil {
		return err
	}
	if *windowsSetup {
		setup := filepath.Join(*out, name+"-Setup.exe")
		installerVersion, err := windowsInstallerVersion(*version)
		if err != nil {
			return err
		}
		if err := buildWindowsSetup(staging, setup, installerVersion, runtime.GOARCH); err != nil {
			return err
		}
		return writeChecksum(setup)
	}
	return nil
}

// windowsInstallerVersion maps a release such as 0.1.8-beta.1 to the fourth
// numeric field in Windows version resources. The application and artifact
// names keep the full prerelease version.
func windowsInstallerVersion(version string) (string, error) {
	stable, prerelease, prereleaseFound := strings.Cut(version, "-")
	parts := strings.Split(stable, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return "", fmt.Errorf("Windows installer version requires a one-to-three part numeric base: %q", version)
	}
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || number > 65535 {
			return "", fmt.Errorf("Windows installer version has an invalid numeric field: %q", version)
		}
	}
	if !prereleaseFound {
		return stable, nil
	}
	segments := strings.Split(prerelease, ".")
	ordinal, err := strconv.Atoi(segments[len(segments)-1])
	if err != nil || ordinal < 1 || ordinal > 65535 {
		return "", fmt.Errorf("Windows installer prerelease must end in an ordinal from 1 to 65535: %q", version)
	}
	return stable + "." + strconv.Itoa(ordinal), nil
}

func writeChecksum(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil)) + "  " + filepath.Base(path) + "\n"
	if err = os.WriteFile(path+".sha256", []byte(sum), 0644); err != nil {
		return err
	}
	fmt.Print(sum)
	return nil
}

type packageBuildFacts struct {
	updateKey  string
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

func packageLinkerFlags(version, commit string, facts packageBuildFacts, windowsGUI bool) string {
	parts := []string{
		"-s", "-w", "-buildid=",
		"-X", "goim/internal/buildinfo.Version=" + version,
		"-X", "goim/internal/buildinfo.Commit=" + commit,
		"-X", "goim/internal/buildinfo.CommitTime=" + facts.commitTime,
		"-X", "goim/internal/buildinfo.Source=" + facts.source,
		"-X", "goim/internal/buildinfo.BuildTime=" + facts.buildTime,
		"-X", "goim/internal/buildinfo.CIRef=" + facts.ciRef,
		"-X", "goim/internal/buildinfo.CIRunID=" + facts.ciRunID,
		"-X", "goim/internal/buildinfo.CIURL=" + facts.ciURL,
	}
	if facts.updateKey != "" {
		parts = append(parts, "-X", "goim/internal/buildinfo.UpdatePublicKey="+facts.updateKey)
	}
	if windowsGUI {
		parts = append(parts, "-H=windowsgui")
	}
	return strings.Join(parts, " ")
}

func validLinkerValue(value string) bool {
	return !strings.ContainsAny(value, " \t\r\n\"'\\")
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

// verifyPortableLinuxBinary keeps Linux packages within the GLIBC baseline
// available on supported Kylin and UOS hosts. Viva's pure-Go Linux path still
// asks the dynamic loader for libc, pthread and libdl through purego, so those
// baseline libraries are valid; a cgo build or any newer GLIBC symbol is not.
func verifyPortableLinuxBinary(path string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	libraries, err := f.ImportedLibraries()
	if err != nil {
		return fmt.Errorf("inspect Linux dynamic dependencies: %w", err)
	}
	symbols, err := f.ImportedSymbols()
	if err != nil {
		return fmt.Errorf("inspect Linux dynamic symbols: %w", err)
	}
	var versions []string
	seen := make(map[string]bool)
	for _, symbol := range symbols {
		if strings.HasPrefix(symbol.Version, "GLIBC_") && !seen[symbol.Version] {
			seen[symbol.Version] = true
			versions = append(versions, symbol.Version)
		}
	}
	return verifyLinuxDynamicRequirements(filepath.Base(path), libraries, versions)
}

func verifyLinuxDynamicRequirements(name string, libraries, versions []string) error {
	allowedLibraries := map[string]bool{
		"libc.so.6":       true,
		"libdl.so.2":      true,
		"libpthread.so.0": true,
	}
	var unexpected []string
	for _, library := range libraries {
		if !allowedLibraries[library] {
			unexpected = append(unexpected, library)
		}
	}
	if len(unexpected) != 0 {
		sort.Strings(unexpected)
		return fmt.Errorf("%s links libraries outside the Linux baseline (%s)", name, strings.Join(unexpected, ", "))
	}

	const maximum = "2.31"
	var newer []string
	for _, requirement := range versions {
		version := strings.TrimPrefix(requirement, "GLIBC_")
		after, valid := numericVersionAfter(version, maximum)
		if !valid || after {
			newer = append(newer, requirement)
		}
	}
	if len(newer) != 0 {
		sort.Strings(newer)
		return fmt.Errorf("%s requires GLIBC %s; the Linux release baseline is %s", name, strings.Join(newer, ", "), maximum)
	}
	return nil
}

func numericVersionAfter(version, maximum string) (after, valid bool) {
	parse := func(value string) ([]int, bool) {
		parts := strings.Split(value, ".")
		result := make([]int, 0, max(len(parts), 3))
		for _, part := range parts {
			number, err := strconv.Atoi(part)
			if err != nil || number < 0 {
				return nil, false
			}
			result = append(result, number)
		}
		for len(result) < 3 {
			result = append(result, 0)
		}
		return result, true
	}
	got, gotOK := parse(version)
	limit, limitOK := parse(maximum)
	if !gotOK || !limitOK {
		return false, false
	}
	for index := 0; index < len(got) || index < len(limit); index++ {
		left, right := 0, 0
		if index < len(got) {
			left = got[index]
		}
		if index < len(limit) {
			right = limit[index]
		}
		if left != right {
			return left > right, true
		}
	}
	return false, true
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
