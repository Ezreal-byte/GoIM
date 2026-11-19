package core

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"

	"goim/internal/domain"
)

const groupAvatarMaxBytes = 1 << 20

func validGroupAvatarHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(hash) == hash
}

func validGroupAvatarMD5(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == md5.Size && strings.ToLower(hash) == hash
}

func imageFromPreview(preview []byte, size int) (image.Image, error) {
	if size < 1 || size > 256 || len(preview) != size*size*4 {
		return nil, errors.New("头像预览已失效，请重新选择")
	}
	pixels := image.NewRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(preview); i += 4 {
		pixels.Pix[i], pixels.Pix[i+1], pixels.Pix[i+2], pixels.Pix[i+3] =
			preview[i+2], preview[i+1], preview[i], preview[i+3]
	}
	return pixels, nil
}

func (n *Node) prepareGroupAvatar(c domain.Command) (path, avatarHash, avatarMD5 string, err error) {
	if c.RemoveAvatar {
		return "", "", "", nil
	}
	var source image.Image
	switch {
	case len(c.AvatarBGRA) > 0:
		source, err = imageFromPreview(c.AvatarBGRA, c.AvatarSize)
	case strings.TrimSpace(c.Value) != "":
		source, err = openPersonalImage(strings.TrimSpace(c.Value), 8<<20)
	default:
		err = errors.New("请选择群头像图片")
	}
	if err != nil {
		return "", "", "", err
	}
	canonical, err := avatarPNG(source)
	if err != nil {
		return "", "", "", err
	}
	if len(canonical) > groupAvatarMaxBytes {
		return "", "", "", errors.New("群头像文件过大")
	}
	sha := sha256.Sum256(canonical)
	sum := md5.Sum(canonical)
	avatarHash, avatarMD5 = hex.EncodeToString(sha[:]), hex.EncodeToString(sum[:])
	path, err = n.writeGroupAvatarAsset(avatarHash, canonical)
	return path, avatarHash, avatarMD5, err
}

func (n *Node) writeGroupAvatarAsset(hash string, data []byte) (string, error) {
	if !validGroupAvatarHash(hash) {
		return "", errors.New("无效的群头像摘要")
	}
	dir := filepath.Join(n.dir, "group-avatars")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hash+".png")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path, nil
	}
	tmp, err := os.CreateTemp(dir, ".group-avatar-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmpPath, path); err != nil {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func (n *Node) localGroupAvatar(hash string) string {
	if !validGroupAvatarHash(hash) {
		return ""
	}
	path := filepath.Join(n.dir, "group-avatars", hash+".png")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	return ""
}

func (n *Node) hydrateGroupAvatars(s *domain.State) {
	for i := range s.Groups {
		s.Groups[i].Avatar = n.localGroupAvatar(s.Groups[i].AvatarHash)
	}
}

func (n *Node) prepareReceivedGroupAvatar(g domain.Group, data []byte) (domain.Group, error) {
	if g.AvatarHash == "" {
		if len(data) != 0 {
			return g, errors.New("群头像缺少摘要")
		}
		g.Avatar = ""
		return g, nil
	}
	if !validGroupAvatarHash(g.AvatarHash) {
		return g, errors.New("群头像摘要无效")
	}
	g.Avatar = n.localGroupAvatar(g.AvatarHash)
	if len(data) == 0 {
		return g, nil
	}
	if len(data) > groupAvatarMaxBytes {
		return g, errors.New("群头像文件过大")
	}
	source, err := decodePersonalImage(bytes.NewReader(data), 512*512)
	if err != nil {
		return g, err
	}
	canonical, err := avatarPNG(source)
	if err != nil {
		return g, err
	}
	digest := sha256.Sum256(canonical)
	if hex.EncodeToString(digest[:]) != g.AvatarHash {
		return g, errors.New("群头像校验失败")
	}
	path, err := n.writeGroupAvatarAsset(g.AvatarHash, canonical)
	if err != nil {
		return g, err
	}
	g.Avatar = path
	return g, nil
}

func groupAvatarBytes(g domain.Group) []byte {
	if g.Avatar == "" || !validGroupAvatarHash(g.AvatarHash) {
		return nil
	}
	data, err := readAvatar(g.Avatar)
	if err != nil || len(data) > groupAvatarMaxBytes {
		return nil
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != g.AvatarHash {
		return nil
	}
	return data
}
