package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/update"
	"goim/internal/debupdate"
	"goim/internal/selfupdate"
)

func TestSignVerifiedReleaseMatrix(t *testing.T) {
	root := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var sums strings.Builder
	for goos, formats := range map[string][]string{
		"windows": {"-Setup.exe", ".zip"}, "darwin": {".zip"}, "linux": {".tar.gz", ".deb"},
	} {
		for _, arch := range []string{"amd64", "arm64"} {
			for _, format := range formats {
				name := selfupdate.AssetName("0.1.5", goos, arch, format)
				path := filepath.Join(root, name)
				if err := os.WriteFile(path, []byte(name), 0600); err != nil {
					t.Fatal(err)
				}
				_, sha, err := packageDigest(path)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&sums, "%s  %s\n", sha, name)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(sums.String()), 0600); err != nil {
		t.Fatal(err)
	}
	public, secret := base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv)
	now := time.Now()
	if err := signFeed(root, "v0.1.5", public, secret, now); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, selfupdate.FeedName))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(root, selfupdate.FeedName+".sig"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(pub, doc, raw) {
		t.Fatal("published feed is not signed by the packaged key")
	}
	var feed update.Feed
	if err := json.Unmarshal(doc, &feed); err != nil || feed.App != selfupdate.AppID || feed.Channel != selfupdate.Channel ||
		feed.Latest.Version != "0.1.5" || len(feed.Latest.Assets) != 8 {
		t.Fatalf("feed = %+v, %v", feed, err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		b, err := os.ReadFile(filepath.Join(root, debupdate.MetadataName(arch)))
		if err != nil {
			t.Fatal(err)
		}
		var p debupdate.Package
		if json.Unmarshal(b, &p) != nil || p.Verify(pub, feed.Latest.Version, arch) != nil {
			t.Fatalf("invalid %s receipt: %s", arch, b)
		}
	}
	badDeb := filepath.Join(root, selfupdate.AssetName("0.1.5", "linux", "amd64", ".deb"))
	if err := os.WriteFile(badDeb, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := signFeed(root, "v0.1.5", public, secret, now); err == nil {
		t.Fatal("an altered DEB was signed")
	}
	if err := os.WriteFile(badDeb, []byte(filepath.Base(badDeb)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, selfupdate.AssetName("0.1.5", "windows", "amd64", "-Setup.exe")), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := signFeed(root, "v0.1.5", public, secret, now); err == nil {
		t.Fatal("an altered installer was signed")
	}
}
