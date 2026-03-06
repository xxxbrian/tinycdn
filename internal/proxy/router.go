package proxy

import (
	"net/http"
	"slices"
	"strings"

	"github.com/darkweak/souin/configurationtypes"
	souinmiddleware "github.com/darkweak/souin/pkg/middleware"
	souinchi "github.com/darkweak/souin/plugins/chi"

	"tinycdn/internal/runtime"
)

func NewRouter(snapshot func() *runtime.Snapshot, cachePath string) http.Handler {
	cache := souinchi.NewHTTPCache(souinmiddleware.BaseConfiguration{
		DefaultCache: &configurationtypes.DefaultCache{
			TTL: configurationtypes.Duration{
				Duration: runtime.DefaultManagedTTL,
			},
			Stale: configurationtypes.Duration{
				Duration: runtime.MaxManagedStaleRetention,
			},
			Badger: configurationtypes.CacheProvider{
				Path: cachePath,
			},
		},
		LogLevel: "error",
	})

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/healthz" {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("ok"))
			return
		}

		matchHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			current := snapshot()
			site := current.SiteByHost(req.Host)
			if site == nil || !site.Source.Enabled {
				http.NotFound(rw, req)
				return
			}

			rule := site.MatchRule(req)
			if rule == nil {
				http.Error(rw, "no matching rule in runtime snapshot", http.StatusInternalServerError)
				return
			}

			ctx := runtime.ContextWithRequestContext(req.Context(), runtime.RequestContext{
				Site: site,
				Rule: rule,
			})
			cacheReq := req.Clone(ctx)
			runtime.ApplyRuleRequestHeaders(cacheReq, rule)
			filteredWriter := newInternalHeaderFilter(rw)

			cache.Handle(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				site.Proxy.ServeHTTP(rw, req.WithContext(ctx))
			})).ServeHTTP(filteredWriter, cacheReq)
		})

		matchHandler.ServeHTTP(rw, req)
	})
}

type internalHeaderFilter struct {
	rw          http.ResponseWriter
	header      http.Header
	wroteHeader bool
}

func newInternalHeaderFilter(rw http.ResponseWriter) *internalHeaderFilter {
	return &internalHeaderFilter{
		rw:     rw,
		header: make(http.Header),
	}
}

func (f *internalHeaderFilter) Header() http.Header {
	return f.header
}

func (f *internalHeaderFilter) WriteHeader(statusCode int) {
	if f.wroteHeader {
		return
	}

	restoreClientCacheControl(f.header)
	f.header.Set(runtime.HeaderTinyCDNCache, deriveTinyCDNCache(f.header, statusCode))

	for key, values := range f.header {
		if isInternalCacheHeader(key) {
			continue
		}
		for _, value := range values {
			f.rw.Header().Add(key, value)
		}
	}

	f.wroteHeader = true
	f.rw.WriteHeader(statusCode)
}

func (f *internalHeaderFilter) Write(body []byte) (int, error) {
	if !f.wroteHeader {
		f.WriteHeader(http.StatusOK)
	}

	return f.rw.Write(body)
}

func (f *internalHeaderFilter) Flush() {
	if flusher, ok := f.rw.(http.Flusher); ok {
		if !f.wroteHeader {
			f.WriteHeader(http.StatusOK)
		}
		flusher.Flush()
	}
}

func restoreClientCacheControl(header http.Header) {
	clientCacheControl := header.Get(runtime.HeaderClientCacheControl)
	clientCacheControlAbsent := header.Get(runtime.HeaderClientCacheControlAbsent)

	switch {
	case clientCacheControlAbsent == "1":
		header.Del("Cache-Control")
	case clientCacheControl != "":
		header.Del("Cache-Control")
		header.Set("Cache-Control", clientCacheControl)
	}

	header.Del(runtime.HeaderClientCacheControl)
	header.Del(runtime.HeaderClientCacheControlAbsent)
}

func isInternalCacheHeader(headerName string) bool {
	return slices.Contains([]string{
		runtime.HeaderClientCacheControl,
		runtime.HeaderClientCacheControlAbsent,
		runtime.HeaderSouinCacheControl,
		runtime.HeaderSurrogateControl,
		runtime.HeaderCDNCacheControl,
	}, headerName)
}

func deriveTinyCDNCache(header http.Header, statusCode int) string {
	cacheStatus := strings.ToLower(header.Get("Cache-Status"))

	switch {
	case strings.Contains(cacheStatus, "detail=serve-http-error"),
		strings.Contains(cacheStatus, "detail=deadline-exceeded"):
		return "ERROR"
	case strings.Contains(cacheStatus, "fwd=stale"):
		return "STALE"
	case strings.Contains(cacheStatus, "; hit"):
		return "HIT"
	case strings.Contains(cacheStatus, "fwd=bypass"):
		return "BYPASS"
	case strings.Contains(cacheStatus, "fwd=uri-miss"),
		strings.Contains(cacheStatus, "fwd=request"):
		return "MISS"
	case statusCode >= http.StatusInternalServerError:
		return "ERROR"
	default:
		return "MISS"
	}
}
