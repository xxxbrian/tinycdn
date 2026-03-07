package proxy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

func TestRouterCacheHeadersAndHeadBehavior(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		rw.Header().Set("Content-Type", "text/plain")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("payload"))
	}))
	defer upstream.Close()

	router := newTestRouter(t, newTestSnapshot(t, upstream.URL))
	defer router.Close()

	getReq := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	getReq.Host = "cdn.example.com"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, getReq)
	if got := first.Result().Header.Get(headerTinyCDNCache); got != "MISS" {
		t.Fatalf("expected first request MISS, got %q", got)
	}
	if got := first.Result().Header.Get(headerCacheStatus); got != "TinyCDN; fwd=uri-miss; key=resp|site-1|GET|/assets/app.js" {
		t.Fatalf("unexpected miss cache status, got %q", got)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, getReq)
	if got := second.Result().Header.Get(headerTinyCDNCache); got != "HIT" {
		t.Fatalf("expected second request HIT, got %q", got)
	}
	if got := second.Result().Header.Get("Age"); got == "" {
		t.Fatalf("expected Age header on cache hit")
	}
	if body := second.Body.String(); body != "payload" {
		t.Fatalf("unexpected hit body %q", body)
	}

	headReq := httptest.NewRequest(http.MethodHead, "http://cdn.example.com/assets/app.js", nil)
	headReq.Host = "cdn.example.com"
	head := httptest.NewRecorder()
	router.ServeHTTP(head, headReq)
	if got := head.Result().Header.Get(headerTinyCDNCache); got != "HIT" {
		t.Fatalf("expected HEAD request HIT, got %q", got)
	}
	if head.Body.Len() != 0 {
		t.Fatalf("expected no HEAD body, got %q", head.Body.String())
	}

	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("expected one upstream fetch, got %d", got)
	}
}

func TestRouterHeadColdMissFetchesGetAndPreservesLength(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		if req.Method != http.MethodGet {
			t.Fatalf("expected HEAD cold miss to fetch GET upstream, got %s", req.Method)
		}
		rw.Header().Set("Content-Type", "text/plain")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("payload"))
	}))
	defer upstream.Close()

	router := newTestRouter(t, newTestSnapshot(t, upstream.URL))
	defer router.Close()

	headReq := httptest.NewRequest(http.MethodHead, "http://cdn.example.com/assets/app.js", nil)
	headReq.Host = "cdn.example.com"
	head := httptest.NewRecorder()
	router.ServeHTTP(head, headReq)

	if got := head.Result().Header.Get(headerTinyCDNCache); got != "MISS" {
		t.Fatalf("expected cold HEAD request MISS, got %q", got)
	}
	if got := head.Result().Header.Get("Content-Length"); got != "7" {
		t.Fatalf("expected HEAD cold miss to preserve GET content length, got %q", got)
	}
	if head.Body.Len() != 0 {
		t.Fatalf("expected no HEAD body, got %q", head.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	getReq.Host = "cdn.example.com"
	get := httptest.NewRecorder()
	router.ServeHTTP(get, getReq)

	if got := get.Result().Header.Get(headerTinyCDNCache); got != "HIT" {
		t.Fatalf("expected GET after HEAD warm to hit, got %q", got)
	}
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("expected one upstream fetch, got %d", got)
	}
}

