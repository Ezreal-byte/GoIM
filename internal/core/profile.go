package core

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"goim/internal/domain"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func avatarPNG(source image.Image) ([]byte, error) {
	size := 192
	target := image.NewRGBA(image.Rect(0, 0, size, size))
	bounds := source.Bounds()
	edge := min(bounds.Dx(), bounds.Dy())
	left := (bounds.Dx() - edge) / 2
	top := (bounds.Dy() - edge) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			target.Set(x, y, source.At(bounds.Min.X+left+x*edge/size, bounds.Min.Y+top+y*edge/size))
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, target); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func readAvatar(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8<<20 {
		return nil, errors.New("头像文件不能超过 8 MB")
	}
	return data, nil
}
func (n *Node) cacheAvatar(id string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	if len(data) > 1<<20 {
		return "", errors.New("联系人头像文件过大")
	}
	source, err := decodePersonalImage(bytes.NewReader(data), 512*512)
	if err != nil {
		return "", err
	}
	return n.writeAvatar(id, source)
}

func (n *Node) writeAvatar(id string, source image.Image) (string, error) {
	canonical, err := avatarPNG(source)
	if err != nil {
		return "", err
	}
	return n.writeAsset("avatars", id+"-", ".png", canonical)
}

func (n *Node) writeAsset(folder, prefix, ext string, data []byte) (string, error) {
	dir := filepath.Join(n.dir, folder)
	var err error
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	path := filepath.Join(dir, fmt.Sprintf("%s%x%s", prefix, hash[:12], ext))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	tmp, err := os.CreateTemp(dir, ".avatar-*")
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
		if _, statErr := os.Stat(path); statErr == nil {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func decodePersonalImage(r io.ReadSeeker, maxPixels int64) (image.Image, error) {
	c, format, err := image.DecodeConfig(r)
	if err != nil {
		return nil, errors.New("无法读取图片，支持 PNG、JPEG 和 GIF")
	}
	// A compressed file size is not a decode memory bound. PNG may expand to
	// eight bytes per pixel; JPEG decoding normally uses a smaller YCbCr buffer.
	if format == "png" || format == "gif" {
		maxPixels = min(maxPixels, 8_000_000)
	}
	if c.Width <= 0 || c.Height <= 0 || c.Width > 8192 || c.Height > 8192 || int64(c.Width)*int64(c.Height) > maxPixels {
		return nil, errors.New("图片像素过多，请选择尺寸更小的图片")
	}
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(r)
	return img, err
}

func openPersonalImage(path string, maxBytes int64) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxBytes {
		return nil, fmt.Errorf("请选择不超过 %d MB 的图片文件", maxBytes>>20)
	}
	return decodePersonalImage(io.NewSectionReader(f, 0, st.Size()), 16_000_000)
}

func (n *Node) importBackground(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	source, err := openPersonalImage(path, 24<<20)
	if err != nil {
		return "", err
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	factor := min(1, float64(1920)/float64(max(w, h)))
	width, height := max(1, int(float64(w)*factor)), max(1, int(float64(h)*factor))
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			target.Set(x, y, source.At(bounds.Min.X+x*w/width, bounds.Min.Y+y*h/height))
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, target, &jpeg.Options{Quality: 86}); err != nil {
		return "", err
	}
	return n.writeAsset("backgrounds", "", ".jpg", b.Bytes())
}
func (n *Node) profileEnvelope() envelope {
	s := n.snapshot(false)
	p := s.Self
	p.Avatar = ""
	p.Address = n.advertisedAddress()
	var imageData []byte
	if s.Settings.Avatar != "" {
		b, err := readAvatar(s.Settings.Avatar)
		if err == nil && len(b) < 1<<20 {
			imageData = b
		}
	}
	return envelope{Type: "profile", Peer: p, Avatar: imageData}
}
func (n *Node) setProfile(name, path string) error {
	return n.setProfilePixels(name, path, nil, 0)
}
func (n *Node) setProfilePixels(name, path string, preview []byte, size int) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return errors.New("昵称需为 1 至 40 个字符")
	}
	s := n.snapshot(false)
	avatar := s.Settings.Avatar
	if path != "" && path != avatar {
		var source image.Image
		var err error
		if len(preview) > 0 {
			if size < 1 || size > 256 || len(preview) != size*size*4 {
				return errors.New("头像预览已失效，请重新选择")
			}
			pixels := image.NewRGBA(image.Rect(0, 0, size, size))
			for i := 0; i < len(preview); i += 4 {
				pixels.Pix[i], pixels.Pix[i+1], pixels.Pix[i+2], pixels.Pix[i+3] =
					preview[i+2], preview[i+1], preview[i], preview[i+3]
			}
			source = pixels
		} else {
			source, err = openPersonalImage(path, 8<<20)
			if err != nil {
				return err
			}
		}
		avatar, err = n.writeAvatar(s.Self.ID, source)
		if err != nil {
			return err
		}
	}
	if err := n.durable(func(p *persisted) []pending {
		p.State.Settings.Nickname = name
		p.State.Self.Name = name
		p.State.Settings.Avatar = avatar
		p.State.Self.Avatar = avatar
		p.State.Notice = "个人资料已保存"
		return nil
	}); err != nil {
		return err
	}
	n.broadcastProfile()
	return nil
}
func (n *Node) receiveProfile(peer string, e envelope) error {
	if e.Peer.ID != peer || len(e.Peer.Name) > 160 || strings.TrimSpace(e.Peer.Name) == "" {
		return errors.New("联系人资料无效")
	}
	avatar := ""
	var err error
	if len(e.Avatar) > 0 {
		avatar, err = n.cacheAvatar(peer, e.Avatar)
		if err != nil {
			return err
		}
	}
	return n.change(func(s *domain.State) {
		if i := peerIndex(s, peer); i >= 0 {
			s.Peers[i].Name = e.Peer.Name
			if avatar != "" {
				s.Peers[i].Avatar = avatar
			}
			for j := range s.Conversations {
				if s.Conversations[j].PeerID == peer {
					s.Conversations[j].Title = e.Peer.Name
				}
			}
		}
	})
}
