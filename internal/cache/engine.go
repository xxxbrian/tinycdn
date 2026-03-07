package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"tinycdn/internal/httpx"
	"tinycdn/internal/model"
)

const (
	DefaultManagedTTL         = 5 * time.Minute
	DefaultOptimisticMaxStale = 1 * time.Hour
)

type State string

const (
	StateBypass State = "BYPASS"
	StateMiss   State = "MISS"
	StateHit    State = "HIT"
	StateStale  State = "STALE"
	StateError  State = "ERROR"
)

type Policy struct {
	SiteID              string
	RuleID              string
	PolicyTag           string
	Mode                model.CacheMode
	TTL                 time.Duration
	HasTTL              bool
	StaleIfError        time.Duration
	HasStaleIfError     bool
	Optimistic          bool
	OptimisticMaxStale  time.Duration
	IgnoreClientControl bool
}

type StoredResponse struct {
	StatusCode    int
	Header        http.Header
	Body          []byte
	BodyPath      string
	ContentLength int64
	CleanupPath   bool
}

type Entry struct {
	Key        string
	SiteID     string
	RuleID     string
	PolicyTag  string
	StoredAt   time.Time
	FreshUntil time.Time
	StaleUntil time.Time
	InvalidAt  time.Time
	BaseAge    int
	Response   StoredResponse
}

type Result struct {
	State         State
	StatusCode    int
	Header        http.Header
	Body          []byte
	BodyPath      string
	ContentLength int64
	CleanupPath   bool
	CacheStatus   string
}

type FetchFunc func(context.Context, *http.Request) (StoredResponse, error)

type FillPlan struct {
	PreparedRequest *http.Request
	baseKey         string
	cacheKey        string
	lookupErr       error
	policy          Policy
	originalMethod  string
	fallbackEntry   Entry
	hasFallback     bool
	revalidate      bool
}

func (p *FillPlan) Request(ctx context.Context) *http.Request {
	req := p.PreparedRequest.Clone(ctx)
	if p.revalidate && p.hasFallback {
		req = applyRevalidationHeaders(req, p.fallbackEntry)
	}
	return req
}

func (p *FillPlan) Policy() Policy {
	return p.policy
}

func (p *FillPlan) OriginalMethod() string {
	return p.originalMethod
}

type inflightFill struct {
	done chan struct{}
}

type Engine struct {
	store   Store
	now     func() time.Time
	refresh singleflight.Group
	fillMu  sync.Mutex
	fills   map[string]*inflightFill
}

func NewEngine(store Store) *Engine {
	return &Engine{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		fills: map[string]*inflightFill{},
	}
}

func BuildPolicy(siteID string, rule model.Rule, ttl time.Duration, hasTTL bool, staleIfError time.Duration, hasStaleIfError bool) Policy {
	policyTag := fmt.Sprintf("%s|%s|%t|%s|%s", rule.ID, rule.Action.Cache.Mode, rule.Action.Cache.Optimistic, rule.Action.Cache.TTL, rule.Action.Cache.StaleIfError)

	return Policy{
		SiteID:              siteID,
		RuleID:              rule.ID,
		PolicyTag:           policyTag,
		Mode:                rule.Action.Cache.Mode,
		TTL:                 ttl,
		HasTTL:              hasTTL,
		StaleIfError:        staleIfError,
		HasStaleIfError:     hasStaleIfError,
		Optimistic:          rule.Action.Cache.Optimistic,
		OptimisticMaxStale:  DefaultOptimisticMaxStale,
		IgnoreClientControl: rule.Action.Cache.Mode == model.CacheModeForceCache || rule.Action.Cache.Mode == model.CacheModeOverrideOrigin,
	}
}

