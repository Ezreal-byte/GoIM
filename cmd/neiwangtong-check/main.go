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
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/neiwangtong"
)

type options struct {
	bind               netip.Addr
	target             netip.AddrPort
	expectedVersion    string
	expectedName       string
	labDriver          string
	labDir             string
	text, avatar       bool
	labLargeFile       bool
	labOffline         bool
	labOfflineSend     bool
	labMultiGroup      bool
	labAvatarUpdate    bool
	labTextRecall      bool
	labGroupRecall     bool
	labGroupDismiss    bool
	labGroupKick       bool
	labGroupFile       bool
	labGroupFileResume bool
	labGroupFolder     bool
	labEmoticons       bool
	labResume          bool
	labDirectory       bool
	labFolderResend    bool
	labDirection       string
	timeout            time.Duration
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
	flags.StringVar(&opt.labDriver, "lab-driver", "", "Local driver for the disposable reference sandbox")
	flags.StringVar(&opt.labDir, "lab-dir", "", "Scratch directory of the disposable reference sandbox")
	flags.BoolVar(&opt.labLargeFile, "lab-large-file", false, "Run a dedicated 4 GiB boundary check instead of the standard file scenarios")
	flags.BoolVar(&opt.labOffline, "lab-offline-replay", false, "Check original-client offline replay in the disposable sandbox")
	flags.BoolVar(&opt.labOfflineSend, "lab-offline-send", false, "Queue data in the GoIM backend until the original returns")
	flags.BoolVar(&opt.labMultiGroup, "lab-multi-group", false, "Check a GoIM group with two original clients on the isolated LAN")
	flags.BoolVar(&opt.labAvatarUpdate, "lab-avatar-update", false, "Check whether the original downloads a GoIM avatar after a profile update")
	flags.BoolVar(&opt.labTextRecall, "lab-text-recall", false, "Verify both directions of direct text recall against the original")
	flags.BoolVar(&opt.labGroupRecall, "lab-group-recall", false, "Verify both directions of group text recall against the original")
	flags.BoolVar(&opt.labGroupDismiss, "lab-group-dismiss", false, "Verify that GoIM accepts dismissal from the original group owner")
	flags.BoolVar(&opt.labGroupKick, "lab-group-kick", false, "Verify that GoIM accepts removal from the original group owner")
	flags.BoolVar(&opt.labGroupFile, "lab-group-file", false, "Verify regular group files with the original client")
	flags.BoolVar(&opt.labGroupFileResume, "lab-group-file-resume", false, "Interrupt and resume a regular group file from the original client")
	flags.BoolVar(&opt.labGroupFolder, "lab-group-folder", false, "Verify group folders with the original client")
	flags.BoolVar(&opt.labEmoticons, "lab-emoticons", false, "Verify the stock emoticon identifier and a mixed native emoji message")
	flags.BoolVar(&opt.labResume, "lab-resume-receive", false, "Interrupt a real file transfer to the original and verify its resumption offset")
	flags.BoolVar(&opt.labDirectory, "lab-directory-retry", false, "Measure whether the original can resume a directory after interruption")
	flags.BoolVar(&opt.labFolderResend, "lab-directory-resend", false, "Verify a fresh directory offer after an interrupted transfer")
	flags.StringVar(&opt.labDirection, "lab-direction", "", "Direction for a large file or directory retry check: from-reference or to-reference")
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
	if opt.text && opt.expectedName == "" {
		return options{}, errors.New("-text-receipt requires -peer with the exact test client's nickname")
	}
	if opt.labDriver != "" || opt.labDir != "" {
		if !opt.text || !filepath.IsAbs(opt.labDriver) || !filepath.IsAbs(opt.labDir) {
			return options{}, errors.New("sandbox checks require absolute -lab-driver and -lab-dir paths, plus -text-receipt and -peer")
		}
	}
	special := 0
	for _, enabled := range []bool{opt.labLargeFile, opt.labOffline, opt.labOfflineSend, opt.labMultiGroup, opt.labAvatarUpdate,
		opt.labTextRecall, opt.labGroupRecall, opt.labGroupDismiss, opt.labGroupKick, opt.labGroupFile, opt.labGroupFileResume, opt.labGroupFolder, opt.labEmoticons,
		opt.labResume, opt.labDirectory, opt.labFolderResend} {
		if enabled {
			special++
		}
	}
	if special > 1 {
		return options{}, errors.New("choose one special lab check per sandbox run")
	}
	if special != 0 && (opt.labDriver == "" || opt.bind.String() != "10.77.0.1" || opt.target.String() != "10.77.0.2:9011" || opt.expectedName != "GOIM-NWT") {
		return options{}, errors.New("special lab checks require the fixed endpoints and peer name of the disposable sandbox")
	}
	if opt.labLargeFile || opt.labDirectory || opt.labFolderResend {
		if opt.labDirection != "from-reference" && opt.labDirection != "to-reference" {
			return options{}, errors.New("choose the direction of the isolated transfer check")
		}
	} else if opt.labDirection != "" {
		return options{}, errors.New("-lab-direction requires a large file or directory retry check")
	}
	timeoutLimit := time.Minute
	if opt.labOfflineSend {
		// A hard stop deliberately has no quit packet. Allow the native
		// session expiry plus time to restart and verify the replay.
		timeoutLimit = 3 * time.Minute
	}
	if opt.timeout <= 0 || opt.timeout > timeoutLimit || opt.expectedVersion == "" {
		return options{}, fmt.Errorf("a reference version and a positive timeout of at most %s are required", timeoutLimit)
	}
	return opt, nil
}

