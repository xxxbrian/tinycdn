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

type ChunkObject struct {
	Key         string
	SiteID      string
	RuleID      string
	PolicyTag   string
	StoredAt    time.Time
	FreshUntil  time.Time
	StaleUntil  time.Time
	InvalidAt   time.Time
	BaseAge     int
	Header      http.Header
	TotalLength int64
	ChunkSize   int64
}

type ChunkEntry struct {
	Key       string
	SiteID    string
	ObjectKey string
	Index     int64
	StoredAt  time.Time
	InvalidAt time.Time
	BodyPath  string
	Size      int64
}

type Store interface {
	GetEntry(ctx context.Context, key string) (Entry, bool, error)
	PutEntry(ctx context.Context, key string, entry Entry) error
	GetChunkObject(ctx context.Context, key string) (ChunkObject, bool, error)
	PutChunkObject(ctx context.Context, key string, object ChunkObject) error
	GetChunk(ctx context.Context, key string) (ChunkEntry, bool, error)
	PutChunk(ctx context.Context, key string, chunk ChunkEntry) error
	GetVary(ctx context.Context, key string) (VarySpec, bool, error)
	PutVary(ctx context.Context, key string, spec VarySpec) error
	ImportBody(ctx context.Context, tempPath string) (string, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) (int, error)
	Inventory(ctx context.Context) ([]Inventory, error)
	Close() error
}

type Inventory struct {
	SiteID  string
	Objects int64
	Bytes   int64
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

func (s *BadgerStore) GetChunkObject(ctx context.Context, key string) (ChunkObject, bool, error) {
	if err := ctx.Err(); err != nil {
		return ChunkObject{}, false, err
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
		return ChunkObject{}, false, nil
	}
	if err != nil {
		return ChunkObject{}, false, err
	}

	object, err := decodeChunkObject(payload)
	if err != nil {
		return ChunkObject{}, false, err
	}
	return object, true, nil
}

func (s *BadgerStore) PutChunkObject(ctx context.Context, key string, object ChunkObject) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	payload, err := encodeChunkObject(object)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), payload)
		if !object.InvalidAt.IsZero() {
			ttl := time.Until(object.InvalidAt)
			if ttl <= 0 {
				return txn.Delete([]byte(key))
			}
			e = e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	})
}

func (s *BadgerStore) GetChunk(ctx context.Context, key string) (ChunkEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return ChunkEntry{}, false, err
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
		return ChunkEntry{}, false, nil
	}
	if err != nil {
		return ChunkEntry{}, false, err
	}

	chunk, err := decodeChunkEntry(payload)
	if err != nil {
		return ChunkEntry{}, false, err
	}
	return chunk, true, nil
}

func (s *BadgerStore) PutChunk(ctx context.Context, key string, chunk ChunkEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	existing, found, err := s.GetChunk(ctx, key)
	if err != nil {
		return err
	}

	payload, err := encodeChunkEntry(chunk)
	if err != nil {
		return err
	}

	if err := s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), payload)
		if !chunk.InvalidAt.IsZero() {
			ttl := time.Until(chunk.InvalidAt)
			if ttl <= 0 {
				return txn.Delete([]byte(key))
			}
			e = e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	}); err != nil {
		return err
	}

	if found && existing.BodyPath != "" && existing.BodyPath != chunk.BodyPath {
		if err := removeBodyPath(existing.BodyPath); err != nil {
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
	} else if strings.HasPrefix(key, "rchunk|") {
		chunk, chunkFound, chunkErr := s.GetChunk(ctx, key)
		if chunkErr != nil {
			return chunkErr
		}
		if chunkFound {
			found = true
			entry.Response.BodyPath = chunk.BodyPath
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
			switch {
			case strings.HasPrefix(string(item.Key()), "resp|"):
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
			case strings.HasPrefix(string(item.Key()), "rchunk|"):
				if err := item.Value(func(value []byte) error {
					chunk, err := decodeChunkEntry(append([]byte(nil), value...))
					if err != nil {
						return err
					}
					if chunk.BodyPath != "" {
						blobPaths = append(blobPaths, chunk.BodyPath)
					}
					return nil
				}); err != nil {
					return err
				}
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

func (s *BadgerStore) Inventory(ctx context.Context) ([]Inventory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bySite := map[string]Inventory{}
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
				current := bySite[entry.SiteID]
				current.SiteID = entry.SiteID
				current.Objects++
				current.Bytes += entry.Response.ContentLength
				bySite[entry.SiteID] = current
				return nil
			}); err != nil {
				return err
			}
		}
		for it.Seek([]byte("rchunk|")); it.ValidForPrefix([]byte("rchunk|")); it.Next() {
			item := it.Item()
			if err := item.Value(func(value []byte) error {
				chunk, err := decodeChunkEntry(append([]byte(nil), value...))
				if err != nil {
					return err
				}
				current := bySite[chunk.SiteID]
				current.SiteID = chunk.SiteID
				current.Objects++
				current.Bytes += chunk.Size
				bySite[chunk.SiteID] = current
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	inventory := make([]Inventory, 0, len(bySite))
	for _, item := range bySite {
		inventory = append(inventory, item)
	}
	slices.SortFunc(inventory, func(a, b Inventory) int {
		return strings.Compare(a.SiteID, b.SiteID)
	})
	return inventory, nil
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
		for it.Seek([]byte("rchunk|")); it.ValidForPrefix([]byte("rchunk|")); it.Next() {
			item := it.Item()
			if err := item.Value(func(value []byte) error {
				chunk, err := decodeChunkEntry(append([]byte(nil), value...))
				if err != nil {
					return err
				}
				if chunk.BodyPath != "" {
					referenced[chunk.BodyPath] = struct{}{}
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

func encodeChunkObject(object ChunkObject) ([]byte, error) {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(object); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeChunkObject(payload []byte) (ChunkObject, error) {
	var object ChunkObject
	err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&object)
	return object, err
}

func encodeChunkEntry(chunk ChunkEntry) ([]byte, error) {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(chunk); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeChunkEntry(payload []byte) (ChunkEntry, error) {
	var chunk ChunkEntry
	err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&chunk)
	return chunk, err
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