func (e *Engine) Handle(ctx context.Context, req *http.Request, policy Policy, fetch FetchFunc) (Result, error) {
	preparedReq, requestBehavior := prepareRequest(req, policy)
	if requestBehavior == StateBypass {
		response, err := fetch(ctx, preparedReq)
		if err != nil {
			return Result{}, err
		}
		return buildResult(StateBypass, response, bypassCacheStatus("request")), nil
	}

	result, plan, err := e.Start(ctx, req, policy)
	if err != nil {
		return Result{}, err
	}
	if plan == nil {
		return result, nil
	}
	if result.State == StateStale {
		go func() {
			refreshCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			response, err := fetch(refreshCtx, plan.Request(refreshCtx))
			if err != nil {
				_, _ = e.HandleFillError(plan, err)
				return
			}
			if _, ok, err := e.RevalidatedResult(refreshCtx, plan, response); err != nil || ok {
				return
			}
			_ = e.CompleteFill(refreshCtx, plan, response)
		}()
		return result, nil
	}

	response, err := fetch(ctx, plan.Request(ctx))
	if err != nil {
		if fallback, ok := e.HandleFillError(plan, err); ok {
			return fallback, nil
		}
		return Result{}, err
	}
	if revalidated, ok, err := e.RevalidatedResult(ctx, plan, response); err != nil {
		return Result{}, err
	} else if ok {
		return revalidated, nil
	}
	if err := e.CompleteFill(ctx, plan, response); err != nil {
		return buildResult(StateMiss, response, missCacheStatus(plan.cacheKey, false, combineStatusDetails(plan.lookupErr, "STORE_ERROR"))), nil
	}
	return buildResult(StateMiss, response, missCacheStatus(plan.cacheKey, false, combineStatusDetails(plan.lookupErr))), nil
}

func (e *Engine) Start(ctx context.Context, req *http.Request, policy Policy) (Result, *FillPlan, error) {
	preparedReq, requestBehavior := prepareRequest(req, policy)
	if requestBehavior == StateBypass {
		return Result{State: StateBypass}, nil, nil
	}

	baseKey := buildBaseCacheKey(policy.SiteID, cacheKeyMethod(req.Method), req.URL.Path, req.URL.RawQuery)

	for {
		cacheKey, entry, found, lookupErr := e.lookupEntry(ctx, baseKey, preparedReq.Header)
		now := e.now()

		if found && entry.PolicyTag == policy.PolicyTag {
			if now.Before(entry.FreshUntil) && !shouldForceRefresh(req, policy) {
				return entryResult(entry, StateHit, now), nil, nil
			}

			if policy.Optimistic && now.Before(entry.StaleUntil) {
				fill, leader := e.claimFill(cacheKey)
				if leader {
					_ = fill
					plan := &FillPlan{
						PreparedRequest: preparedReq,
						baseKey:         baseKey,
						cacheKey:        cacheKey,
						lookupErr:       lookupErr,
						policy:          policy,
						originalMethod:  req.Method,
						fallbackEntry:   entry,
						hasFallback:     true,
						revalidate:      canRevalidate(policy, entry),
					}
					if plan.revalidate {
						plan.PreparedRequest = applyRevalidationHeaders(plan.PreparedRequest, entry)
					}
					return entryResult(entry, StateStale, now), plan, nil
				}
				return entryResult(entry, StateStale, now), nil, nil
			}
		}

		fill, leader := e.claimFill(cacheKey)
		if leader {
			plan := &FillPlan{
				PreparedRequest: preparedReq,
				baseKey:         baseKey,
				cacheKey:        cacheKey,
				lookupErr:       lookupErr,
				policy:          policy,
				originalMethod:  req.Method,
			}
			if found {
				plan.fallbackEntry = entry
				plan.hasFallback = true
				plan.revalidate = canRevalidate(policy, entry)
				if plan.revalidate {
					plan.PreparedRequest = applyRevalidationHeaders(plan.PreparedRequest, entry)
				}
			}
			return Result{}, plan, nil
		}

		select {
		case <-fill.done:
		case <-ctx.Done():
			return Result{}, nil, ctx.Err()
		}
	}
}

