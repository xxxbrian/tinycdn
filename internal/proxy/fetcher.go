package proxy

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"tinycdn/internal/cache"
	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

type upstreamFetcher struct {
	client *http.Client
}

func newUpstreamFetcher() *upstreamFetcher {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &upstreamFetcher{
		client: &http.Client{Transport: transport},
	}
}

func (f *upstreamFetcher) Fetch(ctx context.Context, req *http.Request, site *runtime.CompiledSite) (cache.StoredResponse, error) {
	upstreamReq, err := buildUpstreamRequest(ctx, req, site)
	if err != nil {
		return cache.StoredResponse{}, err
	}

	resp, err := f.client.Do(upstreamReq)
	if err != nil {
		return cache.StoredResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return cache.StoredResponse{}, err
	}

	return cache.StoredResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
	}, nil
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