func TestRouterServesCachedBodyAfterOriginRevalidation(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		hit := upstreamHits.Add(1)
		switch hit {
		case 1:
			rw.Header().Set("Cache-Control", "public, no-cache")
			rw.Header().Set("ETag", `"v1"`)
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("payload"))
		case 2:
			if got := req.Header.Get("If-None-Match"); got != `"v1"` {
				t.Fatalf("expected edge revalidation header, got %q", got)
			}
			rw.Header().Set("Cache-Control", "public, max-age=60")
			rw.Header().Set("ETag", `"v1"`)
			rw.WriteHeader(http.StatusNotModified)
		default:
			t.Fatalf("unexpected third upstream fetch")
		}
	}))
	defer upstream.Close()

	router := newTestRouter(t, newFollowOriginSnapshot(t, upstream.URL))
	defer router.Close()

	req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"

	first := httptest.NewRecorder()
	router.ServeHTTP(first, req)
	if got := first.Result().Header.Get(headerTinyCDNCache); got != "MISS" {
		t.Fatalf("expected first request MISS, got %q", got)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, req)
	if got := second.Result().StatusCode; got != http.StatusOK {
		t.Fatalf("expected revalidated request to return cached 200, got %d", got)
	}
	if got := second.Result().Header.Get(headerTinyCDNCache); got != "HIT" {
		t.Fatalf("expected revalidated request HIT, got %q", got)
	}
	if !strings.Contains(second.Result().Header.Get(headerCacheStatus), "detail=REVALIDATED") {
		t.Fatalf("expected revalidated cache status, got %q", second.Result().Header.Get(headerCacheStatus))
	}
	if second.Body.String() != "payload" {
		t.Fatalf("expected cached body after 304 revalidation, got %q", second.Body.String())
	}

	third := httptest.NewRecorder()
	router.ServeHTTP(third, req)
	if got := third.Result().Header.Get(headerTinyCDNCache); got != "HIT" {
		t.Fatalf("expected third request HIT, got %q", got)
	}
	if got := upstreamHits.Load(); got != 2 {
		t.Fatalf("expected two upstream fetches, got %d", got)
	}
}

func TestRouterBypassesRangeRequests(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		rw.Header().Set("Content-Type", "text/plain")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("payload"))
	}))
	defer upstream.Close()

	router := newTestRouter(t, newTestSnapshot(t, upstream.URL))
	defer router.Close()

	req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"
	req.Header.Set("Range", "bytes=0-10")

	first := httptest.NewRecorder()
	router.ServeHTTP(first, req)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, req)

	if got := first.Result().Header.Get(headerTinyCDNCache); got != "BYPASS" {
		t.Fatalf("expected range request BYPASS, got %q", got)
	}
	if got := second.Result().Header.Get(headerTinyCDNCache); got != "BYPASS" {
		t.Fatalf("expected repeated range request BYPASS, got %q", got)
	}
	if got := upstreamHits.Load(); got != 2 {
		t.Fatalf("expected two upstream hits for range bypass, got %d", got)
	}
}

func TestRouterStreamsBypassRequests(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		rw.Header().Set("Content-Type", "text/plain")
		rw.Header().Set("Content-Length", "7")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("payload"))
	}))
	defer upstream.Close()

	router := newTestRouter(t, newTestSnapshot(t, upstream.URL))
	defer router.Close()

	req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"
	req.Header.Set("Cookie", "session=abc123")

	first := httptest.NewRecorder()
	router.ServeHTTP(first, req)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, req)

	if got := first.Result().Header.Get(headerTinyCDNCache); got != "BYPASS" {
		t.Fatalf("expected sensitive request BYPASS, got %q", got)
	}
	if got := first.Result().Header.Get(headerCacheStatus); got != "TinyCDN; fwd=bypass; detail=request" {
		t.Fatalf("unexpected bypass cache status %q", got)
	}
	if first.Body.String() != "payload" {
		t.Fatalf("unexpected bypass body %q", first.Body.String())
	}
	if got := second.Result().Header.Get(headerTinyCDNCache); got != "BYPASS" {
		t.Fatalf("expected repeated sensitive request BYPASS, got %q", got)
	}
	if got := upstreamHits.Load(); got != 2 {
		t.Fatalf("expected two upstream hits for streamed bypass, got %d", got)
	}
}

