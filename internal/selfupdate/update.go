// Package selfupdate is GoIM's release policy. Viva owns verification,
// transport, downloads and native installation.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/update"
	"goim/internal/buildinfo"
)

const (
	AppID    = "dev.goim.desktop"
	Channel  = "stable"
	Releases = buildinfo.RepositoryURL + "/releases"
	FeedName = "goim-update.json"
	FeedURL  = Releases + "/latest/download/" + FeedName
	// ReleaseNotesBytes bounds untrusted release-note text before it reaches
	// the UI. Notes are plain text from GitHub's JSON API.
	ReleaseNotesBytes = 512 << 10
)

var stableVersion = regexp.MustCompile("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")

func StableVersion(value string) (string, bool) {
	version := strings.TrimPrefix(strings.TrimSpace(value), "v")
	return version, len(version) <= 32 && stableVersion.MatchString(version)
}

type Kind uint8

const (
	Unknown Kind = iota
	WindowsSetup
	WindowsMSI
	WindowsZIP
	MacApp
	MacDownload
	LinuxDeb
	LinuxTar
)

type Target struct {
	OS, Arch string
	Kind     Kind
}

func (t Target) automatic() bool {
	return t.OS == "windows" && t.Kind == WindowsSetup || t.OS == "darwin" && t.Kind == MacApp
}

func ReleaseURL(version string) string { return Releases + "/tag/v" + version }

func ReleaseNotesURL(version string) string {
	return "https://api.github.com/repos/Ezreal-byte/GoIM/releases/tags/v" + version
}

func AssetName(version, goos, arch, suffix string) string {
	return "GoIM_" + version + "_" + goos + "_" + arch + suffix
}

func AssetURL(version, goos, arch, suffix string) string {
	return Releases + "/download/v" + version + "/" + AssetName(version, goos, arch, suffix)
}

func (t Target) downloadSuffix() string {
	switch t.Kind {
	case WindowsSetup:
		return "-Setup.exe"
	case WindowsMSI:
		return ".msi"
	case WindowsZIP:
		return ".zip"
	case MacApp, MacDownload:
		return ".dmg"
	case LinuxDeb:
		return ".deb"
	case LinuxTar:
		return ".tar.gz"
	default:
		return ""
	}
}

// Offer was obtained from a verified GoIM stable feed.
type Offer struct {
	Version, NotesURL, DownloadURL string
	Bytes                          int64
	Automatic                      bool
	asset                          update.Asset
}

func selectOffer(feed *update.Feed, target Target) (Offer, error) {
	version, ok := StableVersion(feed.Latest.Version)
	if !ok || version != feed.Latest.Version {
		return Offer{}, errors.New("update release is not a stable GoIM version")
	}
	offer := Offer{Version: version, NotesURL: ReleaseURL(version), DownloadURL: ReleaseURL(version)}
	if (target.Arch == "amd64" || target.Arch == "arm64") && target.downloadSuffix() != "" {
		offer.DownloadURL = AssetURL(version, target.OS, target.Arch, target.downloadSuffix())
	}
	if !target.automatic() {
		return offer, nil
	}
	format, suffix := update.FormatZIP, ".zip"
	if target.OS == "windows" {
		format, suffix = update.FormatNSIS, "-Setup.exe"
	}
	for _, asset := range feed.Latest.Assets {
		if asset.OS == target.OS && asset.Arch == target.Arch && asset.Format == format {
			if asset.URL != AssetURL(version, target.OS, target.Arch, suffix) {
				return Offer{}, errors.New("update asset is outside the expected GoIM release")
			}
			offer.asset, offer.Automatic, offer.Bytes = asset, true, asset.Size
			break
		}
	}
	return offer, nil
}

type Client struct {
	client      *update.Client
	notesClient *http.Client
	target      Target
	cache       string
	version     string
}

// Open constructs the updater for this installation. An unconfigured build
// does not make network requests or run an installer.
func Open(version, publicKey string) (*Client, error) {
	if publicKey == "" {
		return nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(resolved))
	cache = filepath.Join(cache, "GoIM", "updates", fmt.Sprintf("%x", sum[:12]))
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: detectInstall(resolved)}
	return newClient(version, publicKey, cache, target, nil)
}

func newClient(version, publicKey, cache string, target Target, transport http.RoundTripper) (*Client, error) {
	version, ok := StableVersion(version)
	if !ok {
		return nil, errors.New("this build does not report a stable version")
	}
	key, err := update.ParsePublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !trustedUpdateHost(req.URL) {
			return errors.New("update redirect is outside GitHub releases")
		}
		return nil
	}}
	client, err := update.New(update.Config{FeedURL: FeedURL, App: AppID, Channel: Channel, Current: version,
		PublicKeys: []ed25519.PublicKey{key}, HTTP: httpClient, MaxAssetBytes: 512 << 20})
	if err != nil {
		return nil, err
	}
	return &Client{client: client, notesClient: httpClient, target: target, cache: cache, version: version}, nil
}

func trustedUpdateHost(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com",
		"github-releases.githubusercontent.com", "api.github.com":
		return true
	default:
		return false
	}
}

func (c *Client) Check(ctx context.Context) (Offer, error) {
	feed, err := c.client.Check(ctx)
	if err != nil || feed == nil {
		return Offer{}, err
	}
	return selectOffer(feed, c.target)
}

// ReleaseNotes returns plain-text release notes. It never interprets markup or
// HTML, caps the response body, and only follows HTTPS GitHub redirects.
func (c *Client) ReleaseNotes(ctx context.Context, offer Offer) (string, error) {
	version, ok := StableVersion(offer.Version)
	if !ok || version != offer.Version || c == nil || c.notesClient == nil {
		return "", errors.New("invalid release notes request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleaseNotesURL(version), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "GoIM updater")
	response, err := c.notesClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release notes returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, ReleaseNotesBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > ReleaseNotesBytes {
		return "", errors.New("release notes are too large")
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	notes := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(payload.Body))
	if utf8.RuneCountInString(notes) > 32_000 {
		notes = string([]rune(notes)[:32_000]) + "…"
	}
	return notes, nil
}

func (c *Client) Download(ctx context.Context, offer Offer, progress func(done, total int64)) (string, error) {
	if !offer.Automatic || offer.asset.URL == "" {
		return "", update.ErrManagedExternally
	}
	return c.client.Download(ctx, offer.asset, filepath.Join(c.cache, offer.Version), progress)
}

func (c *Client) Install(offer Offer, artifact string, args []string, quit func()) error {
	if !offer.Automatic {
		return update.ErrManagedExternally
	}
	// A moved bundle or changed installer registration must not cause the
	// helper to replace a different copy.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil || detectInstall(exe) != c.target.Kind {
		return update.ErrNotWritable
	}
	pending := filepath.Join(c.cache, "installing")
	if err := os.WriteFile(pending, []byte(offer.Version), 0600); err != nil {
		return err
	}
	err = update.Install(update.InstallOptions{
		Artifact: artifact, Asset: offer.asset, Version: offer.Version, App: AppID, Args: args,
	}, quit)
	if err != nil {
		_ = os.Remove(pending)
	}
	return err
}

// PreviousInstall consumes the restart marker. The running version, rather
// than the fact that a helper was started, determines success.
func (c *Client) PreviousInstall() (version string, succeeded bool) {
	path := filepath.Join(c.cache, "installing")
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	var data [40]byte
	n, _ := f.Read(data[:])
	_ = f.Close()
	_ = os.Remove(path)
	version, ok := StableVersion(string(data[:n]))
	if !ok {
		return "", false
	}
	return version, c.version == version
}
