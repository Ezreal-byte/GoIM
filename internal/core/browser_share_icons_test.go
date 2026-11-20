package core

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"goim/internal/fileicon"
)

func TestBrowserShareUsesBundledTypeIcons(t *testing.T) {
	sheet := webFileItem("成员清单.ODS", "", 16, time.Time{}, "/download")
	pdf := webFileItem("guide", "application/pdf", 16, time.Time{}, "/download")
	unknown := webFileItem("blob.unknown", "", 1, time.Time{}, "/blob")
	source := webFileItem("server.go", "", 32, time.Time{}, "/source")
	manifest := webFileItem("package.json", "", 64, time.Time{}, "/manifest")
	for _, page := range []sharePageData{
		{Title: "project", Ready: true, Items: []sharePageItem{sheet, sheet, pdf, source, manifest, unknown}},
		{Title: "directory", Folder: true, Ready: true, Items: []sharePageItem{sheet, pdf, source, manifest, unknown}},
		{Device: "device", Catalog: []deviceShareEntry{{sharePageItem: sheet}, {sharePageItem: pdf}, {sharePageItem: source}, {sharePageItem: manifest}, {sharePageItem: unknown}}},
	} {
		var body bytes.Buffer
		if err := sharePage.Execute(&body, page); err != nil {
			t.Fatal(err)
		}
		html := body.String()
		for _, kind := range []string{"pdf", "table", "go", "nodejs", "file"} {
			if !strings.Contains(html, "file-type type-"+kind) ||
				strings.Count(html, base64.StdEncoding.EncodeToString(fileicon.SVG(kind))) != 1 {
				t.Fatalf("missing or repeated %s icon", kind)
			}
		}
		if strings.Contains(html, "ZgotmplZ") {
			t.Fatal("browser rejected an icon style or URL")
		}
	}
}
