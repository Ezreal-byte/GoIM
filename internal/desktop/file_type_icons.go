package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/desktop/fileglyph"
	"goim/internal/domain"
	"goim/internal/fileicon"
)

var fileTypeGlyphs = map[string]*gfx.Icon{
	"archive":     &fileglyph.Archive,
	"audio":       &fileglyph.Audio,
	"c":           &fileglyph.C,
	"certificate": &fileglyph.Certificate,
	"console":     &fileglyph.Console,
	"cpp":         &fileglyph.Cpp,
	"csharp":      &fileglyph.Csharp,
	"css":         &fileglyph.Css,
	"database":    &fileglyph.Database,
	"docker":      &fileglyph.Docker,
	"document":    &fileglyph.Document,
	"exe":         &fileglyph.Exe,
	"file":        &fileglyph.File,
	"folder":      &fileglyph.Folder,
	"git":         &fileglyph.Git,
	"go":          &fileglyph.Go,
	"gradle":      &fileglyph.Gradle,
	"html":        &fileglyph.Html,
	"image":       &fileglyph.Image,
	"java":        &fileglyph.Java,
	"javascript":  &fileglyph.Javascript,
	"json":        &fileglyph.Json,
	"key":         &fileglyph.Key,
	"license":     &fileglyph.License,
	"lock":        &fileglyph.Lock,
	"log":         &fileglyph.Log,
	"lua":         &fileglyph.Lua,
	"markdown":    &fileglyph.Markdown,
	"nginx":       &fileglyph.Nginx,
	"nodejs":      &fileglyph.Nodejs,
	"pdf":         &fileglyph.Pdf,
	"php":         &fileglyph.Php,
	"powerpoint":  &fileglyph.Powerpoint,
	"powershell":  &fileglyph.Powershell,
	"python":      &fileglyph.Python,
	"react":       &fileglyph.React,
	"readme":      &fileglyph.Readme,
	"ruby":        &fileglyph.Ruby,
	"rust":        &fileglyph.Rust,
	"sass":        &fileglyph.Sass,
	"settings":    &fileglyph.Settings,
	"swift":       &fileglyph.Swift,
	"table":       &fileglyph.Table,
	"toml":        &fileglyph.Toml,
	"typescript":  &fileglyph.Typescript,
	"video":       &fileglyph.Video,
	"vue":         &fileglyph.Vue,
	"xml":         &fileglyph.Xml,
	"yaml":        &fileglyph.Yaml,
}

func fileTypeIcon(file domain.File) *gfx.Icon {
	return fileTypeGlyphs[fileicon.Kind(file.Name, file.MIME, file.Folder || file.ShareFolder)]
}

func drawFileIcon(f ui.C, theme ui.Theme, icon *gfx.Icon, tile, size float32) {
	ui.Row(f).Size(tile, tile).Radius(radiusControl).Background(theme.Muted).Center().Children(func() {
		ui.Icon(f, *icon).Size(size, size).Color(theme.MutedForeground)
	})
}
