package fileicon

import "testing"

func TestKind(t *testing.T) {
	cases := []struct {
		name, mime string
		folder     bool
		want       string
	}{
		{"invoice.PdF", "", false, "pdf"}, {"table.ods", "", false, "table"}, {"slides.PPTX", "", false, "powerpoint"},
		{"release.tar.gz", "", false, "archive"}, {"notes.md", "", false, "markdown"}, {"photo.jpg", "", true, "folder"},
		{"录音", "Audio/ogg; codecs=opus", false, "audio"}, {"movie", "video/mp4", false, "video"},
		{"blob", "application/pdf", false, "pdf"}, {"source.go", "text/plain", false, "go"},
		{"Main.TSX", "", false, "react"}, {"app.PY", "", false, "python"}, {"install.PS1", "", false, "powershell"},
		{"project.TOML", "", false, "toml"}, {"folder.yml", "", true, "folder"}, {"Tool.APP", "", true, "folder"},
		{"README.md", "", false, "readme"}, {"PACKAGE.JSON", "", false, "nodejs"}, {"docker-compose.yaml", "", false, "docker"},
		{".gitignore", "", false, "git"}, {".profile", "", false, "settings"}, {"talk.key", "", false, "powerpoint"}, {"id_rsa.pub", "", false, "key"},
		{"client.deb", "", false, "exe"}, {"unnamed", "text/css", false, "css"}, {"", "", false, "file"},
		{"unusual.unknown", "application/octet-stream", false, "file"},
	}
	for _, tc := range cases {
		if got := Kind(tc.name, tc.mime, tc.folder); got != tc.want {
			t.Errorf("Kind(%q,%q,%t) = %q; want %q", tc.name, tc.mime, tc.folder, got, tc.want)
		}
	}
	for name, kind := range byName {
		if len(SVG(kind)) == 0 {
			t.Fatalf("%s refers to missing SVG %q", name, kind)
		}
	}
	for ext, kind := range byExtension {
		if len(SVG(kind)) == 0 {
			t.Fatalf("%s refers to missing SVG %q", ext, kind)
		}
	}
}
