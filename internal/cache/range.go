package cache

import (
	"context"
	"errors"
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

const DefaultRangeChunkSize int64 = 1 << 20

var ErrRangeBypass = errors.New("range request should bypass cache")

type RangePart struct {
	Body        []byte
	Path        string
	Offset      int64
	Length      int64
	CleanupPath bool
}

type RangeResult struct {
	State         State
	StatusCode    int
	Header        http.Header
	Parts         []RangePart
	ContentLength int64
	CacheStatus   string
}

type byteRangeSpec struct {
	Start    int64
	End      int64
	HasEnd   bool
	Suffix   bool
	SuffixN  int64
	Original string
}

type resolvedByteRange struct {
	Start int64
	End   int64
}

type RangeCache struct {
	store     Store
	now       func() time.Time
	chunkSize int64
	fill      singleflight.Group
}

func NewRangeCache(store Store) *RangeCache {
	return &RangeCache{
		store:     store,
		now:       func() time.Time { return time.Now().UTC() },
		chunkSize: DefaultRangeChunkSize,
	}
}

func (c *RangeCache) CanHandle(req *http.Request, policy Policy) bool {
	_, _, ok := prepareRangeRequest(req, policy)
	return ok
}

func (c *RangeCache) Handle(ctx context.Context, req *http.Request, policy Policy, fetch FetchFunc) (RangeResult, error) {
	preparedReq, requested, ok := prepareRangeRequest(req, policy)
	if !ok {
		return RangeResult{}, ErrRangeBypass
	}

	if result, ok, err := c.lookupFreshFullObject(ctx, preparedReq, requested, policy); err != nil {
		return RangeResult{}, err
	} else if ok {
		return result, nil
	}

	baseKey := buildBaseCacheKey(policy.SiteID, http.MethodGet, req.URL.Path, req.URL.RawQuery)
	objectKey, object, found, lookupErr, err := c.lookupChunkObject(ctx, baseKey, preparedReq.Header)
	if err != nil {
		return RangeResult{}, err
	}

	now := c.now()
	if found && object.PolicyTag == policy.PolicyTag {
		resolved, valid := resolveByteRange(requested, object.TotalLength)
		if !valid {
			return c.rangeNotSatisfiable(object, requested), nil
		}
		parts, complete, err := c.loadChunkParts(ctx, objectKey, requestedChunkIndexes(resolved, object.ChunkSize), resolved, object.ChunkSize, false)
		if err != nil {
			return RangeResult{}, err
		}
		if complete && now.Before(object.FreshUntil) {
			return buildChunkRangeResult(object, parts, resolved, StateHit, hitChunkCacheStatus(object, StateHit, now, false)), nil
		}
		if complete && policy.Optimistic && now.Before(object.StaleUntil) {
			go c.refreshInBackground(baseKey, preparedReq, requested, policy, fetch)
			return buildChunkRangeResult(object, parts, resolved, StateStale, hitChunkCacheStatus(object, StateStale, now, false)), nil
		}
	}

	result, err := c.fillRange(ctx, baseKey, preparedReq, requested, policy, fetch, lookupErr)
	if err != nil {
		return RangeResult{}, err
	}
	return result, nil
}

func (c *RangeCache) PurgeSite(ctx context.Context, siteID string) (int, error) {
	objDeleted, err := c.store.DeletePrefix(ctx, rangeObjectSitePrefix(siteID))
	if err != nil {
		return 0, err
	}
	chunkDeleted, err := c.store.DeletePrefix(ctx, rangeChunkSitePrefix(siteID))
	if err != nil {
		return objDeleted, err
	}
	varyDeleted, err := c.store.DeletePrefix(ctx, rangeVarySitePrefix(siteID))
	if err != nil {
		return objDeleted + chunkDeleted, err
	}
	return objDeleted + chunkDeleted + varyDeleted, nil
}

func (c *RangeCache) PurgeURL(ctx context.Context, siteID, path, rawQuery string) (int, error) {
	baseKey := buildBaseCacheKey(siteID, http.MethodGet, path, rawQuery)
	deleted := 0

	if _, found, err := c.store.GetVary(ctx, rangeVaryKey(baseKey)); err != nil {
		return deleted, err
	} else if found {
		if err := c.store.Delete(ctx, rangeVaryKey(baseKey)); err != nil {
			return deleted, err
		}
		deleted++
	}

	removed, err := c.store.DeletePrefix(ctx, rangeBaseObjectPrefix(baseKey))
	if err != nil {
		return deleted, err
	}
	deleted += removed

	removed, err = c.store.DeletePrefix(ctx, rangeBaseChunkPrefix(baseKey))
	if err != nil {
		return deleted, err
	}
	deleted += removed

	removed, err = c.store.DeletePrefix(ctx, rangeVariantObjectPrefix(baseKey))
	if err != nil {
		return deleted, err
	}
	deleted += removed

	removed, err = c.store.DeletePrefix(ctx, rangeVariantChunkPrefix(baseKey))
	if err != nil {
		return deleted, err
	}
	deleted += removed
	return deleted, nil
}

func (c *RangeCache) refreshInBackground(baseKey string, req *http.Request, requested byteRangeSpec, policy Policy, fetch FetchFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = c.fillRange(ctx, baseKey, req, requested, policy, fetch, nil)
}

func (c *RangeCache) lookupFreshFullObject(ctx context.Context, req *http.Request, requested byteRangeSpec, policy Policy) (RangeResult, bool, error) {
	baseKey := buildBaseCacheKey(policy.SiteID, http.MethodGet, req.URL.Path, req.URL.RawQuery)
	cacheKey, entry, found, err := c.lookupRegularEntry(ctx, baseKey, req.Header)
	if err != nil || !found || entry.PolicyTag != policy.PolicyTag {
		return RangeResult{}, false, err
	}
	if !c.now().Before(entry.FreshUntil) {
		return RangeResult{}, false, nil
	}
	if entry.Response.ContentLength <= 0 {
		return RangeResult{}, false, nil
	}
	resolved, valid := resolveByteRange(requested, entry.Response.ContentLength)
	if !valid {
		return c.rangeNotSatisfiableChunkHeader(entry.Response.Header, entry.Response.ContentLength, cacheKey, entry.StoredAt, entry.BaseAge), true, nil
	}
	part := RangePart{
		Body:   append([]byte(nil), entry.Response.Body...),
		Path:   entry.Response.BodyPath,
		Offset: resolved.Start,
		Length: resolved.End - resolved.Start + 1,
	}
	result := RangeResult{
		State:         StateHit,
		StatusCode:    http.StatusPartialContent,
		Header:        buildPartialHeader(entry.Response.Header, entry.Response.ContentLength, resolved, entry.StoredAt, entry.BaseAge, c.now()),
		Parts:         []RangePart{part},
		ContentLength: part.Length,
		CacheStatus:   hitCacheStatus(entry, StateHit, c.now()) + "; detail=RANGE",
	}
	return result, true, nil
}

func (c *RangeCache) lookupRegularEntry(ctx context.Context, baseKey string, requestHeader http.Header) (string, Entry, bool, error) {
	cacheKey := responseKey(baseKey)
	spec, found, err := c.store.GetVary(ctx, varyKey(baseKey))
	if err != nil {
		return cacheKey, Entry{}, false, err
	}
	if found && len(spec.Headers) > 0 {
		cacheKey = buildVariantKey(baseKey, spec.Headers, requestHeader)
	}
	entry, found, err := c.store.GetEntry(ctx, cacheKey)
	if err != nil {
		return cacheKey, Entry{}, false, err
	}
	return cacheKey, entry, found, nil
}

func (c *RangeCache) lookupChunkObject(ctx context.Context, baseKey string, requestHeader http.Header) (string, ChunkObject, bool, error, error) {
	storageKey := responseKey(baseKey)
	spec, found, err := c.store.GetVary(ctx, rangeVaryKey(baseKey))
	if err != nil {
		return storageKey, ChunkObject{}, false, err, err
	}
	if found && len(spec.Headers) > 0 {
		storageKey = buildVariantKey(baseKey, spec.Headers, requestHeader)
	}
	object, found, err := c.store.GetChunkObject(ctx, rangeObjectKey(storageKey))
	if err != nil {
		return storageKey, ChunkObject{}, false, err, err
	}
	return storageKey, object, found, nil, nil
}

func (c *RangeCache) fillRange(ctx context.Context, baseKey string, req *http.Request, requested byteRangeSpec, policy Policy, fetch FetchFunc, lookupErr error) (RangeResult, error) {
	initialObjectKey, currentObject, found, _, err := c.lookupChunkObject(ctx, baseKey, req.Header)
	if err != nil {
		return RangeResult{}, err
	}

	var totalLength int64
	if found && currentObject.TotalLength > 0 {
		totalLength = currentObject.TotalLength
	}
	if requested.Suffix && totalLength == 0 {
		return RangeResult{}, ErrRangeBypass
	}

	resolved, ok := resolveByteRange(requested, totalLength)
	if !ok && totalLength > 0 {
		return c.rangeNotSatisfiable(currentObject, requested), nil
	}

	chunkIndexes := requestedChunkIndexesKnown(requested, resolved, totalLength, c.chunkSize)
	if len(chunkIndexes) == 0 {
		return RangeResult{}, ErrRangeBypass
	}

	object := currentObject
	objectKey := initialObjectKey
	partsByIndex := map[int64]RangePart{}

	for i, chunkIndex := range chunkIndexes {
		if found && object.PolicyTag == policy.PolicyTag {
			if chunk, chunkFound, chunkErr := c.store.GetChunk(ctx, rangeChunkKey(objectKey, chunkIndex)); chunkErr != nil {
				return RangeResult{}, chunkErr
			} else if chunkFound && c.now().Before(object.InvalidAt) {
				partsByIndex[chunkIndex] = RangePart{Path: chunk.BodyPath, Length: chunk.Size}
				continue
			}
		}

		fetched, nextObject, nextObjectKey, fullObject, err := c.fetchAndMaybeStoreChunk(ctx, req, baseKey, policy, chunkIndex, fetch)
		if err != nil {
			return RangeResult{}, err
		}
		if fullObject {
			resolved, ok := resolveByteRange(requested, nextObject.TotalLength)
			if !ok {
				return c.rangeNotSatisfiable(nextObject, requested), nil
			}
			fetched.part.Offset = resolved.Start
			fetched.part.Length = resolved.End - resolved.Start + 1
			return RangeResult{
				State:         StateMiss,
				StatusCode:    http.StatusPartialContent,
				Header:        buildPartialHeader(nextObject.Header, nextObject.TotalLength, resolved, nextObject.StoredAt, nextObject.BaseAge, c.now()),
				Parts:         []RangePart{fetched.part},
				ContentLength: fetched.part.Length,
				CacheStatus:   missCacheStatus(nextObjectKey, false, combineStatusDetails(lookupErr, "RANGE,FULL_OBJECT")),
			}, nil
		}
		if i == 0 && totalLength == 0 {
			totalLength = nextObject.TotalLength
			resolved, ok = resolveByteRange(requested, totalLength)
			if !ok {
				return c.rangeNotSatisfiable(nextObject, requested), nil
			}
			chunkIndexes = requestedChunkIndexes(resolved, nextObject.ChunkSize)
		}
		object = nextObject
		objectKey = nextObjectKey
		found = true
		partsByIndex[chunkIndex] = fetched.part
		_ = fetched.entry
		_ = fetched.resp
	}

	if totalLength == 0 {
		return RangeResult{}, ErrRangeBypass
	}
	resolved, ok = resolveByteRange(requested, totalLength)
	if !ok {
		return c.rangeNotSatisfiable(object, requested), nil
	}

	ordered := make([]RangePart, 0, len(chunkIndexes))
	for _, chunkIndex := range chunkIndexes {
		part, ok := partsByIndex[chunkIndex]
		if !ok {
			return RangeResult{}, ErrRangeBypass
		}
		part.Offset = chunkSliceOffset(resolved, chunkIndex, object.ChunkSize)
		part.Length = chunkSliceLength(resolved, chunkIndex, object.ChunkSize)
		ordered = append(ordered, part)
	}

	return buildChunkRangeResult(object, ordered, resolved, StateMiss, missCacheStatus(objectKey, false, combineStatusDetails(lookupErr, "RANGE"))), nil
}

func (c *RangeCache) fetchAndMaybeStoreChunk(ctx context.Context, req *http.Request, baseKey string, policy Policy, chunkIndex int64, fetch FetchFunc) (struct {
	entry ChunkEntry
	part  RangePart
	resp  StoredResponse
}, ChunkObject, string, bool, error) {
	fillKey := rangeChunkKey(responseKey(baseKey), chunkIndex)
	value, err, _ := c.fill.Do(fillKey, func() (any, error) {
		response, object, objectKey, decision, fullObject, err := c.fetchChunk(ctx, req, policy, chunkIndex, fetch)
		if err != nil {
			return nil, err
		}

		chunk := ChunkEntry{
			Key:       rangeChunkKey(objectKey, chunkIndex),
			SiteID:    policy.SiteID,
			ObjectKey: objectKey,
			Index:     chunkIndex,
			StoredAt:  c.now(),
			InvalidAt: object.InvalidAt,
			BodyPath:  response.BodyPath,
			Size:      response.ContentLength,
		}
		part := RangePart{
			Path:        response.BodyPath,
			Length:      response.ContentLength,
			CleanupPath: response.CleanupPath,
		}

		if fullObject {
			return struct {
				entry      ChunkEntry
				part       RangePart
				resp       StoredResponse
				obj        ChunkObject
				key        string
				fullObject bool
			}{entry: chunk, part: part, resp: response, obj: object, key: objectKey, fullObject: true}, nil
		}

		if decision.Store {
			existingObject, existingFound, err := c.store.GetChunkObject(ctx, rangeObjectKey(objectKey))
			if err != nil {
				return nil, err
			}

			finalResponse, err := c.persistChunkResponse(ctx, response)
			if err != nil {
				return nil, err
			}
			response = finalResponse
			chunk.BodyPath = response.BodyPath
			chunk.Size = response.ContentLength
			part.Path = response.BodyPath
			part.CleanupPath = false

			if err := c.storeChunkObject(ctx, baseKey, objectKey, object, decision.VaryHeaders, existingObject, existingFound); err != nil {
				part.CleanupPath = true
				return struct {
					entry ChunkEntry
					part  RangePart
					resp  StoredResponse
					obj   ChunkObject
					key   string
				}{entry: chunk, part: part, resp: response, obj: object, key: objectKey}, nil
			}

			if existingFound && !sameStoredValidators(existingObject.Header, object.Header) {
				if _, err := c.store.DeletePrefix(ctx, rangeChunkPrefix(objectKey)); err != nil {
					return nil, err
				}
			}
			if err := c.store.PutChunk(ctx, chunk.Key, chunk); err != nil {
				part.CleanupPath = true
			}
		}

		return struct {
			entry      ChunkEntry
			part       RangePart
			resp       StoredResponse
			obj        ChunkObject
			key        string
			fullObject bool
		}{entry: chunk, part: part, resp: response, obj: object, key: objectKey}, nil
	})
	if err != nil {
		return struct {
			entry ChunkEntry
			part  RangePart
			resp  StoredResponse
		}{}, ChunkObject{}, "", false, err
	}

	result := value.(struct {
		entry      ChunkEntry
		part       RangePart
		resp       StoredResponse
		obj        ChunkObject
		key        string
		fullObject bool
	})
	return struct {
		entry ChunkEntry
		part  RangePart
		resp  StoredResponse
	}{entry: result.entry, part: result.part, resp: result.resp}, result.obj, result.key, result.fullObject, nil
}

func (c *RangeCache) fetchChunk(ctx context.Context, req *http.Request, policy Policy, chunkIndex int64, fetch FetchFunc) (StoredResponse, ChunkObject, string, storeDecision, bool, error) {
	chunkStart := chunkIndex * c.chunkSize
	chunkEnd := chunkStart + c.chunkSize - 1

	chunkReq := req.Clone(ctx)
	chunkReq.Header = req.Header.Clone()
	chunkReq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunkStart, chunkEnd))
	chunkReq.Header.Set("Accept-Encoding", "identity")

	response, err := fetch(ctx, chunkReq)
	if err != nil {
		return StoredResponse{}, ChunkObject{}, "", storeDecision{}, false, err
	}
	if response.StatusCode == http.StatusOK && response.Header.Get("Content-Range") == "" && response.ContentLength > 0 {
		object := buildChunkObject(c.now(), responseKey(buildBaseCacheKey(policy.SiteID, http.MethodGet, req.URL.Path, req.URL.RawQuery)), policy, storeDecision{}, response, response.ContentLength, response.ContentLength)
		return response, object, object.Key, storeDecision{}, true, nil
	}
	if response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Range") == "" {
		return StoredResponse{}, ChunkObject{}, "", storeDecision{}, false, ErrRangeBypass
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return StoredResponse{}, ChunkObject{}, "", storeDecision{}, false, ErrRangeBypass
	}

	contentRange, ok := parseContentRange(response.Header.Get("Content-Range"))
	if !ok {
		return StoredResponse{}, ChunkObject{}, "", storeDecision{}, false, ErrRangeBypass
	}
	decision := decideChunkStore(c.now(), policy, response)
	objectKey := buildStorageKey(
		buildBaseCacheKey(policy.SiteID, http.MethodGet, req.URL.Path, req.URL.RawQuery),
		decision.VaryHeaders,
		req.Header,
	)
	object := buildChunkObject(c.now(), objectKey, policy, decision, response, contentRange.Total, c.chunkSize)
	return response, object, objectKey, decision, false, nil
}

