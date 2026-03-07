package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"tinycdn/internal/cache"
	"tinycdn/internal/httpx"
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
		fetcher:  newUpstreamFetcher(cachePath),
		snapshot: snapshot,
	}, nil
}

func (r *Router) Close() error {
	return r.store.Close()
}

func (r *Router) PurgeSite(ctx context.Context, siteID string) (int, error) {
	return r.engine.PurgeSite(ctx, siteID)
}

func (r *Router) PurgeURL(ctx context.Context, siteID, path, rawQuery string) (int, error) {
	return r.engine.PurgeURL(ctx, siteID, path, rawQuery)
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
	if cache.PreviewRequestBehavior(req, policy) == cache.StateBypass {
		r.streamBypass(rw, req, site, rule, "request")
		return
	}

	result, plan, err := r.engine.Start(req.Context(), req, policy)
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseTooLarge) {
			r.streamBypass(rw, req, site, rule, "object-too-large")
			return
		}
		writeErrorResponse(rw, site, rule, err)
		return
	}
	if plan != nil {
		if result.State == cache.StateStale {
			go r.fillInBackground(site, plan)
			writeResult(rw, req, site, rule, result)
			return
		}
		r.streamFill(rw, req, site, rule, plan)
		return
	}

	writeResult(rw, req, site, rule, result)
}

func (r *Router) fillInBackground(site *runtime.CompiledSite, plan *cache.FillPlan) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultUpstreamTimeout)
	defer cancel()

	response, err := r.fetcher.Fetch(ctx, plan.Request(ctx), site)
	if err != nil {
		_, _ = r.engine.HandleFillError(plan, err)
		return
	}
	if _, ok, err := r.engine.RevalidatedResult(ctx, plan, response); err != nil {
		return
	} else if ok {
		if response.CleanupPath {
			_ = os.Remove(response.BodyPath)
		}
		return
	}
	_ = r.engine.CompleteFill(ctx, plan, response)
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
	if result.ContentLength >= 0 {
		header.Set("Content-Length", strconv.FormatInt(result.ContentLength, 10))
	}

	rw.WriteHeader(result.StatusCode)
	if result.CleanupPath {
		defer func() { _ = os.Remove(result.BodyPath) }()
	}
	if req.Method == http.MethodHead {
		return
	}
	if result.BodyPath != "" {
		body, err := os.Open(result.BodyPath)
		if err != nil {
			http.Error(rw, fmt.Sprintf("proxy body open error: %v", err), http.StatusInternalServerError)
			return
		}
		defer body.Close()
		_, _ = io.Copy(rw, body)
		return
	}
	_, _ = rw.Write(result.Body)
}

func (r *Router) streamFill(rw http.ResponseWriter, req *http.Request, site *runtime.CompiledSite, rule *runtime.CompiledRule, plan *cache.FillPlan) {
	resp, err := r.fetcher.Open(req.Context(), plan.Request(req.Context()), site)
	if err != nil {
		if fallback, ok := r.engine.HandleFillError(plan, err); ok {
			writeResult(rw, req, site, rule, fallback)
			return
		}
		writeErrorResponse(rw, site, rule, err)
		return
	}
	defer resp.Body.Close()

	if result, ok, err := r.engine.RevalidatedResult(req.Context(), plan, cache.StoredResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
	}); err != nil {
		writeErrorResponse(rw, site, rule, err)
		return
	} else if ok {
		writeResult(rw, req, site, rule, result)
		return
	}

	if responseTooLarge(resp, r.fetcher.maxCacheableObjectBytes) {
		r.engine.AbortFill(plan, cache.StoredResponse{})
		r.writeStreamedResponse(rw, req, site, rule, resp, cache.StateBypass, "TinyCDN; fwd=bypass; detail=object-too-large")
		return
	}

	responseHeader := resp.Header.Clone()
	httpx.StripHopByHopHeaders(responseHeader)

	if err := os.MkdirAll(r.fetcher.tempDir, 0o755); err != nil {
		r.engine.AbortFill(plan, cache.StoredResponse{})
		writeErrorResponse(rw, site, rule, err)
		return
	}
	tempFile, err := os.CreateTemp(r.fetcher.tempDir, "body-*")
	if err != nil {
		r.engine.AbortFill(plan, cache.StoredResponse{})
		writeErrorResponse(rw, site, rule, err)
		return
	}
	tempPath := tempFile.Name()
	defer tempFile.Close()

	header := rw.Header()
	for key, values := range responseHeader {
		for _, value := range values {
			header.Add(key, value)
		}
	}
	header.Set(headerTinyCDNCache, string(cache.StateMiss))
	header.Set(headerTinyCDNSite, site.Source.ID)
	header.Set(headerTinyCDNRule, rule.Source.ID)
	header.Set(headerCacheStatus, r.engine.MissCacheStatus(plan))
	if resp.ContentLength >= 0 {
		header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	rw.WriteHeader(resp.StatusCode)

	var copyErr error
	var copied int64
	if req.Method == http.MethodHead {
		copied, copyErr = io.Copy(tempFile, resp.Body)
	} else {
		copied, copyErr = io.Copy(io.MultiWriter(rw, tempFile), resp.Body)
	}

	stored := cache.StoredResponse{
		StatusCode:    resp.StatusCode,
		Header:        resp.Header.Clone(),
		BodyPath:      tempPath,
		ContentLength: contentLength(resp, copied),
		CleanupPath:   true,
	}

	if closeErr := tempFile.Close(); copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		r.engine.AbortFill(plan, stored)
		return
	}
	if err := r.engine.CompleteFill(req.Context(), plan, stored); err != nil {
		return
	}
}

func (r *Router) streamBypass(rw http.ResponseWriter, req *http.Request, site *runtime.CompiledSite, rule *runtime.CompiledRule, detail string) {
	resp, err := r.fetcher.Open(req.Context(), req, site)
	if err != nil {
		writeErrorResponse(rw, site, rule, err)
		return
	}
	defer resp.Body.Close()

	r.writeStreamedResponse(rw, req, site, rule, resp, cache.StateBypass, "TinyCDN; fwd=bypass; detail="+detail)
}

func (r *Router) writeStreamedResponse(rw http.ResponseWriter, req *http.Request, site *runtime.CompiledSite, rule *runtime.CompiledRule, resp *http.Response, state cache.State, cacheStatus string) {
	header := rw.Header()
	responseHeader := resp.Header.Clone()
	httpx.StripHopByHopHeaders(responseHeader)
	for key, values := range responseHeader {
		for _, value := range values {
			header.Add(key, value)
		}
	}
	header.Set(headerTinyCDNCache, string(state))
	header.Set(headerTinyCDNSite, site.Source.ID)
	header.Set(headerTinyCDNRule, rule.Source.ID)
	header.Set(headerCacheStatus, cacheStatus)
	if resp.ContentLength >= 0 {
		header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}

	rw.WriteHeader(resp.StatusCode)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(rw, resp.Body)
}

func writeErrorResponse(rw http.ResponseWriter, site *runtime.CompiledSite, rule *runtime.CompiledRule, err error) {
	header := rw.Header()
	header.Set(headerTinyCDNCache, string(cache.StateError))
	header.Set(headerTinyCDNSite, site.Source.ID)
	header.Set(headerTinyCDNRule, rule.Source.ID)
	header.Set(headerCacheStatus, "TinyCDN; fwd=error; detail=UPSTREAM")
	http.Error(rw, fmt.Sprintf("proxy upstream error: %v", err), http.StatusBadGateway)
}