func (e *Engine) CompleteFill(ctx context.Context, plan *FillPlan, response StoredResponse) error {
	defer e.releaseFill(plan.cacheKey)

	if plan.revalidate && response.StatusCode == http.StatusNotModified && plan.hasFallback {
		_, err := e.completeRevalidation(ctx, plan.baseKey, plan.PreparedRequest.Header, plan.policy, plan.fallbackEntry, response)
		return err
	}

	decision := decideStore(e.now(), plan.policy, response)
	if !decision.Store || !shouldStoreResponse(plan.originalMethod) {
		return cleanupResponsePath(response)
	}

	response, err := e.persistResponseBody(ctx, response)
	if err != nil {
		return err
	}
	cacheKey := buildStorageKey(plan.baseKey, decision.VaryHeaders, plan.PreparedRequest.Header)
	entry := buildEntry(e.now(), cacheKey, plan.policy, decision, response)
	if err := e.storeResponse(ctx, plan.baseKey, entry, decision.VaryHeaders); err != nil {
		response.CleanupPath = true
		_ = cleanupResponsePath(response)
		return err
	}
	return nil
}

func (e *Engine) AbortFill(plan *FillPlan, response StoredResponse) {
	defer e.releaseFill(plan.cacheKey)
	_ = cleanupResponsePath(response)
}

func (e *Engine) HandleFillError(plan *FillPlan, err error) (Result, bool) {
	defer e.releaseFill(plan.cacheKey)
	if plan.hasFallback && canServeStaleOnError(e.now(), plan.policy, plan.fallbackEntry) {
		return entryResult(plan.fallbackEntry, StateStale, e.now()), true
	}
	return Result{}, false
}

func (e *Engine) MissCacheStatus(plan *FillPlan) string {
	return missCacheStatus(plan.cacheKey, false, combineStatusDetails(plan.lookupErr))
}

func (e *Engine) RevalidatedResult(ctx context.Context, plan *FillPlan, response StoredResponse) (Result, bool, error) {
	if !(plan.revalidate && response.StatusCode == http.StatusNotModified && plan.hasFallback) {
		return Result{}, false, nil
	}
	defer e.releaseFill(plan.cacheKey)
	result, err := e.completeRevalidation(ctx, plan.baseKey, plan.PreparedRequest.Header, plan.policy, plan.fallbackEntry, response)
	if err != nil {
		return Result{}, false, err
	}
	return result, true, nil
}

func (e *Engine) refreshInBackground(ctx context.Context, cacheKey string, req *http.Request, policy Policy, entry Entry, fetch FetchFunc) {
	_, _, _ = e.refresh.Do(cacheKey, func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		refreshReq := req.Clone(refreshCtx)
		if canRevalidate(policy, entry) {
			refreshReq = applyRevalidationHeaders(refreshReq, entry)
		}

		response, err := fetch(refreshCtx, refreshReq)
		if err != nil {
			return nil, err
		}

		baseKey := buildBaseCacheKey(policy.SiteID, cacheKeyMethod(req.Method), req.URL.Path, req.URL.RawQuery)
		if response.StatusCode == http.StatusNotModified {
			_, err := e.completeRevalidation(refreshCtx, baseKey, refreshReq.Header, policy, entry, response)
			return nil, err
		}

		decision := decideStore(e.now(), policy, response)
		if !decision.Store || !shouldStoreResponse(req.Method) {
			return nil, nil
		}

		response, err = e.persistResponseBody(refreshCtx, response)
		if err != nil {
			return nil, err
		}

		now := e.now()
		nextKey := buildStorageKey(baseKey, decision.VaryHeaders, req.Header)
		entry := buildEntry(now, nextKey, policy, decision, response)
		return nil, e.storeResponse(refreshCtx, baseKey, entry, decision.VaryHeaders)
	})
}

func (e *Engine) claimFill(cacheKey string) (*inflightFill, bool) {
	e.fillMu.Lock()
	defer e.fillMu.Unlock()
	if fill, ok := e.fills[cacheKey]; ok {
		return fill, false
	}
	fill := &inflightFill{done: make(chan struct{})}
	e.fills[cacheKey] = fill
	return fill, true
}

