package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
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
	defaultMaxBufferedObjectBytes = 32 << 20
)

var ErrUpstreamResponseTooLarge = errors.New("upstream response exceeds buffered object limit")

type upstreamFetcher struct {
	client                 *http.Client
	maxBufferedObjectBytes int64
}

func newUpstreamFetcher() *upstreamFetcher {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &upstreamFetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   defaultUpstreamTimeout,
		},
		maxBufferedObjectBytes: defaultMaxBufferedObjectBytes,
	}
}

func (f *upstreamFetcher) Fetch(ctx context.Context, req *http.Request, site *runtime.CompiledSite) (cache.StoredResponse, error) {
	resp, err := f.Open(ctx, req, site)
	if err != nil {
		return cache.StoredResponse{}, err
	}
	defer resp.Body.Close()

	if responseTooLarge(resp, f.maxBufferedObjectBytes) {
		return cache.StoredResponse{}, ErrUpstreamResponseTooLarge
	}

	body, err := readLimitedBody(resp.Body, f.maxBufferedObjectBytes)
	if err != nil {
		return cache.StoredResponse{}, err
	}

	return cache.StoredResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
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

func readLimitedBody(body io.Reader, maxBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: body, N: maxBytes + 1}
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, ErrUpstreamResponseTooLarge
	}
	return payload, nil
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
