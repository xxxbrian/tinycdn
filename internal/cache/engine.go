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
	StatusCode int
	Header     http.Header
	Body       []byte
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
	State       State
	StatusCode  int
	Header      http.Header
	Body        []byte
	CacheStatus string
}

type FetchFunc func(context.Context, *http.Request) (StoredResponse, error)

type Engine struct {
	store   Store
	now     func() time.Time
	refresh singleflight.Group
}

func NewEngine(store Store) *Engine {
	return &Engine{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
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

	keyMethod := cacheKeyMethod(req.Method)
	baseKey := buildBaseCacheKey(policy.SiteID, keyMethod, req.URL.Path, req.URL.RawQuery)
	cacheKey, entry, found, lookupErr := e.lookupEntry(ctx, baseKey, preparedReq.Header)
	now := e.now()

	if found && entry.PolicyTag == policy.PolicyTag {
		if !now.After(entry.FreshUntil) && !shouldForceRefresh(req, policy) {
			return entryResult(entry, StateHit, now), nil
		}

		if policy.Optimistic && now.Before(entry.StaleUntil) {
			go e.refreshInBackground(context.Background(), cacheKey, preparedReq, policy, fetch)
			return entryResult(entry, StateStale, now), nil
		}
	}

	response, err := fetch(ctx, preparedReq)
	if err != nil {
		if found && canServeStaleOnError(now, policy, entry) {
			return entryResult(entry, StateStale, now), nil
		}
		return Result{}, err
	}

	decision := decideStore(now, policy, response)
	if decision.Store && shouldStoreResponse(req.Method) {
		cacheKey = buildStorageKey(baseKey, decision.VaryHeaders, preparedReq.Header)
		entry := buildEntry(now, cacheKey, policy, decision, response)
		if err := e.storeResponse(ctx, baseKey, entry, decision.VaryHeaders); err != nil {
			return buildResult(StateMiss, response, missCacheStatus(cacheKey, false, combineStatusDetails(lookupErr, "STORE_ERROR"))), nil
		}
		return buildResult(StateMiss, response, missCacheStatus(cacheKey, true, combineStatusDetails(lookupErr))), nil
	}

	if lookupErr != nil {
		return buildResult(StateMiss, response, missCacheStatus(cacheKey, false, combineStatusDetails(lookupErr))), nil
	}
	return buildResult(StateMiss, response, missCacheStatus(cacheKey, false, "")), nil
}

func (e *Engine) refreshInBackground(ctx context.Context, cacheKey string, req *http.Request, policy Policy, fetch FetchFunc) {
	_, _, _ = e.refresh.Do(cacheKey, func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		response, err := fetch(refreshCtx, req.Clone(refreshCtx))
		if err != nil {
			return nil, err
		}

		decision := decideStore(e.now(), policy, response)
		if !decision.Store || !shouldStoreResponse(req.Method) {
			return nil, nil
		}

		now := e.now()
		baseKey := buildBaseCacheKey(policy.SiteID, cacheKeyMethod(req.Method), req.URL.Path, req.URL.RawQuery)
		nextKey := buildStorageKey(baseKey, decision.VaryHeaders, req.Header)
		entry := buildEntry(now, nextKey, policy, decision, response)
		return nil, e.storeResponse(refreshCtx, baseKey, entry, decision.VaryHeaders)
	})
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
	if err := e.store.Delete(ctx, responseKey(baseKey)); err != nil {
		return err
	}
	if err := e.store.PutVary(ctx, varyKey(baseKey), VarySpec{Headers: varyHeaders}); err != nil {
		return err
	}
	return e.store.PutEntry(ctx, entry.Key, entry)
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
			StatusCode: response.StatusCode,
			Header:     sanitizeStoredHeader(response.Header),
			Body:       append([]byte(nil), response.Body...),
		},
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
		if directives.noStore || directives.isPrivate || directives.noCache {
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
	return Result{
		State:       state,
		StatusCode:  response.StatusCode,
		Header:      sanitizeStoredHeader(response.Header),
		Body:        append([]byte(nil), response.Body...),
		CacheStatus: cacheStatus,
	}
}

func entryResult(entry Entry, state State, now time.Time) Result {
	header := sanitizeStoredHeader(entry.Response.Header)
	header.Set("Age", strconv.Itoa(entry.BaseAge+int(now.Sub(entry.StoredAt).Seconds())))

	cacheStatus := hitCacheStatus(entry, state, now)
	return Result{
		State:       state,
		StatusCode:  entry.Response.StatusCode,
		Header:      header,
		Body:        append([]byte(nil), entry.Response.Body...),
		CacheStatus: cacheStatus,
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

	return cloned, StateMiss
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
	return method == http.MethodGet
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
		if len(values) == 0 {
			parts = append(parts, headerName+"=")
			continue
		}
		parts = append(parts, headerName+"="+strings.Join(values, ","))
	}
	return strings.Join(parts, "\n")
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
