package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"tinycdn/internal/cache"
	"tinycdn/internal/runtime"
)

const (
	headerTinyCDNCache = "X-TinyCDN-Cache"
	headerTinyCDNSite  = "X-TinyCDN-Site"
	headerTinyCDNRule  = "X-TinyCDN-Rule"
	headerCacheStatus  = "Cache-Status"
)

type Router struct {
	engine   *cache.Engine
	store    cache.Store
	fetcher  *upstreamFetcher
	snapshot func() *runtime.Snapshot
}

func NewRouter(snapshot func() *runtime.Snapshot, cachePath string) (*Router, error) {
	store, err := cache.NewBadgerStore(cachePath)
	if err != nil {
		return nil, err
	}

	return &Router{
		engine:   cache.NewEngine(store),
		store:    store,
		fetcher:  newUpstreamFetcher(),
		snapshot: snapshot,
	}, nil
}

func (r *Router) Close() error {
	return r.store.Close()
}

func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/healthz" {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("ok"))
		return
	}

	current := r.snapshot()
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

	policy := cache.BuildPolicy(site.Source.ID, rule.Source, rule.TTL, rule.HasTTL, rule.StaleIfError, rule.HasStaleIfError)
	result, err := r.engine.Handle(req.Context(), req, policy, func(ctx context.Context, fetchReq *http.Request) (cache.StoredResponse, error) {
		return r.fetcher.Fetch(ctx, fetchReq, site)
	})
	if err != nil {
		writeErrorResponse(rw, site, rule, err)
		return
	}

	writeResult(rw, req, site, rule, result)
}

func writeResult(rw http.ResponseWriter, req *http.Request, site *runtime.CompiledSite, rule *runtime.CompiledRule, result cache.Result) {
	header := rw.Header()
	for key, values := range result.Header {
		for _, value := range values {
			header.Add(key, value)
		}
	}
	header.Set(headerTinyCDNCache, string(result.State))
	header.Set(headerTinyCDNSite, site.Source.ID)
	header.Set(headerTinyCDNRule, rule.Source.ID)
	header.Set(headerCacheStatus, result.CacheStatus)
	header.Set("Content-Length", strconv.Itoa(len(result.Body)))

	rw.WriteHeader(result.StatusCode)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = rw.Write(result.Body)
}

func writeErrorResponse(rw http.ResponseWriter, site *runtime.CompiledSite, rule *runtime.CompiledRule, err error) {
	header := rw.Header()
	header.Set(headerTinyCDNCache, string(cache.StateError))
	header.Set(headerTinyCDNSite, site.Source.ID)
	header.Set(headerTinyCDNRule, rule.Source.ID)
	header.Set(headerCacheStatus, "TinyCDN; fwd=error; detail=UPSTREAM")
	http.Error(rw, fmt.Sprintf("proxy upstream error: %v", err), http.StatusBadGateway)
}
