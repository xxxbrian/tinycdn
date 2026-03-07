package httpx

import (
	"net/http"
	"strings"
)

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

func CombinedHeaderValue(header http.Header, key string) string {
	return strings.Join(header.Values(key), ",")
}

func StripHopByHopHeaders(header http.Header) {
	connectionTokens := make([]string, 0)
	for _, value := range header.Values("Connection") {
		connectionTokens = append(connectionTokens, strings.Split(value, ",")...)
	}

	for _, key := range hopByHopHeaders {
		header.Del(key)
	}

	for _, token := range connectionTokens {
		name := http.CanonicalHeaderKey(strings.TrimSpace(token))
		if name == "" {
			continue
		}
		header.Del(name)
	}
}
