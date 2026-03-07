package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"tinycdn/internal/cache"
	"tinycdn/internal/httpx"
	"tinycdn/internal/observe"
	"tinycdn/internal/runtime"
)

const (
	headerTinyCDNCache = "X-TinyCDN-Cache"
	headerTinyCDNSite  = "X-TinyCDN-Site"
	headerTinyCDNRule  = "X-TinyCDN-Rule"
	headerCacheStatus  = "Cache-Status"
	headerRequestID    = "X-TinyCDN-Request-ID"
)

type Router struct {
	engine   *cache.Engine
	store    cache.Store
	fetcher  *upstreamFetcher
	snapshot func() *runtime.Snapshot
	recorder observe.Recorder
}

func NewRouter(snapshot func() *runtime.Snapshot, cachePath string, recorder observe.Recorder) (*Router, error) {
	store, err := cache.NewBadgerStore(cachePath)
	if err != nil {
		return nil, err
	}
	fetcher, err := newUpstreamFetcher(cachePath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if recorder == nil {
		recorder = observe.NopRecorder{}
	}

	return &Router{
		engine:   cache.NewEngine(store),
		store:    store,
		fetcher:  fetcher,
		snapshot: snapshot,
		recorder: recorder,
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

func (r *Router) CacheInventory(ctx context.Context) ([]cache.Inventory, error) {
	return r.store.Inventory(ctx)
}

func (r *Router) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	startedAt := time.Now()
	responseWriter := httpx.NewStatusCapturingResponseWriter(rw)
	requestID := uuid.NewString()
	responseWriter.Header().Set(headerRequestID, requestID)

	event := observe.RequestEvent{
		Timestamp: startedAt.UTC(),
		RequestID: requestID,
		Method:    req.Method,
		Scheme:    requestScheme(req),
		Host:      req.Host,
		Path:      req.URL.Path,
		RawQuery:  req.URL.RawQuery,
		RemoteIP:  forwardedClientIP(req.RemoteAddr),
		UserAgent: req.UserAgent(),
		Referer:   req.Referer(),
	}
	defer func() {
		event.StatusCode = responseWriter.StatusCode()
		event.ResponseBytes = responseWriter.BytesWritten()
		event.TotalDurationMS = time.Since(startedAt).Milliseconds()
		if event.ContentType == "" {
			event.ContentType = responseWriter.Header().Get("Content-Type")
		}
		r.recorder.RecordRequest(event)
	}()

	if req.URL.Path == "/healthz" {
		responseWriter.WriteHeader(http.StatusOK)
		_, _ = responseWriter.Write([]byte("ok"))
		return
	}

	current := r.snapshot()
	site := current.SiteByHost(req.Host)
	if site == nil || !site.Source.Enabled {
		event.CacheState = string(cache.StateBypass)
		event.CacheStatus = "TinyCDN; fwd=bypass; detail=site-not-found"
		event.ErrorKind = "site_not_found"
		http.NotFound(responseWriter, req)
		return
	}
	event.SiteID = site.Source.ID
	event.SiteName = site.Source.Name
	event.UpstreamHost = site.UpstreamHost

	rule := site.MatchRule(req)
	if rule == nil {
		event.CacheState = string(cache.StateError)
		event.CacheStatus = "TinyCDN; fwd=error; detail=RULE"
		event.ErrorKind = "rule_match"
		http.Error(responseWriter, "no matching rule in runtime snapshot", http.StatusInternalServerError)
		return
	}
	event.RuleID = rule.Source.ID

	policy := cache.BuildPolicy(site.Source.ID, rule.Source, rule.TTL, rule.HasTTL, rule.StaleIfError, rule.HasStaleIfError)
	if cache.PreviewRequestBehavior(req, policy) == cache.StateBypass {
		r.streamBypass(responseWriter, req, site, rule, &event, "request")
		return
	}

	result, plan, err := r.engine.Start(req.Context(), req, policy)
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseTooLarge) {
			r.streamBypass(responseWriter, req, site, rule, &event, "object-too-large")
			return
		}
		event.CacheState = string(cache.StateError)
		event.CacheStatus = "TinyCDN; fwd=error; detail=UPSTREAM"
		event.ErrorKind = "engine_start"
		writeErrorResponse(responseWriter, site, rule, err)
		return
	}
	if plan != nil {
		if result.State == cache.StateStale {
			event.CacheState = string(result.State)
			event.CacheStatus = result.CacheStatus
			go r.fillInBackground(site, plan)
			writeResult(responseWriter, req, site, rule, result)
			return
		}
		r.streamFill(responseWriter, req, site, rule, plan, &event)
		return
	}

	event.CacheState = string(result.State)
	event.CacheStatus = result.CacheStatus
	writeResult(responseWriter, req, site, rule, result)
}

