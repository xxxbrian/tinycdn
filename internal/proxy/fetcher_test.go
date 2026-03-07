package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
