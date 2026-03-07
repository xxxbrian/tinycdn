package cache

import (
	"context"
	"net/http"
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