func (e *Engine) releaseFill(cacheKey string) {
	e.fillMu.Lock()
	fill, ok := e.fills[cacheKey]
	if ok {
		delete(e.fills, cacheKey)
	}
	e.fillMu.Unlock()
	if ok {
		close(fill.done)
	}
}

func (e *Engine) PurgeSite(ctx context.Context, siteID string) (int, error) {
	respDeleted, err := e.store.DeletePrefix(ctx, responseSitePrefix(siteID))
	if err != nil {
		return 0, err
	}
	varyDeleted, err := e.store.DeletePrefix(ctx, varySitePrefix(siteID))
	if err != nil {
		return respDeleted, err
	}
	return respDeleted + varyDeleted, nil
}

func (e *Engine) PurgeURL(ctx context.Context, siteID, path, rawQuery string) (int, error) {
	baseKey := buildBaseCacheKey(siteID, http.MethodGet, path, rawQuery)

	deleted := 0
	if _, found, err := e.store.GetEntry(ctx, responseKey(baseKey)); err == nil && found {
		if err := e.store.Delete(ctx, responseKey(baseKey)); err != nil {
			return deleted, err
		}
		deleted++
	}
	if _, found, err := e.store.GetVary(ctx, varyKey(baseKey)); err == nil && found {
		if err := e.store.Delete(ctx, varyKey(baseKey)); err != nil {
			return deleted, err
		}
		deleted++
	}

	variantDeleted, err := e.store.DeletePrefix(ctx, variantPrefix(baseKey))
	if err != nil {
		return deleted, err
	}
	return deleted + variantDeleted, nil
}

func (e *Engine) lookupEntry(ctx context.Context, baseKey string, requestHeader http.Header) (string, Entry, bool, error) {
	cacheKey := responseKey(baseKey)

	spec, found, err := e.store.GetVary(ctx, varyKey(baseKey))
	if err != nil {
		return cacheKey, Entry{}, false, err
	}
	if found && len(spec.Headers) > 0 {
		cacheKey = buildVariantKey(baseKey, spec.Headers, requestHeader)
	}

	entry, found, err := e.store.GetEntry(ctx, cacheKey)
	if err != nil {
		return cacheKey, Entry{}, false, err
	}
	return cacheKey, entry, found, nil
}

func (e *Engine) storeResponse(ctx context.Context, baseKey string, entry Entry, varyHeaders []string) error {
	if len(varyHeaders) == 0 {
		if _, err := e.store.DeletePrefix(ctx, variantPrefix(baseKey)); err != nil {
			return err
		}
		_ = e.store.Delete(ctx, varyKey(baseKey))
		return e.store.PutEntry(ctx, responseKey(baseKey), entry)
	}

	if existing, found, err := e.store.GetVary(ctx, varyKey(baseKey)); err == nil && found && !slices.Equal(existing.Headers, varyHeaders) {
		if _, err := e.store.DeletePrefix(ctx, variantPrefix(baseKey)); err != nil {
			return err
		}
	}
	_ = e.store.Delete(ctx, responseKey(baseKey))
	if err := e.store.PutVary(ctx, varyKey(baseKey), VarySpec{Headers: varyHeaders}); err != nil {
		return err
	}
	return e.store.PutEntry(ctx, entry.Key, entry)
}

func (e *Engine) persistResponseBody(ctx context.Context, response StoredResponse) (StoredResponse, error) {
	if response.BodyPath == "" || !response.CleanupPath {
		if response.ContentLength == 0 && response.Body != nil {
			response.ContentLength = int64(len(response.Body))
		}
		return response, nil
	}

	finalPath, err := e.store.ImportBody(ctx, response.BodyPath)
	if err != nil {
		return StoredResponse{}, err
	}
	response.BodyPath = finalPath
	response.CleanupPath = false
	return response, nil
}

