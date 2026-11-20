package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/desktop/fileglyph"
	"goim/internal/domain"
	"goim/internal/fileicon"
)

var fileTypeGlyphs = map[string]*gfx.Icon{
	"archive": &fileglyph.Archive, "audio": &fileglyph.Audio, "console": &fileglyph.Console,
	"database": &fileglyph.Database, "document": &fileglyph.Document, "exe": &fileglyph.Exe,
	"file": &fileglyph.File, "folder": &fileglyph.Folder, "image": &fileglyph.Image,
	"json": &fileglyph.Json, "markdown": &fileglyph.Markdown, "pdf": &fileglyph.Pdf,
	"powerpoint": &fileglyph.Powerpoint, "settings": &fileglyph.Settings, "table": &fileglyph.Table,
	"video": &fileglyph.Video, "xml": &fileglyph.Xml,
}

func fileTypeIcon(file domain.File) *gfx.Icon {
	return fileTypeGlyphs[fileicon.Kind(file.Name, file.MIME, file.Folder || file.ShareFolder)]
}

func drawFileIcon(f ui.C, theme ui.Theme, icon *gfx.Icon, tile, size float32) {
	ui.Row(f).Size(tile, tile).Radius(radiusControl).Background(theme.Muted).Center().Children(func() {
		ui.Icon(f, *icon).Size(size, size).Color(theme.MutedForeground)
	})
}
