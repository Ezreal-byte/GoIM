package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/update"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func fixtureFeed(data string) update.Feed {
	sum := sha256.Sum256([]byte(data))
	return update.Feed{Schema: update.SchemaVersion, App: AppID, Channel: Channel,
		Latest: update.Release{Version: "0.1.5", Assets: []update.Asset{
			{OS: "windows", Arch: "amd64", Format: update.FormatNSIS, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
				URL: AssetURL("0.1.5", "windows", "amd64", "-Setup.exe")},
			{OS: "darwin", Arch: "arm64", Format: update.FormatZIP, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
				URL: AssetURL("0.1.5", "darwin", "arm64", ".zip")},
		}}}
}

func signedFixture(t *testing.T, f update.Feed) (string, string, string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := update.Sign(body, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub), string(body), signature
}

func fixtureResponse(req *http.Request, status int, body string, declared int64) *http.Response {
	return &http.Response{Request: req, StatusCode: status, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: declared}
}

type cutConnection struct{ io.Reader }

func (r cutConnection) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func TestCheckAndVerifiedDownload(t *testing.T) {
	data := strings.Repeat("goim native setup fixture\n", 4096)
	feed := fixtureFeed(data)
	key, body, signature := signedFixture(t, feed)
	deliveries := 0
	mode := "resume"
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case latestReleaseURL:
			return releaseRedirectFixture(req, "0.1.5"), nil
		case Releases + "/download/v0.1.5/" + FeedName:
			return fixtureResponse(req, 200, body, int64(len(body))), nil
		case Releases + "/download/v0.1.5/" + FeedName + ".sig":
			return fixtureResponse(req, 200, signature, int64(len(signature))), nil
		case feed.Latest.Assets[0].URL:
			if mode == "corrupt" {
				return fixtureResponse(req, 200, strings.Repeat("x", len(data)), int64(len(data))), nil
			}
			deliveries++
			part := len(data) / 2
			if deliveries == 1 {
				r := fixtureResponse(req, 200, data[:part], int64(len(data)))
				r.Body = io.NopCloser(cutConnection{strings.NewReader(data[:part])})
				return r, nil
			}
			if req.Header.Get("Range") != fmt.Sprintf("bytes=%d-", part) {
				return nil, fmt.Errorf("resume range = %q", req.Header.Get("Range"))
			}
			response := fixtureResponse(req, http.StatusPartialContent, data[part:], int64(len(data)-part))
			response.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", part, len(data)-1, len(data)))
			return response, nil
		default:
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		}
	})
	ctx := context.Background()
	target := Target{"windows", "amd64", WindowsSetup}
	client, err := newClient("0.1.4", key, t.TempDir(), target, transport)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := client.Check(ctx)
	if err != nil || !offer.Automatic || offer.Version != "0.1.5" {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
	if _, err := client.Download(ctx, offer, nil); !errors.Is(err, update.ErrInterrupted) {
		t.Fatalf("interruption = %v", err)
	}
	path, err := client.Download(ctx, offer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != data {
		t.Fatal("downloaded bytes differ after resume")
	}
	mode = "corrupt"
	broken, err := newClient("0.1.4", key, t.TempDir(), target, transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Download(ctx, offer, nil); !errors.Is(err, update.ErrChecksum) {
		t.Fatalf("corrupt download = %v", err)
	}
	body += " "
	if _, err := client.Check(ctx); !errors.Is(err, update.ErrSignature) {
		t.Fatalf("modified signed bytes = %v", err)
	}
}

func TestUpgradeUsesOnlyItsInstallationType(t *testing.T) {
	feed := fixtureFeed("abc")
	for _, test := range []struct {
		target Target
		suffix string
		auto   bool
	}{
		{Target{"windows", "amd64", WindowsSetup}, "-Setup.exe", true},
		{Target{"windows", "arm64", WindowsSetup}, "-Setup.exe", false},
		{Target{"windows", "amd64", WindowsMSI}, ".msi", false},
		{Target{"windows", "amd64", WindowsZIP}, ".zip", false},
		{Target{"darwin", "arm64", MacApp}, ".dmg", true},
		{Target{"darwin", "arm64", MacDownload}, ".dmg", false},
		{Target{"linux", "amd64", LinuxDeb}, ".deb", false},
		{Target{"linux", "arm64", LinuxTar}, ".tar.gz", false},
		{Target{"linux", "arm64", Unknown}, "", false},
	} {
		offer, err := selectOffer(&feed, test.target)
		want := ReleaseURL("0.1.5")
		if test.suffix != "" {
			want = AssetURL("0.1.5", test.target.OS, test.target.Arch, test.suffix)
		}
		if err != nil || offer.Automatic != test.auto || offer.DownloadURL != want {
			t.Fatalf("%+v: offer=%+v err=%v", test.target, offer, err)
		}
	}
	feed.Latest.Assets[0].URL = "https://example.test/installer.exe"
	if _, err := selectOffer(&feed, Target{"windows", "amd64", WindowsSetup}); err == nil {
		t.Fatal("a signed but unrelated installer URL was accepted")
	}
}

func TestInstallResultUsesRunningVersion(t *testing.T) {
	for _, version := range []string{"0.1.4", "0.1.5"} {
		client := &Client{cache: t.TempDir(), version: version}
		if err := os.WriteFile(filepath.Join(client.cache, "installing"), []byte("0.1.5"), 0600); err != nil {
			t.Fatal(err)
		}
		target, ok := client.PreviousInstall()
		if target != "0.1.5" || ok != (version == target) {
			t.Fatalf("running %s: target %s success %v", version, target, ok)
		}
		if target, _ := client.PreviousInstall(); target != "" {
			t.Fatal("restart marker was reported twice")
		}
	}
}
