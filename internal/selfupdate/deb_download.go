package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/vivaui/viva/update"
	"goim/internal/debupdate"
)

func (c *Client) debOffer(ctx context.Context, offer Offer) (Offer, error) {
	url := Releases + "/download/v" + offer.Version + "/" + debupdate.MetadataName(c.target.Arch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Offer{}, err
	}
	res, err := c.notesClient.Do(req)
	if err != nil {
		return Offer{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return offer, nil
	} // Release predates DEB updates.
	if res.StatusCode != http.StatusOK {
		return Offer{}, updateHTTPError(res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4097))
	if err != nil {
		return Offer{}, err
	}
	var p debupdate.Package
	if len(data) > 4096 || json.Unmarshal(data, &p) != nil ||
		p.Verify(c.publicKey, offer.Version, c.target.Arch) != nil {
		return Offer{}, update.ErrSignature
	}
	offer.deb, offer.Bytes, offer.Automatic = &p, p.Size, true
	return offer, nil
}

type debProgress struct {
	done, total int64
	report      func(int64, int64)
}

func (p *debProgress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.report != nil {
		p.report(p.done, p.total)
	}
	return len(b), nil
}

func (c *Client) downloadDeb(ctx context.Context, p debupdate.Package, report func(int64, int64)) (string, error) {
	if err := p.Verify(c.publicKey, p.Version, c.target.Arch); err != nil {
		return "", update.ErrSignature
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	dir := filepath.Join(c.cache, p.Version)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, AssetName(p.Version, "linux", p.Arch, ".deb"))
	if f, err := os.Open(path); err == nil {
		err = p.Stage(io.Discard, f)
		f.Close()
		if err == nil {
			if report != nil {
				report(p.Size, p.Size)
			}
			return path, nil
		}
	}
	// Claim an interrupted download by rename. Concurrent clients never write
	// the same open file, even when they share a cache.
	work, err := os.MkdirTemp(dir, ".deb-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	tmp := filepath.Join(work, "download")
	part := filepath.Join(dir, p.SHA256+".part")
	if err := os.Rename(part, tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	start, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	if start >= p.Size {
		start = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AssetURL(p.Version, "linux", p.Arch, ".deb"), nil)
	if err != nil {
		return "", err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	keep := func() {
		if f.Close() == nil {
			_ = os.Rename(tmp, part)
		}
	}
	res, err := c.notesClient.Do(req)
	if err != nil {
		keep()
		return "", errors.Join(update.ErrInterrupted, err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		start = 0
	case http.StatusPartialContent:
		if start == 0 || res.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, p.Size-1, p.Size) {
			return "", update.ErrSize
		}
	default:
		keep()
		return "", updateHTTPError(res)
	}
	if err := f.Truncate(start); err != nil {
		return "", err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	remaining := p.Size - start
	progress := &debProgress{start, p.Size, report}
	n, err := io.Copy(io.MultiWriter(f, progress), io.LimitReader(res.Body, remaining+1))
	if err != nil || n < remaining {
		keep()
		return "", errors.Join(update.ErrInterrupted, err)
	}
	if n != remaining {
		return "", update.ErrSize
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err := p.Stage(io.Discard, f); err != nil {
		return "", fmt.Errorf("%w: %v", update.ErrChecksum, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
