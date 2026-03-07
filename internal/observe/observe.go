package observe

import "time"

type Recorder interface {
	RecordRequest(RequestEvent)
	RecordAudit(AuditEvent)
}

type NopRecorder struct{}

func (NopRecorder) RecordRequest(RequestEvent) {}

func (NopRecorder) RecordAudit(AuditEvent) {}

type RequestEvent struct {
	Timestamp        time.Time
	RequestID        string
	SiteID           string
	SiteName         string
	RuleID           string
	IsInternal       bool
	Method           string
	Scheme           string
	Host             string
	Path             string
	RawQuery         string
	RemoteIP         string
	UserAgent        string
	Referer          string
	CacheState       string
	CacheStatus      string
	StatusCode       int
	ResponseBytes    int64
	TotalDurationMS  int64
	OriginRequests   int
	OriginStatusCode int
	OriginDurationMS int64
	ErrorKind        string
	UpstreamHost     string
	ContentType      string
}

type AuditEvent struct {
	Timestamp    time.Time
	RequestID    string
	RemoteIP     string
	Method       string
	Path         string
	Action       string
	ResourceType string
	ResourceID   string
	Summary      string
	StatusCode   int
}
