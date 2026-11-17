package core

import (
	"testing"

	"goim/internal/domain"
)

func TestAutomaticUpdateCheckDefaultsOnAndPersistsOptOut(t *testing.T) {
	dir := t.TempDir()
	n := testOptimizationNode(t, dir)
	if n.Snapshot().Settings.DisableAutoUpdateCheck {
		t.Fatal("automatic update checking was not enabled for a new profile")
	}
	if err := n.execute(domain.Command{
		Kind: domain.SetSetting, Setting: "disable_auto_update_check", Bool: true,
	}); err != nil {
		t.Fatal(err)
	}
	if !n.Snapshot().Settings.DisableAutoUpdateCheck {
		t.Fatal("automatic update opt-out was not saved")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	n = testOptimizationNode(t, dir)
	if !n.Snapshot().Settings.DisableAutoUpdateCheck {
		t.Fatal("automatic update opt-out did not survive restart")
	}
}
