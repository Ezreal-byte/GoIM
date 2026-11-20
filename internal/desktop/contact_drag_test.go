package desktop

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
)

func TestContactDragScrollsAndCancelsOutsideList(t *testing.T) {
	state := shellTestState(t)
	state.Settings.LastSection = sectionContacts
	state.Groups, state.Invitations = nil, nil
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("extra-%02d", i)
		state.Peers = append(state.Peers, domain.Peer{ID: id, Name: id, ContactGroup: "工程团队", Online: true})
	}
	h := newShellTestHarnessAtSize(t, state, 1000, 720)
	bounds := h.ui.contactListBounds
	hit, ok := h.findHit(h.ui.peerRowIDs["peer-a"])
	if !ok {
		t.Fatal("missing contact")
	}
	x, y := hit.Bounds.X+12, hit.Bounds.Y+12
	if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(x, bounds.Bottom()-2); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := h.driver.Advance(40 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if after, ok := h.findHit(h.ui.peerRowIDs["peer-a"]); ok && after.Bounds.Y >= hit.Bounds.Y {
		t.Fatal("holding the contact by the bottom edge did not scroll")
	}
	if err := h.driver.Release(x, bounds.Bottom()+2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if cmd, ok := findShellCommand(h.backend.commandLog(), domain.SetContactGroup); ok {
		t.Fatalf("release below clipped viewport moved the contact: %+v", cmd)
	}
}

func TestContactDropAcrossGroups(t *testing.T) {
	for _, target := range []string{"工程团队", "空分组", "未分组", "peer-c", "产品与设计", "escape"} {
		t.Run(target, func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.LastSection = sectionContacts
			state.Peers = state.Peers[:3] // No ungrouped peer; its target appears during drag.
			state.ContactGroups = []string{"空分组"}
			h := newShellTestHarness(t, state)
			h.ui.collapsed["空分组"] = true
			h.settle()
			hit, ok := h.findHit(h.ui.peerRowIDs["peer-a"])
			if !ok {
				t.Fatal("missing source")
			}
			x, y := hit.Bounds.X+15, hit.Bounds.Y+15
			if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			if err := h.driver.Move(x+10, y); err != nil {
				t.Fatal(err)
			}
			if !h.ui.contactDrag.Dragging() {
				t.Fatal("contact drag did not start")
			}
			dst := h.ui.groupHeadIDs[target]
			if target == "escape" {
				dst = h.ui.groupHeadIDs["工程团队"]
			}
			if target == "peer-c" {
				dst = h.ui.peerRowIDs[target]
			}
			hit, ok = h.findHit(dst)
			if !ok {
				t.Fatal("missing target")
			}
			x, y = hit.Bounds.X+8, hit.Bounds.Y+8
			if err := h.driver.Move(x, y); err != nil {
				t.Fatal(err)
			}
			if target == "escape" {
				if err := h.driver.Key(gfx.KeyEscape, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.driver.Release(x, y, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			cmd, moved := findShellCommand(h.backend.commandLog(), domain.SetContactGroup)
			switch target {
			case "escape", "产品与设计":
				if moved {
					t.Fatalf("unexpected group change: %+v", cmd)
				}
			default:
				name := target
				if target == "未分组" {
					name = ""
				}
				if target == "peer-c" {
					name = "工程团队"
				}
				if !moved || cmd.PeerID != "peer-a" || cmd.Name != name {
					t.Fatalf("move command: %+v", cmd)
				}
			}
		})
	}
}
