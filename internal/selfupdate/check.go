package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vivaui/viva/update"
)

const (
	latestReleaseURL = Releases + "/latest"
	latestAPIURL     = "https://api.github.com/repos/Ezreal-byte/GoIM/releases/latest"
	lookupTimeout    = 4 * time.Second
	checkTimeout     = 20 * time.Second
)

var (
	ErrUpdateSource = errors.New("unrecognized GoIM update source")
	ErrUpdateRate   = errors.New("GitHub is limiting update checks")
	ErrUpdateHTTP   = errors.New("GitHub update data is unavailable")
)

func (c *Client) Check(ctx context.Context) (Offer, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	version, lookupErr := c.latestRelease(ctx)
	feedURL := FeedURL
	if lookupErr == nil {
		order, err := update.Compare(version, c.version)
		if err != nil || order <= 0 {
			return Offer{}, err
		}
		// Fetch both files from the same immutable tag. During publication the
		// two /latest/download aliases can point to different releases.
		feedURL = Releases + "/download/v" + version + "/" + FeedName
	}

	httpClient := *c.notesClient
	httpClient.Transport = checkTransport{httpClient.Transport}
	verifier, err := c.signedClient(feedURL, &httpClient)
	if err != nil {
		return Offer{}, err
	}
	feed, err := verifier.Check(ctx)
	if err != nil {
		if lookupErr == nil && !verificationFailed(err) && !errors.Is(ctx.Err(), context.Canceled) {
			// GitHub's public release metadata can link to the official release
			// page, but only a verified feed can authorize automatic installation.
			offer := manualOffer(version, c.target)
			offer.DownloadURL, offer.FeedUnavailable = offer.NotesURL, true
			return offer, nil
		}
		return Offer{}, errors.Join(lookupErr, err)
	}
	if lookupErr == nil && (feed == nil || feed.Latest.Version != version) {
		return Offer{}, fmt.Errorf("%w: signed feed does not match release v%s", update.ErrManifest, version)
	}
	if feed == nil {
		return Offer{}, nil
	}
	offer, err := selectOffer(feed, c.target)
	if err == nil && c.target.OS == "linux" && c.target.Kind == LinuxDeb &&
		(c.target.Arch == "amd64" || c.target.Arch == "arm64") {
		return c.debOffer(ctx, offer)
	}
	return offer, err
}

func verificationFailed(err error) bool {
	return errors.Is(err, update.ErrSignature) || errors.Is(err, update.ErrManifest) ||
		errors.Is(err, update.ErrInsecureURL) || errors.Is(err, update.ErrTooLarge) || errors.Is(err, ErrUpdateSource)
}

func (c *Client) latestRelease(ctx context.Context) (string, error) {
	redirectCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	version, redirectErr := c.releaseRedirect(redirectCtx)
	cancel()
	if redirectErr == nil {
		return version, nil
	}
	apiCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	version, err := c.releaseFromAPI(apiCtx)
	if err != nil {
		return "", errors.Join(redirectErr, err)
	}
	return version, nil
}

func (c *Client) releaseRedirect(ctx context.Context) (string, error) {
	client := *c.notesClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "GoIM updater")
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 300 || response.StatusCode >= 400 {
		return "", updateHTTPError(response)
	}
	location, err := response.Location()
	if err == nil && trustedUpdateHost(location) {
		if tag, found := strings.CutPrefix(location.String(), Releases+"/tag/"); found {
			if version, ok := StableVersion(tag); ok && tag == "v"+version {
				return version, nil
			}
		}
	}
	return "", ErrUpdateSource
}

func (c *Client) releaseFromAPI(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GoIM updater")
	response, err := c.notesClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", updateHTTPError(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, ReleaseNotesBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > ReleaseNotesBytes {
		return "", update.ErrTooLarge
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpdateSource, err)
	}
	version, ok := StableVersion(release.Tag)
	if !ok || release.Tag != "v"+version || release.Draft || release.Prerelease {
		return "", ErrUpdateSource
	}
	return version, nil
}

type checkTransport struct{ http.RoundTripper }

func (t checkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.RoundTripper.RoundTrip(req)
	if err != nil || response.StatusCode < 400 {
		return response, err
	}
	_ = response.Body.Close()
	return nil, updateHTTPError(response)
}

func updateHTTPError(response *http.Response) error {
	if response.StatusCode == http.StatusTooManyRequests ||
		(response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0") {
		return fmt.Errorf("%w: HTTP %d", ErrUpdateRate, response.StatusCode)
	}
	return fmt.Errorf("%w: HTTP %d", ErrUpdateHTTP, response.StatusCode)
}
