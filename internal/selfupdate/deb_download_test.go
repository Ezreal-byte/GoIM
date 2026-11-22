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
	"strings"
	"testing"

	"github.com/vivaui/viva/update"
	"goim/internal/debupdate"
)

func TestDebSignedOfferResumeAndRetry(t *testing.T) {
	const data = "signed deb fixture"
	const version = "0.1.5"
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(data))
	p := debupdate.Package{Version: version, Arch: "amd64", Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
	p.Signature = ed25519.Sign(priv, p.SignedBytes())
	describe := func(p debupdate.Package) string {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	key := base64.StdEncoding.EncodeToString(pub)
	feed, err := json.Marshal(fixtureFeed(data))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := update.Sign(feed, priv)
	if err != nil {
		t.Fatal(err)
	}
	base := Releases + "/download/v" + version + "/"
	description := describe(p)
	deliveries := 0
	metadataStatus := http.StatusOK
	c, err := newClient("0.1.4", key, t.TempDir(), Target{"linux", "amd64", LinuxDeb}, transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case latestReleaseURL:
			return releaseRedirectFixture(req, version), nil
		case base + FeedName:
			return fixtureResponse(req, 200, string(feed), int64(len(feed))), nil
		case base + FeedName + ".sig":
			return fixtureResponse(req, 200, sig, int64(len(sig))), nil
		case base + debupdate.MetadataName("amd64"):
			return fixtureResponse(req, metadataStatus, description, int64(len(description))), nil
		case AssetURL(version, "linux", "amd64", ".deb"):
			deliveries++
			switch deliveries {
			case 1:
				r := fixtureResponse(req, 200, "", int64(len(data)))
				r.Body = io.NopCloser(cutConnection{strings.NewReader(data[:6])})
				return r, nil
			case 2:
				if req.Header.Get("Range") != "bytes=6-" {
					t.Fatalf("range %q", req.Header.Get("Range"))
				}
				r := fixtureResponse(req, 206, data[6:], int64(len(data)-6))
				r.Header.Set("Content-Range", fmt.Sprintf("bytes 6-%d/%d", len(data)-1, len(data)))
				return r, nil
			default:
				return fixtureResponse(req, 200, strings.Repeat("x", len(data)), int64(len(data))), nil
			}
		}
		return nil, fmt.Errorf("unexpected request %s", req.URL)
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	offer, err := c.Check(ctx)
	if err != nil || !offer.Automatic || offer.Bytes != p.Size {
		t.Fatalf("offer %+v: %v", offer, err)
	}
	if _, err = c.Download(ctx, offer, nil); !errors.Is(err, update.ErrInterrupted) {
		t.Fatal(err)
	}
	path, err := c.Download(ctx, offer, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != data {
		t.Fatalf("bytes %q: %v", b, err)
	}
	if _, err = c.Download(ctx, offer, nil); err != nil || deliveries != 2 {
		t.Fatalf("cache hit: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Download(ctx, offer, nil); !errors.Is(err, update.ErrChecksum) {
		t.Fatalf("corrupt package: %v", err)
	}
	for _, mutate := range []func(*debupdate.Package){
		func(v *debupdate.Package) { v.Arch = "arm64"; v.Signature = ed25519.Sign(priv, v.SignedBytes()) },
		func(v *debupdate.Package) { v.Version = "0.1.6"; v.Signature = ed25519.Sign(priv, v.SignedBytes()) },
		func(v *debupdate.Package) { v.Size++ },
	} {
		altered := p
		mutate(&altered)
		description = describe(altered)
		if _, err := c.Check(ctx); !errors.Is(err, update.ErrSignature) {
			t.Fatalf("altered receipt: %v", err)
		}
	}
	metadataStatus = 404
	offer, err = c.Check(ctx)
	if err != nil || offer.Automatic || offer.DownloadURL != AssetURL(version, "linux", "amd64", ".deb") {
		t.Fatalf("old feed fallback: %+v, %v", offer, err)
	}
}
