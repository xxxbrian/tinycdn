package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"tinycdn/internal/model"
)

type memoryStore struct {
	mu           sync.RWMutex
	entries      map[string]Entry
	chunkObjects map[string]ChunkObject
	chunks       map[string]ChunkEntry
	varies       map[string]VarySpec
}

type errorStore struct {
	getEntryErr error
	getVaryErr  error
	putErr      error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		entries:      map[string]Entry{},
		chunkObjects: map[string]ChunkObject{},
		chunks:       map[string]ChunkEntry{},
		varies:       map[string]VarySpec{},
	}
}

func (s *memoryStore) GetEntry(_ context.Context, key string) (Entry, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[key]
	return entry, ok, nil
}

func (s *memoryStore) PutEntry(_ context.Context, key string, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[key]; ok && existing.Response.BodyPath != "" && existing.Response.BodyPath != entry.Response.BodyPath {
		_ = os.Remove(existing.Response.BodyPath)
	}
	s.entries[key] = entry
	return nil
}

func (s *memoryStore) GetVary(_ context.Context, key string) (VarySpec, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.varies[key]
	return spec, ok, nil
}

func (s *memoryStore) PutVary(_ context.Context, key string, spec VarySpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.varies[key] = spec
	return nil
}

func (s *memoryStore) GetChunkObject(_ context.Context, key string) (ChunkObject, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	object, ok := s.chunkObjects[key]
	return object, ok, nil
}

func (s *memoryStore) PutChunkObject(_ context.Context, key string, object ChunkObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunkObjects[key] = object
	return nil
}

func (s *memoryStore) GetChunk(_ context.Context, key string) (ChunkEntry, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chunk, ok := s.chunks[key]
	return chunk, ok, nil
}

func (s *memoryStore) PutChunk(_ context.Context, key string, chunk ChunkEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.chunks[key]; ok && existing.BodyPath != "" && existing.BodyPath != chunk.BodyPath {
		_ = os.Remove(existing.BodyPath)
	}
	s.chunks[key] = chunk
	return nil
}

func (s *memoryStore) TouchChunkPrefix(_ context.Context, prefix string, invalidAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, chunk := range s.chunks {
		if strings.HasPrefix(key, prefix) {
			chunk.InvalidAt = invalidAt
			s.chunks[key] = chunk
		}
	}
	return nil
}

func (s *memoryStore) ImportBody(_ context.Context, tempPath string) (string, error) {
	return tempPath, nil
}

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[key]; ok {
		_ = os.Remove(entry.Response.BodyPath)
	}
	if chunk, ok := s.chunks[key]; ok {
		_ = os.Remove(chunk.BodyPath)
	}
	delete(s.entries, key)
	delete(s.chunkObjects, key)
	delete(s.chunks, key)
	delete(s.varies, key)
	return nil
}

func (s *memoryStore) DeletePrefix(_ context.Context, prefix string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for key := range s.entries {
		if strings.HasPrefix(key, prefix) {
			_ = os.Remove(s.entries[key].Response.BodyPath)
			delete(s.entries, key)
			deleted++
		}
	}
	for key := range s.chunkObjects {
		if strings.HasPrefix(key, prefix) {
			delete(s.chunkObjects, key)
			deleted++
		}
	}
	for key := range s.chunks {
		if strings.HasPrefix(key, prefix) {
			_ = os.Remove(s.chunks[key].BodyPath)
			delete(s.chunks, key)
			deleted++
		}
	}
	for key := range s.varies {
		if strings.HasPrefix(key, prefix) {
			delete(s.varies, key)
			deleted++
		}
	}
	return deleted, nil
}

func (s *memoryStore) Inventory(_ context.Context) ([]Inventory, error) {
	return nil, nil
}

func (s *memoryStore) Close() error {
	return nil
}

func (s *errorStore) GetEntry(_ context.Context, _ string) (Entry, bool, error) {
	return Entry{}, false, s.getEntryErr
}

func (s *errorStore) PutEntry(_ context.Context, _ string, _ Entry) error {
	return s.putErr
}

func (s *errorStore) GetVary(_ context.Context, _ string) (VarySpec, bool, error) {
	return VarySpec{}, false, s.getVaryErr
}

func (s *errorStore) GetChunkObject(_ context.Context, _ string) (ChunkObject, bool, error) {
	return ChunkObject{}, false, s.getEntryErr
}

func (s *errorStore) PutChunkObject(_ context.Context, _ string, _ ChunkObject) error {
	return s.putErr
}

func (s *errorStore) GetChunk(_ context.Context, _ string) (ChunkEntry, bool, error) {
	return ChunkEntry{}, false, s.getEntryErr
}

func (s *errorStore) PutChunk(_ context.Context, _ string, _ ChunkEntry) error {
	return s.putErr
}

