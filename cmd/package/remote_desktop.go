package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/vivaui/viva/tools/appbundle"
)

const (
	remoteDesktopProtocol = 1
	rustDeskUpstream      = "95cca3f661f635b3cc85c04935fe8d96d8f432ef"
	remoteHelperLimit     = 256 << 20
	remoteSourceLimit     = 512 << 20
	remoteSourceArchive   = "source.tar.gz"
)

type remoteDesktopManifest struct {
	Version       string `json:"version"`
	Protocol      int    `json:"protocol"`
	Platform      string `json:"platform"`
	Architecture  string `json:"architecture"`
	SHA256        string `json:"sha256"`
	Source        string `json:"source,omitempty"`
	Commit        string `json:"commit"`
	SourceArchive string `json:"source_archive,omitempty"`
	SourceSHA256  string `json:"source_sha256,omitempty"`
}

type remoteDesktopBundle struct {
	root         *os.Root
	manifest     remoteDesktopManifest
	manifestJSON []byte
	license      []byte
	notice       []byte
}

func openRemoteDesktopBundle(dir, manifestSHA, version string) (*remoteDesktopBundle, error) {
	if dir == "" && manifestSHA == "" {
		return nil, nil
	}
	if dir == "" || !exactHex(manifestSHA, sha256.Size*2) {
		return nil, errors.New("remote desktop requires --remote-desktop-bundle and a pinned --remote-desktop-manifest-sha256")
	}
	if !remoteDesktopStableVersion(version) && !remoteDesktopPreviewVersion(version) {
		return nil, errors.New("remote desktop requires a stable version or an rd-preview version")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open remote desktop bundle: %w", err)
	}
	bundle := &remoteDesktopBundle{root: root}
	if err := bundle.read(manifestSHA); err != nil {
		root.Close()
		return nil, fmt.Errorf("read remote desktop bundle: %w", err)
	}
	if remoteDesktopStableVersion(version) && bundle.manifest.SourceArchive == "" {
		root.Close()
		return nil, errors.New("stable remote desktop packages require an offline source archive")
	}
	return bundle, nil
}

func remoteDesktopPreviewVersion(version string) bool {
	base, suffix, ok := strings.Cut(version, "-rd-preview.")
	return ok && remoteDesktopStableVersion(base) && decimalVersionField(suffix, false)
}

func remoteDesktopStableVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !decimalVersionField(part, true) {
			return false
		}
	}
	return true
}

func decimalVersionField(value string, allowZero bool) bool {
	number, err := strconv.ParseUint(value, 10, 16)
	return err == nil && (allowZero || number != 0) && strconv.FormatUint(number, 10) == value
}

func (bundle *remoteDesktopBundle) read(manifestSHA string) error {
	data, err := readBundleFile(bundle.root, "manifest.json", 16<<10)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), manifestSHA) {
		return errors.New("manifest SHA-256 does not match the pin")
	}
	bundle.manifestJSON = data
	if err := json.Unmarshal(data, &bundle.manifest); err != nil {
		return fmt.Errorf("invalid manifest JSON: %w", err)
	}
	m := bundle.manifest
	if m.Protocol != remoteDesktopProtocol || m.Platform != runtime.GOOS || m.Architecture != runtime.GOARCH {
		return fmt.Errorf("manifest must target protocol %d on %s/%s", remoteDesktopProtocol, runtime.GOOS, runtime.GOARCH)
	}
	if m.Version == "" || len(m.Version) > 96 || !validLinkerValue(m.Version) || !exactHex(m.SHA256, sha256.Size*2) {
		return errors.New("invalid remote desktop helper version or SHA-256")
	}
	if !exactHex(m.Commit, 40) {
		return errors.New("manifest commit must be the full fork commit hash")
	}
	if m.Source != "" {
		if !pinnedRemoteDesktopSource(m.Source, m.Commit) || m.SourceArchive != "" || m.SourceSHA256 != "" {
			return errors.New("manifest source must be an HTTPS URL referencing the full fork commit, or a pinned offline archive")
		}
	} else if m.SourceArchive != remoteSourceArchive || !exactHex(m.SourceSHA256, sha256.Size*2) {
		return errors.New("manifest source must identify a pinned HTTPS URL or provide source.tar.gz and source_sha256")
	}
	bundle.license, err = readBundleFile(bundle.root, "LICENSE", 256<<10)
	if err != nil {
		return err
	}
	if !bytes.Contains(bundle.license, []byte("GNU AFFERO GENERAL PUBLIC LICENSE")) ||
		!bytes.Contains(bundle.license, []byte("Version 3, 19 November 2007")) ||
		!bytes.Contains(bundle.license, []byte("END OF TERMS AND CONDITIONS")) {
		return errors.New("bundle LICENSE must include the AGPL-3.0 text")
	}
	bundle.notice, err = readBundleFile(bundle.root, "NOTICE", 256<<10)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func exactHex(value string, length int) bool {
	return len(value) == length && hexRevision(value)
}

func pinnedRemoteDesktopSource(source, commit string) bool {
	if len(source) > 2048 {
		return false
	}
	location, err := url.Parse(source)
	if err != nil || location.Scheme != "https" || location.Hostname() == "" ||
		location.User != nil || location.RawQuery != "" || location.Fragment != "" {
		return false
	}
	for _, part := range strings.Split(location.Path, "/") {
		if strings.EqualFold(part, commit) || strings.EqualFold(part, commit+".tar.gz") || strings.EqualFold(part, commit+".zip") {
			return true
		}
	}
	return false
}

func openBundleFile(root *os.Root, name string, limit int64) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("bundle %s must be a regular file of at most %d bytes", name, limit)
	}
	return root.Open(name)
}

func readBundleFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := openBundleFile(root, name, limit)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("bundle %s exceeds %d bytes", name, limit)
	}
	return data, err
}

func remoteDesktopHelperName() string {
	if runtime.GOOS == "windows" {
		return "goim-rd.exe"
	}
	return "goim-rd"
}

func (bundle *remoteDesktopBundle) stage(staging, platformVersion string) error {
	name := remoteDesktopHelperName()
	var dir, app string
	switch runtime.GOOS {
	case "windows":
		dir = filepath.Join(staging, "remote")
	case "linux":
		dir = filepath.Join(staging, "lib", "goim", "remote")
	case "darwin":
		app = filepath.Join(staging, "GoIM.app", "Contents", "Helpers", "GoIMRemote.app")
		dir = filepath.Join(app, "Contents", "MacOS")
	default:
		return errors.New("remote desktop helper is unsupported on this build platform")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := bundle.copyHelper(name, dir); err != nil {
		return err
	}
	if app == "" {
		return nil
	}
	helperVersion := platformVersion
	if remoteDesktopStableVersion(bundle.manifest.Version) {
		helperVersion = bundle.manifest.Version
	}
	info := appbundle.Info{
		Name: "GoIM Remote Desktop", ID: "dev.goim.desktop.remote-desktop",
		Version: helperVersion, Executable: name, MinSystem: "13.0",
		Extra: []appbundle.KeyValue{{
			Key: "NSAudioCaptureUsageDescription", Value: "GoIM Remote Desktop captures system audio when you choose to share it.",
		}},
	}
	plist, err := info.Plist()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), plist, 0o644); err != nil {
		return err
	}
	if err := run("codesign", "--force", "--sign", "-", app); err != nil {
		return fmt.Errorf("sign remote desktop helper: %w", err)
	}
	return run("codesign", "--verify", "--strict", app)
}

func (bundle *remoteDesktopBundle) copyHelper(name, dir string) error {
	return bundle.stagePinnedFile(name, dir, bundle.manifest.SHA256, remoteHelperLimit, 0o755, verifyRemoteDesktopHelper)
}

func verifyRemoteDesktopHelper(path string) error {
	if err := verifyArchitecture(path); err != nil {
		return fmt.Errorf("verify remote desktop helper architecture: %w", err)
	}
	if runtime.GOOS == "linux" {
		_, versions, err := readLinuxDynamicRequirements(path)
		if err != nil {
			return err
		}
		// The helper uses desktop libraries separately from GoIM's pure-Go
		// process. Both still have the same supported GLIBC baseline.
		return verifyLinuxGLIBC(remoteDesktopHelperName(), versions)
	}
	return nil
}

func (bundle *remoteDesktopBundle) stagePinnedFile(name, dir, digest string, limit int64, mode os.FileMode, verify func(string) error) error {
	source, err := openBundleFile(bundle.root, name, limit)
	if err != nil {
		return err
	}
	defer source.Close()
	file, err := os.CreateTemp(dir, ".remote-desktop-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, limit+1))
	closeErr := file.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	if size > limit || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), digest) {
		return fmt.Errorf("bundle %s SHA-256 does not match the manifest", name)
	}
	if verify != nil {
		if err := verify(file.Name()); err != nil {
			return err
		}
	}
	if err := os.Chmod(file.Name(), mode); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, name))
}

func (bundle *remoteDesktopBundle) recordProvenance(staging string, info map[string]string) error {
	dir := filepath.Join(staging, "licenses", "rustdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	source := bundle.manifest.Source
	if bundle.manifest.SourceArchive != "" {
		if err := bundle.stagePinnedFile(remoteSourceArchive, dir, bundle.manifest.SourceSHA256, remoteSourceLimit, 0o644, verifyRemoteDesktopSourceArchive); err != nil {
			return fmt.Errorf("stage remote desktop corresponding source: %w", err)
		}
		source = "licenses/rustdesk/" + remoteSourceArchive
		info["remote_desktop_source_archive"] = source
	} else {
		info["remote_desktop_source"] = source
	}
	for file, data := range map[string][]byte{
		"manifest.json": bundle.manifestJSON, "LICENSE": bundle.license, "NOTICE": bundle.notice,
	} {
		if len(data) != 0 {
			if err := os.WriteFile(filepath.Join(dir, file), data, 0o644); err != nil {
				return err
			}
		}
	}
	description := "\n## Remote desktop helper\n\n" +
		"This package includes the RustDesk-derived remote desktop helper under AGPL-3.0. " +
		"The helper license and build manifest are provided in `licenses/rustdesk/`. " +
		"The source for the pinned helper commit is available at " + source + ".\n"
	notices, err := os.OpenFile(filepath.Join(staging, "THIRD_PARTY_NOTICES.md"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(notices, description)
	if err := errors.Join(writeErr, notices.Close()); err != nil {
		return err
	}
	if info["version"] == "" || remoteDesktopPreviewVersion(info["version"]) {
		info["edition"] = "remote-desktop-validation"
	}
	info["remote_desktop_protocol"] = strconv.Itoa(bundle.manifest.Protocol)
	info["remote_desktop_version"] = bundle.manifest.Version
	info["remote_desktop_commit"] = bundle.manifest.Commit
	info["remote_desktop_upstream_commit"] = rustDeskUpstream
	return nil
}

func verifyRemoteDesktopSourceArchive(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read source gzip: %w", err)
	}
	defer gz.Close()
	if _, err := tar.NewReader(gz).Next(); err != nil {
		return fmt.Errorf("read source tar entry: %w", err)
	}
	return nil
}