func (c *RangeCache) persistChunkResponse(ctx context.Context, response StoredResponse) (StoredResponse, error) {
	if response.BodyPath == "" || !response.CleanupPath {
		return response, nil
	}
	finalPath, err := c.store.ImportBody(ctx, response.BodyPath)
	if err != nil {
		return StoredResponse{}, err
	}
	response.BodyPath = finalPath
	response.CleanupPath = false
	return response, nil
}

func (c *RangeCache) loadChunkParts(ctx context.Context, objectKey string, chunkIndexes []int64, resolved resolvedByteRange, chunkSize int64, allowStale bool) ([]RangePart, bool, error) {
	parts := make([]RangePart, 0, len(chunkIndexes))
	for _, chunkIndex := range chunkIndexes {
		chunk, found, err := c.store.GetChunk(ctx, rangeChunkKey(objectKey, chunkIndex))
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		part := RangePart{
			Path:   chunk.BodyPath,
			Offset: chunkSliceOffset(resolved, chunkIndex, chunkSize),
			Length: chunkSliceLength(resolved, chunkIndex, chunkSize),
		}
		if !allowStale && !c.now().Before(chunk.InvalidAt) {
			return nil, false, nil
		}
		parts = append(parts, part)
	}
	return parts, true, nil
}

func (c *RangeCache) storeChunkObject(ctx context.Context, baseKey, objectKey string, object ChunkObject, varyHeaders []string, existingObject ChunkObject, existingFound bool) error {
	currentSpec, found, err := c.store.GetVary(ctx, rangeVaryKey(baseKey))
	if err != nil {
		return err
	}
	if (found && !slices.Equal(currentSpec.Headers, varyHeaders)) || (!found && len(varyHeaders) > 0) {
		if err := c.purgeRangeStorage(ctx, baseKey); err != nil {
			return err
		}
	}
	if len(varyHeaders) > 0 {
		if err := c.store.PutVary(ctx, rangeVaryKey(baseKey), VarySpec{Headers: varyHeaders}); err != nil {
			return err
		}
	}
	if existingFound && !sameStoredValidators(existingObject.Header, object.Header) {
		if _, err := c.store.DeletePrefix(ctx, rangeChunkPrefix(objectKey)); err != nil {
			return err
		}
	}
	return c.store.PutChunkObject(ctx, rangeObjectKey(objectKey), object)
}

