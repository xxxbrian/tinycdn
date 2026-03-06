package runtime

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"

	"tinycdn/internal/model"
)

const (
	HeaderClientCacheControl       = "X-TinyCDN-Client-Cache-Control"
	HeaderClientCacheControlAbsent = "X-TinyCDN-Client-Cache-Control-Absent"
	HeaderSouinCacheControl        = "Souin-Cache-Control"
	HeaderSurrogateControl         = "Surrogate-Control"
	HeaderCDNCacheControl          = "CDN-Cache-Control"
)

func buildReverseProxy(site *CompiledSite) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(site.Upstream)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		switch site.UpstreamMode {
		case model.UpstreamHostModeFollowRequest:
			return
		case model.UpstreamHostModeCustom:
			req.Host = site.UpstreamHost
			req.Header.Set("Host", site.UpstreamHost)
		default:
			req.Host = site.Upstream.Host
			req.Header.Set("Host", site.Upstream.Host)
		}
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		requestContext, ok := RequestContextFrom(resp.Request.Context())
		if !ok || requestContext.Rule == nil {
			return nil
		}

		ApplyRuleResponseHeaders(resp, requestContext.Site, requestContext.Rule)
		resp.Header.Set("X-TinyCDN-Site", requestContext.Site.Source.ID)
		resp.Header.Set("X-TinyCDN-Rule", requestContext.Rule.Source.ID)

		return nil
	}

	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		http.Error(rw, fmt.Sprintf("proxy upstream error: %v", err), http.StatusBadGateway)
	}

	return proxy
}

func ApplyRuleRequestHeaders(req *http.Request, rule *CompiledRule) {
	if rule == nil {
		return
	}

	if rule.Source.Action.Cache.Mode == model.CacheModeBypass {
		req.Header.Set("Cache-Control", "no-store")
		req.Header.Set("Pragma", "no-cache")
	}
}

func ApplyRuleResponseHeaders(resp *http.Response, site *CompiledSite, rule *CompiledRule) {
	if rule == nil {
		return
	}

	switch rule.Source.Action.Cache.Mode {
	case model.CacheModeFollowOrigin:
		if resp.Header.Get("Cache-Control") != "" {
			if site != nil && site.Source.Cache.OptimisticRefresh {
				preserveClientCacheControl(resp.Header)
				resp.Header.Set("Cache-Control", withOptimisticRefresh(resp.Header.Get("Cache-Control")))
			}
			return
		}
		if edgePolicy := firstAvailableEdgeCacheControl(resp.Header); edgePolicy != "" {
			preserveClientCacheControl(resp.Header)
			if site != nil && site.Source.Cache.OptimisticRefresh {
				edgePolicy = withOptimisticRefresh(edgePolicy)
			}
			resp.Header.Set("Cache-Control", edgePolicy)
			return
		}
		if !hasAnyEdgeCacheControl(resp.Header) {
			preserveClientCacheControl(resp.Header)
			resp.Header.Set("Cache-Control", "no-store")
		}
	case model.CacheModeBypass:
		preserveClientCacheControl(resp.Header)
		resp.Header.Set("Cache-Control", "no-store")
	case model.CacheModeForceCache, model.CacheModeOverrideOrigin:
		ttl := DefaultManagedTTL
		if rule.HasTTL {
			ttl = rule.TTL
		}

		directives := []string{
			"public",
			fmt.Sprintf("max-age=%d", int(ttl.Seconds())),
		}
		if rule.HasStaleIfError {
			directives = append(directives, fmt.Sprintf("stale-if-error=%d", int(rule.StaleIfError.Seconds())))
		}
		if site != nil && site.Source.Cache.OptimisticRefresh {
			directives = append(directives, fmt.Sprintf("stale-while-revalidate=%d", int(DefaultOptimisticSWR.Seconds())))
		}

		preserveClientCacheControl(resp.Header)
		resp.Header.Set("Cache-Control", strings.Join(directives, ", "))
	}
}

func withOptimisticRefresh(cacheControl string) string {
	if strings.Contains(strings.ToLower(cacheControl), "stale-while-revalidate=") {
		return cacheControl
	}

	return cacheControl + fmt.Sprintf(", stale-while-revalidate=%d", int(DefaultOptimisticSWR.Seconds()))
}

func preserveClientCacheControl(header http.Header) {
	if header.Get(HeaderClientCacheControl) != "" || header.Get(HeaderClientCacheControlAbsent) != "" {
		return
	}

	values := header.Values("Cache-Control")
	if len(values) == 0 {
		header.Set(HeaderClientCacheControlAbsent, "1")
		return
	}

	header.Set(HeaderClientCacheControl, strings.Join(values, ", "))
}

func hasAnyEdgeCacheControl(header http.Header) bool {
	return header.Get(HeaderSouinCacheControl) != "" ||
		header.Get(HeaderSurrogateControl) != "" ||
		header.Get(HeaderCDNCacheControl) != "" ||
		header.Get("Cache-Control") != ""
}

func firstAvailableEdgeCacheControl(header http.Header) string {
	for _, headerName := range []string{HeaderSouinCacheControl, HeaderSurrogateControl, HeaderCDNCacheControl} {
		if value := header.Get(headerName); value != "" {
			return value
		}
	}

	return ""
}
