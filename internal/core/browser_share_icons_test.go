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
	sheet := webFileItem("成员清单.ODS", 16, time.Time{}, "/download")
	pdf := webFileItem("guide.PDF", 16, time.Time{}, "/download")
	for _, page := range []sharePageData{
		{Title: "project", Ready: true, Items: []sharePageItem{sheet, sheet, pdf}},
		{Title: "directory", Folder: true, Ready: true, Items: []sharePageItem{sheet, pdf}},
		{Device: "device", Catalog: []deviceShareEntry{{sharePageItem: sheet}, {sharePageItem: pdf}}},
	} {
		var body bytes.Buffer
		if err := sharePage.Execute(&body, page); err != nil {
			t.Fatal(err)
		}
		html := body.String()
		for _, kind := range []string{"pdf", "table"} {
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
