// Package localsend implements the LocalSend v2.2 LAN protocol.
package localsend

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
)

const (
	Port         = domain.LocalSendDefaultPort
	prefix       = "/api/localsend/v2/"
	version      = "2.2"
	maxMetadata  = 2 << 20
	maxItems     = 5000
	maxText      = 128 << 10
	approvalTime = 3 * time.Minute
)

type info struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	Model       string `json:"deviceModel,omitempty"`
	Type        string `json:"deviceType,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Port        int    `json:"port,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Download    bool   `json:"download"`
}

type announcement struct {
	info
	Announce bool `json:"announce"`
}

type fileInfo struct {
	ID       string        `json:"id"`
	Name     string        `json:"fileName"`
	Size     int64         `json:"size"`
	Type     string        `json:"fileType"`
	SHA      string        `json:"sha256,omitempty"`
	Preview  *string       `json:"preview,omitempty"`
	Metadata *fileMetadata `json:"metadata,omitempty"`
}

type fileMetadata struct {
	Modified string `json:"modified,omitempty"`
	Accessed string `json:"accessed,omitempty"`
}

type prepare struct {
	Info  info                `json:"info"`
	Files map[string]fileInfo `json:"files"`
}

type uploadGrant struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"`
}

func newID() string {
	return hex.EncodeToString(randomBytes(16))
}

func randomBytes(size int) []byte {
	b := make([]byte, size)
	rand.Read(b)
	return b
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMetadata))
	if err := dec.Decode(dst); err != nil || dec.Decode(new(json.RawMessage)) != io.EOF {
		fail(w, http.StatusBadRequest, "Invalid request")
		return false
	}
	return true
}

func reply(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, code int, problem string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	reply(w, struct {
		Message string `json:"message"`
	}{problem})
}

func remoteIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func localAddress(raw string, port int) (string, error) {
	raw = strings.TrimSpace(raw)
	var addr netip.Addr
	if endpoint, err := netip.ParseAddrPort(raw); err == nil {
		addr, port = endpoint.Addr(), int(endpoint.Port())
	} else {
		addr, _ = netip.ParseAddr(raw)
	}
	addr = addr.Unmap()
	if !isLAN(addr) || port < 1 || port > 65535 {
		return "", errors.New("请输入有效的局域网 IP 地址和端口")
	}
	return net.JoinHostPort(addr.String(), strconv.Itoa(port)), nil
}

func isLAN(addr netip.Addr) bool {
	return addr.IsValid() && (addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(addr))
}

func validInfo(i info) bool {
	return len(i.Alias) <= 256 && strings.TrimSpace(i.Alias) != "" &&
		strings.HasPrefix(i.Version, "2.") && len(i.Fingerprint) <= 128 &&
		i.Fingerprint != "" && i.Port >= 1 && i.Port <= 65535 &&
		(i.Protocol == "http" || i.Protocol == "https")
}

func validFile(f fileInfo) bool {
	if f.Size < 0 || len(f.Type) > 256 || (f.Preview != nil && len(*f.Preview) > maxText) {
		return false
	}
	if f.SHA != "" {
		if value, err := hex.DecodeString(f.SHA); err != nil || len(value) != sha256.Size {
			return false
		}
	}
	return validName(f.Name)
}

func validName(name string) bool {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || len(component) > 255 || component == "." || component == ".." ||
			strings.TrimRight(component, ". ") != component ||
			strings.ContainsAny(component, "\\:*?\"<>|\x00") || !filepath.IsLocal(component) {
			return false
		}
		for _, r := range component {
			if r < 32 {
				return false
			}
		}
	}
	return true
}

func targetURL(scheme, endpoint, route string, query url.Values) string {
	u := url.URL{Scheme: scheme, Host: endpoint, Path: prefix + route, RawQuery: query.Encode()}
	return u.String()
}

func (s *Service) selfInfo() info {
	s.mu.Lock()
	defer s.mu.Unlock()
	scheme := "https"
	if s.cfg.HTTP {
		scheme = "http"
	}
	return info{Alias: s.alias, Version: version, Model: runtime.GOOS, Type: "desktop",
		Fingerprint: s.fingerprint, Port: s.port, Protocol: scheme}
}

func transferError(status int) error {
	var label string
	switch status {
	case http.StatusUnauthorized:
		label = "对方需要 PIN 或 PIN 不正确"
	case http.StatusForbidden:
		label = "对方已拒绝请求"
	case http.StatusConflict:
		label = "对方正在接收其他文件"
	case http.StatusTooManyRequests:
		label = "PIN 尝试次数过多"
	case http.StatusUnprocessableEntity:
		label = "对方校验文件失败"
	default:
		label = fmt.Sprintf("对端请求失败（HTTP %d）", status)
	}
	return errors.New(label)
}
