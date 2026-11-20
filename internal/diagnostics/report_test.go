package diagnostics

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"goim/internal/buildinfo"
	"goim/internal/domain"
)

func TestReportFallbackFilesAreUniqueAndPrivate(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "reports")
	first, err := WritePrivateTemp(directory, "goim-diagnostics", "first report")
	if err != nil {
		t.Fatal(err)
	}
	second, err := WritePrivateTemp(directory, "goim-diagnostics", "second report")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("fallback export overwrote an existing report")
	}
	for path, want := range map[string]string{first: "first report", second: "second report"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("report %q: data=%q err=%v", path, data, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("report permissions are not private: %v", info.Mode())
		}
	}
}

func TestReportFallbackRejectsInvalidDestinations(t *testing.T) {
	for _, directory := range []string{"", "relative"} {
		if _, err := WritePrivateTemp(directory, "goim-diagnostics", "report"); err == nil {
			t.Fatalf("accepted invalid directory %q", directory)
		}
	}
	if _, err := WritePrivateTemp(t.TempDir(), "../other", "report"); err == nil {
		t.Fatal("accepted a filename outside the destination")
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePrivateTemp(file, "goim-diagnostics", "report"); err == nil {
		t.Fatal("export to a regular file succeeded")
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep" {
		t.Fatal("failed export modified the existing destination")
	}
}

func TestReportExcludesPrivateApplicationData(t *testing.T) {
	private := []string{
		"Alice-Private",
		"Skynet Project",
		"payroll-secret.pdf",
		"our private conversation",
		"alice@example.com",
		"13800138000",
		"192.168.17.42",
		"2001:db8::42",
		"C:\\Users\\Alice\\Private\\payroll-secret.pdf",
		"https://private.example/share?token=secret-token",
	}
	state := domain.State{
		Self: domain.Peer{
			ID: "self-private-id", Name: "Alice-Private", Address: "192.168.17.42:47842",
			Signature: "private signature",
		},
		Peers: []domain.Peer{{
			ID: "peer-private-id", Name: "Alice-Private", Address: "192.168.17.42:47842",
			Details: domain.ProfileDetails{Email: "alice@example.com", Mobile: "13800138000"},
		}},
		Conversations: []domain.Conversation{{
			ID: "conversation-private-id", Title: "Alice-Private", Preview: "our private conversation",
		}},
		Messages: []domain.Message{{
			ID: "message-private-id", Kind: "text", Text: "our private conversation",
		}},
		Groups: []domain.Group{{ID: "group-private-id", Name: "Skynet Project"}},
		Files: []domain.File{{
			ID: "file-private-id", Name: "payroll-secret.pdf",
			SourcePath: `C:\Users\Alice\Private\payroll-secret.pdf`,
			ShareURL:   "https://private.example/share?token=secret-token",
		}},
		Transfers: []domain.Transfer{{
			ID: "transfer-private-id", Name: "payroll-secret.pdf",
			Path: `C:\Users\Alice\Private\payroll-secret.pdf`,
		}},
		Settings: domain.DefaultSettings(`C:\Users\Alice\Downloads`),
		Error: "connect 192.168.17.42:47842 for Alice-Private failed while opening " +
			`C:\Users\Alice\Private\payroll-secret.pdf token=secret-token`,
	}
	state.Settings.ProfileDetails.Email = "alice@example.com"
	state.Settings.ProfileDetails.Mobile = "13800138000"

	report := Build(Input{
		Now:       time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC),
		Build:     buildinfo.Info{Version: "0.1.5", Platform: "Windows · x86-64", GoVersion: "go1.26"},
		State:     state,
		Autostart: "disabled",
		DataDir:   t.TempDir(),
		Interfaces: func() ([]net.Interface, error) {
			return []net.Interface{{Name: "Ethernet 2", Flags: net.FlagUp}}, nil
		},
	})

	for _, value := range private {
		if strings.Contains(report, value) {
			t.Errorf("report contains private value %q:\n%s", value, report)
		}
	}
	for _, want := range []string{
		"== GoIM diagnostics ==",
		"platform: Windows · x86-64",
		"neiwangtong: enabled",
		"ipmsg: enabled",
		"adapter 1: name=Ethernet 2 state=up",
		"peers: total=1 online=0",
		"messages: total=1",
		"Excluded: message text, contact names",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
}

func TestReportKeepsUsefulNonPrivateErrorContext(t *testing.T) {
	state := domain.State{Error: "network connection timeout while opening 192.168.1.2"}
	report := Build(Input{
		Now:   time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC),
		State: state,
		Interfaces: func() ([]net.Interface, error) {
			return nil, errors.New("permission denied")
		},
	})
	if !strings.Contains(report, "kind: timeout") ||
		!strings.Contains(report, "summary: network connection timeout while opening [ip]") {
		t.Fatalf("error context was lost:\n%s", report)
	}
}

func TestStorageProbesVerifyWritabilityAndDatabaseReadability(t *testing.T) {
	directory := t.TempDir()
	if got := directoryState(directory); got != "present, writable" {
		t.Fatalf("directory state = %q", got)
	}
	if got := directoryState(filepath.Join(directory, "missing")); got != "unavailable" {
		t.Fatalf("missing directory state = %q", got)
	}
	filePath := filepath.Join(directory, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := directoryState(filePath); got != "not a directory" {
		t.Fatalf("file as directory state = %q", got)
	}

	database := filepath.Join(directory, "goim.db")
	if err := os.WriteFile(database, []byte{0}, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := databaseFileState(database); got != "present, readable" {
		t.Fatalf("database state = %q", got)
	}
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := databaseFileState(database); got != "present, empty" {
		t.Fatalf("empty database state = %q", got)
	}
	if got := databaseFileState(filepath.Join(directory, "missing.db")); got != "unavailable" {
		t.Fatalf("missing database state = %q", got)
	}
}

func TestWritePrivateFileCreatesOwnerReadableReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goim-diagnostics.txt")
	if err := WritePrivateFile(path, "report"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "report" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if err := WritePrivateFile("", "report"); err == nil {
		t.Fatal("empty destination was accepted")
	}
}
