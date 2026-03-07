package cache

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

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
	cacheKey := buildCacheKey(policy.SiteID, keyMethod, req.URL.Path, req.URL.RawQuery)
	now := e.now()

	entry, found, err := e.store.Get(ctx, cacheKey)
	if err != nil {
		found = false
	}

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
		_ = e.store.Put(ctx, cacheKey, entry)
		return buildResult(StateMiss, response, missCacheStatus(cacheKey, true)), nil
	}

	return buildResult(StateMiss, response, missCacheStatus(cacheKey, false)), nil
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
		if entry.StaleUntil.Before(entry.FreshUntil) {
			entry.StaleUntil = entry.FreshUntil
		}
		if entry.InvalidAt.Before(entry.StoredAt) {
			entry.InvalidAt = entry.StoredAt
		}

		return nil, e.store.Put(refreshCtx, cacheKey, entry)
	})
}

type storeDecision struct {
	Store        bool
	TTL          time.Duration
	StaleWindow  time.Duration
	StaleIfError time.Duration
}

func decideStore(now time.Time, policy Policy, response StoredResponse) storeDecision {
	if !isCacheableStatus(response.StatusCode) {
		return storeDecision{}
	}

	switch policy.Mode {
	case model.CacheModeBypass:
		return storeDecision{}
	case model.CacheModeFollowOrigin:
		directives := parseCacheControl(response.Header.Get("Cache-Control"))
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

		staleIfError := directives.staleIfError
		return storeDecision{
			Store:        true,
			TTL:          ttl,
			StaleWindow:  staleWindow,
			StaleIfError: staleIfError,
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

		return storeDecision{
			Store:        true,
			TTL:          ttl,
			StaleWindow:  staleWindow,
			StaleIfError: staleIfError,
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
	if req.Header.Get("Range") != "" {
		return cloned, StateBypass
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return cloned, StateBypass
	}

	if policy.IgnoreClientControl {
		cloned.Header.Del("Cache-Control")
		cloned.Header.Del("Pragma")
		for _, headerName := range conditionalRequestHeaders {
			cloned.Header.Del(headerName)
		}
	}

	return cloned, StateMiss
}

func shouldForceRefresh(req *http.Request, policy Policy) bool {
	if policy.IgnoreClientControl {
		return false
	}

	cacheControl := strings.ToLower(req.Header.Get("Cache-Control"))
	return strings.Contains(cacheControl, "no-cache") ||
		strings.Contains(cacheControl, "max-age=0") ||
		strings.EqualFold(req.Header.Get("Pragma"), "no-cache")
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

func buildCacheKey(siteID, method, path, rawQuery string) string {
	key := siteID + "|" + method + "|" + path
	if rawQuery != "" {
		key += "?" + rawQuery
	}
	return key
}

func sanitizeStoredHeader(header http.Header) http.Header {
	cloned := header.Clone()
	for _, hopByHop := range hopByHopHeaders {
		cloned.Del(hopByHop)
	}
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

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func isCacheableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusOK,
		http.StatusNonAuthoritativeInfo,
		http.StatusNoContent,
		http.StatusPartialContent,
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

func missCacheStatus(key string, stored bool) string {
	status := "TinyCDN; fwd=uri-miss; key=" + key
	if stored {
		return status + "; stored"
	}
	return status
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
