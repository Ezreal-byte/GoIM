package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/vivaui/viva/update"
)

func releaseRedirectFixture(req *http.Request, version string) *http.Response {
	response := fixtureResponse(req, http.StatusFound, "", 0)
	response.Header.Set("Location", ReleaseURL(version))
	return response
}

func TestCheckCurrentVersionDoesNotNeedReleaseAssets(t *testing.T) {
	client, err := newClient("0.1.5", testPublicKey(t), t.TempDir(), Target{}, transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != latestReleaseURL {
			t.Fatalf("unnecessary request to %s", req.URL)
		}
		return releaseRedirectFixture(req, "0.1.5"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if offer, err := client.Check(context.Background()); err != nil || offer.Version != "" {
		t.Fatalf("current version outcome = %+v, %v", offer, err)
	}
}

func TestCheckUsesReleaseAPIWhenRedirectIsUnavailable(t *testing.T) {
	key, body, _ := signedFixture(t, fixtureFeed("installer data"))
	const metadata = `{"tag_name":"v0.1.5","draft":false,"prerelease":false}`
	const pinned = Releases + "/download/v0.1.5/" + FeedName
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case latestReleaseURL:
			return fixtureResponse(req, http.StatusServiceUnavailable, "", 0), nil
		case latestAPIURL:
			return fixtureResponse(req, 200, metadata, int64(len(metadata))), nil
		case pinned:
			return fixtureResponse(req, 200, body, int64(len(body))), nil
		case pinned + ".sig":
			return fixtureResponse(req, http.StatusNotFound, "", 0), nil
		default:
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		}
	})
	client, err := newClient("0.1.4", key, t.TempDir(), Target{"darwin", "arm64", MacApp}, transport)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := client.Check(context.Background())
	if err != nil || offer.Version != "0.1.5" || offer.Automatic || !offer.FeedUnavailable || offer.DownloadURL != ReleaseURL("0.1.5") {
		t.Fatalf("manual release fallback = %+v, %v", offer, err)
	}
	if _, err := client.Download(context.Background(), offer, nil); !errors.Is(err, update.ErrManagedExternally) {
		t.Fatalf("unverified download was available in-app: %v", err)
	}
}

func TestCheckRejectsSignedFeedForAnotherTag(t *testing.T) {
	key, body, signature := signedFixture(t, fixtureFeed("installer data"))
	const pinned = Releases + "/download/v0.1.6/" + FeedName
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case latestReleaseURL:
			return releaseRedirectFixture(req, "0.1.6"), nil
		case pinned:
			return fixtureResponse(req, 200, body, int64(len(body))), nil
		case pinned + ".sig":
			return fixtureResponse(req, 200, signature, int64(len(signature))), nil
		default:
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		}
	})
	client, err := newClient("0.1.4", key, t.TempDir(), Target{}, transport)
	if err != nil {
		t.Fatal(err)
	}
	if offer, err := client.Check(context.Background()); offer.Version != "" || !errors.Is(err, update.ErrManifest) {
		t.Fatalf("signed but mismatched release outcome = %+v, %v", offer, err)
	}
}

func TestCheckKeepsSignedEndpointWhenReleaseLookupsFail(t *testing.T) {
	key, body, signature := signedFixture(t, fixtureFeed("installer data"))
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case latestReleaseURL, latestAPIURL:
			return fixtureResponse(req, http.StatusServiceUnavailable, "", 0), nil
		case FeedURL:
			return fixtureResponse(req, 200, body, int64(len(body))), nil
		case FeedURL + ".sig":
			return fixtureResponse(req, 200, signature, int64(len(signature))), nil
		default:
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		}
	})
	client, err := newClient("0.1.4", key, t.TempDir(), Target{"windows", "amd64", WindowsSetup}, transport)
	if err != nil {
		t.Fatal(err)
	}
	if offer, err := client.Check(context.Background()); err != nil || offer.Version != "0.1.5" || !offer.Automatic {
		t.Fatalf("verified legacy fallback = %+v, %v", offer, err)
	}
}
