// Package debupdate authenticates the Debian package on both sides of Polkit.
package debupdate

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	Helper     = "/usr/lib/goim/deb-update"
	Executable = "/usr/bin/GoIM"
	MaxSize    = 512 << 20
)

// Package describes one stable release. The signature binds the file to GoIM,
// the release channel, the architecture and the version, including at install time.
type Package struct {
	Version   string `json:"version"`
	Arch      string `json:"arch"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature []byte `json:"signature"`
}

func MetadataName(arch string) string { return "goim-update.json.deb-" + arch }

var stable = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func (p Package) SignedBytes() []byte {
	return []byte(fmt.Sprintf("dev.goim.desktop/stable/deb/v1\n%s\n%s\n%d\n%s\n", p.Version, p.Arch, p.Size, p.SHA256))
}

func (p Package) Verify(key ed25519.PublicKey, version, arch string) error {
	digest, err := hex.DecodeString(p.SHA256)
	if err != nil || len(digest) != sha256.Size || p.Size <= 0 || p.Size > MaxSize ||
		p.Version != version || len(version) > 32 || !stable.MatchString(version) ||
		p.Arch != arch || (arch != "amd64" && arch != "arm64") ||
		len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, p.SignedBytes(), p.Signature) {
		return errors.New("invalid signed GoIM Debian package metadata")
	}
	return nil
}

// Stage copies at most the signed length plus one byte. Only the private copy
// that passed this check may be installed by dpkg.
func (p Package) Stage(dst io.Writer, src io.Reader) error {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, p.Size+1))
	if err != nil {
		return err
	}
	if n != p.Size || hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return errors.New("Debian package length or digest mismatch")
	}
	return nil
}