func TestRouterStreamsOversizedObjectsWithoutCaching(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		rw.Header().Set("Content-Type", "text/plain")
		rw.Header().Set("Content-Length", "10")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("1234567890"))
	}))
	defer upstream.Close()

	router := newTestRouter(t, newTestSnapshot(t, upstream.URL))
	router.fetcher.maxCacheableObjectBytes = 4
	defer router.Close()

	req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"

	first := httptest.NewRecorder()
	router.ServeHTTP(first, req)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, req)

	if got := first.Result().Header.Get(headerTinyCDNCache); got != "BYPASS" {
		t.Fatalf("expected oversized response to bypass, got %q", got)
	}
	if got := first.Result().Header.Get(headerCacheStatus); got != "TinyCDN; fwd=bypass; detail=object-too-large" {
		t.Fatalf("unexpected oversized cache status %q", got)
	}
	if first.Body.String() != "1234567890" {
		t.Fatalf("unexpected oversized body %q", first.Body.String())
	}
	if got := second.Result().Header.Get(headerTinyCDNCache); got != "BYPASS" {
		t.Fatalf("expected repeated oversized response to bypass, got %q", got)
	}
	if got := upstreamHits.Load(); got != 2 {
		t.Fatalf("expected one upstream fetch per oversized bypass request, got %d", got)
	}
}

func TestRouterStripsHopByHopResponseHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Connection", "keep-alive, X-Debug")
		rw.Header().Set("Keep-Alive", "timeout=5")
		rw.Header().Set("X-Debug", "upstream-secret")
		rw.Header().Set("Content-Type", "text/plain")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("payload"))
	}))
	defer upstream.Close()

	router := newTestRouter(t, newTestSnapshot(t, upstream.URL))
	defer router.Close()

	req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	response := recorder.Result()

	if got := response.Header.Get("Connection"); got != "" {
		t.Fatalf("expected Connection to be stripped, got %q", got)
	}
	if got := response.Header.Get("Keep-Alive"); got != "" {
		t.Fatalf("expected Keep-Alive to be stripped, got %q", got)
	}
	if got := response.Header.Get("X-Debug"); got != "" {
		t.Fatalf("expected Connection-token header to be stripped, got %q", got)
	}
}

func newTestRouter(t *testing.T, snapshot *runtime.Snapshot) *Router {
	t.Helper()
	router, err := NewRouter(func() *runtime.Snapshot { return snapshot }, filepath.Join(t.TempDir(), "badger"), nil)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	return router
}

func newTestSnapshot(t *testing.T, upstreamURL string) *runtime.Snapshot {
	t.Helper()
	cfg := model.AppConfig{
		Sites: []model.Site{
			{
				ID:      "site-1",
				Name:    "Site 1",
				Enabled: true,
				Hosts:   []string{"cdn.example.com"},
				Upstream: model.Upstream{
					URL: upstreamURL,
				},
				Rules: []model.Rule{
					{
						ID:      "rule-1",
						Name:    "Assets",
						Enabled: true,
						Match: model.MatchSpec{
							Clauses: []model.MatchClause{
								{
									Field:    model.MatchFieldURIPath,
									Operator: model.MatchOperatorStartsWith,
									Value:    "/assets/",
								},
							},
						},
						Action: model.RuleAction{
							Cache: model.CacheAction{
								Mode: model.CacheModeForceCache,
								TTL:  "5m",
							},
						},
					},
					model.NewDefaultRule(),
				},
			},
		},
	}

	snapshot, err := runtime.Compile(cfg)
	if err != nil {
		t.Fatalf("compile runtime: %v", err)
	}
	return snapshot
}

func newFollowOriginSnapshot(t *testing.T, upstreamURL string) *runtime.Snapshot {
	t.Helper()
	cfg := model.AppConfig{
		Sites: []model.Site{
			{
				ID:      "site-1",
				Name:    "Site 1",
				Enabled: true,
				Hosts:   []string{"cdn.example.com"},
				Upstream: model.Upstream{
					URL: upstreamURL,
				},
				Rules: []model.Rule{
					{
						ID:      "rule-1",
						Name:    "Assets",
						Enabled: true,
						Match: model.MatchSpec{
							Clauses: []model.MatchClause{
								{
									Field:    model.MatchFieldURIPath,
									Operator: model.MatchOperatorStartsWith,
									Value:    "/assets/",
								},
							},
						},
						Action: model.RuleAction{
							Cache: model.CacheAction{
								Mode: model.CacheModeFollowOrigin,
							},
						},
					},
					model.NewDefaultRule(),
				},
			},
		},
	}

	snapshot, err := runtime.Compile(cfg)
	if err != nil {
		t.Fatalf("compile runtime: %v", err)
	}
	return snapshot
}
