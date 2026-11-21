package shareclient

import (
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"

	"goim/internal/domain"
)

// Session is passed only in memory between the catalog page and its download.
// Both the member address and resolved project must match before using a cookie.
func (c *Client) Session() domain.ShareSession {
	s := domain.ShareSession{Address: c.origin.String(), Project: c.project}
	u, err := c.target(c.project, false)
	if err == nil {
		for _, cookie := range c.http.Jar.Cookies(u) {
			if cookie.Name == "goim_share" {
				s.Token = cookie.Value
			}
		}
	}
	return s
}
func (c *Client) Restore(s domain.ShareSession) {
	if s.Token == "" || s.Address != c.origin.String() || !projectPath.MatchString(s.Project) {
		return
	}
	u, err := c.target(s.Project, false)
	if err != nil {
		return
	}
	c.project = s.Project
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "goim_share", Value: s.Token, Path: s.Project}})
}

func (e Entry) Index() int {
	u, err := url.Parse(e.URL)
	if err != nil {
		return 0
	}
	key := "file"
	if e.Folder {
		key = "dir"
	}
	value, _ := strconv.Atoi(u.Query().Get(key))
	return value
}
func (p Page) Whole() Entry {
	// A standalone shared file has no numeric entry id, also on 0.1.17.
	if len(p.Items) == 1 && !p.Items[0].Folder && p.Items[0].Index() == 0 {
		return p.Items[0]
	}
	name := p.Title
	if p.ParentURL != "" && p.Current != "" {
		name = p.Current
	}
	return Entry{Name: name, URL: p.Path, Folder: true}
}

type Part struct {
	Entry Entry
	Path  string
}
type Plan struct {
	Parts []Part
	Bytes int64
}

func (c *Client) Plan(ctx context.Context, root Page, name string, index int, folder bool) (Plan, error) {
	var plan Plan
	base := c.project
	if !safeName(name) || index < 0 || base == "" {
		return plan, errors.New("无效的共享项目")
	}
	link := base
	if !folder {
		link += "/download"
	}
	if index > 0 {
		key := "file"
		if folder {
			key = "dir"
		}
		link += "?" + key + "=" + strconv.Itoa(index)
	}
	seen := make(map[string]bool)
	var walk func(Entry, string, int) error
	walk = func(e Entry, path string, depth int) error {
		if depth > 32 || len(plan.Parts) >= 4001 || seen[e.URL] || !safeName(e.Name) {
			return errors.New("无效的共享目录结构")
		}
		seen[e.URL] = true
		if !e.Folder {
			if e.Bytes == nil {
				resp, err := c.openFile(ctx, e, http.MethodHead)
				if err != nil {
					return err
				}
				size := resp.ContentLength
				resp.Body.Close()
				e.Bytes = &size
			}
			if *e.Bytes < 0 || *e.Bytes > math.MaxInt64-plan.Bytes {
				return errors.New("无效的共享文件大小")
			}
			plan.Bytes += *e.Bytes
		}
		plan.Parts = append(plan.Parts, Part{Entry: e, Path: path})
		if !e.Folder {
			return nil
		}
		page := root
		if e.URL != root.Path {
			var err error
			page, err = c.List(ctx, e.URL, "")
			if err != nil {
				return err
			}
		}
		if page.Locked {
			return errors.New("请重新打开共享并输入密码")
		}
		for _, child := range page.Items {
			if !safeName(child.Name) {
				return errors.New("无效的共享文件名")
			}
			if err := walk(child, filepath.Join(path, child.Name), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	selected := Entry{Name: name, Folder: folder, URL: link}
	for _, e := range root.Items {
		if e.URL == link {
			if e.Name != name || e.Folder != folder {
				return plan, errors.New("文件已变化或访问已过期，请重新打开共享")
			}
			selected = e
			break
		}
	}
	err := walk(selected, "", 0)
	return plan, err
}
func (c *Client) openFile(ctx context.Context, e Entry, method string) (*http.Response, error) {
	u, err := c.target(e.URL, true)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, remoteError(ctx)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || mt != "application/octet-stream" || resp.ContentLength < 0 || resp.Request.URL.String() != u.String() || e.Bytes != nil && *e.Bytes != resp.ContentLength {
		resp.Body.Close()
		return nil, errors.New("文件已变化或访问已过期，请重新打开共享")
	}
	return resp, nil
}
func (c *Client) Copy(ctx context.Context, e Entry, w io.Writer) (int64, error) {
	resp, err := c.openFile(ctx, e, http.MethodGet)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, err := io.Copy(w, resp.Body)
	if err == nil && n != resp.ContentLength {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
