package selfupdate

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func updateTransport() http.RoundTripper {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	transport = transport.Clone()
	transport.Proxy = windowsUpdateProxy
	return transport
}

func windowsUpdateProxy(req *http.Request) (*url.URL, error) {
	proxy, err := http.ProxyFromEnvironment(req)
	// An explicit environment proxy takes precedence, including its NO_PROXY
	// exceptions. Desktop launches often only have the Windows setting.
	if err != nil || proxy != nil || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("https_proxy") != "" {
		return proxy, err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer key.Close()
	enabled, _, _ := key.GetIntegerValue("ProxyEnable")
	if enabled == 0 {
		return nil, nil
	}
	server, _, _ := key.GetStringValue("ProxyServer")
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	return manualWindowsProxy(req.URL, server, bypass), nil
}

func manualWindowsProxy(destination *url.URL, server, bypass string) *url.URL {
	host := strings.ToLower(destination.Hostname())
	for _, entry := range strings.Split(bypass, ";") {
		pattern := strings.ToLower(strings.TrimSpace(entry))
		matched, _ := path.Match(pattern, host)
		if matched || (pattern == "<local>" && !strings.Contains(host, ".")) {
			return nil
		}
	}
	if strings.Contains(server, "=") {
		var selected string
		for _, entry := range strings.Split(server, ";") {
			protocol, address, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(strings.TrimSpace(protocol), destination.Scheme) {
				selected = address
				break
			}
		}
		server = selected
	}
	server = strings.TrimSpace(server)
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	proxy, err := url.Parse(server)
	if err != nil || proxy.Hostname() == "" {
		return nil
	}
	switch proxy.Scheme {
	case "http", "https", "socks5", "socks5h":
		return proxy
	default:
		return nil
	}
}
