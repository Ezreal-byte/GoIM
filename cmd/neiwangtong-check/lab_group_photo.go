package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"goim/internal/neiwangtong"
)

type labGroupPhoto struct {
	peer, room, digest string
	version            uint64
	data               []byte
}

type groupPhotoSample struct {
	name string
	fill color.RGBA
	data []byte
}

func (l *labChecks) checkGroupPhotos(ctx context.Context, r *report, svc *neiwangtong.Service, peer string, stockRoom neiwangtong.GroupRoster) bool {
	samples := []groupPhotoSample{
		{name: "red", fill: color.RGBA{R: 216, A: 255}},
		{name: "green", fill: color.RGBA{G: 184, A: 255}},
		{name: "default"},
	}
	dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	if !r.run("prepare_group_photos", func() (string, error) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		for i := range samples[:2] {
			sample := &samples[i]
			file := filepath.Join(dir, "profile-"+sample.name+".png")
			data, err := writeFixtureImage(file, "png", 128, 128, sample.fill)
			if err != nil {
				return "", err
			}
			sample.data = data
		}
		return "created two distinct group photo fixtures", nil
	}) {
		return false
	}
	passed := true
	version := stockRoom.InfoVersion
	for _, sample := range samples {
		ok := r.run("original_group_owner_photo_"+sample.name, func() (string, error) {
			path := "default"
			if sample.data != nil {
				path = `C:\goim-qa\profile-` + sample.name + ".png"
			}
			if err := l.driveFor(ctx, "group-photo-set", stockRoom.Name, path); err != nil {
				return "", err
			}
			event, err := wait(ctx, 7*time.Second, l.groupPhotos, func(event labGroupPhoto) bool {
				return event.peer == peer && event.room == stockRoom.ID && event.version > version
			})
			if err != nil {
				return "", err
			}
			version = event.version
			if sample.data == nil {
				if event.digest != "" {
					return "", errors.New("the original did not clear the group photo digest")
				}
				return "received the original owner's reset to the default group photo", nil
			}
			data, err := svc.FetchGroupAvatar(ctx, peer, event.digest)
			if err != nil {
				return "", err
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				return "", err
			}
			b := img.Bounds()
			red, green, blue, _ := img.At(b.Dx()/2, b.Dy()*3/4).RGBA()
			if sample.name == "red" && !(red > 40000 && green < 8000 && blue < 8000) ||
				sample.name == "green" && !(green > 32000 && red < 8000 && blue < 8000) {
				return "", errors.New("received group photo does not show the fixture color")
			}
			return fmt.Sprintf("received the %s group photo, %d PNG bytes, md5=%s", sample.name, len(data), event.digest), nil
		})
		passed = passed && ok
	}
	roomID := md5.Sum([]byte("photo group " + l.runID))
	room := neiwangtong.GroupRoster{ID: hex.EncodeToString(roomID[:]), Name: "GoIM QA 群聊 " + l.runID,
		OwnerID: l.runID + "0000000000000000", Members: stockRoom.Members, InfoVersion: 2, MembersVersion: 2}
	if !r.run("open_goim_photo_group", func() (string, error) {
		if err := svc.SendGroupRoster(peer, room); err != nil {
			return "", err
		}
		return "original opened the group created by GoIM", l.driveFor(ctx, "group-open", room.Name)
	}) {
		return false
	}
	defer l.servedPhoto.Store(nil)
	for _, sample := range samples {
		ok := r.run("goim_group_owner_photo_"+sample.name, func() (string, error) {
			room.InfoVersion++
			room.PhotoMD5 = ""
			l.servedPhoto.Store(nil)
			if sample.data != nil {
				sum := md5.Sum(sample.data)
				room.PhotoMD5 = hex.EncodeToString(sum[:])
				l.servedPhoto.Store(&labGroupPhoto{peer: peer, digest: room.PhotoMD5, data: sample.data})
			}
			if err := svc.SendGroupPhoto(peer, room); err != nil {
				return "", err
			}
			if sample.data != nil {
				cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "face", room.PhotoMD5)
				if err := waitForCachedImage(ctx, cache, sample.data); err != nil {
					return "", err
				}
			}
			if err := l.driveFor(ctx, "group-photo-check", room.Name, sample.name); err != nil {
				return "", err
			}
			return "original group information displays the " + sample.name + " photo", nil
		})
		passed = passed && ok
	}
	return passed
}