func buildEntry(now time.Time, cacheKey string, policy Policy, decision storeDecision, response StoredResponse) Entry {
	entry := Entry{
		Key:        cacheKey,
		SiteID:     policy.SiteID,
		RuleID:     policy.RuleID,
		PolicyTag:  policy.PolicyTag,
		StoredAt:   now,
		FreshUntil: now.Add(decision.TTL),
		StaleUntil: now.Add(decision.TTL + decision.StaleWindow),
		InvalidAt:  now.Add(decision.TTL + decision.StaleWindow + decision.StaleIfError),
		BaseAge:    parseAge(response.Header.Get("Age")),
		Response: StoredResponse{
			StatusCode:    response.StatusCode,
			Header:        sanitizeStoredHeader(response.Header),
			Body:          append([]byte(nil), response.Body...),
			BodyPath:      response.BodyPath,
			ContentLength: response.ContentLength,
		},
	}
	if entry.Response.ContentLength == 0 && entry.Response.Body != nil {
		entry.Response.ContentLength = int64(len(entry.Response.Body))
	}
	if !entry.InvalidAt.After(entry.StoredAt) && hasRevalidationValidators(response.Header) {
		entry.InvalidAt = entry.StoredAt.Add(DefaultManagedTTL)
	}
	if entry.InvalidAt.Before(entry.StoredAt) {
		entry.InvalidAt = entry.StoredAt
	}
	if entry.StaleUntil.Before(entry.FreshUntil) {
		entry.StaleUntil = entry.FreshUntil
	}
	return entry
}

type storeDecision struct {
	Store        bool
	TTL          time.Duration
	StaleWindow  time.Duration
	StaleIfError time.Duration
	VaryHeaders  []string
}

func decideStore(now time.Time, policy Policy, response StoredResponse) storeDecision {
	if !isCacheableStatus(response.StatusCode) || response.Header.Get("Content-Range") != "" {
		return storeDecision{}
	}
	if len(response.Header.Values("Set-Cookie")) > 0 {
		return storeDecision{}
	}

	switch policy.Mode {
	case model.CacheModeBypass:
		return storeDecision{}
	case model.CacheModeFollowOrigin:
		directives := parseCacheControl(httpx.CombinedHeaderValue(response.Header, "Cache-Control"))
		if directives.noStore || directives.isPrivate {
			return storeDecision{}
		}

		ttl, ok := directives.ttl(now, response.Header.Get("Expires"))
		if !ok {
			return storeDecision{}
		}

		staleWindow := directives.staleWhileRevalidate
		if policy.Optimistic {
			staleWindow = maxDuration(staleWindow, policy.OptimisticMaxStale)
		}
		varyHeaders, cacheable := parseVary(response.Header.Values("Vary"))
		if !cacheable {
			return storeDecision{}
		}

		staleIfError := directives.staleIfError
		if ttl == 0 && staleWindow == 0 && staleIfError == 0 && !hasRevalidationValidators(response.Header) {
			return storeDecision{}
		}
		return storeDecision{
			Store:        true,
			TTL:          ttl,
			StaleWindow:  staleWindow,
			StaleIfError: staleIfError,
			VaryHeaders:  varyHeaders,
		}
	default:
		ttl := policy.TTL
		if !policy.HasTTL {
			ttl = DefaultManagedTTL
		}

		staleWindow := time.Duration(0)
		if policy.Optimistic {
			staleWindow = policy.OptimisticMaxStale
		}

		staleIfError := time.Duration(0)
		if policy.HasStaleIfError {
			staleIfError = policy.StaleIfError
		}
		varyHeaders, cacheable := parseVary(response.Header.Values("Vary"))
		if !cacheable {
			return storeDecision{}
		}

		return storeDecision{
			Store:        true,
			TTL:          ttl,
			StaleWindow:  staleWindow,
			StaleIfError: staleIfError,
			VaryHeaders:  varyHeaders,
		}
	}
}

func buildResult(state State, response StoredResponse, cacheStatus string) Result {
	contentLength := response.ContentLength
	if contentLength == 0 && response.Body != nil {
		contentLength = int64(len(response.Body))
	}
	return Result{
		State:         state,
		StatusCode:    response.StatusCode,
		Header:        sanitizeStoredHeader(response.Header),
		Body:          append([]byte(nil), response.Body...),
		BodyPath:      response.BodyPath,
		ContentLength: contentLength,
		CleanupPath:   response.CleanupPath,
		CacheStatus:   cacheStatus,
	}
}

