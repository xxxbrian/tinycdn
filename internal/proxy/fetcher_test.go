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
