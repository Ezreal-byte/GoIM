package shareclient

import (
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// 0.1.17 exposes the same entries as accessible links. Read only its
// directory metadata. Never evaluate markup or follow arbitrary links.
func legacyPage(r io.Reader) (Page, error) {
	var p Page
	z := html.NewTokenizer(r)
	z.SetMaxBuf(1 << 20)
	var heading, sizeText bool
	last := -1
	for {
		switch z.Next() {
		case html.ErrorToken:
			if z.Err() != io.EOF {
				return Page{}, z.Err()
			}
			if !p.Locked && p.Title == "" {
				return Page{}, errors.New("missing project heading")
			}
			return p, nil
		case html.TextToken:
			if heading {
				p.Title += string(z.Text())
			}
			if sizeText && last >= 0 {
				p.Items[last].Size += string(z.Text())
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == "h1" {
				heading = false
			}
			if string(name) == "span" {
				sizeText = false
			}
			if string(name) == "a" {
				last = -1
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			var href, label, class, typ string
			for _, a := range t.Attr {
				switch a.Key {
				case "href":
					href = a.Val
				case "aria-label":
					label = a.Val
				case "class":
					class = a.Val
				case "type":
					typ = a.Val
				}
			}
			switch t.Data {
			case "h1":
				heading = true
			case "input":
				if typ == "password" {
					p.Locked = true
				}
			case "span":
				sizeText = class == "size"
			case "a":
				if class == "parent-row" {
					p.ParentURL = href
					continue
				}
				folder := strings.HasPrefix(label, "打开文件夹：")
				if folder || strings.HasPrefix(label, "下载文件：") {
					_, name, _ := strings.Cut(label, "：")
					p.Items = append(p.Items, Entry{Name: name, Folder: folder, URL: href})
					last = len(p.Items) - 1
				}
			}
		}
	}
}
