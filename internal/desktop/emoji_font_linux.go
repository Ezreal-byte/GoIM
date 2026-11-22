package desktop

import (
	_ "embed"

	"github.com/vivaui/viva/app"
)

//go:embed emoji/fallback.ttf
var emojiFallbackFont []byte

// Register with the same Pango font map Viva uses to shape editable and
// read-only text. Unicode offsets and selection stay native.
func registerEmojiFallback() func() {
	_, unregister, err := app.RegisterFont(emojiFallbackFont)
	if err != nil {
		return func() {}
	}
	return unregister
}