func (r *Router) fillInBackground(site *runtime.CompiledSite, plan *cache.FillPlan) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultUpstreamTimeout)
	defer cancel()

	startedAt := time.Now()
	event := observe.RequestEvent{
		Timestamp:    startedAt.UTC(),
		RequestID:    uuid.NewString(),
		SiteID:       site.Source.ID,
		SiteName:     site.Source.Name,
		RuleID:       plan.Policy().RuleID,
		IsInternal:   true,
		Method:       plan.OriginalMethod(),
		Scheme:       requestScheme(plan.PreparedRequest),
		Host:         plan.PreparedRequest.Host,
		Path:         plan.PreparedRequest.URL.Path,
		RawQuery:     plan.PreparedRequest.URL.RawQuery,
		RemoteIP:     "127.0.0.1",
		CacheState:   "REFRESH",
		CacheStatus:  "TinyCDN; fwd=refresh",
		UpstreamHost: site.UpstreamHost,
	}

	response, err := r.fetcher.Fetch(ctx, plan.Request(ctx), site)
	event.OriginRequests = 1
	event.OriginDurationMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		event.ErrorKind = "background_refresh"
		event.StatusCode = http.StatusBadGateway
		event.TotalDurationMS = event.OriginDurationMS
		r.recorder.RecordRequest(event)
		_, _ = r.engine.HandleFillError(plan, err)
		return
	}
	event.OriginStatusCode = response.StatusCode
	event.StatusCode = response.StatusCode
	event.ResponseBytes = response.ContentLength
	event.ContentType = response.Header.Get("Content-Type")
	event.TotalDurationMS = time.Since(startedAt).Milliseconds()
	if _, ok, err := r.engine.RevalidatedResult(ctx, plan, response); err != nil {
		event.ErrorKind = "background_revalidate"
		r.recorder.RecordRequest(event)
		return
	} else if ok {
		event.CacheState = string(cache.StateHit)
		event.CacheStatus = "TinyCDN; stored=revalidated"
		r.recorder.RecordRequest(event)
		if response.CleanupPath {
			_ = os.Remove(response.BodyPath)
		}
		return
	}
	if err := r.engine.CompleteFill(ctx, plan, response); err != nil {
		event.ErrorKind = "background_store"
		r.recorder.RecordRequest(event)
		return
	}
	r.recorder.RecordRequest(event)
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

func (r *Router) streamFill(rw http.ResponseWriter, req *http.Request, site *runtime.CompiledSite, rule *runtime.CompiledRule, plan *cache.FillPlan, event *observe.RequestEvent) {
	originStartedAt := time.Now()
	resp, err := r.fetcher.Open(req.Context(), plan.Request(req.Context()), site)
	event.OriginRequests = 1
	event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
	if err != nil {
		event.ErrorKind = "origin_fetch"
		if fallback, ok := r.engine.HandleFillError(plan, err); ok {
			event.CacheState = string(fallback.State)
			event.CacheStatus = fallback.CacheStatus
			writeResult(rw, req, site, rule, fallback)
			return
		}
		event.CacheState = string(cache.StateError)
		event.CacheStatus = "TinyCDN; fwd=error; detail=UPSTREAM"
		writeErrorResponse(rw, site, rule, err)
		return
	}
	defer resp.Body.Close()
	event.OriginStatusCode = resp.StatusCode
	event.ContentType = resp.Header.Get("Content-Type")

	if result, ok, err := r.engine.RevalidatedResult(req.Context(), plan, cache.StoredResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
	}); err != nil {
		event.ErrorKind = "revalidate"
		event.CacheState = string(cache.StateError)
		event.CacheStatus = "TinyCDN; fwd=error; detail=REVALIDATE"
		writeErrorResponse(rw, site, rule, err)
		return
	} else if ok {
		event.CacheState = string(result.State)
		event.CacheStatus = result.CacheStatus
		writeResult(rw, req, site, rule, result)
		return
	}

	if responseTooLarge(resp, r.fetcher.maxCacheableObjectBytes) {
		r.engine.AbortFill(plan, cache.StoredResponse{})
		event.CacheState = string(cache.StateBypass)
		event.CacheStatus = "TinyCDN; fwd=bypass; detail=object-too-large"
		r.writeStreamedResponse(rw, req, site, rule, resp, cache.StateBypass, "TinyCDN; fwd=bypass; detail=object-too-large")
		event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
		return
	}

	responseHeader := resp.Header.Clone()
	httpx.StripHopByHopHeaders(responseHeader)

	if err := os.MkdirAll(r.fetcher.tempDir, 0o755); err != nil {
		event.ErrorKind = "temp_dir"
		r.engine.AbortFill(plan, cache.StoredResponse{})
		writeErrorResponse(rw, site, rule, err)
		return
	}
	tempFile, err := os.CreateTemp(r.fetcher.tempDir, "body-*")
	if err != nil {
		event.ErrorKind = "temp_file"
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
		event.ErrorKind = "stream_copy"
		event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
		r.engine.AbortFill(plan, stored)
		return
	}
	if err := r.engine.CompleteFill(req.Context(), plan, stored); err != nil {
		event.ErrorKind = "store_fill"
		event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
		return
	}
	event.CacheState = string(cache.StateMiss)
	event.CacheStatus = r.engine.MissCacheStatus(plan)
	event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
}

func (r *Router) streamBypass(rw http.ResponseWriter, req *http.Request, site *runtime.CompiledSite, rule *runtime.CompiledRule, event *observe.RequestEvent, detail string) {
	originStartedAt := time.Now()
	resp, err := r.fetcher.Open(req.Context(), req, site)
	event.OriginRequests = 1
	event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
	if err != nil {
		event.ErrorKind = "bypass_fetch"
		event.CacheState = string(cache.StateError)
		event.CacheStatus = "TinyCDN; fwd=error; detail=UPSTREAM"
		writeErrorResponse(rw, site, rule, err)
		return
	}
	defer resp.Body.Close()

	event.OriginStatusCode = resp.StatusCode
	event.ContentType = resp.Header.Get("Content-Type")
	event.CacheState = string(cache.StateBypass)
	event.CacheStatus = "TinyCDN; fwd=bypass; detail=" + detail
	r.writeStreamedResponse(rw, req, site, rule, resp, cache.StateBypass, "TinyCDN; fwd=bypass; detail="+detail)
	event.OriginDurationMS = time.Since(originStartedAt).Milliseconds()
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