func (s *errorStore) TouchChunkPrefix(_ context.Context, _ string, _ time.Time) error {
	return s.putErr
}

func (s *errorStore) PutVary(_ context.Context, _ string, _ VarySpec) error {
	return s.putErr
}

func (s *errorStore) ImportBody(_ context.Context, tempPath string) (string, error) {
	return tempPath, s.putErr
}

func (s *errorStore) Delete(_ context.Context, _ string) error {
	return s.putErr
}

func (s *errorStore) DeletePrefix(_ context.Context, _ string) (int, error) {
	return 0, s.putErr
}

func (s *errorStore) Inventory(_ context.Context) ([]Inventory, error) {
	return nil, s.putErr
}

func (s *errorStore) Close() error {
	return nil
}

func TestEngineForceCacheMissThenHit(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)

	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       5 * time.Minute,
		HasTTL:    true,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/javascript"}},
			Body:       []byte("console.log('v1')"),
		}, nil
	}

	first, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if first.State != StateMiss {
		t.Fatalf("expected first request to miss, got %s", first.State)
	}
	_, found, err := store.GetEntry(context.Background(), responseKey(buildBaseCacheKey("site-1", http.MethodGet, "/assets/app.js", "")))
	if err != nil || !found {
		t.Fatalf("expected first response to be stored, found=%t err=%v", found, err)
	}

	second, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if second.State != StateHit {
		t.Fatalf("expected second request to hit, got %s", second.State)
	}
	if fetches != 1 {
		t.Fatalf("expected one upstream fetch, got %d", fetches)
	}
}

func TestEngineOptimisticServesStaleAndRefreshes(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)

	currentTime := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return currentTime }

	policy := Policy{
		SiteID:             "site-1",
		RuleID:             "rule-1",
		PolicyTag:          "rule-1|force|optimistic",
		Mode:               model.CacheModeForceCache,
		TTL:                time.Second,
		HasTTL:             true,
		Optimistic:         true,
		OptimisticMaxStale: time.Minute,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	refreshDone := make(chan struct{}, 1)
	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		body := []byte("v1")
		if fetches > 1 {
			body = []byte("v2")
			refreshDone <- struct{}{}
		}
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/javascript"}},
			Body:       body,
		}, nil
	}

	if _, err := engine.Handle(context.Background(), req, policy, fetch); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	currentTime = currentTime.Add(2 * time.Second)
	stale, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("stale handle: %v", err)
	}
	if stale.State != StateStale {
		t.Fatalf("expected stale response, got %s", stale.State)
	}
	if string(stale.Body) != "v1" {
		t.Fatalf("expected stale body v1, got %q", string(stale.Body))
	}

	select {
	case <-refreshDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("background refresh did not complete")
	}

	currentTime = currentTime.Add(500 * time.Millisecond)
	hit, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("post-refresh handle: %v", err)
	}
	if hit.State != StateHit {
		t.Fatalf("expected refreshed entry to hit, got %s", hit.State)
	}
	if string(hit.Body) != "v2" {
		t.Fatalf("expected refreshed body v2, got %q", string(hit.Body))
	}
}

func TestEngineForceCacheStripsClientValidators(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	policy := Policy{
		SiteID:              "site-1",
		RuleID:              "rule-1",
		PolicyTag:           "rule-1|force",
		Mode:                model.CacheModeForceCache,
		TTL:                 time.Minute,
		HasTTL:              true,
		IgnoreClientControl: true,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	req.Header.Set("If-None-Match", "\"abc123\"")
	req.Header.Set("If-Modified-Since", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	_, err := engine.Handle(context.Background(), req, policy, func(_ context.Context, req *http.Request) (StoredResponse, error) {
		for _, headerName := range conditionalRequestHeaders {
			if got := req.Header.Get(headerName); got != "" {
				t.Fatalf("expected %s to be stripped, got %q", headerName, got)
			}
		}
		if got := req.Header.Get("Cache-Control"); got != "" {
			t.Fatalf("expected Cache-Control to be stripped, got %q", got)
		}
		if got := req.Header.Get("Pragma"); got != "" {
			t.Fatalf("expected Pragma to be stripped, got %q", got)
		}

		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": {"public, max-age=60"}},
			Body:       []byte("ok"),
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
}

func TestEngineBypassesSharedSensitiveRequests(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{
			name:   "authorization header",
			header: http.Header{"Authorization": {"Bearer secret"}},
		},
		{
			name:   "cookie header",
			header: http.Header{"Cookie": {"session=abc123"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMemoryStore()
			engine := NewEngine(store)
			policy := Policy{
				SiteID:    "site-1",
				RuleID:    "rule-1",
				PolicyTag: "rule-1|force",
				Mode:      model.CacheModeForceCache,
				TTL:       time.Minute,
				HasTTL:    true,
			}

			req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/account", nil)
			req.Header = tt.header.Clone()
			fetches := 0
			fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
				fetches++
				return StoredResponse{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"text/plain"}},
					Body:       []byte("private"),
				}, nil
			}

			first, err := engine.Handle(context.Background(), req, policy, fetch)
			if err != nil {
				t.Fatalf("first handle: %v", err)
			}
			second, err := engine.Handle(context.Background(), req, policy, fetch)
			if err != nil {
				t.Fatalf("second handle: %v", err)
			}

			if first.State != StateBypass || second.State != StateBypass {
				t.Fatalf("expected sensitive shared-cache request to bypass, got %s then %s", first.State, second.State)
			}
			if fetches != 2 {
				t.Fatalf("expected two upstream fetches, got %d", fetches)
			}
		})
	}
}

func TestEngineFollowOriginRespectsCacheControl(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|origin",
		Mode:      model.CacheModeFollowOrigin,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": {"public, max-age=30, stale-while-revalidate=120"}},
			Body:       []byte("origin"),
		}, nil
	}

	if _, err := engine.Handle(context.Background(), req, policy, fetch); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	now = now.Add(10 * time.Second)
	hit, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("follow-origin hit: %v", err)
	}
	if hit.State != StateHit {
		t.Fatalf("expected follow-origin hit, got %s", hit.State)
	}
	if fetches != 1 {
		t.Fatalf("expected one upstream fetch, got %d", fetches)
	}
}

