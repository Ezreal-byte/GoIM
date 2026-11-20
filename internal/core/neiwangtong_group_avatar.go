package core

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

func (n *Node) setNativeGroupAvatar(groupID string, c domain.Command) error {
	path, hash, digest, err := n.prepareGroupAvatar(c)
	if err != nil {
		return err
	}
	var denied error
	err = n.durable(func(p *persisted) []pending {
		group := p.State.Group(groupID)
		room := p.NativeGroups[groupID]
		if room.WireID == "" || group.Left || group.Dissolved || group.OwnerID != p.State.Self.ID {
			denied = errors.New("只有群主可以执行此操作")
			return nil
		}
		if group.AvatarHash == hash && group.AvatarMD5 == digest {
			return nil
		}
		room.InfoVersion++
		room.PhotoVersion = room.InfoVersion
		p.NativeGroups[groupID] = room
		group.Avatar, group.AvatarHash, group.AvatarMD5 = path, hash, digest
		group.Epoch = max(room.InfoVersion, room.MembersVersion)
		installGroupState(&p.State, group)
		p.State.Notice = "群头像已更新"
		return nativeGroupDeliveries(group, nativeGroupPhotoDelivery)
	})
	return errors.Join(err, denied)
}

// The native file request names a photo digest but no group. Only the
// current photo of a group we own and the requesting peer still belongs to
// can be served, even if the old image is present in the disk cache.
func (n *Node) serveNativeGroupAvatar(peer, digest string) []byte {
	if !validGroupAvatarMD5(digest) {
		return nil
	}
	n.mu.Lock()
	var group domain.Group
	member := n.compatibilitySenderLocked(transportNWT, peer).ID
	if !n.closing && member != "" {
		for _, g := range n.data.State.Groups {
			if g.Transport == transportNWT && g.OwnerID == n.data.State.Self.ID && !g.Left && !g.Dissolved &&
				g.AvatarMD5 == digest && contains(g.Members, member) && contains(g.Members, g.OwnerID) {
				group = g
				break
			}
		}
	}
	n.mu.Unlock()
	group.Avatar = n.localGroupAvatar(group.AvatarHash)
	return groupAvatarBytes(group)
}

func (n *Node) receiveNativeGroupPhoto(peer, roomID, digest string, version uint64) {
	if version == 0 || digest != "" && !validGroupAvatarMD5(digest) {
		return
	}
	id := nativeGroupID(roomID)
	n.mu.Lock()
	accept := n.canUpdateNativePhoto(peer, id, version)
	n.mu.Unlock()
	if !accept {
		return
	}
	err := n.durableFromReader(func(p *persisted) []pending {
		room, g := p.NativeGroups[id], p.State.Group(id)
		if !n.canUpdateNativePhoto(peer, id, version) {
			return nil
		}
		room.PhotoVersion = version
		p.NativeGroups[id] = room
		if g.AvatarMD5 != digest {
			g.AvatarMD5, g.AvatarHash, g.Avatar = digest, "", ""
			p.State.Groups[groupIndex(&p.State, id)] = g
		}
		return nil
	})
	if err == nil {
		n.fetchNativeGroupAvatar(peer, id)
	}
}

// Caller holds n.mu.
func (n *Node) canUpdateNativePhoto(peer, id string, version uint64) bool {
	room, g := n.data.NativeGroups[id], n.data.State.Group(id)
	owner := n.compatibilitySenderLocked(transportNWT, peer)
	return room.WireID != "" && room.Owner == peer && owner.ID != "" && g.OwnerID == owner.ID &&
		!g.Left && !g.Dissolved && contains(g.Members, n.data.State.Self.ID) &&
		version >= room.InfoVersion && version > room.PhotoVersion
}

type nativeAvatarFetch struct {
	service             *neiwangtong.Service
	peer, group, digest string
}

type nativeAvatarDownload struct {
	request nativeAvatarFetch
	cancel  context.CancelFunc
}

// Caller holds n.mu. A late response must not replace a newer image or
// resurrect the old one after a clear, membership change or service restart.
func (n *Node) missingNativeAvatar(request nativeAvatarFetch) bool {
	group := n.data.State.Group(request.group)
	return !n.closing && n.neiwangtong == request.service && n.data.NativeGroups[group.ID].Owner == request.peer &&
		group.OwnerID != n.data.State.Self.ID && !group.Left && !group.Dissolved && contains(group.Members, n.data.State.Self.ID) &&
		group.AvatarMD5 == request.digest && group.Avatar == ""
}

func (n *Node) fetchNativeGroupAvatar(peer, group string) {
	n.mu.Lock()
	if n.data.NativeGroups[group].Owner != peer {
		n.mu.Unlock()
		return
	}
	req := nativeAvatarFetch{n.neiwangtong, peer, group, n.data.State.Group(group).AvatarMD5}
	needed := req.service != nil && validGroupAvatarMD5(req.digest) && n.missingNativeAvatar(req)
	if current := n.nativeAvatarFetches[group]; current != nil {
		if needed && current.request == req {
			n.mu.Unlock()
			return
		}
		current.cancel()
		delete(n.nativeAvatarFetches, group)
	}
	if !needed {
		n.mu.Unlock()
		return
	}
	if n.nativeAvatarFetches == nil {
		n.nativeAvatarFetches = make(map[string]*nativeAvatarDownload)
	}
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	job := &nativeAvatarDownload{req, cancel}
	n.nativeAvatarFetches[group] = job
	n.workers.Add(1)
	n.mu.Unlock()
	go func() {
		defer n.workers.Done()
		defer cancel()
		defer func() {
			n.mu.Lock()
			if n.nativeAvatarFetches[group] == job {
				delete(n.nativeAvatarFetches, group)
			}
			n.mu.Unlock()
		}()
		data, err := req.service.FetchGroupAvatar(ctx, req.peer, req.digest)
		if err == nil {
			n.cacheNativeGroupPhoto(req, data)
		}
	}()
}

func (n *Node) cacheNativeGroupPhoto(req nativeAvatarFetch, data []byte) {
	n.mu.Lock()
	current := n.missingNativeAvatar(req)
	n.mu.Unlock()
	if !current {
		return
	}
	if len(data) == 0 || len(data) > groupAvatarMaxBytes {
		return
	}
	wireDigest := md5.Sum(data)
	if hex.EncodeToString(wireDigest[:]) != req.digest {
		return
	}
	source, err := decodePersonalImage(bytes.NewReader(data), 512*512)
	if err != nil {
		return
	}
	canonical, err := avatarPNG(source)
	if err != nil {
		return
	}
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	path, err := n.writeGroupAvatarAsset(hash, canonical)
	if err != nil {
		return
	}
	_ = n.change(func(s *domain.State) {
		if n.missingNativeAvatar(req) {
			group := &s.Groups[groupIndex(s, req.group)]
			group.Avatar, group.AvatarHash = path, hash
		}
	})
}
