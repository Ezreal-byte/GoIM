package localsend

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/filemove"
)

const identityFilename = "localsend-tls.pem"

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func certFingerprint(r *http.Request) string {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		return fingerprint(r.TLS.PeerCertificates[0].Raw)
	}
	return ""
}

func identity(dir string) (tls.Certificate, error) {
	path := filepath.Join(dir, identityFilename)
	cert, err := readIdentity(path)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return cert, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial := new(big.Int).SetBytes(randomBytes(16))
	serial.SetBit(serial, 0, 1)
	now := time.Now()
	template := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "LocalSend User"},
		SerialNumber: serial,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := saveIdentity(path, der, key); err != nil && !errors.Is(err, os.ErrExist) {
		return tls.Certificate{}, fmt.Errorf("无法保存 LocalSend 本机证书: %w", err)
	}
	// Reuse the identity installed by the first successful writer.
	return readIdentity(path)
}

func readIdentity(path string) (tls.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("LocalSend 本机证书或私钥无效: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("无法解析 LocalSend 本机证书: %w", err)
	}
	if err := verifyLocalSendCertificate(leaf); err != nil {
		return tls.Certificate{}, fmt.Errorf("LocalSend 本机证书无效: %w", err)
	}
	cert.Leaf = leaf
	return cert, nil
}

func saveIdentity(path string, der []byte, key *rsa.PrivateKey) error {
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	data := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})...)
	// Keep staging files in the app-owned cache so a user-data reset also
	// removes any private keys left by an interrupted write.
	stageDir := filepath.Join(filepath.Dir(path), "cache")
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(stageDir, ".localsend-tls-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return filemove.Exclusive(f.Name(), path)
}

func verifyLocalSendCertificate(leaf *x509.Certificate) error {
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return errors.New("LocalSend 设备证书已过期或尚未生效")
	}
	if err := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		return errors.New("LocalSend 设备证书签名无效")
	}
	return nil
}

func clientTLS(cert tls.Certificate, expected string, observed *string) *tls.Config {
	// LocalSend addresses peers by IP and uses self-signed device certificates.
	// VerifyConnection checks the device certificate and optional protocol pin
	// before any HTTP data is sent.
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &cert, nil
		},
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("LocalSend 设备未提供证书")
			}
			leaf := state.PeerCertificates[0]
			if err := verifyLocalSendCertificate(leaf); err != nil {
				return err
			}
			actual := fingerprint(leaf.Raw)
			if expected != "" && !strings.EqualFold(expected, actual) {
				return errors.New("LocalSend 设备证书已变更")
			}
			if observed != nil {
				*observed = actual
			}
			return nil
		},
	}
}

func serverTLS(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		// Older native clients may omit their certificate. The transfer offer
		// still requires local approval, and the upload session remains scoped
		// to its source address.
		ClientAuth: tls.RequestClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return nil
			}
			return verifyLocalSendCertificate(state.PeerCertificates[0])
		},
	}
}
