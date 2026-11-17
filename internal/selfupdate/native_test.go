//go:build windows || darwin

package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/update"
)

const nativeOld = "0.1.4"
const nativeNew = "0.1.5"

type nativeFixture struct {
	Key, Feed, Signature, Artifact string
	Size                           int64
}

// TestNativeUpgrade uses real NSIS installers or signed app bundles. The
// payload is a small test entry point so both launches need no desktop input.
func TestNativeUpgrade(t *testing.T) {
	if os.Getenv("GOIM_NATIVE_UPGRADE_TEST") != "1" {
		t.Skip("enable on an isolated native packaging runner")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "fixture-probe"+nativeExeSuffix())
	args := []string{"test", "-c", "-o", binary}
	if runtime.GOOS == "windows" {
		args = append(args, "-ldflags=-H=windowsgui")
	}
	args = append(args, ".")
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	installed, artifact := prepareNativeUpgrade(t, root, binary)
	payload, err := os.Open(artifact)
	if err != nil {
		t.Fatal(err)
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, payload)
	_ = payload.Close()
	if err != nil {
		t.Fatal(err)
	}
	format, suffix := update.FormatZIP, ".zip"
	if runtime.GOOS == "windows" {
		format, suffix = update.FormatNSIS, "-Setup.exe"
	}
	feed := update.Feed{Schema: update.SchemaVersion, App: AppID, Channel: Channel,
		Latest: update.Release{Version: nativeNew, Assets: []update.Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Format: format, Size: size, SHA256: hex.EncodeToString(hasher.Sum(nil)),
			URL: AssetURL(nativeNew, runtime.GOOS, runtime.GOARCH, suffix),
		}}}}
	key, body, sig := signedFixture(t, feed)
	config, err := json.Marshal(nativeFixture{Key: key, Feed: body, Signature: sig, Artifact: artifact, Size: size})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{"fixture.json": config, "user.data": []byte("existing GoIM user data")} {
		if err := os.WriteFile(filepath.Join(root, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, installed, "-test.run=^TestNativeUpgradeChild$", "--", root).CombinedOutput(); err != nil {
		detail, _ := os.ReadFile(filepath.Join(root, "child.error"))
		t.Fatalf("old version did not hand off: %v\n%s\n%s", err, output, detail)
	}
	for {
		if result, err := os.ReadFile(filepath.Join(root, "restarted")); err == nil {
			if string(result) != nativeNew {
				t.Fatalf("restart still runs %q", result)
			}
			break
		}
		if failure, err := os.ReadFile(filepath.Join(root, "child.error")); err == nil {
			t.Fatalf("restarted fixture: %s", failure)
		}
		select {
		case <-ctx.Done():
			t.Fatal("the installer did not restart the new version with the original arguments")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "user.data")); err != nil || string(data) != "existing GoIM user data" {
		t.Fatal("unrelated user data changed")
	}
}

func TestNativeUpgradeChild(t *testing.T) {
	if flag.NArg() != 1 {
		t.Skip("child process only")
	}
	root := flag.Arg(0)
	fail := func(err error) {
		_ = os.WriteFile(filepath.Join(root, "child.error"), []byte(err.Error()), 0600)
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "fixture.json"))
	if err != nil {
		t.Skip("not a native upgrade fixture")
	}
	var fixture nativeFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		fail(err)
	}
	exe, err := os.Executable()
	if err != nil {
		fail(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		fail(err)
	}
	dir := filepath.Dir(exe)
	markerPath := filepath.Join(dir, "fixture-version")
	if runtime.GOOS == "darwin" {
		// macOS validates every code object under Contents/MacOS. Keep fixture
		// metadata in Resources so only the executable helper needs a signature.
		markerPath = filepath.Join(dir, "..", "Resources", "fixture-version")
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		fail(err)
	}
	version := string(marker)
	snipPath := filepath.Join(dir, "goim-snip"+nativeExeSuffix())
	snip, err := os.ReadFile(snipPath)
	if err != nil {
		fail(fmt.Errorf("screenshot companion is missing: %w", err))
	}
	if runtime.GOOS == "darwin" {
		info, err := os.Stat(snipPath)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			fail(fmt.Errorf("screenshot companion does not match %s", version))
		}
	} else if string(snip) != "companion "+version {
		fail(fmt.Errorf("screenshot companion does not match %s", version))
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: detectInstall(exe)}
	if !target.automatic() {
		fail(fmt.Errorf("native fixture installation type %d is not eligible", target.Kind))
	}
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case FeedURL:
			return fixtureResponse(req, 200, fixture.Feed, int64(len(fixture.Feed))), nil
		case FeedURL + ".sig":
			return fixtureResponse(req, 200, fixture.Signature, int64(len(fixture.Signature))), nil
		default:
			if !strings.HasPrefix(req.URL.String(), Releases+"/download/v"+nativeNew+"/") {
				return nil, fmt.Errorf("unexpected fixture request %s", req.URL)
			}
			b, err := os.Open(fixture.Artifact)
			return &http.Response{Request: req, StatusCode: 200, Header: make(http.Header), Body: b, ContentLength: fixture.Size}, err
		}
	})
	client, err := newClient(version, fixture.Key, filepath.Join(root, "cache"), target, transport)
	if err != nil {
		fail(err)
	}
	if version == nativeNew {
		if previous, ok := client.PreviousInstall(); !ok || previous != nativeNew {
			fail(fmt.Errorf("new version did not recognize the completed update"))
		}
		if err := os.WriteFile(filepath.Join(root, "restarted"), marker, 0600); err != nil {
			fail(err)
		}
		return
	}
	if version != nativeOld {
		fail(fmt.Errorf("unexpected installed version %s", version))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	offer, err := client.Check(ctx)
	if err != nil {
		fail(err)
	}
	asset, err := client.Download(ctx, offer, nil)
	if err != nil {
		fail(err)
	}
	// Let the test entry point exit normally after Viva hands off. The same
	// arguments must reach the executable the helper starts after replacement.
	quit := false
	if err := client.Install(offer, asset, os.Args[1:], func() { quit = true }); err != nil {
		fail(err)
	}
	if !quit {
		fail(fmt.Errorf("installer was not handed off"))
	}
}

func nativeCommand(t *testing.T, command *exec.Cmd) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	// The caller already set any platform-specific process parameters.
	done := make(chan error, 1)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		_ = command.Process.Kill()
		<-done
		t.Fatalf("native fixture command timed out: %s", command.Path)
	}
}
