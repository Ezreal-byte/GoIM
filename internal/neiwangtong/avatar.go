package neiwangtong

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/netip"
	"time"
)

const (
	avatarSelector = 18000
	selectorOffset = 56
	avatarHeader   = 100
	avatarMaxBytes = 1 << 20
	avatarMaxEdge  = 512
)

var ErrNoAvatarOffer = errors.New("native peer did not send an avatar after confirming the request")

func avatarRequest() []byte {
	return avatarRequestFor(avatarSelector)
}

func avatarRequestFor(selector uint32) []byte {
	p := make([]byte, 500)
	binary.BigEndian.PutUint32(p, uint32(len(p)))
	binary.BigEndian.PutUint32(p[4:], 1)
	binary.BigEndian.PutUint32(p[8:], 1)
	binary.BigEndian.PutUint32(p[selectorOffset:], selector)
	return p
}

func readAvatarHeader(r io.Reader, kind, minLength, maxLength uint32) (uint32, error) {
	var p [12]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return 0, err
	}
	length := binary.BigEndian.Uint32(p[:])
	if length < minLength || length > maxLength || binary.BigEndian.Uint32(p[4:]) != 1 || binary.BigEndian.Uint32(p[8:]) != kind {
		return 0, errors.New("unexpected native avatar record header")
	}
	return length, nil
}

// ReadAvatar validates a confirmation and its following PNG data record. The
// bounded image is decoded before the caller can replace a cached avatar.
func ReadAvatar(r io.Reader) ([]byte, error) {
	return readAvatar(r, avatarSelector, nil)
}

func readAvatar(r io.Reader, selector uint32, next func() error) ([]byte, error) {
	const frameHeader = 12
	if _, err := readAvatarHeader(r, 2, 108, 108); err != nil {
		return nil, fmt.Errorf("invalid native avatar confirmation: %w", err)
	}
	var confirmation [108 - frameHeader]byte
	if _, err := io.ReadFull(r, confirmation[:]); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint32(confirmation[selectorOffset-frameHeader:]) != selector {
		return nil, errors.New("native transfer is not an avatar")
	}
	if next != nil {
		if err := next(); err != nil {
			return nil, err
		}
	}
	length, err := readAvatarHeader(r, 4, avatarHeader+1, avatarHeader+avatarMaxBytes)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, ErrNoAvatarOffer
		}
		return nil, fmt.Errorf("invalid native avatar data: %w", err)
	}
	var header [avatarHeader - frameHeader]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint32(header[selectorOffset-frameHeader:]) != selector {
		return nil, errors.New("native transfer selector changed")
	}
	image := make([]byte, length-avatarHeader)
	if _, err := io.ReadFull(r, image); err != nil {
		return nil, err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > avatarMaxEdge || cfg.Height > avatarMaxEdge {
		return nil, errors.New("invalid native avatar PNG or dimensions")
	}
	if _, err := png.Decode(bytes.NewReader(image)); err != nil {
		return nil, fmt.Errorf("invalid native avatar PNG: %w", err)
	}
	return image, nil
}

// FetchOfferedAvatar pulls a pending avatar from the same IPv4 endpoint as the
// validated control peer. A connection alone does not cause a peer to offer one.
func FetchOfferedAvatar(ctx context.Context, bind netip.Addr, target netip.AddrPort) ([]byte, error) {
	return fetchOfferedAvatar(ctx, bind, target, avatarSelector)
}

func fetchOfferedAvatar(ctx context.Context, bind netip.Addr, target netip.AddrPort, selector uint32) ([]byte, error) {
	if !validUnicast(target) || bind.IsValid() && !bind.Is4() {
		return nil, errors.New("native avatar requires IPv4 endpoints")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d := net.Dialer{}
	if bind.IsValid() {
		d.LocalAddr = net.TCPAddrFromAddrPort(netip.AddrPortFrom(bind, 0))
	}
	conn, err := d.DialContext(ctx, "tcp4", target.String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := io.Copy(conn, bytes.NewReader(avatarRequestFor(selector))); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	data, err := readAvatar(reader, selector, func() error {
		// The change event can push the PNG immediately; first discovery
		// may instead wait for the file request after the confirmation.
		_ = conn.SetReadDeadline(minTime(deadline, time.Now().Add(180*time.Millisecond)))
		_, err := reader.Peek(1)
		_ = conn.SetReadDeadline(deadline)
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() && ctx.Err() == nil {
			_, err = conn.Write(nativeFileRequest(selector))
		}
		return err
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return data, err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
