package core

import (
	"testing"

	"goim/internal/domain"
)

func TestLastSectionIsValidatedPersistedAndSilent(t *testing.T) {
	node := testNode(t, "启动页")
	dir := node.dir

	if err := node.execute(domain.Command{
		Kind: domain.SetSetting, Setting: "last_section", Value: "unknown",
	}); err == nil {
		t.Fatal("invalid startup section was accepted")
	}
	for _, section := range []string{"messages", "contacts", "transfers", "shares", "settings"} {
		if err := node.execute(domain.Command{
			Kind: domain.SetSetting, Setting: "last_section", Value: section,
		}); err != nil {
			t.Fatalf("save startup section %q: %v", section, err)
		}
		state := node.Snapshot()
		if state.Settings.LastSection != section {
			t.Fatalf("startup section = %q, want %q", state.Settings.LastSection, section)
		}
		if state.Notice != "" {
			t.Fatalf("saving startup section produced notice %q", state.Notice)
		}
	}

	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "启动页", dir)
	if got := reopened.Snapshot().Settings.LastSection; got != "settings" {
		t.Fatalf("startup section after restart = %q, want settings", got)
	}
}
