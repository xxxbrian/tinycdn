package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tinycdn/internal/cache"
	"tinycdn/internal/httpx"
	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

const (
	defaultUpstreamTimeout        = 60 * time.Second
	defaultMaxCacheableObjectSize = 10 << 30
)

var ErrUpstreamResponseTooLarge = errors.New("upstream response exceeds buffered object limit")

type upstreamFetcher struct {
	client                  *http.Client
	maxCacheableObjectBytes int64
	tempDir                 string
}

func newUpstreamFetcher(cachePath string) (*upstreamFetcher, error) {
	tempDir := filepath.Join(cachePath, "tmp")
	if err := cleanupDirectoryFiles(tempDir); err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &upstreamFetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   defaultUpstreamTimeout,
		},
		maxCacheableObjectBytes: defaultMaxCacheableObjectSize,
		tempDir:                 tempDir,
	}, nil
}

func (f *upstreamFetcher) Fetch(ctx context.Context, req *http.Request, site *runtime.CompiledSite) (cache.StoredResponse, error) {
	resp, err := f.Open(ctx, req, site)
	if err != nil {
		return cache.StoredResponse{}, err
	}
	defer resp.Body.Close()

	if responseTooLarge(resp, f.maxCacheableObjectBytes) {
		return cache.StoredResponse{}, ErrUpstreamResponseTooLarge
	}

	if err := os.MkdirAll(f.tempDir, 0o755); err != nil {
		return cache.StoredResponse{}, err
	}
	tempFile, err := os.CreateTemp(f.tempDir, "body-*")
	if err != nil {
		return cache.StoredResponse{}, err
	}
	defer tempFile.Close()

	size, err := io.Copy(tempFile, io.LimitReader(resp.Body, f.maxCacheableObjectBytes+1))
	if err != nil {
		_ = os.Remove(tempFile.Name())
		return cache.StoredResponse{}, err
	}
	if size > f.maxCacheableObjectBytes {
		_ = os.Remove(tempFile.Name())
		return cache.StoredResponse{}, ErrUpstreamResponseTooLarge
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempFile.Name())
		return cache.StoredResponse{}, err
	}

	return cache.StoredResponse{
		StatusCode:    resp.StatusCode,
		Header:        resp.Header.Clone(),
		BodyPath:      tempFile.Name(),
		ContentLength: contentLength(resp, size),
		CleanupPath:   true,
	}, nil
}

func (f *upstreamFetcher) Open(ctx context.Context, req *http.Request, site *runtime.CompiledSite) (*http.Response, error) {
	upstreamReq, err := buildUpstreamRequest(ctx, req, site)
	if err != nil {
		return nil, err
	}

	return f.client.Do(upstreamReq)
}

func buildUpstreamRequest(ctx context.Context, req *http.Request, site *runtime.CompiledSite) (*http.Request, error) {
	targetURL := new(url.URL)
	*targetURL = *site.Upstream
	targetURL.Path = req.URL.Path
	targetURL.RawPath = req.URL.RawPath
	targetURL.RawQuery = req.URL.RawQuery
	targetURL.Fragment = ""

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL.String(), req.Body)
	if err != nil {
		return nil, err
	}

	upstreamReq.Header = req.Header.Clone()
	httpx.StripHopByHopHeaders(upstreamReq.Header)
	applyForwardedHeaders(upstreamReq, req)
	upstreamReq.ContentLength = req.ContentLength
	upstreamReq.TransferEncoding = append([]string(nil), req.TransferEncoding...)
	upstreamReq.Trailer = req.Trailer.Clone()
	upstreamReq.Close = req.Close

	switch site.UpstreamMode {
	case model.UpstreamHostModeFollowRequest:
		upstreamReq.Host = req.Host
	case model.UpstreamHostModeCustom:
		upstreamReq.Host = site.UpstreamHost
	default:
		upstreamReq.Host = site.Upstream.Host
	}

	return upstreamReq, nil
}

func responseTooLarge(resp *http.Response, maxBytes int64) bool {
	return resp.ContentLength > maxBytes && resp.ContentLength >= 0
}

func contentLength(resp *http.Response, copied int64) int64 {
	if resp.ContentLength >= 0 {
		return resp.ContentLength
	}
	return copied
}

func cleanupDirectoryFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func applyForwardedHeaders(upstreamReq *http.Request, req *http.Request) {
	clientIP := forwardedClientIP(req.RemoteAddr)
	if clientIP != "" {
		appendHeaderValue(upstreamReq.Header, "X-Forwarded-For", clientIP)
	}
	upstreamReq.Header.Set("X-Forwarded-Host", req.Host)
	upstreamReq.Header.Set("X-Forwarded-Proto", requestScheme(req))
	appendHeaderValue(upstreamReq.Header, "Forwarded", buildForwardedHeader(req, clientIP))
	appendHeaderValue(upstreamReq.Header, "Via", "1.1 tinycdn")
}

func appendHeaderValue(header http.Header, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if existing := header.Values(key); len(existing) > 0 {
		header.Set(key, strings.Join(existing, ", ")+", "+value)
		return
	}
	header.Set(key, value)
}

func forwardedClientIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return addr.String()
}

func requestScheme(req *http.Request) string {
	if req.TLS != nil {
		return "https"
	}
	return "http"
}

func buildForwardedHeader(req *http.Request, clientIP string) string {
	parts := make([]string, 0, 3)
	if clientIP != "" {
		parts = append(parts, "for="+formatForwardedNode(clientIP))
	}
	if req.Host != "" {
		parts = append(parts, "host="+strconv.Quote(req.Host))
	}
	parts = append(parts, "proto="+requestScheme(req))
	return strings.Join(parts, ";")
}

func formatForwardedNode(value string) string {
	if strings.Contains(value, ":") {
		return strconv.Quote("[" + value + "]")
	}
	return strconv.Quote(value)
}