func TestEngineFollowOriginRevalidatesWithETag(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|origin",
		Mode:      model.CacheModeFollowOrigin,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		switch fetches {
		case 1:
			return StoredResponse{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": {"public, no-cache"},
					"Etag":          {`"v1"`},
				},
				Body: []byte("payload"),
			}, nil
		case 2:
			if got := req.Header.Get("If-None-Match"); got != `"v1"` {
				t.Fatalf("expected conditional revalidation header, got %q", got)
			}
			return StoredResponse{
				StatusCode: http.StatusNotModified,
				Header: http.Header{
					"Cache-Control": {"public, max-age=60"},
					"Etag":          {`"v1"`},
				},
			}, nil
		default:
			return StoredResponse{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Cache-Control": {"public, max-age=60"}},
				Body:       []byte("unexpected"),
			}, nil
		}
	}

	first, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if first.State != StateMiss {
		t.Fatalf("expected first request to miss, got %s", first.State)
	}
	stored, found, err := store.GetEntry(context.Background(), responseKey(buildBaseCacheKey("site-1", http.MethodGet, "/assets/app.js", "")))
	if err != nil || !found {
		t.Fatalf("expected first response to be stored, found=%t err=%v", found, err)
	}
	if got := stored.Response.Header.Get("ETag"); got != `"v1"` {
		t.Fatalf("expected stored validator to be preserved, got %q", got)
	}
	startResult, plan, err := engine.Start(context.Background(), req, policy)
	if err != nil {
		t.Fatalf("start before revalidation: %v", err)
	}
	if startResult.State != "" {
		t.Fatalf("expected stale follow_origin revalidation to require fill plan, got state %s", startResult.State)
	}
	if plan == nil {
		t.Fatalf("expected fill plan for revalidation")
	}
	if got := plan.Request(context.Background()).Header.Get("If-None-Match"); got != `"v1"` {
		t.Fatalf("expected plan request to carry conditional revalidation header, got %q", got)
	}
	engine.releaseFill(plan.cacheKey)

	second, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if second.State != StateHit {
		t.Fatalf("expected revalidated response to return HIT, got %s", second.State)
	}
	if string(second.Body) != "payload" {
		t.Fatalf("expected cached body after revalidation, got %q", string(second.Body))
	}
	if !strings.Contains(second.CacheStatus, "detail=REVALIDATED") {
		t.Fatalf("expected revalidated cache status, got %q", second.CacheStatus)
	}

	third, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("third handle: %v", err)
	}
	if third.State != StateHit {
		t.Fatalf("expected post-revalidation request to hit, got %s", third.State)
	}
	if fetches != 2 {
		t.Fatalf("expected two upstream fetches, got %d", fetches)
	}
}