func entryResult(entry Entry, state State, now time.Time) Result {
	header := sanitizeStoredHeader(entry.Response.Header)
	header.Set("Age", strconv.Itoa(entry.BaseAge+int(now.Sub(entry.StoredAt).Seconds())))

	cacheStatus := hitCacheStatus(entry, state, now)
	return Result{
		State:         state,
		StatusCode:    entry.Response.StatusCode,
		Header:        header,
		Body:          append([]byte(nil), entry.Response.Body...),
		BodyPath:      entry.Response.BodyPath,
		ContentLength: entry.Response.ContentLength,
		CacheStatus:   cacheStatus,
	}
}

func canServeStaleOnError(now time.Time, policy Policy, entry Entry) bool {
	if entry.PolicyTag != policy.PolicyTag {
		return false
	}
	if now.Before(entry.FreshUntil) {
		return true
	}
	if policy.Optimistic && now.Before(entry.StaleUntil) {
		return true
	}
	return policy.HasStaleIfError && now.Before(entry.InvalidAt)
}

func prepareRequest(req *http.Request, policy Policy) (*http.Request, State) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()

	if policy.Mode == model.CacheModeBypass {
		return cloned, StateBypass
	}
	if hasSharedCacheSensitiveRequestHeaders(req.Header) {
		return cloned, StateBypass
	}
	if req.Header.Get("Range") != "" {
		return cloned, StateBypass
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return cloned, StateBypass
	}

	cloned.Header.Del("Cache-Control")
	cloned.Header.Del("Pragma")
	for _, headerName := range conditionalRequestHeaders {
		cloned.Header.Del(headerName)
	}
	if cloned.Method == http.MethodHead {
		cloned.Method = http.MethodGet
	}

	return cloned, StateMiss
}

func PreviewRequestBehavior(req *http.Request, policy Policy) State {
	_, state := prepareRequest(req, policy)
	return state
}

func hasSharedCacheSensitiveRequestHeaders(header http.Header) bool {
	return header.Get("Authorization") != "" || len(header.Values("Cookie")) > 0
}

func shouldForceRefresh(req *http.Request, policy Policy) bool {
	_ = req
	_ = policy
	return false
}

func shouldStoreResponse(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func cacheKeyMethod(method string) string {
	if method == http.MethodHead {
		return http.MethodGet
	}
	return method
}

func buildBaseCacheKey(siteID, method, path, rawQuery string) string {
	key := siteID + "|" + method + "|" + path
	if rawQuery != "" {
		key += "?" + rawQuery
	}
	return key
}

func responseKey(baseKey string) string {
	return "resp|" + baseKey
}

func varyKey(baseKey string) string {
	return "vary|" + baseKey
}

func variantPrefix(baseKey string) string {
	return responseKey(baseKey) + "|vary|"
}

func responseSitePrefix(siteID string) string {
	return "resp|" + siteID + "|"
}

func varySitePrefix(siteID string) string {
	return "vary|" + siteID + "|"
}

func buildStorageKey(baseKey string, varyHeaders []string, requestHeader http.Header) string {
	if len(varyHeaders) == 0 {
		return responseKey(baseKey)
	}
	return buildVariantKey(baseKey, varyHeaders, requestHeader)
}

func buildVariantKey(baseKey string, varyHeaders []string, requestHeader http.Header) string {
	sum := sha256.Sum256([]byte(buildVariantFingerprint(varyHeaders, requestHeader)))
	return variantPrefix(baseKey) + hex.EncodeToString(sum[:])
}

func buildVariantFingerprint(varyHeaders []string, requestHeader http.Header) string {
	parts := make([]string, 0, len(varyHeaders))
	for _, headerName := range varyHeaders {
		values := requestHeader.Values(headerName)
		normalized := normalizeVaryHeaderValue(headerName, values)
		if normalized == "" {
			parts = append(parts, headerName+"=")
			continue
		}
		parts = append(parts, headerName+"="+normalized)
	}
	return strings.Join(parts, "\n")
}

func normalizeVaryHeaderValue(headerName string, values []string) string {
	switch http.CanonicalHeaderKey(headerName) {
	case "Accept-Encoding", "Accept-Language":
		return normalizeCommaSeparatedTokens(values, true)
	default:
		normalized := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			normalized = append(normalized, value)
		}
		return strings.Join(normalized, ",")
	}
}

