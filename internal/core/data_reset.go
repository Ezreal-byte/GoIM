package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

const resetMarker = ".goim-reset-pending"
const resetMarkerContent = "GoIM user data reset v1\n"

var ownedDataEntries = []string{
	"goim.db", "identity.key", "avatars", "backgrounds", "chat-images", "group-avatars",
	"partials", "nwt-partials", "print-jobs", "diagnostics", "cache",
}

func lockDataDirectory(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, ".goim-instance.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockDataFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("此数据目录已有 GoIM 实例运行，请先退出：%w", err)
	}
	return file, nil
}

func requestDataReset(directory string) error {
	file, err := os.CreateTemp(directory, ".goim-reset-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(resetMarkerContent)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, resetMarker))
}

func completePendingDataReset(directory string) error {
	marker := filepath.Join(directory, resetMarker)
	content, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(content) != resetMarkerContent {
		return errors.New("数据清除标记无效，未删除任何数据")
	}
	root, err := filepath.Abs(directory)
	if err != nil || filepath.Dir(root) == root {
		return errors.New("数据目录不安全，未删除任何数据")
	}
	databasePath := filepath.Join(root, "goim.db")
	if _, err := os.Stat(databasePath); err == nil {
		database, err := bolt.Open(databasePath, 0o600, &bolt.Options{Timeout: time.Second})
		if err != nil {
			return fmt.Errorf("请先退出使用此目录的其他 GoIM 实例，再重新启动以清除数据：%w", err)
		}
		if err := database.Close(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, name := range ownedDataEntries {
		target := filepath.Join(root, name)
		if filepath.Dir(target) != root {
			return errors.New("数据清除目标超出应用数据目录")
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("无法清除本地数据，请退出后重试：%w", err)
		}
	}
	return os.Remove(marker)
}
