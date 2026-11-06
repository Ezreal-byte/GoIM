package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func keyID(key []byte) string { h := sha256.Sum256(key); return hex.EncodeToString(h[:16]) }
func loadIdentity(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, "identity.key")
	b, err := os.ReadFile(path)
	if err == nil {
		if len(b) != ed25519.SeedSize {
			return nil, errors.New("本地身份文件无效")
		}
		return ed25519.NewKeyFromSeed(b), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(key.Seed())
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return key, nil
}

func tlsIdentity(key ed25519.PrivateKey) (*tls.Config, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}))
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAnyClientCert, InsecureSkipVerify: true, // Peer identity is verified against its Ed25519 certificate below, not a public CA.
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) != 1 {
				return errors.New("无效的设备证书链")
			}
			c, e := x509.ParseCertificate(raw[0])
			if e != nil {
				return e
			}
			if _, ok := c.PublicKey.(ed25519.PublicKey); !ok {
				return errors.New("设备必须使用 Ed25519 身份")
			}
			if time.Now().Before(c.NotBefore) || time.Now().After(c.NotAfter) {
				return errors.New("设备证书已过期")
			}
			return c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature)
		}}, nil
}