func normalizeCommaSeparatedTokens(values []string, lower bool) string {
	tokens := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			token := strings.TrimSpace(part)
			if token == "" {
				continue
			}
			if lower {
				token = strings.ToLower(token)
			}
			tokens = append(tokens, token)
		}
	}
	return strings.Join(tokens, ",")
}

func sanitizeStoredHeader(header http.Header) http.Header {
	cloned := header.Clone()
	httpx.StripHopByHopHeaders(cloned)
	cloned.Del("Content-Length")
	return cloned
}

var conditionalRequestHeaders = []string{
	"If-Match",
	"If-Modified-Since",
	"If-None-Match",
	"If-Range",
	"If-Unmodified-Since",
}

func isCacheableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusOK,
		http.StatusNonAuthoritativeInfo,
		http.StatusNoContent,
		http.StatusMultipleChoices,
		http.StatusMovedPermanently,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusGone,
		http.StatusRequestURITooLong,
		http.StatusNotImplemented:
		return true
	default:
		return false
	}
}

func bypassCacheStatus(detail string) string {
	return "TinyCDN; fwd=bypass; detail=" + detail
}

func missCacheStatus(key string, stored bool, detail string) string {
	status := "TinyCDN; fwd=uri-miss; key=" + key
	if detail != "" {
		status += "; detail=" + detail
	}
	if stored {
		return status + "; stored"
	}
	return status
}

func combineStatusDetails(lookupErr error, extra ...string) string {
	details := make([]string, 0, len(extra)+1)
	if lookupErr != nil {
		details = append(details, "STORE_READ_ERROR")
	}
	for _, value := range extra {
		if value != "" {
			details = append(details, value)
		}
	}
	return strings.Join(details, ",")
}

func hitCacheStatus(entry Entry, state State, now time.Time) string {
	ttl := maxDuration(0, entry.FreshUntil.Sub(now))
	status := fmt.Sprintf("TinyCDN; hit; ttl=%d; key=%s", int(ttl.Seconds()), entry.Key)
	if state == StateStale {
		return status + "; fwd=stale"
	}
	return status
}

func parseAge(value string) int {
	age, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || age < 0 {
		return 0
	}
	return age
}

func parseVary(values []string) ([]string, bool) {
	headers := make([]string, 0)
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			headerName := http.CanonicalHeaderKey(strings.TrimSpace(part))
			switch headerName {
			case "":
				continue
			case "*":
				return nil, false
			}
			if _, ok := seen[headerName]; ok {
				continue
			}
			seen[headerName] = struct{}{}
			headers = append(headers, headerName)
		}
	}
	slices.Sort(headers)
	return headers, true
}

type cacheControlDirectives struct {
	noStore              bool
	isPrivate            bool
	noCache              bool
	maxAge               *time.Duration
	sMaxAge              *time.Duration
	staleIfError         time.Duration
	staleWhileRevalidate time.Duration
}