func (c *RangeCache) purgeRangeStorage(ctx context.Context, baseKey string) error {
	_ = c.store.Delete(ctx, rangeVaryKey(baseKey))
	_, err := c.store.DeletePrefix(ctx, rangeBaseObjectPrefix(baseKey))
	if err != nil {
		return err
	}
	_, err = c.store.DeletePrefix(ctx, rangeBaseChunkPrefix(baseKey))
	if err != nil {
		return err
	}
	_, err = c.store.DeletePrefix(ctx, rangeVariantObjectPrefix(baseKey))
	if err != nil {
		return err
	}
	_, err = c.store.DeletePrefix(ctx, rangeVariantChunkPrefix(baseKey))
	return err
}

func prepareRangeRequest(req *http.Request, policy Policy) (*http.Request, byteRangeSpec, bool) {
	if policy.Mode == model.CacheModeBypass {
		return nil, byteRangeSpec{}, false
	}
	if req.Method != http.MethodGet {
		return nil, byteRangeSpec{}, false
	}
	if hasSharedCacheSensitiveRequestHeaders(req.Header) {
		return nil, byteRangeSpec{}, false
	}
	if req.Header.Get("If-Range") != "" {
		return nil, byteRangeSpec{}, false
	}
	spec, ok := parseSingleByteRange(req.Header.Get("Range"))
	if !ok {
		return nil, byteRangeSpec{}, false
	}

	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Del("Cache-Control")
	cloned.Header.Del("Pragma")
	for _, headerName := range conditionalRequestHeaders {
		cloned.Header.Del(headerName)
	}
	cloned.Header.Del("Range")
	return cloned, spec, true
}

