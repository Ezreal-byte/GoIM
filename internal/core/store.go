package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
	"goim/internal/domain"
)

type pending struct {
	PeerID   string
	Envelope envelope
}
type ownershipOffer struct {
	PeerID    string
	GroupID   string
	Stage     string
	CreatedAt time.Time
}
type persisted struct {
	Version     int
	State       domain.State
	Outbox      map[string]pending
	Seen        map[string]bool
	OwnerOffers map[string]ownershipOffer
}

func openStore(dir string) (*bolt.DB, persisted, error) {
	var data persisted
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, data, err
	}
	db, err := bolt.Open(filepath.Join(dir, "goim.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, data, fmt.Errorf("打开本地数据库（可能已有实例使用此目录）: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b, e := tx.CreateBucketIfNotExists([]byte("app"))
		if e != nil {
			return e
		}
		v := b.Get([]byte("state"))
		if len(v) > 0 {
			return json.Unmarshal(v, &data)
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, data, err
	}
	if data.Version > 1 {
		db.Close()
		return nil, data, fmt.Errorf("数据库版本 %d 高于当前应用支持版本", data.Version)
	}
	data.Version = 1
	if data.OwnerOffers == nil {
		data.OwnerOffers = make(map[string]ownershipOffer)
	}
	if data.Outbox == nil {
		data.Outbox = make(map[string]pending)
	}
	if data.Seen == nil {
		data.Seen = make(map[string]bool)
	}
	return db, data, nil
}

func saveStore(db *bolt.DB, p *persisted) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return db.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte("app")).Put([]byte("state"), data) })
}
