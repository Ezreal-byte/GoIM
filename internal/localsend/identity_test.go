package localsend

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentityPreservesExistingCertificate(t *testing.T) {
	dir := t.TempDir()
	created, err := identity(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := identity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(created.Certificate[0]) != fingerprint(loaded.Certificate[0]) {
		t.Fatal("LocalSend fingerprint changed after reload")
	}
}

func TestIdentityRejectsUnreadableCertificateWithoutRewriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, identityFilename)
	const original = "invalid identity"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := identity(dir); err == nil {
		t.Fatal("invalid identity accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatalf("existing identity was changed: %q, %v", data, err)
	}
}

func TestLocalSendHTTPSChecksPresentedClientCertificates(t *testing.T) {
	receiver := newFixture(t, "receiver", false, false)
	now := time.Now()
	for _, tc := range []struct {
		name                string
		notBefore, notAfter time.Time
		externalSigner      bool
		present, accepted   bool
	}{
		{name: "legacy clients without certificates", accepted: true},
		{name: "current clients with valid self-signed certificates", notBefore: now.Add(-time.Hour), notAfter: now.Add(time.Hour), present: true, accepted: true},
		{name: "expired certificates", notBefore: now.Add(-2 * time.Hour), notAfter: now.Add(-time.Hour), present: true},
		{name: "certificates signed with another key", notBefore: now.Add(-time.Hour), notAfter: now.Add(time.Hour), present: true, externalSigner: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cert tls.Certificate
			if tc.present {
				public, private, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				template := &x509.Certificate{
					SerialNumber: big.NewInt(1),
					Subject:      pkix.Name{CommonName: "LocalSend test peer"},
					NotBefore:    tc.notBefore, NotAfter: tc.notAfter,
					KeyUsage: x509.KeyUsageDigitalSignature,
				}
				signer := private
				if tc.externalSigner {
					_, signer, err = ed25519.GenerateKey(rand.Reader)
					if err != nil {
						t.Fatal(err)
					}
				}
				der, err := x509.CreateCertificate(rand.Reader, template, template, public, signer)
				if err != nil {
					t.Fatal(err)
				}
				cert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}
			}
			client := &http.Client{
				Timeout: 3 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: clientTLS(cert, receiver.fingerprint, nil),
				},
			}
			defer client.CloseIdleConnections()
			resp, err := client.Get("https://" + receiver.address() + prefix + "info")
			if err != nil {
				if tc.accepted {
					t.Fatal(err)
				}
				return
			}
			defer resp.Body.Close()
			if !tc.accepted || resp.StatusCode != http.StatusOK {
				t.Fatalf("unexpected HTTP status %d, want accepted %v", resp.StatusCode, tc.accepted)
			}
		})
	}
}
