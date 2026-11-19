package selfupdate

import (
	"net/url"
	"testing"
)

func TestManualWindowsProxyUsesHTTPSMappingAndSystemExceptions(t *testing.T) {
	u, err := url.Parse(latestReleaseURL)
	if err != nil {
		t.Fatal(err)
	}
	const server = "http=127.0.0.1:8080; HTTPS=127.0.0.1:8081"
	proxy := manualWindowsProxy(u, server, "*.local;<local>")
	if proxy == nil || proxy.String() != "http://127.0.0.1:8081" {
		t.Fatalf("HTTPS proxy = %v", proxy)
	}
	if proxy = manualWindowsProxy(u, server, "*.local;GITHUB.com"); proxy != nil {
		t.Fatalf("GitHub bypass used proxy %v", proxy)
	}
}
