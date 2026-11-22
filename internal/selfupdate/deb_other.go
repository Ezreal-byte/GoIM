//go:build !linux

package selfupdate

import (
	"github.com/vivaui/viva/update"
	"goim/internal/debupdate"
)

func installDeb(debupdate.Package, string, []string, func()) error {
	return update.ErrManagedExternally
}