func parseSingleByteRange(value string) (byteRangeSpec, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return byteRangeSpec{}, false
	}
	if !strings.HasPrefix(value, "bytes=") {
		return byteRangeSpec{}, false
	}
	specs := strings.Split(strings.TrimPrefix(value, "bytes="), ",")
	if len(specs) != 1 {
		return byteRangeSpec{}, false
	}
	part := strings.TrimSpace(specs[0])
	startRaw, endRaw, ok := strings.Cut(part, "-")
	if !ok {
		return byteRangeSpec{}, false
	}
	if startRaw == "" {
		suffix, err := strconv.ParseInt(strings.TrimSpace(endRaw), 10, 64)
		if err != nil || suffix <= 0 {
			return byteRangeSpec{}, false
		}
		return byteRangeSpec{Suffix: true, SuffixN: suffix, Original: value}, true
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startRaw), 10, 64)
	if err != nil || start < 0 {
		return byteRangeSpec{}, false
	}
	if strings.TrimSpace(endRaw) == "" {
		return byteRangeSpec{Start: start, Original: value}, true
	}
	end, err := strconv.ParseInt(strings.TrimSpace(endRaw), 10, 64)
	if err != nil || end < start {
		return byteRangeSpec{}, false
	}
	return byteRangeSpec{Start: start, End: end, HasEnd: true, Original: value}, true
}

