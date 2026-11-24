package core

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"goim/internal/domain"
	"goim/internal/localsend"
)

func TestLocalSendStartsOnlyAfterExplicitEnable(t *testing.T) {
	dir := t.TempDir()
	opts := Options{DataDir: dir, Listen: "127.0.0.1:0", DisableDiscovery: true, DisableIPMSG: true}
	node, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = node.Close() }()
	if state := node.Snapshot(); state.Settings.LocalSend.Enabled || state.LocalSend.Active {
		t.Fatal("LocalSend is enabled by default")
	}
	if _, err := os.Stat(filepath.Join(dir, "localsend-tls.pem")); !os.IsNotExist(err) {
		t.Fatalf("disabled module created an identity: %v", err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	cfg := domain.LocalSendConfig{Enabled: true, Port: port}
	configure := func(cfg domain.LocalSendConfig) {
		t.Helper()
		if err := node.execute(domain.Command{Kind: domain.LocalSendConfigure, LocalSend: domain.LocalSendRequest{Config: cfg}}); err != nil {
			t.Fatal(err)
		}
	}
	configure(cfg)
	if state := node.Snapshot(); !state.LocalSend.Active || state.LocalSend.Port != port {
		t.Fatalf("service not listening after enable: %+v", state.LocalSend)
	}
	identity, err := os.ReadFile(filepath.Join(dir, "localsend-tls.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	node, err = New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if state := node.Snapshot(); !state.LocalSend.Active || state.LocalSend.Port != port {
		t.Fatalf("module did not restart: %+v", state.LocalSend)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "localsend-tls.pem"))
	if err != nil || string(saved) != string(identity) {
		t.Fatal("device identity changed on restart")
	}
	cfg.Enabled = false
	configure(cfg)
	if state := node.Snapshot(); state.Settings.LocalSend.Enabled || state.LocalSend.Active {
		t.Fatal("service still active after switch off")
	}
}

func TestLocalSendReceiveDirectoryIsIndependentAndUsedByLiveReceiver(t *testing.T) {
	base := t.TempDir()
	nativeDir := filepath.Join(base, "native")
	localSendDir := filepath.Join(base, "localsend")
	n, err := New(Options{DataDir: filepath.Join(base, "profile"), Listen: "127.0.0.1:0", DownloadDir: nativeDir, DisableDiscovery: true, DisableIPMSG: true})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if actual := n.Snapshot().Settings.LocalSendReceiveDirectory(); actual != nativeDir {
		t.Fatalf("default LocalSend folder = %q", actual)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	config := domain.LocalSendConfig{Enabled: true, Port: port}
	if err := n.execute(domain.Command{Kind: domain.LocalSendConfigure, LocalSend: domain.LocalSendRequest{Config: config}}); err != nil {
		t.Fatal(err)
	}
	active := n.localSend.Load()
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "localsend_download_dir", Value: localSendDir}); err != nil {
		t.Fatal(err)
	}
	settings := n.Snapshot().Settings
	if n.localSend.Load() != active || settings.LocalSendReceiveDirectory() != localSendDir || settings.DownloadDir != nativeDir {
		t.Fatalf("wrong folder scope or service restarted: GoIM %q, LocalSend %q", settings.DownloadDir, settings.LocalSendReceiveDirectory())
	}
	sender, err := localsend.Start(localsend.Options{IdentityDir: t.TempDir(), Alias: "LocalSend QA Sender", NoDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if err := sender.AddDevice(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), false); err != nil {
		t.Fatal(err)
	}
	view, _ := sender.View()
	if len(view.Devices) != 1 {
		t.Fatalf("expected one receiver, got %+v", view.Devices)
	}
	input := filepath.Join(base, "测试文件.txt")
	payload := []byte("LocalSend directory selection")
	if err := os.WriteFile(input, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send([]string{view.Devices[0].ID}, []string{input}, "", "", false); err != nil {
		t.Fatal(err)
	}
	var offer domain.LocalSendOffer
	eventually(t, "LocalSend offer", func() bool {
		offer = n.Snapshot().LocalSend.Offer
		return offer.ID != "" && len(offer.Files) == 1
	})
	if err := n.execute(domain.Command{Kind: domain.LocalSendAnswer, TransferID: offer.ID, IDs: []string{offer.Files[0].ID}, Bool: true}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "file published to the LocalSend folder", func() bool {
		result, err := os.ReadFile(filepath.Join(localSendDir, filepath.Base(input)))
		return err == nil && bytes.Equal(result, payload)
	})
}
