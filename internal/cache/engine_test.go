package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tinycdn/internal/model"
)

type memoryStore struct {
	mu      sync.RWMutex
	entries map[string]Entry
	varies  map[string]VarySpec
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		entries: map[string]Entry{},
		varies:  map[string]VarySpec{},
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

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	delete(s.varies, key)
	return nil
}

func (s *memoryStore) DeletePrefix(_ context.Context, prefix string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for key := range s.entries {
		if strings.HasPrefix(key, prefix) {
			delete(s.entries, key)
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

func (s *memoryStore) Close() error {
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