func resolveByteRange(spec byteRangeSpec, totalLength int64) (resolvedByteRange, bool) {
	if totalLength <= 0 {
		if spec.Suffix {
			return resolvedByteRange{}, false
		}
		if spec.HasEnd {
			return resolvedByteRange{Start: spec.Start, End: spec.End}, true
		}
		return resolvedByteRange{Start: spec.Start, End: spec.Start}, true
	}
	if spec.Suffix {
		if spec.SuffixN > totalLength {
			spec.SuffixN = totalLength
		}
		return resolvedByteRange{Start: totalLength - spec.SuffixN, End: totalLength - 1}, true
	}
	if spec.Start >= totalLength {
		return resolvedByteRange{}, false
	}
	end := totalLength - 1
	if spec.HasEnd && spec.End < end {
		end = spec.End
	}
	return resolvedByteRange{Start: spec.Start, End: end}, true
}

func requestedChunkIndexesKnown(spec byteRangeSpec, resolved resolvedByteRange, totalLength, chunkSize int64) []int64 {
	if totalLength > 0 {
		return requestedChunkIndexes(resolved, chunkSize)
	}
	if spec.Suffix {
		return nil
	}
	end := spec.Start
	if spec.HasEnd {
		end = spec.End
	}
	first := spec.Start / chunkSize
	last := end / chunkSize
	indexes := make([]int64, 0, last-first+1)
	for idx := first; idx <= last; idx++ {
		indexes = append(indexes, idx)
	}
	return indexes
}

