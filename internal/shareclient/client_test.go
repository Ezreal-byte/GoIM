package shareclient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyFolderNavigationAndPartialDownload(t *testing.T) {
	member := "/m/" + strings.Repeat("b", 32)
	project := "/s/" + strings.Repeat("a", 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case member:
			http.Redirect(w, r, project, http.StatusFound)
		case project:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.URL.RawQuery == "" {
				fmt.Fprintf(w, `<h1>项目</h1><a class="entry" href="%s?dir=1" aria-label="打开文件夹：子目录"></a>`, project)
				return
			}
			fmt.Fprintf(w, `<h1>子目录</h1><a class="parent-row" href="%s">返回</a><a href="%s/download?file=2" aria-label="下载文件：表格.xlsx"><span class="size">5 B</span></a>`, project, project)
		case project + "/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "5")
			fmt.Fprint(w, "bad")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL + member)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root, err := c.List(t.Context(), "", "")
	if err != nil || len(root.Items) != 1 || !root.Items[0].Folder {
		t.Fatal(root, err)
	}
	sub, err := c.List(t.Context(), root.Items[0].URL, "")
	if err != nil || len(sub.Items) != 1 || sub.ParentURL != project || sub.Items[0].Size != "5 B" {
		t.Fatal(sub, err)
	}
	plan, err := c.Plan(t.Context(), root, root.Title, 0, true)
	if err != nil || plan.Bytes != 5 || len(plan.Parts) != 3 || plan.Parts[2].Path != filepath.Join("子目录", "表格.xlsx") {
		t.Fatal("legacy folder plan", plan, err)
	}
	if _, err := c.Copy(t.Context(), sub.Items[0], io.Discard); err == nil {
		t.Fatal("accepted partial file")
	}
	for _, ref := range []string{"https://example.invalid", project + "/../secret", "/s/" + strings.Repeat("c", 32) + "/download?file=1", project + "/download?file=-1"} {
		if _, err := c.target(ref, true); err == nil {
			t.Fatalf("accepted %q", ref)
		}
	}
}
