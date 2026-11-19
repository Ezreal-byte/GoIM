package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"goim/internal/domain"
)

func TestGroupAvatarIsSignedPersistedAndDelivered(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "成员一"), testNode(t, "成员二")
	id := createTestGroup(t, a, b, c)

	preview := make([]byte, 2*2*4)
	for i := 0; i < len(preview); i += 4 {
		preview[i], preview[i+1], preview[i+2], preview[i+3] = 221, 96, 31, 255
	}
	if err := b.manageGroup(domain.Command{
		Kind: domain.SetGroupAvatar, GroupID: id, AvatarBGRA: preview, AvatarSize: 2,
	}); err == nil {
		t.Fatal("a non-owner changed the group avatar")
	}
	if err := a.manageGroup(domain.Command{
		Kind: domain.SetGroupAvatar, GroupID: id, AvatarBGRA: preview, AvatarSize: 2,
	}); err != nil {
		t.Fatal(err)
	}

	owner := a.Snapshot().Group(id)
	if !validGroupAvatarHash(owner.AvatarHash) || owner.Avatar == "" || !verifyGroup(owner) {
		t.Fatalf("owner group avatar is not signed and cached: %+v", owner)
	}
	data, err := os.ReadFile(owner.Avatar)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != owner.AvatarHash {
		t.Fatal("owner group avatar file does not match its signed digest")
	}

	for _, node := range []*Node{b, c} {
		eventually(t, "group avatar delivery", func() bool {
			group := node.Snapshot().Group(id)
			return group.AvatarHash == owner.AvatarHash && group.Avatar != ""
		})
		group := node.Snapshot().Group(id)
		remote, err := os.ReadFile(group.Avatar)
		if err != nil || !bytes.Equal(remote, data) || !verifyGroup(group) {
			t.Fatalf("received group avatar is invalid: group=%+v err=%v", group, err)
		}
	}

	tampered := owner
	tampered.Epoch++
	signGroup(&tampered, a.key)
	if err := c.receiveGroup(a.Snapshot().Self.ID, envelope{
		Type: "group_state", Group: tampered, GroupAvatar: []byte("not an image"),
	}); err == nil {
		t.Fatal("tampered group avatar bytes were accepted")
	}

	secondPreview := make([]byte, 2*2*4)
	for i := 0; i < len(secondPreview); i += 4 {
		secondPreview[i], secondPreview[i+1], secondPreview[i+2], secondPreview[i+3] = 31, 180, 96, 255
	}
	secondSource, err := imageFromPreview(secondPreview, 2)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := avatarPNG(secondSource)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest := sha256.Sum256(secondData)
	invalidHash := hex.EncodeToString(secondDigest[:])
	invalid := owner
	invalid.Epoch++
	invalid.AvatarHash = invalidHash
	if err := c.receiveGroup(a.Snapshot().Self.ID, envelope{
		Type: "group_state", Group: invalid, GroupAvatar: secondData,
	}); err == nil {
		t.Fatal("a group state with an invalid signature was accepted")
	}
	if _, err := os.Stat(filepath.Join(c.dir, "group-avatars", invalidHash+".png")); !os.IsNotExist(err) {
		t.Fatalf("an unauthenticated group avatar was written to the cache: %v", err)
	}

	dir := a.dir
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testNodeAt(t, "群主", dir)
	restored := reopened.Snapshot().Group(id)
	if restored.AvatarHash != owner.AvatarHash || restored.Avatar != owner.Avatar {
		t.Fatalf("group avatar was not restored after restart: %+v", restored)
	}
}
