package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

func TestBuildUpstreamRequestHostModes(t *testing.T) {
	upstream, err := url.Parse("https://origin.example.com")
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}

	tests := []struct {
		name         string
		mode         model.UpstreamHostMode
		explicitHost string
		wantHost     string
	}{
		{
			name:     "follow origin",
			mode:     model.UpstreamHostModeFollowOrigin,
			wantHost: "origin.example.com",
		},
		{
			name:     "follow request",
			mode:     model.UpstreamHostModeFollowRequest,
			wantHost: "cdn.example.com",
		},
		{
			name:         "custom",
			mode:         model.UpstreamHostModeCustom,
			explicitHost: "a.com",
			wantHost:     "a.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := &runtime.CompiledSite{
				Upstream:     upstream,
				UpstreamMode: tt.mode,
				UpstreamHost: upstream.Host,
			}
			if tt.explicitHost != "" {
				site.UpstreamHost = tt.explicitHost
			}

			req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js?build=1", nil)
			req.Host = "cdn.example.com"

			upstreamReq, err := buildUpstreamRequest(req.Context(), req, site)
			if err != nil {
				t.Fatalf("build upstream request: %v", err)
			}

			if upstreamReq.Host != tt.wantHost {
				t.Fatalf("expected upstream host %q, got %q", tt.wantHost, upstreamReq.Host)
			}
			if upstreamReq.URL.String() != "https://origin.example.com/assets/app.js?build=1" {
				t.Fatalf("unexpected upstream url %q", upstreamReq.URL.String())
			}
		})
	}
}

func TestBuildUpstreamRequestStripsHopByHopHeaders(t *testing.T) {
	upstream, err := url.Parse("https://origin.example.com")
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}

	site := &runtime.CompiledSite{
		Upstream:     upstream,
		UpstreamMode: model.UpstreamHostModeFollowOrigin,
		UpstreamHost: upstream.Host,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"
	req.Header.Set("Connection", "keep-alive, X-Debug")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("X-Debug", "edge-only")

	upstreamReq, err := buildUpstreamRequest(req.Context(), req, site)
	if err != nil {
		t.Fatalf("build upstream request: %v", err)
	}

	if got := upstreamReq.Header.Get("Connection"); got != "" {
		t.Fatalf("expected Connection to be stripped, got %q", got)
	}
	if got := upstreamReq.Header.Get("Keep-Alive"); got != "" {
		t.Fatalf("expected Keep-Alive to be stripped, got %q", got)
	}
	if got := upstreamReq.Header.Get("X-Debug"); got != "" {
		t.Fatalf("expected Connection-token header to be stripped, got %q", got)
	}
}

func TestBuildUpstreamRequestAddsForwardingHeaders(t *testing.T) {
	upstream, err := url.Parse("https://origin.example.com")
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}

	site := &runtime.CompiledSite{
		Upstream:     upstream,
		UpstreamMode: model.UpstreamHostModeFollowOrigin,
		UpstreamHost: upstream.Host,
	}

	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"
	req.RemoteAddr = "203.0.113.10:4321"
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Via", "1.0 upstream-gateway")

	upstreamReq, err := buildUpstreamRequest(req.Context(), req, site)
	if err != nil {
		t.Fatalf("build upstream request: %v", err)
	}

	if got := upstreamReq.Header.Get("X-Forwarded-For"); got != "198.51.100.7, 203.0.113.10" {
		t.Fatalf("unexpected X-Forwarded-For %q", got)
	}
	if got := upstreamReq.Header.Get("X-Forwarded-Host"); got != "cdn.example.com" {
		t.Fatalf("unexpected X-Forwarded-Host %q", got)
	}
	if got := upstreamReq.Header.Get("X-Forwarded-Proto"); got != "https" {
		t.Fatalf("unexpected X-Forwarded-Proto %q", got)
	}
	if got := upstreamReq.Header.Get("Via"); got != "1.0 upstream-gateway, 1.1 tinycdn" {
		t.Fatalf("unexpected Via header %q", got)
	}
	if got := upstreamReq.Header.Get("Forwarded"); got != `for="203.0.113.10";host="cdn.example.com";proto=https` {
		t.Fatalf("unexpected Forwarded header %q", got)
	}
}