func TestEngineVarySeparatesVariants(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|origin",
		Mode:      model.CacheModeFollowOrigin,
	}

	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		body := "identity"
		if strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
			body = "gzip"
		}
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Cache-Control":    {"public, max-age=60"},
				"Vary":             {"Accept-Encoding"},
				"Content-Type":     {"text/plain"},
				"Content-Encoding": {body},
			},
			Body: []byte(body),
		}, nil
	}

	gzipReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	gzipReq.Header.Set("Accept-Encoding", "gzip")
	identityReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)

	first, err := engine.Handle(context.Background(), gzipReq, policy, fetch)
	if err != nil {
		t.Fatalf("prime gzip variant: %v", err)
	}
	if first.State != StateMiss {
		t.Fatalf("expected gzip prime to miss, got %s", first.State)
	}

	second, err := engine.Handle(context.Background(), gzipReq, policy, fetch)
	if err != nil {
		t.Fatalf("gzip hit: %v", err)
	}
	if second.State != StateHit || string(second.Body) != "gzip" {
		t.Fatalf("expected gzip variant hit, got %s body=%q", second.State, string(second.Body))
	}

	third, err := engine.Handle(context.Background(), identityReq, policy, fetch)
	if err != nil {
		t.Fatalf("identity fetch: %v", err)
	}
	if third.State != StateMiss || string(third.Body) != "identity" {
		t.Fatalf("expected identity variant miss, got %s body=%q", third.State, string(third.Body))
	}

	if fetches != 2 {
		t.Fatalf("expected two upstream fetches for two vary variants, got %d", fetches)
	}
}

func TestEngineVaryNormalizesAcceptEncodingFormatting(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|origin",
		Mode:      model.CacheModeFollowOrigin,
	}

	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Cache-Control":    {"public, max-age=60"},
				"Vary":             {"Accept-Encoding"},
				"Content-Type":     {"text/plain"},
				"Content-Encoding": {"gzip"},
			},
			Body: []byte("gzip"),
		}, nil
	}

	firstReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	firstReq.Header.Set("Accept-Encoding", "gzip, br")
	first, err := engine.Handle(context.Background(), firstReq, policy, fetch)
	if err != nil {
		t.Fatalf("prime variant: %v", err)
	}
	if first.State != StateMiss {
		t.Fatalf("expected first variant request to miss, got %s", first.State)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	secondReq.Header.Set("Accept-Encoding", "gzip,br")
	second, err := engine.Handle(context.Background(), secondReq, policy, fetch)
	if err != nil {
		t.Fatalf("normalized variant hit: %v", err)
	}
	if second.State != StateHit {
		t.Fatalf("expected normalized Accept-Encoding request to hit, got %s", second.State)
	}
	if fetches != 1 {
		t.Fatalf("expected one upstream fetch for equivalent Accept-Encoding variants, got %d", fetches)
	}
}

func TestEngineManagedOriginSeparatesCORSVariants(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|follow",
		Mode:      model.CacheModeFollowOrigin,
	}

	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		header := http.Header{
			"Cache-Control": {"public, max-age=30"},
			"Content-Type":  {"text/plain"},
		}
		if origin := req.Header.Get("Origin"); origin != "" {
			header.Set("Access-Control-Allow-Origin", origin)
			return StoredResponse{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       []byte("cors-" + origin),
			}, nil
		}
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       []byte("plain"),
		}, nil
	}

	plainReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	plain, err := engine.Handle(context.Background(), plainReq, policy, fetch)
	if err != nil {
		t.Fatalf("plain handle: %v", err)
	}
	if plain.State != StateMiss {
		t.Fatalf("expected plain miss, got %s", plain.State)
	}

	originReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	originReq.Header.Set("Origin", "https://app.example.com")
	originMiss, err := engine.Handle(context.Background(), originReq, policy, fetch)
	if err != nil {
		t.Fatalf("origin miss handle: %v", err)
	}
	if originMiss.State != StateMiss {
		t.Fatalf("expected first origin request miss, got %s", originMiss.State)
	}
	if string(originMiss.Body) != "cors-https://app.example.com" {
		t.Fatalf("unexpected origin body %q", string(originMiss.Body))
	}

	originHit, err := engine.Handle(context.Background(), originReq, policy, fetch)
	if err != nil {
		t.Fatalf("origin hit handle: %v", err)
	}
	if originHit.State != StateHit {
		t.Fatalf("expected repeated origin request hit, got %s", originHit.State)
	}
	if string(originHit.Body) != "cors-https://app.example.com" {
		t.Fatalf("unexpected origin hit body %q", string(originHit.Body))
	}

	if fetches != 2 {
		t.Fatalf("expected two fetches (plain + origin), got %d", fetches)
	}

	spec, found, err := store.GetVary(context.Background(), varyKey(buildBaseCacheKey("site-1", http.MethodGet, "/assets/app.js", "")))
	if err != nil || !found {
		t.Fatalf("expected vary spec, found=%t err=%v", found, err)
	}
	if len(spec.Headers) != 1 || spec.Headers[0] != "Origin" {
		t.Fatalf("expected managed origin vary, got %#v", spec.Headers)
	}
}

