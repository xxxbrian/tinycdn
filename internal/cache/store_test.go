package cache

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBadgerStoreEntryAndVaryRoundTrip(t *testing.T) {
	store, err := NewBadgerStore(filepath.Join(t.TempDir(), "badger"))
	if err != nil {
		t.Fatalf("new badger store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	entry := Entry{
		Key:        "resp|site-1|GET|/asset.js",
		SiteID:     "site-1",
		RuleID:     "rule-1",
		PolicyTag:  "rule-1|force",
		StoredAt:   now,
		FreshUntil: now.Add(time.Minute),
		StaleUntil: now.Add(2 * time.Minute),
		InvalidAt:  now.Add(3 * time.Minute),
		BaseAge:    7,
		Response: StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/javascript"}},
			Body:       []byte("console.log('ok')"),
		},
	}

	if err := store.PutEntry(context.Background(), entry.Key, entry); err != nil {
		t.Fatalf("put entry: %v", err)
	}
	if err := store.PutVary(context.Background(), "vary|site-1|GET|/asset.js", VarySpec{
		Headers: []string{"Accept-Encoding", "Origin"},
	}); err != nil {
		t.Fatalf("put vary: %v", err)
	}

	stored, found, err := store.GetEntry(context.Background(), entry.Key)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if !found {
		t.Fatalf("expected stored entry to be found")
	}
	if stored.Key != entry.Key || string(stored.Response.Body) != string(entry.Response.Body) {
		t.Fatalf("unexpected stored entry: %#v", stored)
	}

	vary, found, err := store.GetVary(context.Background(), "vary|site-1|GET|/asset.js")
	if err != nil {
		t.Fatalf("get vary: %v", err)
	}
	if !found || len(vary.Headers) != 2 {
		t.Fatalf("unexpected vary spec: %#v found=%v", vary, found)
	}
}

func TestBadgerStoreDeletePrefixAndExpiredWrite(t *testing.T) {
	store, err := NewBadgerStore(filepath.Join(t.TempDir(), "badger"))
	if err != nil {
		t.Fatalf("new badger store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	keys := []string{
		"resp|site-1|GET|/a.js",
		"resp|site-1|GET|/b.js",
		"vary|site-1|GET|/b.js",
		"resp|site-2|GET|/c.js",
	}
	for _, key := range keys {
		err := store.PutEntry(context.Background(), key, Entry{
			Key:        key,
			StoredAt:   now,
			FreshUntil: now.Add(time.Minute),
			StaleUntil: now.Add(2 * time.Minute),
			InvalidAt:  now.Add(3 * time.Minute),
			Response: StoredResponse{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/plain"}},
				Body:       []byte(key),
			},
		})
		if err != nil {
			t.Fatalf("put entry %s: %v", key, err)
		}
	}

	deleted, err := store.DeletePrefix(context.Background(), "resp|site-1|")
	if err != nil {
		t.Fatalf("delete prefix: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted entries, got %d", deleted)
	}

	if _, found, err := store.GetEntry(context.Background(), "resp|site-1|GET|/a.js"); err != nil || found {
		t.Fatalf("expected prefixed key to be deleted, found=%v err=%v", found, err)
	}
	if _, found, err := store.GetEntry(context.Background(), "resp|site-2|GET|/c.js"); err != nil || !found {
		t.Fatalf("expected other site key to remain, found=%v err=%v", found, err)
	}

	expired := Entry{
		Key:        "resp|site-1|GET|/expired.js",
		StoredAt:   now,
		FreshUntil: now,
		StaleUntil: now,
		InvalidAt:  now.Add(-time.Second),
		Response: StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       []byte("expired"),
		},
	}
	if err := store.PutEntry(context.Background(), expired.Key, expired); err != nil {
		t.Fatalf("put expired entry: %v", err)
	}
	if _, found, err := store.GetEntry(context.Background(), expired.Key); err != nil || found {
		t.Fatalf("expected expired entry to be removed, found=%v err=%v", found, err)
	}
}

func TestBadgerStorePutEntryReplacesOldBlob(t *testing.T) {
	store, err := NewBadgerStore(filepath.Join(t.TempDir(), "badger"))
	if err != nil {
		t.Fatalf("new badger store: %v", err)
	}
	defer store.Close()

	tempOne := filepath.Join(t.TempDir(), "body-one")
	if err := os.WriteFile(tempOne, []byte("first"), 0o644); err != nil {
		t.Fatalf("write temp one: %v", err)
	}
	blobOne, err := store.ImportBody(context.Background(), tempOne)
	if err != nil {
		t.Fatalf("import body one: %v", err)
	}

	entry := Entry{
		Key:        "resp|site-1|GET|/asset.js",
		StoredAt:   time.Now().UTC(),
		FreshUntil: time.Now().UTC().Add(time.Minute),
		StaleUntil: time.Now().UTC().Add(2 * time.Minute),
		InvalidAt:  time.Now().UTC().Add(3 * time.Minute),
		Response: StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/javascript"}},
			BodyPath:   blobOne,
		},
	}
	if err := store.PutEntry(context.Background(), entry.Key, entry); err != nil {
		t.Fatalf("put first entry: %v", err)
	}

	tempTwo := filepath.Join(t.TempDir(), "body-two")
	if err := os.WriteFile(tempTwo, []byte("second"), 0o644); err != nil {
		t.Fatalf("write temp two: %v", err)
	}
	blobTwo, err := store.ImportBody(context.Background(), tempTwo)
	if err != nil {
		t.Fatalf("import body two: %v", err)
	}

	entry.Response.BodyPath = blobTwo
	if err := store.PutEntry(context.Background(), entry.Key, entry); err != nil {
		t.Fatalf("replace entry: %v", err)
	}

	if _, err := os.Stat(blobOne); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected replaced blob to be removed, err=%v", err)
	}
	if _, err := os.Stat(blobTwo); err != nil {
		t.Fatalf("expected latest blob to remain, err=%v", err)
	}
}

func TestBadgerStorePrunesOrphanBlobsOnOpen(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "badger")
	store, err := NewBadgerStore(cachePath)
	if err != nil {
		t.Fatalf("new badger store: %v", err)
	}

	tempLive := filepath.Join(t.TempDir(), "live-body")
	if err := os.WriteFile(tempLive, []byte("live"), 0o644); err != nil {
		t.Fatalf("write live temp body: %v", err)
	}
	blobLive, err := store.ImportBody(context.Background(), tempLive)
	if err != nil {
		t.Fatalf("import live body: %v", err)
	}

	entry := Entry{
		Key:        "resp|site-1|GET|/asset.js",
		StoredAt:   time.Now().UTC(),
		FreshUntil: time.Now().UTC().Add(time.Minute),
		StaleUntil: time.Now().UTC().Add(2 * time.Minute),
		InvalidAt:  time.Now().UTC().Add(3 * time.Minute),
		Response: StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/javascript"}},
			BodyPath:   blobLive,
		},
	}
	if err := store.PutEntry(context.Background(), entry.Key, entry); err != nil {
		t.Fatalf("put entry: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	orphanPath := filepath.Join(cachePath, "blobs", "orphan-body")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan body: %v", err)
	}

	reopened, err := NewBadgerStore(cachePath)
	if err != nil {
		t.Fatalf("reopen badger store: %v", err)
	}
	defer reopened.Close()

	if _, err := os.Stat(blobLive); err != nil {
		t.Fatalf("expected referenced blob to remain, err=%v", err)
	}
	if _, err := os.Stat(orphanPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected orphan blob to be removed, err=%v", err)
	}
}
