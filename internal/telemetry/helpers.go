package telemetry

import "strings"

const (
	httpMethodKeyGet     = "GET"
	httpMethodKeyHead    = "HEAD"
	httpMethodKeyPost    = "POST"
	httpMethodKeyPut     = "PUT"
	httpMethodKeyDelete  = "DELETE"
	httpMethodKeyPatch   = "PATCH"
	httpMethodKeyOptions = "OPTIONS"
	httpMethodKeyOther   = "OTHER"
)

var histogramBounds = [...]int64{10, 25, 50, 100, 250, 500, 1000, 2500}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intForState(value bool) int {
	if value {
		return 1
	}
	return 0
}

func int64If(cond bool, value int64) int64 {
	if !cond {
		return 0
	}
	return value
}

func methodCount(method string, target string) int {
	if target == httpMethodKeyOther {
		switch method {
		case httpMethodKeyGet, httpMethodKeyHead, httpMethodKeyPost, httpMethodKeyPut, httpMethodKeyDelete, httpMethodKeyPatch, httpMethodKeyOptions:
			return 0
		default:
			return 1
		}
	}
	if method == target {
		return 1
	}
	return 0
}

func statusClass(statusCode int) string {
	switch {
	case statusCode >= 500:
		return "5xx"
	case statusCode >= 400:
		return "4xx"
	case statusCode >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

func histogramCounts(durationMS int64) [9]int {
	var counts [9]int
	for index, bound := range histogramBounds {
		if durationMS <= bound {
			counts[index] = 1
			return counts
		}
	}
	counts[len(counts)-1] = 1
	return counts
}

func estimatePercentile(counts []int64, percentile float64) int64 {
	total := int64(0)
	for _, count := range counts {
		total += count
	}
	if total == 0 {
		return 0
	}
	target := int64(percentile * float64(total))
	if target <= 0 {
		target = 1
	}
	running := int64(0)
	for index, count := range counts {
		running += count
		if running >= target {
			if index < len(histogramBounds) {
				return histogramBounds[index]
			}
			return histogramBounds[len(histogramBounds)-1] + 1
		}
	}
	return histogramBounds[len(histogramBounds)-1] + 1
}

func likePattern(search string) string {
	return "%" + strings.TrimSpace(search) + "%"
}