func labIPv4(ip netip.Addr) bool { return ip.Is4() && (ip.IsPrivate() || ip.IsLoopback()) }

// Core exercises the app's wildcard listeners and automatic discovery. Refuse
// those checks without the launcher-created link, or with another active NIC.
func requireSandboxLink(bind netip.Addr) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	seen, found := false, false
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.Name) != 10 || !strings.HasPrefix(iface.Name, "ng") || seen {
			return errors.New("core discovery checks require the disposable sandbox link")
		}
		seen = true
		addrs, err := iface.Addrs()
		if err != nil {
			return err
		}
		for _, addr := range addrs {
			if p, err := netip.ParsePrefix(addr.String()); err == nil {
				if p.Addr() == bind {
					found = true
				} else if !p.Addr().IsLinkLocalUnicast() {
					return errors.New("unexpected address on the disposable sandbox link")
				}
			}
		}
	}
	if !found {
		return errors.New("core discovery checks require the assigned sandbox address")
	}
	return nil
}

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
	groupCheck := opt.labTextRecall || opt.labGroupRecall || opt.labGroupFile || opt.labGroupFolder || opt.labEmoticons
	if opt.labOfflineSend || opt.labMultiGroup || groupCheck {
		if err := requireSandboxLink(opt.bind); err != nil {
			r := report{Mode: "sandbox", Target: opt.target.String()}
			r.run("sandbox_link", func() (string, error) { return "", err })
			return r
		}
	}
	if opt.labOfflineSend {
		return verifyOfflineSend(ctx, opt)
	}
	if opt.labMultiGroup {
		return verifyMultiGroup(ctx, opt)
	}
	if groupCheck {
		return verifyCoreChat(ctx, opt)
	}
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
	qaName := "GoIM QA " + hex.EncodeToString(runID[:4])
	cfg := neiwangtong.Config{
		BindIP:           opt.bind,
		Profile:          neiwangtong.Peer{Name: qaName},
		GoIM:             true,
		DisableBroadcast: true,
		EnableFolders:    opt.labDriver != "" && !opt.labLargeFile && !opt.labOffline && !opt.labResume,
		AllowRemote:      func(addr netip.AddrPort) bool { return addr.Addr() == opt.target.Addr() },
		Callbacks: neiwangtong.Callbacks{
			Peer:             func(p neiwangtong.Peer) { publish(peers, p) },
			TextAcknowledged: func(id string, packet uint64) { publish(receipts, receipt{id, packet}) },
			AvatarUpdated:    func(id string, _ uint64, png []byte) { publish(avatars, avatar{id, png}) },
		},
	}
	copy(cfg.ID[:], runID[:])
	var lab *labChecks
	if opt.labDriver != "" {
		r.Mode = "sandbox"
		lab = newLabChecks(opt, qaName, hex.EncodeToString(runID[:]))
		lab.attach(&cfg)
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
		if sent && lab != nil {
			labPassed := lab.run(ctx, &r, service, reference)
			passed = passed && labPassed
			if opt.labOffline {
				offline := lab.checkOffline(ctx, &r, service, reference.ID, cfg)
				passed = passed && offline
			}
			if opt.labResume && labPassed {
				passed = r.run("resumed_by_reference_receiver", func() (string, error) {
					return lab.checkStockReceiveResume(ctx, service, reference.ID, cfg)
				}) && passed
			}
			if (opt.labDirectory || opt.labFolderResend) && labPassed {
				if opt.labDirection == "from-reference" {
					checkName := "original_directory_reaccepts_after_drop"
					if opt.labFolderResend {
						checkName = "fresh_directory_offer_from_original_after_drop"
					}
					passed = r.run(checkName, func() (string, error) {
						return lab.checkOriginalDirectoryRetry(ctx, service, reference.ID)
					}) && passed
				} else {
					checkName := "directory_resumed_by_reference_receiver"
					if opt.labFolderResend {
						checkName = "fresh_directory_offer_to_original_after_drop"
					}
					passed = r.run(checkName, func() (string, error) {
						return lab.checkStockDirectoryResume(ctx, service, reference.ID, cfg)
					}) && passed
				}
			}
		}
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
