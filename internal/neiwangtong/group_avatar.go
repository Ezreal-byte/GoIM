package neiwangtong

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"
)

type groupPhotoFields struct {
	ID          string  `xml:"QGROUP_ID"`
	OwnerID     string  `xml:"QGROUP_MASTER"`
	InfoVersion uint64  `xml:"QGROUP_INFO_VER"`
	PhotoMD5    *string `xml:"QGROUP_PHOTO_MD5"`
}

func parseGroupPhoto(data []byte, peerID string) (groupPhotoFields, bool) {
	var wire struct {
		Info groupPhotoFields `xml:"INFO"`
	}
	data = bytes.ReplaceAll(data, []byte("</GROUP_INFO_VER>"), []byte("</QGROUP_INFO_VER>"))
	if err := decodeNativeXML(data, "X_QGROUP_PUSH_INFO", &wire); err != nil {
		return groupPhotoFields{}, false
	}
	info := wire.Info
	info.ID = strings.ToLower(info.ID)
	if !validNativeID(info.ID) || !strings.EqualFold(info.OwnerID, peerID) || info.InfoVersion == 0 || info.PhotoMD5 == nil {
		return groupPhotoFields{}, false
	}
	hash := strings.ToLower(strings.TrimSpace(*info.PhotoMD5))
	info.PhotoMD5 = &hash
	return info, hash == "" || validNativeID(hash)
}

// Stock group photos use selector zero and the advertised PNG MD5 as a
// UTF-16LE name in the ordinary 500-byte media-open record.
const groupPhotoNameOffset = 160

func groupAvatarRequest(digest string) []byte {
	open := transferOpenRequest(0)
	for i := range len(digest) {
		open[groupPhotoNameOffset+2*i] = digest[i]
	}
	return open
}

func groupAvatarDigest(open []byte) string {
	if len(open) != 500 {
		return ""
	}
	var name [md5.Size * 2]byte
	for i := range name {
		c := binary.LittleEndian.Uint16(open[groupPhotoNameOffset+2*i:])
		if c > 127 {
			return ""
		}
		name[i] = byte(c)
	}
	digest := string(name[:])
	if binary.LittleEndian.Uint16(open[groupPhotoNameOffset+2*len(name):]) != 0 || !validNativeID(digest) {
		return ""
	}
	return strings.ToLower(digest)
}

func matchesGroupPhoto(data []byte, digest string) bool {
	if len(data) == 0 || len(data) > avatarMaxBytes {
		return false
	}
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:]) == digest
}

func (s *Service) FetchGroupAvatar(ctx context.Context, peerID, photoMD5 string) ([]byte, error) {
	if !validNativeID(photoMD5) {
		return nil, errors.New("invalid native group photo digest")
	}
	_, tcp, err := s.control(peerID)
	if err != nil {
		return nil, err
	}
	digest := strings.ToLower(photoMD5)
	data, err := fetchMediaRequest(ctx, s.cfg.BindIP, tcp, 0, avatarMaxBytes, 5*time.Second, groupAvatarRequest(digest))
	if err != nil {
		return nil, err
	}
	if !matchesGroupPhoto(data, digest) {
		return nil, errors.New("native group photo digest mismatch")
	}
	return validateAvatar(data)
}

func (s *Service) groupAvatarForPeer(ip netip.Addr, digest string) []byte {
	if s.cfg.Callbacks.GroupAvatar == nil {
		return nil
	}
	// The photo request carries a digest without a member or group ID.
	// Only resolve it for an unambiguous, online control peer.
	s.mu.Lock()
	var peerID string
	for id, p := range s.peers {
		if p.peer.Online && p.control.Addr() == ip {
			if peerID != "" {
				s.mu.Unlock()
				return nil
			}
			peerID = id
		}
	}
	s.mu.Unlock()
	if peerID == "" {
		return nil
	}
	return s.cfg.Callbacks.GroupAvatar(peerID, digest)
}

func (s *TransferServer) serveGroupAvatar(conn net.Conn, open []byte) bool {
	digest := groupAvatarDigest(open)
	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if digest == "" || !ok || s.groupAvatar == nil {
		return false
	}
	ip := remote.AddrPort().Addr().Unmap()
	data := s.groupAvatar(ip, digest)
	if !matchesGroupPhoto(data, digest) {
		return false
	}
	if err := writeTransferConfirmation(conn, 0, int64(len(data)), time.Time{}); err != nil {
		return false
	}
	next, err := readNativeTCPFrame(conn, 108)
	if err != nil || len(next) != 108 || binary.BigEndian.Uint32(next[8:]) != 3 ||
		binary.BigEndian.Uint32(next[selectorOffset:]) != 0 || binary.BigEndian.Uint64(next[60:]) != 0 {
		return false
	}
	// A member can be removed, or a picture replaced, between confirmation
	// and download. Ask for the currently authorized image again.
	data = s.groupAvatar(ip, digest)
	if !matchesGroupPhoto(data, digest) {
		return false
	}
	if _, err := validateAvatar(data); err != nil {
		return false
	}
	frame := make([]byte, nativeFileHeader+len(data))
	encodeFileHeader(frame, 0, int64(len(data)), 0, uint32(len(data)))
	copy(frame[nativeFileHeader:], data)
	_, err = conn.Write(frame)
	return err == nil
}
