package core

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"goim/internal/domain"
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