func parseCacheControl(value string) cacheControlDirectives {
	directives := cacheControlDirectives{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		key, rawValue, hasValue := strings.Cut(part, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		rawValue = strings.Trim(strings.TrimSpace(rawValue), "\"")

		switch key {
		case "no-store":
			directives.noStore = true
		case "private":
			directives.isPrivate = true
		case "no-cache":
			directives.noCache = true
		case "s-maxage":
			if hasValue {
				directives.sMaxAge = parseDirectiveDuration(rawValue)
			}
		case "max-age":
			if hasValue {
				directives.maxAge = parseDirectiveDuration(rawValue)
			}
		case "stale-if-error":
			if hasValue {
				if duration := parseDirectiveDuration(rawValue); duration != nil {
					directives.staleIfError = *duration
				}
			}
		case "stale-while-revalidate":
			if hasValue {
				if duration := parseDirectiveDuration(rawValue); duration != nil {
					directives.staleWhileRevalidate = *duration
				}
			}
		}
	}

	return directives
}

func (d cacheControlDirectives) ttl(now time.Time, expires string) (time.Duration, bool) {
	switch {
	case d.sMaxAge != nil:
		return *d.sMaxAge, true
	case d.maxAge != nil:
		return *d.maxAge, true
	case d.noCache:
		return 0, true
	case expires != "":
		parsed, err := http.ParseTime(expires)
		if err != nil {
			return 0, false
		}
		return maxDuration(0, parsed.Sub(now)), true
	default:
		return 0, false
	}
}

func parseDirectiveDuration(value string) *time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return nil
	}
	duration := time.Duration(seconds) * time.Second
	return &duration
}

func maxDuration(a, b time.Duration) time.Duration {
	if b > a {
		return b
	}
	return a
}

func cleanupResponsePath(response StoredResponse) error {
	if !response.CleanupPath {
		return nil
	}
	return removeBodyPath(response.BodyPath)
}

func canRevalidate(policy Policy, entry Entry) bool {
	if policy.Mode != model.CacheModeFollowOrigin {
		return false
	}
	return hasRevalidationValidators(entry.Response.Header)
}

func hasRevalidationValidators(header http.Header) bool {
	return header.Get("ETag") != "" || header.Get("Last-Modified") != ""
}

func applyRevalidationHeaders(req *http.Request, entry Entry) *http.Request {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	if etag := entry.Response.Header.Get("ETag"); etag != "" {
		cloned.Header.Set("If-None-Match", etag)
	}
	if lastModified := entry.Response.Header.Get("Last-Modified"); lastModified != "" {
		cloned.Header.Set("If-Modified-Since", lastModified)
	}
	return cloned
}

func mergeRevalidatedHeader(stored http.Header, revalidated http.Header) http.Header {
	merged := sanitizeStoredHeader(stored)
	updates := sanitizeStoredHeader(revalidated)
	for key, values := range updates {
		merged.Del(key)
		for _, value := range values {
			merged.Add(key, value)
		}
	}
	return merged
}

func (e *Engine) completeRevalidation(ctx context.Context, baseKey string, requestHeader http.Header, policy Policy, entry Entry, response StoredResponse) (Result, error) {
	mergedHeader := mergeRevalidatedHeader(entry.Response.Header, response.Header)
	effective := StoredResponse{
		StatusCode:    entry.Response.StatusCode,
		Header:        mergedHeader,
		Body:          append([]byte(nil), entry.Response.Body...),
		BodyPath:      entry.Response.BodyPath,
		ContentLength: entry.Response.ContentLength,
	}
	decision := decideStore(e.now(), policy, effective)
	if !decision.Store {
		result := entryResult(entry, StateHit, e.now())
		result.CacheStatus = hitCacheStatus(entry, StateHit, e.now()) + "; detail=REVALIDATED"
		return result, nil
	}

	updatedKey := buildStorageKey(baseKey, decision.VaryHeaders, requestHeader)
	updated := buildEntry(e.now(), updatedKey, policy, decision, effective)
	if err := e.storeResponse(ctx, baseKey, updated, decision.VaryHeaders); err != nil {
		result := entryResult(updated, StateHit, e.now())
		result.CacheStatus = hitCacheStatus(updated, StateHit, e.now()) + "; detail=REVALIDATED,STORE_ERROR"
		return result, nil
	}
	result := entryResult(updated, StateHit, e.now())
	result.CacheStatus = hitCacheStatus(updated, StateHit, e.now()) + "; detail=REVALIDATED"
	return result, nil
}
