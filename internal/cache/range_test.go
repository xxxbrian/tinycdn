package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"tinycdn/internal/model"
)

func TestRangeCacheCanHandleMatrix(t *testing.T) {
	cache := NewRangeCache(newMemoryStore())
	policy := Policy{Mode: model.CacheModeForceCache}

	tests := []struct {
		name     string
		method   string
		rangeHdr string
		ifRange  string
		want     bool
	}{
		{name: "single range get", method: http.MethodGet, rangeHdr: "bytes=0-99", want: true},
		{name: "multi range bypass", method: http.MethodGet, rangeHdr: "bytes=0-99,200-299", want: false},
		{name: "if-range bypass", method: http.MethodGet, rangeHdr: "bytes=0-99", ifRange: `"v1"`, want: false},
		{name: "head bypass", method: http.MethodHead, rangeHdr: "bytes=0-99", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "https://cdn.example.com/video.mp4", nil)
			if tt.rangeHdr != "" {
				req.Header.Set("Range", tt.rangeHdr)
			}
			if tt.ifRange != "" {
				req.Header.Set("If-Range", tt.ifRange)
			}
			if got := cache.CanHandle(req, policy); got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestRangeCacheSlicesFreshFullObject(t *testing.T) {
	store := newMemoryStore()
	cache := NewRangeCache(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }

	temp, err := os.CreateTemp(t.TempDir(), "full-*")
	if err != nil {
		t.Fatalf("create temp body: %v", err)
	}
	if _, err := temp.WriteString("abcdefghijklmnopqrstuvwxyz"); err != nil {
		t.Fatalf("write temp body: %v", err)
	}
	if err := temp.Close(); err != nil {
		t.Fatalf("close temp body: %v", err)
	}

	baseKey := buildBaseCacheKey("site-1", http.MethodGet, "/video.mp4", "")
	entry := Entry{
		Key:        responseKey(baseKey),
		SiteID:     "site-1",
		RuleID:     "rule-1",
		PolicyTag:  "rule-1|force",
		StoredAt:   now,
		FreshUntil: now.Add(time.Minute),
		InvalidAt:  now.Add(time.Hour),
		Response: StoredResponse{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": {"text/plain"}},
			BodyPath:      temp.Name(),
			ContentLength: 26,
		},
	}
	if err := store.PutEntry(context.Background(), entry.Key, entry); err != nil {
		t.Fatalf("put entry: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/video.mp4", nil)
	req.Header.Set("Range", "bytes=5-9")
	result, err := cache.Handle(context.Background(), req, Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}, func(context.Context, *http.Request) (StoredResponse, error) {
		t.Fatalf("unexpected upstream fetch")
		return StoredResponse{}, nil
	})
	if err != nil {
		t.Fatalf("handle range: %v", err)
	}
	if result.State != StateHit {
		t.Fatalf("expected hit, got %s", result.State)
	}
	if got := result.Header.Get("Content-Range"); got != "bytes 5-9/26" {
		t.Fatalf("unexpected content-range %q", got)
	}
	if len(result.Parts) != 1 || result.Parts[0].Offset != 5 || result.Parts[0].Length != 5 {
		t.Fatalf("unexpected parts %#v", result.Parts)
	}
}

func TestRangeCachePurgeURLRemovesRangeMetadataAndChunks(t *testing.T) {
	store := newMemoryStore()
	cache := NewRangeCache(store)

	baseKey := buildBaseCacheKey("site-1", http.MethodGet, "/video.mp4", "")
	objectKey := responseKey(baseKey)
	object := ChunkObject{
		Key:         objectKey,
		SiteID:      "site-1",
		RuleID:      "rule-1",
		PolicyTag:   "rule-1|force",
		StoredAt:    time.Now().UTC(),
		FreshUntil:  time.Now().UTC().Add(time.Minute),
		InvalidAt:   time.Now().UTC().Add(time.Hour),
		Header:      http.Header{"Content-Type": {"video/mp4"}},
		TotalLength: 1024,
		ChunkSize:   512,
	}
	if err := store.PutChunkObject(context.Background(), rangeObjectKey(objectKey), object); err != nil {
		t.Fatalf("put chunk object: %v", err)
	}
	temp, err := os.CreateTemp(t.TempDir(), "chunk-*")
	if err != nil {
		t.Fatalf("create chunk body: %v", err)
	}
	if _, err := temp.Write(make([]byte, 512)); err != nil {
		t.Fatalf("write chunk body: %v", err)
	}
	if err := temp.Close(); err != nil {
		t.Fatalf("close chunk body: %v", err)
	}
	if err := store.PutChunk(context.Background(), rangeChunkKey(objectKey, 0), ChunkEntry{
		Key:       rangeChunkKey(objectKey, 0),
		SiteID:    "site-1",
		ObjectKey: objectKey,
		Index:     0,
		StoredAt:  time.Now().UTC(),
		InvalidAt: time.Now().UTC().Add(time.Hour),
		BodyPath:  temp.Name(),
		Size:      512,
	}); err != nil {
		t.Fatalf("put chunk: %v", err)
	}

	if _, err := cache.PurgeURL(context.Background(), "site-1", "/video.mp4", ""); err != nil {
		t.Fatalf("purge url: %v", err)
	}
	if _, found, err := store.GetChunkObject(context.Background(), rangeObjectKey(objectKey)); err != nil || found {
		t.Fatalf("expected chunk object removed, found=%t err=%v", found, err)
	}
	if _, found, err := store.GetChunk(context.Background(), rangeChunkKey(objectKey, 0)); err != nil || found {
		t.Fatalf("expected chunk removed, found=%t err=%v", found, err)
	}
}
