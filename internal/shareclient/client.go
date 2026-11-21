// Package shareclient reads GoIM member projects over the address advertised
// by a connected peer. Each open project has an isolated password session.
package shareclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MediaType = "application/vnd.goim.directory+json"
const maxListing = 12 << 20

var projectPath = regexp.MustCompile("^/s/[A-Za-z0-9_-]{32}$")
var memberPath = regexp.MustCompile("^/m/[0-9a-f]{32}$")

type Entry struct {
	Name, Size, URL string
	Folder          bool
	Bytes           *int64 `json:",omitempty"`
}
type Page struct {
	Title, Current, ParentURL, Path string
	Locked                          bool
	Items                           []Entry
}
type Client struct {
	origin  *url.URL
	project string
	http    *http.Client
}

func New(address string) (*Client, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Port() == "" || net.ParseIP(strings.Split(u.Hostname(), "%")[0]) == nil || !memberPath.MatchString(u.Path) || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("共享地址无效")
	}
	jar, _ := cookiejar.New(nil)
	client := &Client{origin: u, http: &http.Client{Jar: jar, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext, ResponseHeaderTimeout: 90 * time.Second, IdleConnTimeout: 30 * time.Second}}}
	client.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.Method != http.MethodGet || !client.allowed(req.URL, false) {
			return errors.New("共享地址已失效")
		}
		return nil
	}
	return client, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) allowed(u *url.URL, download bool) bool {
	if u.Scheme != c.origin.Scheme || u.Host != c.origin.Host || u.User != nil || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	path := u.Path
	key := "dir"
	if download {
		if !strings.HasSuffix(path, "/download") {
			return false
		}
		path = strings.TrimSuffix(path, "/download")
		key = "file"
	}
	if !projectPath.MatchString(path) || c.project != "" && path != c.project {
		return false
	}
	if u.RawQuery == "" {
		return true
	}
	v, err := strconv.Atoi(u.Query().Get(key))
	return err == nil && v > 0 && u.RawQuery == key+"="+strconv.Itoa(v)
}
func (c *Client) target(ref string, download bool) (*url.URL, error) {
	if ref == "" && !download {
		return c.origin, nil
	}
	if !strings.HasPrefix(ref, "/s/") {
		return nil, errors.New("无效的共享项目")
	}
	u, err := c.origin.Parse(ref)
	if err != nil || !c.allowed(u, download) {
		return nil, errors.New("无效的共享项目")
	}
	return u, nil
}
func (c *Client) List(ctx context.Context, ref, password string) (Page, error) {
	var p Page
	u, err := c.target(ref, false)
	if err != nil {
		return p, err
	}
	method := http.MethodGet
	var body io.Reader
	if password != "" {
		if c.project == "" || u.Path != c.project {
			return p, errors.New("请重新打开共享")
		}
		method = http.MethodPost
		body = strings.NewReader(url.Values{"password": {password}}.Encode())
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return p, err
	}
	req.Header.Set("Accept", MediaType)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return p, remoteError(ctx)
	}
	defer resp.Body.Close()
	if !c.allowed(resp.Request.URL, false) {
		return p, errors.New("共享地址已失效")
	}
	base := resp.Request.URL.Path
	if resp.StatusCode == http.StatusUnauthorized {
		return Page{Path: base, Locked: true}, errors.New("密码不正确，请重试。")
	}
	if resp.StatusCode != http.StatusOK {
		return p, fmt.Errorf("共享暂不可用 (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxListing+1))
	if err != nil || len(data) > maxListing {
		return p, errors.New("无法读取共享目录")
	}
	switch t, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); t {
	case MediaType:
		err = json.Unmarshal(data, &p)
	case "text/html":
		p, err = legacyPage(bytes.NewReader(data))
	default:
		err = errors.New("不支持的目录格式")
	}
	if err != nil {
		return Page{}, errors.New("无法读取共享目录")
	}
	c.project = base
	p.Path = resp.Request.URL.RequestURI()
	if p.Locked {
		return Page{Path: p.Path, Locked: true}, nil
	}
	if p.ParentURL != "" {
		if _, err = c.target(p.ParentURL, false); err != nil {
			return Page{}, err
		}
	}
	for _, e := range p.Items {
		if e.URL == "" || strings.TrimSpace(e.Name) == "" {
			return Page{}, errors.New("无效的共享文件名")
		}
		if _, err = c.target(e.URL, !e.Folder); err != nil {
			return Page{}, err
		}
	}
	return p, nil
}
func remoteError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("无法连接共享设备")
}
func safeName(name string) bool {
	if !filepath.IsLocal(name) || name == "." || len(name) > 255 || strings.ContainsAny(name, "/\\:*?\"<>|") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
