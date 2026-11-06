package core

import (
	"bytes"
	"errors"
	"goim/internal/domain"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func avatarPNG(data []byte) ([]byte, error) {
	if len(data) > 8<<20 {
		return nil, errors.New("头像文件不能超过 8 MB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 24_000_000 {
		return nil, errors.New("头像尺寸过大")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
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
	if err = png.Encode(&b, target); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (n *Node) cacheAvatar(id string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	canonical, err := avatarPNG(data)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(n.dir, "avatars")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+".png")
	tmp, err := os.CreateTemp(dir, ".avatar-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(canonical); err != nil {
		tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}
func (n *Node) profileEnvelope() envelope {
	s := n.Snapshot()
	p := s.Self
	p.Avatar = ""
	p.Address = n.advertisedAddress()
	var imageData []byte
	if s.Settings.Avatar != "" {
		b, err := os.ReadFile(s.Settings.Avatar)
		if err == nil && len(b) < 1<<20 {
			imageData = b
		}
	}
	return envelope{Type: "profile", Peer: p, Avatar: imageData}
}
func (n *Node) setProfile(name, path string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return errors.New("昵称需为 1 至 40 个字符")
	}
	s := n.Snapshot()
	avatar := s.Settings.Avatar
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		avatar, err = n.cacheAvatar(s.Self.ID, data)
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
	n.change(func(s *domain.State) {
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
	return nil
}