func requestedChunkIndexes(resolved resolvedByteRange, chunkSize int64) []int64 {
	first := resolved.Start / chunkSize
	last := resolved.End / chunkSize
	indexes := make([]int64, 0, last-first+1)
	for idx := first; idx <= last; idx++ {
		indexes = append(indexes, idx)
	}
	return indexes
}

func chunkSliceOffset(resolved resolvedByteRange, chunkIndex, chunkSize int64) int64 {
	chunkStart := chunkIndex * chunkSize
	if resolved.Start > chunkStart {
		return resolved.Start - chunkStart
	}
	return 0
}

func chunkSliceLength(resolved resolvedByteRange, chunkIndex, chunkSize int64) int64 {
	chunkStart := chunkIndex * chunkSize
	chunkEnd := chunkStart + chunkSize - 1
	start := maxInt64(resolved.Start, chunkStart)
	end := minInt64(resolved.End, chunkEnd)
	return end - start + 1
}

func buildChunkObject(now time.Time, objectKey string, policy Policy, decision storeDecision, response StoredResponse, totalLength, chunkSize int64) ChunkObject {
	object := ChunkObject{
		Key:         objectKey,
		SiteID:      policy.SiteID,
		RuleID:      policy.RuleID,
		PolicyTag:   policy.PolicyTag,
		StoredAt:    now,
		FreshUntil:  now.Add(decision.TTL),
		StaleUntil:  now.Add(decision.TTL + decision.StaleWindow),
		InvalidAt:   now.Add(decision.TTL + decision.StaleWindow + decision.StaleIfError),
		BaseAge:     parseAge(response.Header.Get("Age")),
		Header:      sanitizeChunkHeader(response.Header),
		TotalLength: totalLength,
		ChunkSize:   chunkSize,
	}
	if !object.InvalidAt.After(object.StoredAt) && hasRevalidationValidators(response.Header) {
		object.InvalidAt = object.StoredAt.Add(DefaultManagedTTL)
	}
	if object.InvalidAt.Before(object.StoredAt) {
		object.InvalidAt = object.StoredAt
	}
	if object.StaleUntil.Before(object.FreshUntil) {
		object.StaleUntil = object.FreshUntil
	}
	return object
}

