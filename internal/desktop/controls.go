package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// namedButton keeps the ID shared with a focus request or a shell action.
func namedButton(c ui.C, id gfx.ID, label string) ui.Box {
	return ui.Button(c, label).Hit(id, label)
}
