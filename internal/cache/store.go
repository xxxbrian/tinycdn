package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"net/http"
	"time"

	"github.com/dgraph-io/badger/v4"
)

func init() {
	gob.Register(http.Header{})
}

type Store interface {
	Get(ctx context.Context, key string) (Entry, bool, error)
	Put(ctx context.Context, key string, entry Entry) error
	Delete(ctx context.Context, key string) error
	Close() error
}

type BadgerStore struct {
	db *badger.DB
}

func NewBadgerStore(path string) (*BadgerStore, error) {
	options := badger.DefaultOptions(path).WithLogger(nil)
	db, err := badger.Open(options)
	if err != nil {
		return nil, err
	}

	return &BadgerStore{db: db}, nil
}

func (s *BadgerStore) Get(ctx context.Context, key string) (Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, false, err
	}

	var payload []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return item.Value(func(value []byte) error {
			payload = append([]byte(nil), value...)
			return nil
		})
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}

	entry, err := decodeEntry(payload)
	if err != nil {
		return Entry{}, false, err
	}

	return entry, true, nil
}

func (s *BadgerStore) Put(ctx context.Context, key string, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	payload, err := encodeEntry(entry)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), payload)
		if !entry.InvalidAt.IsZero() {
			ttl := time.Until(entry.InvalidAt)
			if ttl <= 0 {
				return txn.Delete([]byte(key))
			}
			e = e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	})
}

func (s *BadgerStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

func (s *BadgerStore) Close() error {
	return s.db.Close()
}

func encodeEntry(entry Entry) ([]byte, error) {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(entry); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func decodeEntry(payload []byte) (Entry, error) {
	var entry Entry
	err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&entry)
	return entry, err
}