func decideChunkStore(now time.Time, policy Policy, response StoredResponse) storeDecision {
	if response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Range") == "" {
		return storeDecision{}
	}
	if len(response.Header.Values("Set-Cookie")) > 0 {
		return storeDecision{}
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
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

func buildChunkRangeResult(object ChunkObject, parts []RangePart, resolved resolvedByteRange, state State, cacheStatus string) RangeResult {
	header := buildPartialHeader(object.Header, object.TotalLength, resolved, object.StoredAt, object.BaseAge, time.Now().UTC())
	return RangeResult{
		State:         state,
		StatusCode:    http.StatusPartialContent,
		Header:        header,
		Parts:         parts,
		ContentLength: resolved.End - resolved.Start + 1,
		CacheStatus:   cacheStatus,
	}
}

func buildPartialHeader(header http.Header, totalLength int64, resolved resolvedByteRange, storedAt time.Time, baseAge int, now time.Time) http.Header {
	cloned := sanitizeChunkHeader(header)
	cloned.Set("Accept-Ranges", "bytes")
	cloned.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", resolved.Start, resolved.End, totalLength))
	cloned.Set("Content-Length", strconv.FormatInt(resolved.End-resolved.Start+1, 10))
	cloned.Set("Age", strconv.Itoa(baseAge+int(now.Sub(storedAt).Seconds())))
	return cloned
}

func sanitizeChunkHeader(header http.Header) http.Header {
	cloned := sanitizeStoredHeader(header)
	cloned.Del("Content-Range")
	return cloned
}

func (c *RangeCache) rangeNotSatisfiable(object ChunkObject, _ byteRangeSpec) RangeResult {
	return RangeResult{
		State:         StateBypass,
		StatusCode:    http.StatusRequestedRangeNotSatisfiable,
		Header:        http.Header{"Content-Range": {fmt.Sprintf("bytes */%d", object.TotalLength)}},
		ContentLength: 0,
		CacheStatus:   bypassCacheStatus("range-unsatisfiable"),
	}
}

func (c *RangeCache) rangeNotSatisfiableChunkHeader(header http.Header, totalLength int64, _ string, storedAt time.Time, baseAge int) RangeResult {
	return RangeResult{
		State:         StateHit,
		StatusCode:    http.StatusRequestedRangeNotSatisfiable,
		Header:        http.Header{"Content-Range": {fmt.Sprintf("bytes */%d", totalLength)}, "Age": {strconv.Itoa(baseAge + int(c.now().Sub(storedAt).Seconds()))}},
		ContentLength: 0,
		CacheStatus:   bypassCacheStatus("range-unsatisfiable"),
	}
}

type contentRangeSpec struct {
	Start int64
	End   int64
	Total int64
}

func parseContentRange(value string) (contentRangeSpec, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return contentRangeSpec{}, false
	}
	rangePart, totalPart, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	if !ok {
		return contentRangeSpec{}, false
	}
	startRaw, endRaw, ok := strings.Cut(rangePart, "-")
	if !ok {
		return contentRangeSpec{}, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startRaw), 10, 64)
	if err != nil || start < 0 {
		return contentRangeSpec{}, false
	}
	end, err := strconv.ParseInt(strings.TrimSpace(endRaw), 10, 64)
	if err != nil || end < start {
		return contentRangeSpec{}, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(totalPart), 10, 64)
	if err != nil || total <= end {
		return contentRangeSpec{}, false
	}
	return contentRangeSpec{Start: start, End: end, Total: total}, true
}

