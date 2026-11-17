package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func testPublicKey(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(public)
}

func TestReleaseNotesLoadsBoundedPlainTextFromGitHubAPI(t *testing.T) {
	notes := "## Changes\r\n\r\n- Fixed detached windows\n- Added update controls\x00"
	body, err := json.Marshal(map[string]string{"body": notes})
	if err != nil {
		t.Fatal(err)
	}
	transport := transportFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != ReleaseNotesURL("0.1.6") {
			return nil, fmt.Errorf("release notes request = %s", request.URL)
		}
		if request.Header.Get("Accept") != "application/vnd.github+json" ||
			request.Header.Get("User-Agent") != "GoIM updater" {
			return nil, fmt.Errorf("release notes headers = %v", request.Header)
		}
		return fixtureResponse(request, http.StatusOK, string(body), int64(len(body))), nil
	})
	client, err := newClient("0.1.5", testPublicKey(t), t.TempDir(),
		Target{OS: "windows", Arch: "amd64", Kind: WindowsSetup}, transport)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ReleaseNotes(context.Background(), Offer{Version: "0.1.6"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Fixed detached windows") || strings.ContainsRune(got, 0) {
		t.Fatalf("release notes = %q", got)
	}
}

func TestReleaseNotesRejectsOversizedResponses(t *testing.T) {
	transport := transportFunc(func(request *http.Request) (*http.Response, error) {
		return fixtureResponse(request, http.StatusOK,
			strings.Repeat("x", ReleaseNotesBytes+1), ReleaseNotesBytes+1), nil
	})
	client, err := newClient("0.1.5", testPublicKey(t), t.TempDir(),
		Target{OS: "windows", Arch: "amd64", Kind: WindowsSetup}, transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReleaseNotes(context.Background(), Offer{Version: "0.1.6"}); err == nil ||
		!strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized release notes error = %v", err)
	}
}

func TestReleaseNotesRejectsUntrustedRedirect(t *testing.T) {
	transport := transportFunc(func(request *http.Request) (*http.Response, error) {
		response := fixtureResponse(request, http.StatusFound, "", 0)
		response.Header.Set("Location", "https://example.com/release-notes.json")
		return response, nil
	})
	client, err := newClient("0.1.5", testPublicKey(t), t.TempDir(),
		Target{OS: "windows", Arch: "amd64", Kind: WindowsSetup}, transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReleaseNotes(context.Background(), Offer{Version: "0.1.6"}); err == nil ||
		!strings.Contains(err.Error(), "outside GitHub releases") {
		t.Fatalf("untrusted redirect error = %v", err)
	}
}

func TestTrustedUpdateHostRequiresHTTPSGitHub(t *testing.T) {
	for _, test := range []struct {
		url  string
		want bool
	}{
		{url: "https://api.github.com/repos/Ezreal-byte/GoIM/releases/tags/v0.1.6", want: true},
		{url: "https://github.com/Ezreal-byte/GoIM/releases", want: true},
		{url: "https://release-assets.githubusercontent.com/file", want: true},
		{url: "http://api.github.com/repos/Ezreal-byte/GoIM", want: false},
		{url: "https://example.com/release", want: false},
		{url: "https://github.com:444/Ezreal-byte/GoIM", want: false},
		{url: "https://user@github.com/Ezreal-byte/GoIM", want: false},
	} {
		parsed, err := url.Parse(test.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := trustedUpdateHost(parsed); got != test.want {
			t.Errorf("trustedUpdateHost(%q) = %v, want %v", test.url, got, test.want)
		}
	}
}
