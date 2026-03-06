package proxy

import (
	"net/http"
	"testing"
)

func TestDeriveTinyCDNCache(t *testing.T) {
	tests := []struct {
		name        string
		cacheStatus string
		statusCode  int
		want        string
	}{
		{
			name:        "hit",
			cacheStatus: "Souin; hit; ttl=4; key=GET-http-example.com-/handled; detail=DEFAULT",
			statusCode:  http.StatusOK,
			want:        "HIT",
		},
		{
			name:        "miss",
			cacheStatus: "Souin; fwd=uri-miss; stored; key=GET-http-example.com-/handled",
			statusCode:  http.StatusOK,
			want:        "MISS",
		},
		{
			name:        "stale",
			cacheStatus: "Souin; hit; ttl=0; key=GET-http-example.com-/handled; detail=DEFAULT; fwd=stale",
			statusCode:  http.StatusOK,
			want:        "STALE",
		},
		{
			name:        "bypass",
			cacheStatus: "Souin; fwd=bypass; detail=UNSUPPORTED-METHOD",
			statusCode:  http.StatusOK,
			want:        "BYPASS",
		},
		{
			name:        "serve http error",
			cacheStatus: "Souin; fwd=uri-miss; key=GET-http-example.com-/broken; detail=SERVE-HTTP-ERROR",
			statusCode:  http.StatusBadGateway,
			want:        "ERROR",
		},
		{
			name:       "plain upstream error fallback",
			statusCode: http.StatusBadGateway,
			want:       "ERROR",
		},
		{
			name:       "no cache status defaults to miss",
			statusCode: http.StatusOK,
			want:       "MISS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := make(http.Header)
			if tt.cacheStatus != "" {
				header.Set("Cache-Status", tt.cacheStatus)
			}

			if got := deriveTinyCDNCache(header, tt.statusCode); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
