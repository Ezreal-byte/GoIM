// Command neiwangtong-check checks the native protocol against one lab client.
// The normal Go tests exercise both directions without a proprietary client.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"time"

	"goim/internal/neiwangtong"
)

type options struct {
	bind            netip.Addr
	target          netip.AddrPort
	expectedVersion string
	expectedName    string
	text, avatar    bool
	timeout         time.Duration
}

type check struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Milliseconds int64  `json:"milliseconds,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

type report struct {
	Mode             string  `json:"mode"`
	Target           string  `json:"target"`
	ReferenceVersion string  `json:"reference_version,omitempty"`
	Passed           bool    `json:"passed"`
	Checks           []check `json:"checks"`
}

func (r *report) run(name string, fn func() (string, error)) bool {
	start := time.Now()
	detail, err := fn()
	status := "pass"
	if err != nil {
		status, detail = "fail", err.Error()
	}
	r.Checks = append(r.Checks, check{name, status, time.Since(start).Milliseconds(), detail})
	return err == nil
}

func (r *report) skip(name string) {
	r.Checks = append(r.Checks, check{Name: name, Status: "not_requested"})
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opt options
	flags := flag.NewFlagSet("neiwangtong-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bind := flags.String("bind", "", "Local IPv4 address on the isolated test network")
	target := flags.String("target", "", "Test client's IPv4 address and discovery port (for example 192.168.100.2:9011)")
	flags.StringVar(&opt.expectedVersion, "version", "3.4.3055", "Required version reported by the reference client")
	flags.StringVar(&opt.expectedName, "peer", "", "Exact reference client's test nickname; required to send text")
	flags.BoolVar(&opt.text, "text-receipt", false, "Send one generated message to the named lab peer and wait for an application receipt")
	flags.BoolVar(&opt.avatar, "avatar", false, "Require a nonempty, valid PNG avatar from the reference client")
	flags.DurationVar(&opt.timeout, "timeout", 15*time.Second, "Timeout for each requested check")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	opt.bind, _ = netip.ParseAddr(*bind)
	opt.target, _ = netip.ParseAddrPort(*target)
	if flags.NArg() != 0 || !labIPv4(opt.bind) || !labIPv4(opt.target.Addr()) || opt.target.Port() == 0 ||
		opt.bind == opt.target.Addr() || opt.bind.IsLoopback() != opt.target.Addr().IsLoopback() {
		return options{}, errors.New("supply distinct private or loopback IPv4 endpoints using -bind and -target IPv4:port")
	}
	if opt.timeout <= 0 || opt.timeout > time.Minute || opt.expectedVersion == "" {
		return options{}, errors.New("a reference version and a positive timeout of at most one minute are required")
	}
	if opt.text && opt.expectedName == "" {
		return options{}, errors.New("-text-receipt requires -peer with the exact test client's nickname")
	}
	return opt, nil
}

func labIPv4(ip netip.Addr) bool { return ip.Is4() && (ip.IsPrivate() || ip.IsLoopback()) }

func publish[T any](events chan<- T, value T) {
	select {
	case events <- value:
	default:
	}
}

func wait[T any](ctx context.Context, timeout time.Duration, events <-chan T, match func(T) bool) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case event := <-events:
			if match(event) {
				return event, nil
			}
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
}

func verify(ctx context.Context, opt options) report {
	r := report{Mode: "reference", Target: opt.target.String()}
	type receipt struct {
		peer   string
		packet uint64
	}
	type avatar struct {
		peer string
		png  []byte
	}
	peers := make(chan neiwangtong.Peer, 8)
	receipts := make(chan receipt, 8)
	avatars := make(chan avatar, 8)
	var runID [8]byte
	_, _ = rand.Read(runID[:])
	cfg := neiwangtong.Config{
		BindIP:           opt.bind,
		Profile:          neiwangtong.Peer{Name: "GoIM QA " + hex.EncodeToString(runID[:4])},
		GoIM:             true,
		DisableBroadcast: true,
		AllowRemote:      func(addr netip.AddrPort) bool { return addr.Addr() == opt.target.Addr() },
		Callbacks: neiwangtong.Callbacks{
			Peer:             func(p neiwangtong.Peer) { publish(peers, p) },
			TextAcknowledged: func(id string, packet uint64) { publish(receipts, receipt{id, packet}) },
			AvatarUpdated:    func(id string, _ uint64, png []byte) { publish(avatars, avatar{id, png}) },
		},
	}
	var service *neiwangtong.Service
	if !r.run("bind", func() (string, error) {
		var err error
		service, err = neiwangtong.Start(cfg)
		return "", err
	}) {
		return r
	}
	defer service.Close()
	var reference neiwangtong.Peer
	if !r.run("native_session", func() (string, error) {
		phase, cancel := context.WithTimeout(ctx, opt.timeout)
		defer cancel()
		if err := service.Probe(phase, opt.target, int(neiwangtong.ControlPort), int(neiwangtong.AvatarPort)); err != nil {
			return "", err
		}
		var err error
		reference, err = wait(phase, opt.timeout, peers, func(p neiwangtong.Peer) bool {
			addr, e := netip.ParseAddrPort(p.Address)
			return e == nil && addr.Addr() == opt.target.Addr() && p.Online
		})
		if err != nil {
			return "", fmt.Errorf("no established reference profile: %w", err)
		}
		r.ReferenceVersion = reference.Version
		if reference.GoIM || reference.Version != opt.expectedVersion {
			return "", fmt.Errorf("unexpected reference client version %q (want %q)", reference.Version, opt.expectedVersion)
		}
		if opt.expectedName != "" && reference.Name != opt.expectedName {
			return "", errors.New("the reference nickname does not match -peer")
		}
		return "directed discovery and established native profile", nil
	}) {
		return r
	}
	passed := true
	if opt.avatar {
		passed = r.run("avatar", func() (string, error) {
			event, err := wait(ctx, opt.timeout, avatars, func(event avatar) bool {
				return event.peer == reference.ID && len(event.png) != 0
			})
			if err != nil {
				return "", fmt.Errorf("no nonempty avatar from the reference (use a test profile with an avatar): %w", err)
			}
			img, err := png.DecodeConfig(bytes.NewReader(event.png))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%dx%d PNG, sha256=%x", img.Width, img.Height, sha256.Sum256(event.png)), nil
		})
	} else {
		r.skip("avatar")
	}
	if opt.text {
		packet := uint64(max(binary.BigEndian.Uint32(runID[4:]), 1))
		body := fmt.Sprintf("[GoIM QA %x] 内网通文字 & 中文 🙂", runID)
		sent := r.run("text_transport", func() (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "ENet reliable command acknowledged", service.SendTextWithID(reference.ID, packet, body)
		})
		if sent {
			sent = r.run("text_application_receipt", func() (string, error) {
				_, err := wait(ctx, opt.timeout, receipts, func(got receipt) bool {
					return got == (receipt{reference.ID, packet})
				})
				if err != nil {
					return "", fmt.Errorf("ENet acknowledged the command but no matching application receipt arrived: %w", err)
				}
				return "matching peer and message ID", nil
			})
		}
		passed = passed && sent
	} else {
		r.skip("text_application_receipt")
	}
	r.Passed = passed
	return r
}

func main() {
	opt, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	result := verify(ctx, opt)
	stop()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !result.Passed {
		os.Exit(1)
	}
}