func TestNewUpstreamFetcherConfiguresTimeout(t *testing.T) {
	fetcher, err := newUpstreamFetcher(t.TempDir())
	if err != nil {
		t.Fatalf("new upstream fetcher: %v", err)
	}
	if fetcher.client.Timeout != defaultUpstreamTimeout {
		t.Fatalf("expected upstream timeout %s, got %s", defaultUpstreamTimeout, fetcher.client.Timeout)
	}
}

func TestFetchRejectsOversizedContentLengthBeforeBuffering(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		upstreamHits++
		rw.Header().Set("Content-Length", "10")
		rw.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(rw, "1234567890")
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}

	cachePath := t.TempDir()
	fetcher, err := newUpstreamFetcher(cachePath)
	if err != nil {
		t.Fatalf("new upstream fetcher: %v", err)
	}
	fetcher.maxCacheableObjectBytes = 4
	site := &runtime.CompiledSite{
		Upstream:     upstreamURL,
		UpstreamMode: model.UpstreamHostModeFollowOrigin,
		UpstreamHost: upstreamURL.Host,
	}
	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"

	_, err = fetcher.Fetch(context.Background(), req, site)
	if !errors.Is(err, ErrUpstreamResponseTooLarge) {
		t.Fatalf("expected oversized fetch error, got %v", err)
	}
	if upstreamHits != 1 {
		t.Fatalf("expected one upstream hit, got %d", upstreamHits)
	}
}

func TestFetchWritesBodyToTempFile(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Length", "7")
		rw.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(rw, "payload")
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}

	cachePath := t.TempDir()
	fetcher, err := newUpstreamFetcher(cachePath)
	if err != nil {
		t.Fatalf("new upstream fetcher: %v", err)
	}
	site := &runtime.CompiledSite{
		Upstream:     upstreamURL,
		UpstreamMode: model.UpstreamHostModeFollowOrigin,
		UpstreamHost: upstreamURL.Host,
	}
	req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com/assets/app.js", nil)
	req.Host = "cdn.example.com"

	response, err := fetcher.Fetch(context.Background(), req, site)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.BodyPath == "" {
		t.Fatalf("expected temp body path to be set")
	}
	if !response.CleanupPath {
		t.Fatalf("expected fetched temp body to request cleanup before store adoption")
	}
	if response.ContentLength != 7 {
		t.Fatalf("expected content length 7, got %d", response.ContentLength)
	}
	payload, err := os.ReadFile(response.BodyPath)
	if err != nil {
		t.Fatalf("read temp body: %v", err)
	}
	if string(payload) != "payload" {
		t.Fatalf("unexpected temp body %q", string(payload))
	}
	if filepath.Dir(response.BodyPath) != filepath.Join(cachePath, "tmp") {
		t.Fatalf("expected temp body under cache tmp dir, got %q", response.BodyPath)
	}
}

func TestNewUpstreamFetcherCleansTempDirectory(t *testing.T) {
	cachePath := t.TempDir()
	tempDir := filepath.Join(cachePath, "tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatalf("mkdir temp dir: %v", err)
	}
	staleFile := filepath.Join(tempDir, "stale-body")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}
	staleDir := filepath.Join(tempDir, "stale-dir")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir stale dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "nested"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write stale nested file: %v", err)
	}

	fetcher, err := newUpstreamFetcher(cachePath)
	if err != nil {
		t.Fatalf("new upstream fetcher: %v", err)
	}
	if fetcher.tempDir != tempDir {
		t.Fatalf("expected temp dir %q, got %q", tempDir, fetcher.tempDir)
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp cleanup to remove stale startup files, got %d entries", len(entries))
	}
}