func sameStoredValidators(a, b http.Header) bool {
	return strings.TrimSpace(a.Get("ETag")) == strings.TrimSpace(b.Get("ETag")) &&
		strings.TrimSpace(a.Get("Last-Modified")) == strings.TrimSpace(b.Get("Last-Modified"))
}

func hitChunkCacheStatus(object ChunkObject, state State, now time.Time, revalidated bool) string {
	ttl := maxDuration(0, object.FreshUntil.Sub(now))
	status := fmt.Sprintf("TinyCDN; hit; ttl=%d; key=%s; detail=RANGE", int(ttl.Seconds()), object.Key)
	if state == StateStale {
		status += "; fwd=stale"
	}
	if revalidated {
		status += ",REVALIDATED"
	}
	return status
}

func rangeObjectKey(storageKey string) string {
	return "rmeta|" + storageKey
}

func rangeChunkKey(storageKey string, index int64) string {
	return fmt.Sprintf("rchunk|%s|%020d", storageKey, index)
}

func rangeChunkPrefix(storageKey string) string {
	return "rchunk|" + storageKey + "|"
}

func rangeVaryKey(baseKey string) string {
	return "rvary|" + baseKey
}

func rangeBaseObjectPrefix(baseKey string) string {
	return rangeObjectKey(responseKey(baseKey))
}

func rangeVariantObjectPrefix(baseKey string) string {
	return "rmeta|" + variantPrefix(baseKey)
}

func rangeBaseChunkPrefix(baseKey string) string {
	return rangeChunkPrefix(responseKey(baseKey))
}

func rangeVariantChunkPrefix(baseKey string) string {
	return "rchunk|" + variantPrefix(baseKey)
}

func rangeObjectSitePrefix(siteID string) string {
	return "rmeta|resp|" + siteID + "|"
}

func rangeChunkSitePrefix(siteID string) string {
	return "rchunk|resp|" + siteID + "|"
}

func rangeVarySitePrefix(siteID string) string {
	return "rvary|" + siteID + "|"
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
