// Command update-feed signs the stable update metadata for a verified build.
package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/update"
	"goim/internal/selfupdate"
)

func main() {
	root := flag.String("dir", "release", "directory containing the verified release packages and SHA256SUMS")
	tag := flag.String("tag", "", "stable tag, such as v0.1.4")
	flag.Parse()
	if err := signFeed(*root, *tag, os.Getenv("GOIM_UPDATE_PUBLIC_KEY"), os.Getenv("GOIM_UPDATE_PRIVATE_KEY"), time.Now()); err != nil {
		log.Fatal(err)
	}
}

func signFeed(root, tag, public, secret string, at time.Time) error {
	version, ok := selfupdate.StableVersion(tag)
	if !ok || tag != "v"+version {
		return fmt.Errorf("update feed needs a stable vMAJOR.MINOR.PATCH tag")
	}
	pub, err := update.ParsePublicKey(public)
	if err != nil {
		return err
	}
	key, err := update.ParsePrivateKey(secret)
	if err != nil {
		return err
	}
	derived := ed25519.NewKeyFromSeed(key.Seed())
	if !bytes.Equal(key, derived) || !bytes.Equal(pub, derived.Public().(ed25519.PublicKey)) {
		return fmt.Errorf("GoIM release signing key does not match the configured public key")
	}
	sums, err := readChecksums(filepath.Join(root, "SHA256SUMS"))
	if err != nil {
		return err
	}
	feed := update.Feed{
		Schema: update.SchemaVersion, App: selfupdate.AppID, Channel: selfupdate.Channel,
		Latest: update.Release{Version: version, Date: at.UTC(), NotesURL: selfupdate.ReleaseURL(version)},
	}
	formats := [...]struct{ os, format, suffix string }{
		{"windows", update.FormatNSIS, "-Setup.exe"}, {"windows", update.FormatZIP, ".zip"},
		{"darwin", update.FormatZIP, ".zip"}, {"linux", update.FormatTarGz, ".tar.gz"},
	}
	for _, item := range formats {
		for _, arch := range []string{"amd64", "arm64"} {
			name := selfupdate.AssetName(version, item.os, arch, item.suffix)
			size, digest, err := packageDigest(filepath.Join(root, name))
			if err != nil {
				return err
			}
			if size <= 0 || size > 512<<20 || sums[name] != digest {
				return fmt.Errorf("%s differs from the verified release checksums", name)
			}
			feed.Latest.Assets = append(feed.Latest.Assets, update.Asset{OS: item.os, Arch: arch,
				Format: item.format, URL: selfupdate.AssetURL(version, item.os, arch, item.suffix), Size: size, SHA256: digest})
		}
	}
	body, err := json.MarshalIndent(feed, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	signature, err := update.Sign(body, key)
	if err != nil {
		return err
	}
	path := filepath.Join(root, selfupdate.FeedName)
	if err := os.WriteFile(path+".sig", []byte(signature+"\n"), 0644); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0644)
}

func readChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	sums := make(map[string]string)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) != 2 || len(fields[0]) != 64 || sums[fields[1]] != "" {
			return nil, fmt.Errorf("invalid SHA256SUMS entry")
		}
		sums[fields[1]] = fields[0]
	}
	return sums, scan.Err()
}

func packageDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	sum := sha256.New()
	size, err := io.Copy(sum, io.LimitReader(file, (512<<20)+1))
	return size, hex.EncodeToString(sum.Sum(nil)), err
}
