package core

import (
	"testing"

	"goim/internal/domain"
)

func TestInterfaceFontSettingIsValidatedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "用户", dir)
	command := domain.Command{Kind: domain.SetSetting, Setting: "interface_font", Value: "Microsoft YaHei UI"}
	if err := n.execute(command); err != nil {
		t.Fatal(err)
	}
	if got := n.Snapshot().Settings.InterfaceFont; got != "Microsoft YaHei UI" {
		t.Fatalf("interface font = %q", got)
	}
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "interface_font", Value: "bad\nfont"}); err == nil {
		t.Fatal("a control character was accepted in an interface font family")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := testNodeAt(t, "用户", dir)
	if got := reopened.Snapshot().Settings.InterfaceFont; got != "Microsoft YaHei UI" {
		t.Fatalf("restored interface font = %q", got)
	}
}

func TestInterfaceFontSizeSettingIsValidatedPersistedAndMigrated(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "用户", dir)
	for _, value := range []string{"8", "29", "large"} {
		if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "interface_font_size", Value: value}); err == nil {
			t.Fatalf("interface font size %q was accepted", value)
		}
	}
	for _, value := range []string{"9", "28"} {
		if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "interface_font_size", Value: value}); err != nil {
			t.Fatalf("interface font size %q was rejected: %v", value, err)
		}
	}
	if got := n.Snapshot().Settings.InterfaceFontSize; got != domain.InterfaceFontSizeMaximum {
		t.Fatalf("interface font size = %d", got)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := testNodeAt(t, "用户", dir)
	if got := reopened.Snapshot().Settings.InterfaceFontSize; got != domain.InterfaceFontSizeMaximum {
		t.Fatalf("restored interface font size = %d", got)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	empty := testNodeAt(t, "旧用户", t.TempDir())
	if got := empty.Snapshot().Settings.InterfaceFontSize; got != domain.InterfaceFontSizeDefault {
		t.Fatalf("legacy interface font size = %d", got)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
}
