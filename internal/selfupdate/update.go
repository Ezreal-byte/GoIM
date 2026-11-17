// Package selfupdate is GoIM's release policy. Viva owns verification,
// transport, downloads and native installation.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/vivaui/viva/update"
	"goim/internal/buildinfo"
)

const (
	AppID    = "dev.goim.desktop"
	Channel  = "stable"
	Releases = buildinfo.RepositoryURL + "/releases"
	FeedName = "goim-update.json"
	FeedURL  = Releases + "/latest/download/" + FeedName
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
	client  *update.Client
	target  Target
	cache   string
	version string
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
		if len(via) >= 10 || !releaseHost(req.URL) {
			return errors.New("update redirect is outside GitHub releases")
		}
		return nil
	}}
	client, err := update.New(update.Config{FeedURL: FeedURL, App: AppID, Channel: Channel, Current: version,
		PublicKeys: []ed25519.PublicKey{key}, HTTP: httpClient, MaxAssetBytes: 512 << 20})
	if err != nil {
		return nil, err
	}
	return &Client{client: client, target: target, cache: cache, version: version}, nil
}

func releaseHost(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com",
		"github-releases.githubusercontent.com":
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