func TestEngineManagedOriginSeparatesDifferentOrigins(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|follow",
		Mode:      model.CacheModeFollowOrigin,
	}

	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		origin := req.Header.Get("Origin")
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Cache-Control":               {"public, max-age=30"},
				"Access-Control-Allow-Origin": {origin},
			},
			Body: []byte(origin),
		}, nil
	}

	reqA := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	reqA.Header.Set("Origin", "https://a.example.com")
	reqB := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	reqB.Header.Set("Origin", "https://b.example.com")

	if _, err := engine.Handle(context.Background(), reqA, policy, fetch); err != nil {
		t.Fatalf("origin a miss: %v", err)
	}
	if _, err := engine.Handle(context.Background(), reqB, policy, fetch); err != nil {
		t.Fatalf("origin b miss: %v", err)
	}
	hitA, err := engine.Handle(context.Background(), reqA, policy, fetch)
	if err != nil {
		t.Fatalf("origin a hit: %v", err)
	}
	if hitA.State != StateHit || string(hitA.Body) != "https://a.example.com" {
		t.Fatalf("unexpected origin a hit state/body %s %q", hitA.State, string(hitA.Body))
	}
	if fetches != 2 {
		t.Fatalf("expected two variant fetches, got %d", fetches)
	}
}

func TestEnginePurgeURLClearsAllVariants(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|origin",
		Mode:      model.CacheModeFollowOrigin,
	}

	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		body := "identity"
		if strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
			body = "gzip"
		}
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Cache-Control": {"public, max-age=60"},
				"Vary":          {"Accept-Encoding"},
			},
			Body: []byte(body),
		}, nil
	}

	gzipReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js?rev=1", nil)
	gzipReq.Header.Set("Accept-Encoding", "gzip")
	identityReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js?rev=1", nil)

	if _, err := engine.Handle(context.Background(), gzipReq, policy, fetch); err != nil {
		t.Fatalf("prime gzip variant: %v", err)
	}
	if _, err := engine.Handle(context.Background(), identityReq, policy, fetch); err != nil {
		t.Fatalf("prime identity variant: %v", err)
	}

	deleted, err := engine.PurgeURL(context.Background(), "site-1", "/assets/app.js", "rev=1")
	if err != nil {
		t.Fatalf("purge url: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deleted records (2 variants + 1 vary spec), got %d", deleted)
	}
}

func TestEngineServesStaleOnErrorWithinStaleIfError(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:          "site-1",
		RuleID:          "rule-1",
		PolicyTag:       "rule-1|force",
		Mode:            model.CacheModeForceCache,
		TTL:             time.Second,
		HasTTL:          true,
		StaleIfError:    time.Minute,
		HasStaleIfError: true,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		if fetches > 1 {
			return StoredResponse{}, context.DeadlineExceeded
		}
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/javascript"}},
			Body:       []byte("v1"),
		}, nil
	}

	if _, err := engine.Handle(context.Background(), req, policy, fetch); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	now = now.Add(2 * time.Second)
	result, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("stale-if-error handle: %v", err)
	}
	if result.State != StateStale {
		t.Fatalf("expected stale result on upstream error, got %s", result.State)
	}
	if string(result.Body) != "v1" {
		t.Fatalf("expected stale body v1, got %q", string(result.Body))
	}
}

func TestEngineRequestBehaviorMatrix(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		rangeHdr  string
		policy    Policy
		wantState State
		wantFetch string
	}{
		{
			name:      "post bypasses cache",
			method:    http.MethodPost,
			policy:    Policy{Mode: model.CacheModeForceCache},
			wantState: StateBypass,
			wantFetch: http.MethodPost,
		},
		{
			name:      "range request bypasses cache",
			method:    http.MethodGet,
			rangeHdr:  "bytes=0-99",
			policy:    Policy{Mode: model.CacheModeForceCache},
			wantState: StateBypass,
			wantFetch: http.MethodGet,
		},
		{
			name:      "bypass mode bypasses cache",
			method:    http.MethodGet,
			policy:    Policy{Mode: model.CacheModeBypass},
			wantState: StateBypass,
			wantFetch: http.MethodGet,
		},
		{
			name:      "head stays cacheable pipeline",
			method:    http.MethodHead,
			policy:    Policy{Mode: model.CacheModeForceCache},
			wantState: StateMiss,
			wantFetch: http.MethodGet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "https://cdn.example.com/assets/app.js", nil)
			if tt.rangeHdr != "" {
				req.Header.Set("Range", tt.rangeHdr)
			}

			prepared, state := prepareRequest(req, tt.policy)
			if state != tt.wantState {
				t.Fatalf("expected %s, got %s", tt.wantState, state)
			}
			if prepared.Method != tt.wantFetch {
				t.Fatalf("expected prepared method %s, got %s", tt.wantFetch, prepared.Method)
			}
		})
	}
}

