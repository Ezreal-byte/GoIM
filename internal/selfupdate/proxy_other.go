//go:build !windows

package selfupdate

import "net/http"

func updateTransport() http.RoundTripper { return http.DefaultTransport }
