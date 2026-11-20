package fileicon

import "embed"

//go:embed svg/*.svg
var sources embed.FS

// SVG returns the bundled image for a built-in kind.
func SVG(kind string) []byte {
	raw, _ := sources.ReadFile("svg/" + kind + ".svg")
	return raw
}
