package core

import (
	"testing"

	"goim/internal/domain"
)

func TestLanguageSettingPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "用户", dir)
	if got := n.Snapshot().Settings.Language; got != "zh-CN" {
		t.Fatalf("default language = %q", got)
	}
	command := domain.Command{Kind: domain.SetSetting, Setting: "language", Value: "en"}
	if err := n.execute(command); err != nil {
		t.Fatal(err)
	}
	command.Value = "unknown"
	if err := n.execute(command); err == nil {
		t.Fatal("unknown language was accepted")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "用户", dir)
	if got := reopened.Snapshot().Settings.Language; got != "en" {
		t.Fatalf("restored language = %q", got)
	}
}