func TestEngineHeadUsesGetCacheKey(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}

	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       []byte("payload"),
		}, nil
	}

	getReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	if _, err := engine.Handle(context.Background(), getReq, policy, fetch); err != nil {
		t.Fatalf("prime get cache: %v", err)
	}

	headReq := httptest.NewRequest(http.MethodHead, "https://cdn.example.com/assets/app.js", nil)
	result, err := engine.Handle(context.Background(), headReq, policy, fetch)
	if err != nil {
		t.Fatalf("head handle: %v", err)
	}
	if result.State != StateHit {
		t.Fatalf("expected HEAD to hit GET cache entry, got %s", result.State)
	}
	if fetches != 1 {
		t.Fatalf("expected one upstream fetch, got %d", fetches)
	}
}

func TestEngineHeadColdMissFetchesGetAndWarmsObject(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}

	seenMethods := make([]string, 0, 2)
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		seenMethods = append(seenMethods, req.Method)
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":   {"text/plain"},
				"Content-Length": {"7"},
			},
			Body: []byte("payload"),
		}, nil
	}

	headReq := httptest.NewRequest(http.MethodHead, "https://cdn.example.com/assets/app.js", nil)
	headResult, err := engine.Handle(context.Background(), headReq, policy, fetch)
	if err != nil {
		t.Fatalf("head miss handle: %v", err)
	}
	if headResult.State != StateMiss {
		t.Fatalf("expected cold HEAD to miss, got %s", headResult.State)
	}
	if len(seenMethods) != 1 || seenMethods[0] != http.MethodGet {
		t.Fatalf("expected HEAD miss to fetch GET once, got %#v", seenMethods)
	}
	if string(headResult.Body) != "payload" {
		t.Fatalf("expected HEAD miss to return fetched body to router layer, got %q", string(headResult.Body))
	}

	getReq := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	getResult, err := engine.Handle(context.Background(), getReq, policy, fetch)
	if err != nil {
		t.Fatalf("get hit after head warm: %v", err)
	}
	if getResult.State != StateHit {
		t.Fatalf("expected GET after HEAD miss to hit, got %s", getResult.State)
	}
	if len(seenMethods) != 1 {
		t.Fatalf("expected cache warm from HEAD miss to avoid second fetch, got %#v", seenMethods)
	}
}

func TestEngineCollapsesConcurrentColdMisses(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}

	const requests = 8
	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	start := make(chan struct{})
	entered := make(chan struct{}, requests)
	fetchCount := 0
	var mu sync.Mutex
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		mu.Lock()
		fetchCount++
		mu.Unlock()
		entered <- struct{}{}
		<-start
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       []byte("payload"),
		}, nil
	}

	var wg sync.WaitGroup
	results := make(chan Result, requests)
	errs := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := engine.Handle(context.Background(), req.Clone(context.Background()), policy, fetch)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}

	<-entered
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("collapsed miss failed: %v", err)
		}
	}

	mu.Lock()
	if fetchCount != 1 {
		t.Fatalf("expected one collapsed upstream fetch, got %d", fetchCount)
	}
	mu.Unlock()

	misses := 0
	hits := 0
	for result := range results {
		switch result.State {
		case StateMiss:
			misses++
		case StateHit:
			hits++
		default:
			t.Fatalf("expected collapsed cold requests to return MISS or HIT, got %s", result.State)
		}
	}
	if misses == 0 {
		t.Fatalf("expected at least one cold request to miss")
	}
	if misses+hits != requests {
		t.Fatalf("expected %d total collapsed results, got %d", requests, misses+hits)
	}

	hit, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("post-collapse hit: %v", err)
	}
	if hit.State != StateHit {
		t.Fatalf("expected subsequent request to hit, got %s", hit.State)
	}
}

