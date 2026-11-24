package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRemoteDesktopPreviewVersion(t *testing.T) {
	for _, version := range []string{"0.2.5", "0.2.5-beta.1", "0.2.5-rd-preview.0", "0.2.5-rd-preview.1.2", "0.2.5-rd-preview.+1", "0.2.5-rd-preview.65536"} {
		if remoteDesktopPreviewVersion(version) {
			t.Errorf("accepted preview version %q", version)
		}
	}
	if !remoteDesktopPreviewVersion("0.2.5-rd-preview.1") {
		t.Fatal("valid preview version was rejected")
	}
	if bundle, err := openRemoteDesktopBundle("", "", "0.2.5"); bundle != nil || err != nil {
		t.Fatalf("default build unexpectedly requires a bundle: %v", err)
	}
}

func TestRemoteDesktopManifestPins(t *testing.T) {
	base := fixtureRemoteDesktopManifest()
	for _, test := range []struct {
		name   string
		change func(*remoteDesktopManifest)
		want   string
	}{
		{"protocol", func(m *remoteDesktopManifest) { m.Protocol++ }, "manifest must target"},
		{"platform", func(m *remoteDesktopManifest) { m.Platform = "android" }, "manifest must target"},
		{"architecture", func(m *remoteDesktopManifest) { m.Architecture = "386" }, "manifest must target"},
		{"short helper digest", func(m *remoteDesktopManifest) { m.SHA256 = "bad" }, "invalid remote desktop helper"},
		{"moving ref", func(m *remoteDesktopManifest) { m.Source = "https://github.com/fork/rustdesk/tree/main" }, "manifest source must"},
		{"credentials", func(m *remoteDesktopManifest) { m.Source = "https://secret@github.com/fork/rustdesk/tree/" + m.Commit }, "manifest source must"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			m := base
			test.change(&m)
			pin := writeRemoteDesktopManifest(t, dir, m)
			_, err := openRemoteDesktopBundle(dir, pin, "0.2.5-rd-preview.1")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("bundle error = %v, want %q", err, test.want)
			}
		})
	}
	dir := t.TempDir()
	writeRemoteDesktopManifest(t, dir, base)
	_, err := openRemoteDesktopBundle(dir, strings.Repeat("0", 64), "0.2.5-rd-preview.1")
	if err == nil || !strings.Contains(err.Error(), "manifest SHA-256 does not match") {
		t.Fatalf("unexpected manifest digest error: %v", err)
	}
}

func TestRemoteDesktopRequiresMatchingNativeHelper(t *testing.T) {
	dir, dest := t.TempDir(), t.TempDir()
	name := remoteDesktopHelperName()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not an executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fixtureRemoteDesktopManifest()
	pin := writeRemoteDesktopManifest(t, dir, m)
	bundle, err := openRemoteDesktopBundle(dir, pin, "0.2.5-rd-preview.1")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.root.Close()
	if err := bundle.copyHelper(name, dest); err == nil || !strings.Contains(err.Error(), "SHA-256 does not match") {
		t.Fatalf("unexpected helper digest error: %v", err)
	}

	bundle.manifest.SHA256 = fileSHA256(t, filepath.Join(dir, name))
	if err := bundle.copyHelper(name, dest); err == nil || !strings.Contains(err.Error(), "verify remote desktop helper architecture") {
		t.Fatalf("unexpected native helper validation error: %v", err)
	}
	remaining, err := os.ReadDir(dest)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("invalid helper left files in package: %v, %v", remaining, err)
	}
}

func TestRemoteDesktopPreviewRetainsPinnedOfflineSource(t *testing.T) {
	dir, staging := t.TempDir(), t.TempDir()
	archive := filepath.Join(dir, remoteSourceArchive)
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "rustdesk-exact-source/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for _, writer := range []io.Closer{tw, gz, file} {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	m := fixtureRemoteDesktopManifest()
	m.Source = ""
	m.SourceArchive = remoteSourceArchive
	m.SourceSHA256 = fileSHA256(t, archive)
	pin := writeRemoteDesktopManifest(t, dir, m)
	bundle, err := openRemoteDesktopBundle(dir, pin, "0.2.5-rd-preview.1")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.root.Close()
	if err := os.WriteFile(filepath.Join(staging, "THIRD_PARTY_NOTICES.md"), []byte("# Existing notices\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := make(map[string]string)
	if err := bundle.recordProvenance(staging, info); err != nil {
		t.Fatal(err)
	}
	location := info["remote_desktop_source_archive"]
	if location != "licenses/rustdesk/source.tar.gz" || fileSHA256(t, filepath.Join(staging, location)) != m.SourceSHA256 {
		t.Fatal("preview did not preserve the pinned offline source")
	}
	bundle.manifest.SourceSHA256 = strings.Repeat("0", 64)
	if err := bundle.recordProvenance(staging, info); err == nil || !strings.Contains(err.Error(), "SHA-256 does not match") {
		t.Fatalf("unexpected changed-source result: %v", err)
	}
}

func TestRemoteDesktopPreviewStagesExactNativeFile(t *testing.T) {
	staging, dir := t.TempDir(), t.TempDir()
	name := remoteDesktopHelperName()
	path := filepath.Join(dir, name)
	source := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(source, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use a native executable without the test binary's dependencies. A Linux
	// runner can build tests against a newer glibc than the one GoIM ships for.
	build := exec.Command("go", "build", "-o", path, source)
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native test fixture: %v\n%s", err, output)
	}
	m := fixtureRemoteDesktopManifest()
	m.SHA256 = fileSHA256(t, path)
	pin := writeRemoteDesktopManifest(t, dir, m)
	bundle, err := openRemoteDesktopBundle(dir, pin, "0.2.5-rd-preview.1")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.root.Close()
	dest := filepath.Join(staging, "binary")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bundle.copyHelper(name, dest); err != nil {
		t.Fatal(err)
	}
	if got := fileSHA256(t, filepath.Join(dest, name)); got != m.SHA256 {
		t.Fatal("staged native helper does not match the pinned input")
	}
	if err := os.WriteFile(filepath.Join(staging, "THIRD_PARTY_NOTICES.md"), []byte("# Existing notices\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := make(map[string]string)
	if err := bundle.recordProvenance(staging, info); err != nil {
		t.Fatal(err)
	}
	if info["edition"] != "remote-desktop-validation" || info["remote_desktop_commit"] != m.Commit {
		t.Fatal("build metadata omitted the exact helper revision")
	}
	for _, file := range []string{"LICENSE", "manifest.json"} {
		provenance := filepath.Join(staging, "licenses", "rustdesk", file)
		if fileSHA256(t, provenance) != fileSHA256(t, filepath.Join(dir, file)) {
			t.Fatalf("staged %s changed", file)
		}
	}
}

func fixtureRemoteDesktopManifest() remoteDesktopManifest {
	const commit = "e3cf7fcfb5e0dc2c64f45254ab026dd28d454b31"
	return remoteDesktopManifest{
		Version: "1.5.3-goim.1", Protocol: 1, Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		SHA256: strings.Repeat("a", 64), Commit: commit, Source: "https://github.com/example/rustdesk/tree/" + commit,
	}
}

func writeRemoteDesktopManifest(t *testing.T, dir string, m remoteDesktopManifest) string {
	t.Helper()
	manifest := filepath.Join(dir, "manifest.json")
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	const excerpt = "GNU AFFERO GENERAL PUBLIC LICENSE\nVersion 3, 19 November 2007\nEND OF TERMS AND CONDITIONS\n"
	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte(excerpt), 0o644); err != nil {
		t.Fatal(err)
	}
	return fileSHA256(t, manifest)
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
