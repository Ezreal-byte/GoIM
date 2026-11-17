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