func TestEngineFollowOriginRequestAndResponseMatrix(t *testing.T) {
	tests := []struct {
		name           string
		header         http.Header
		now            time.Time
		optimistic     bool
		wantStored     bool
		wantSecond     State
		wantFetches    int
		requestNoCache bool
	}{
		{
			name:        "max-age caches",
			header:      http.Header{"Cache-Control": {"public, max-age=30"}},
			wantStored:  true,
			wantSecond:  StateHit,
			wantFetches: 1,
		},
		{
			name:        "s-maxage wins",
			header:      http.Header{"Cache-Control": {"public, max-age=0, s-maxage=30"}},
			wantStored:  true,
			wantSecond:  StateHit,
			wantFetches: 1,
		},
		{
			name:        "expires caches",
			header:      http.Header{"Expires": {"Fri, 07 Mar 2026 00:00:30 GMT"}},
			now:         time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC),
			wantStored:  true,
			wantSecond:  StateHit,
			wantFetches: 1,
		},
		{
			name:        "no-store does not cache",
			header:      http.Header{"Cache-Control": {"public, no-store, max-age=30"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "private does not cache",
			header:      http.Header{"Cache-Control": {"private, max-age=30"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "vary star does not cache",
			header:      http.Header{"Cache-Control": {"public, max-age=30"}, "Vary": {"*"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "uncacheable status does not cache",
			header:      http.Header{"Cache-Control": {"public, max-age=30"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:           "client no-cache is ignored by edge cache",
			header:         http.Header{"Cache-Control": {"public, max-age=30"}},
			wantStored:     true,
			wantSecond:     StateHit,
			wantFetches:    1,
			requestNoCache: true,
		},
		{
			name:        "response no-cache without validators does not store",
			header:      http.Header{"Cache-Control": {"public, no-cache"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "partial content is not stored",
			header:      http.Header{"Cache-Control": {"public, max-age=30"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "content-range response is not stored",
			header:      http.Header{"Cache-Control": {"public, max-age=30"}, "Content-Range": {"bytes 0-9/100"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "set-cookie response is not stored",
			header:      http.Header{"Cache-Control": {"public, max-age=30"}, "Set-Cookie": {"session=abc123; Path=/; HttpOnly"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
		{
			name:        "multi header cache-control is parsed",
			header:      http.Header{"Cache-Control": {"public", "s-maxage=30"}},
			wantStored:  true,
			wantSecond:  StateHit,
			wantFetches: 1,
		},
		{
			name:        "multi header cache-control no-store wins",
			header:      http.Header{"Cache-Control": {"public, max-age=30", "no-store"}},
			wantStored:  false,
			wantSecond:  StateMiss,
			wantFetches: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMemoryStore()
			engine := NewEngine(store)
			now := tt.now
			if now.IsZero() {
				now = time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
			}
			engine.now = func() time.Time { return now }

			policy := Policy{
				SiteID:     "site-1",
				RuleID:     "rule-1",
				PolicyTag:  "rule-1|origin",
				Mode:       model.CacheModeFollowOrigin,
				Optimistic: tt.optimistic,
			}

			fetches := 0
			statusCode := http.StatusOK
			switch tt.name {
			case "uncacheable status does not cache":
				statusCode = http.StatusCreated
			case "partial content is not stored":
				statusCode = http.StatusPartialContent
			}
			fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
				fetches++
				return StoredResponse{
					StatusCode: statusCode,
					Header:     tt.header.Clone(),
					Body:       []byte("body"),
				}, nil
			}

			req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
			first, err := engine.Handle(context.Background(), req, policy, fetch)
			if err != nil {
				t.Fatalf("first handle: %v", err)
			}
			if tt.wantStored && first.State != StateMiss {
				t.Fatalf("expected first result to miss/store, got %s", first.State)
			}

			if tt.requestNoCache {
				req = httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
				req.Header.Set("Cache-Control", "no-cache")
			}

			second, err := engine.Handle(context.Background(), req, policy, fetch)
			if err != nil {
				t.Fatalf("second handle: %v", err)
			}
			if second.State != tt.wantSecond {
				t.Fatalf("expected second state %s, got %s", tt.wantSecond, second.State)
			}
			if fetches != tt.wantFetches {
				t.Fatalf("expected %d fetches, got %d", tt.wantFetches, fetches)
			}
			if tt.name == "response no-cache stores but revalidates every time" {
				baseKey := buildBaseCacheKey("site-1", http.MethodGet, "/assets/app.js", "")
				if _, found, err := store.GetEntry(context.Background(), responseKey(baseKey)); err != nil || !found {
					t.Fatalf("expected no-cache response to remain stored for edge revalidation, found=%t err=%v", found, err)
				}
			}
		})
	}
}

func TestEngineOptimisticUsesOriginStaleWindow(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:             "site-1",
		RuleID:             "rule-1",
		PolicyTag:          "rule-1|origin|optimistic",
		Mode:               model.CacheModeFollowOrigin,
		Optimistic:         true,
		OptimisticMaxStale: time.Minute,
	}

	fetches := 0
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		fetches++
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": {"public, max-age=1, stale-while-revalidate=120"}},
			Body:       []byte("body"),
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	if _, err := engine.Handle(context.Background(), req, policy, fetch); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	now = now.Add(90 * time.Second)
	result, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("stale optimistic hit: %v", err)
	}
	if result.State != StateStale {
		t.Fatalf("expected stale result inside origin stale-while-revalidate window, got %s", result.State)
	}
}

func TestEnginePurgeSiteClearsResponsesAndVarySpecs(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	now := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|origin",
		Mode:      model.CacheModeFollowOrigin,
	}

	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		header := http.Header{"Cache-Control": {"public, max-age=60"}}
		if strings.Contains(req.URL.Path, "vary") {
			header.Set("Vary", "Accept-Encoding")
		}
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       []byte(req.URL.Path),
		}, nil
	}

	req1 := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/static/a.js", nil)
	req2 := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/static/vary.js", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	if _, err := engine.Handle(context.Background(), req1, policy, fetch); err != nil {
		t.Fatalf("prime static entry: %v", err)
	}
	if _, err := engine.Handle(context.Background(), req2, policy, fetch); err != nil {
		t.Fatalf("prime vary entry: %v", err)
	}

	deleted, err := engine.PurgeSite(context.Background(), "site-1")
	if err != nil {
		t.Fatalf("purge site: %v", err)
	}
	if deleted < 3 {
		t.Fatalf("expected at least 3 deleted records, got %d", deleted)
	}

	if _, found, _ := store.GetEntry(context.Background(), responseKey(buildBaseCacheKey("site-1", http.MethodGet, "/static/a.js", ""))); found {
		t.Fatalf("expected static entry to be purged")
	}
	if _, found, _ := store.GetVary(context.Background(), varyKey(buildBaseCacheKey("site-1", http.MethodGet, "/static/vary.js", ""))); found {
		t.Fatalf("expected vary spec to be purged")
	}
}

func TestBadgerCacheHelpers(t *testing.T) {
	directives := parseCacheControl(`public, max-age="30", s-maxage=60, stale-if-error=120, stale-while-revalidate=30`)
	if directives.sMaxAge == nil || directives.maxAge == nil {
		t.Fatalf("expected max-age and s-maxage to parse")
	}
	if *directives.sMaxAge != 60*time.Second || *directives.maxAge != 30*time.Second {
		t.Fatalf("unexpected parsed durations: %#v", directives)
	}
	if directives.staleIfError != 120*time.Second || directives.staleWhileRevalidate != 30*time.Second {
		t.Fatalf("unexpected stale directives: %#v", directives)
	}

	headers, cacheable := parseVary([]string{"Accept-Encoding, accept-language", "Origin"})
	if !cacheable {
		t.Fatalf("expected vary to be cacheable")
	}
	expected := []string{"Accept-Encoding", "Accept-Language", "Origin"}
	if strings.Join(headers, ",") != strings.Join(expected, ",") {
		t.Fatalf("unexpected vary headers: %#v", headers)
	}

	if normalized := normalizeVaryHeaderValue("Accept-Encoding", []string{"gzip, br", "deflate"}); normalized != "gzip,br,deflate" {
		t.Fatalf("unexpected normalized accept-encoding %q", normalized)
	}
	if normalized := normalizeVaryHeaderValue("Accept-Language", []string{"en-US, zh-CN"}); normalized != "en-us,zh-cn" {
		t.Fatalf("unexpected normalized accept-language %q", normalized)
	}
}

func TestEntryResultDoesNotReplaySetCookieWhenNotStored(t *testing.T) {
	store := newMemoryStore()
	engine := NewEngine(store)
	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	fetch := func(_ context.Context, req *http.Request) (StoredResponse, error) {
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Cache-Control": {"public, max-age=60"},
				"Set-Cookie":    {"session=abc123; Path=/; HttpOnly"},
			},
			Body: []byte("body"),
		}, nil
	}

	first, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	second, err := engine.Handle(context.Background(), req, policy, fetch)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}

	if first.Header.Get("Set-Cookie") == "" {
		t.Fatalf("expected bypassed uncached response to keep Set-Cookie")
	}
	if second.State != StateMiss {
		t.Fatalf("expected second request to miss because Set-Cookie response should not be cached, got %s", second.State)
	}
}

func TestEngineStoreErrorsDoNotPretendStored(t *testing.T) {
	engine := NewEngine(&errorStore{putErr: context.DeadlineExceeded})
	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	result, err := engine.Handle(context.Background(), req, policy, func(_ context.Context, req *http.Request) (StoredResponse, error) {
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       []byte("body"),
		}, nil
	})
	if err != nil {
		t.Fatalf("handle with store error: %v", err)
	}
	if strings.Contains(result.CacheStatus, "stored") {
		t.Fatalf("did not expect stored cache status on store error: %q", result.CacheStatus)
	}
	if !strings.Contains(result.CacheStatus, "STORE_ERROR") {
		t.Fatalf("expected STORE_ERROR detail, got %q", result.CacheStatus)
	}
}

func TestEngineLookupErrorsSurfaceInCacheStatus(t *testing.T) {
	engine := NewEngine(&errorStore{getVaryErr: context.DeadlineExceeded})
	policy := Policy{
		SiteID:    "site-1",
		RuleID:    "rule-1",
		PolicyTag: "rule-1|force",
		Mode:      model.CacheModeForceCache,
		TTL:       time.Minute,
		HasTTL:    true,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	result, err := engine.Handle(context.Background(), req, policy, func(_ context.Context, req *http.Request) (StoredResponse, error) {
		return StoredResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       []byte("body"),
		}, nil
	})
	if err != nil {
		t.Fatalf("handle with lookup error: %v", err)
	}
	if !strings.Contains(result.CacheStatus, "STORE_READ_ERROR") {
		t.Fatalf("expected STORE_READ_ERROR detail, got %q", result.CacheStatus)
	}
}
