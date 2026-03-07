package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

func init() {
	gob.Register(http.Header{})
}

type VarySpec struct {
	Headers []string
}

type Store interface {
	GetEntry(ctx context.Context, key string) (Entry, bool, error)
	PutEntry(ctx context.Context, key string, entry Entry) error
	GetVary(ctx context.Context, key string) (VarySpec, bool, error)
	PutVary(ctx context.Context, key string, spec VarySpec) error
	ImportBody(ctx context.Context, tempPath string) (string, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) (int, error)
	Close() error
}

type BadgerStore struct {
	db      *badger.DB
	blobDir string
}

func NewBadgerStore(path string) (*BadgerStore, error) {
	options := badger.DefaultOptions(path).WithLogger(nil)
	if err := os.MkdirAll(filepath.Join(path, "blobs"), 0o755); err != nil {
		return nil, err
	}
	db, err := badger.Open(options)
	if err != nil {
		return nil, err
	}

	store := &BadgerStore{db: db, blobDir: filepath.Join(path, "blobs")}
	if err := store.pruneOrphanBodies(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *BadgerStore) GetEntry(ctx context.Context, key string) (Entry, bool, error) {
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

func (s *BadgerStore) PutEntry(ctx context.Context, key string, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	existing, found, err := s.GetEntry(ctx, key)
	if err != nil {
		return err
	}

	payload, err := encodeEntry(entry)
	if err != nil {
		return err
	}

	if err := s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), payload)
		if !entry.InvalidAt.IsZero() {
			ttl := time.Until(entry.InvalidAt)
			if ttl <= 0 {
				return txn.Delete([]byte(key))
			}
			e = e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	}); err != nil {
		return err
	}

	if found && existing.Response.BodyPath != "" && existing.Response.BodyPath != entry.Response.BodyPath {
		if err := removeBodyPath(existing.Response.BodyPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *BadgerStore) GetVary(ctx context.Context, key string) (VarySpec, bool, error) {
	if err := ctx.Err(); err != nil {
		return VarySpec{}, false, err
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
		return VarySpec{}, false, nil
	}
	if err != nil {
		return VarySpec{}, false, err
	}

	spec, err := decodeVarySpec(payload)
	if err != nil {
		return VarySpec{}, false, err
	}

	return spec, true, nil
}

func (s *BadgerStore) PutVary(ctx context.Context, key string, spec VarySpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	payload, err := encodeVarySpec(spec)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), payload)
	})
}

func (s *BadgerStore) ImportBody(ctx context.Context, tempPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	finalPath := filepath.Join(s.blobDir, filepath.Base(tempPath))
	if err := os.MkdirAll(s.blobDir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", err
	}
	return finalPath, nil
}

func (s *BadgerStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var (
		entry Entry
		found bool
		err   error
	)
	if strings.HasPrefix(key, "resp|") {
		entry, found, err = s.GetEntry(ctx, key)
		if err != nil {
			return err
		}
	}

	err = s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
	if err != nil {
		return err
	}
	if found {
		_ = removeBodyPath(entry.Response.BodyPath)
	}
	return nil
}

func (s *BadgerStore) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	keys := make([][]byte, 0)
	blobPaths := make([]string, 0)
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			item := it.Item()
			keys = append(keys, slices.Clone(item.Key()))
			if !strings.HasPrefix(string(item.Key()), "resp|") {
				continue
			}
			if err := item.Value(func(value []byte) error {
				entry, err := decodeEntry(append([]byte(nil), value...))
				if err != nil {
					return err
				}
				if entry.Response.BodyPath != "" {
					blobPaths = append(blobPaths, entry.Response.BodyPath)
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}

	err = s.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			if err := txn.Delete(key); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, bodyPath := range blobPaths {
		_ = removeBodyPath(bodyPath)
	}

	return len(keys), nil
}

func (s *BadgerStore) Close() error {
	return s.db.Close()
}

func (s *BadgerStore) pruneOrphanBodies(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	referenced := map[string]struct{}{}
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte("resp|")); it.ValidForPrefix([]byte("resp|")); it.Next() {
			item := it.Item()
			if err := item.Value(func(value []byte) error {
				entry, err := decodeEntry(append([]byte(nil), value...))
				if err != nil {
					return err
				}
				if entry.Response.BodyPath != "" {
					referenced[entry.Response.BodyPath] = struct{}{}
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(s.blobDir, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(s.blobDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := referenced[path]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
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

func encodeVarySpec(spec VarySpec) ([]byte, error) {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(spec); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func decodeVarySpec(payload []byte) (VarySpec, error) {
	var spec VarySpec
	err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&spec)
	return spec, err
}

func removeBodyPath(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove body %q: %w", path, err)
	}
	return nil
}
