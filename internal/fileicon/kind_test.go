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
		{"blob", "application/pdf", false, "pdf"}, {"source.go", "text/plain", false, "xml"},
		{"unusual.unknown", "application/octet-stream", false, "file"},
	}
	for _, tc := range cases {
		if got := Kind(tc.name, tc.mime, tc.folder); got != tc.want {
			t.Errorf("Kind(%q,%q,%t) = %q; want %q", tc.name, tc.mime, tc.folder, got, tc.want)
		}
	}
	for ext, kind := range byExtension {
		if len(SVG(kind)) == 0 {
			t.Fatalf("%s refers to missing SVG %q", ext, kind)
		}
	}
}
