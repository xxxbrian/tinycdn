package telemetry

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"tinycdn/internal/observe"
)

func TestServiceFlushAndQuery(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	service := &Service{db: db}
	if err := service.initSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	now := time.Now().UTC()
	batch := []envelope{
		{
			request: &observe.RequestEvent{
				Timestamp:        now,
				RequestID:        "req-1",
				SiteID:           "site-1",
				SiteName:         "Example",
				RuleID:           "rule-1",
				Method:           "GET",
				Scheme:           "https",
				Host:             "cdn.example.com",
				Path:             "/assets/app.js",
				CacheState:       "HIT",
				CacheStatus:      "TinyCDN; hit",
				StatusCode:       200,
				ResponseBytes:    2048,
				TotalDurationMS:  12,
				OriginRequests:   0,
				OriginStatusCode: 0,
				OriginDurationMS: 0,
				ContentType:      "application/javascript",
			},
		},
		{
			request: &observe.RequestEvent{
				Timestamp:        now.Add(2 * time.Minute),
				RequestID:        "req-2",
				SiteID:           "site-1",
				SiteName:         "Example",
				RuleID:           "rule-2",
				Method:           "GET",
				Scheme:           "https",
				Host:             "cdn.example.com",
				Path:             "/assets/style.css",
				CacheState:       "MISS",
				CacheStatus:      "TinyCDN; miss",
				StatusCode:       200,
				ResponseBytes:    1024,
				TotalDurationMS:  120,
				OriginRequests:   1,
				OriginStatusCode: 200,
				OriginDurationMS: 90,
				ContentType:      "text/css",
			},
		},
		{
			audit: &observe.AuditEvent{
				Timestamp:    now,
				RequestID:    "admin-1",
				RemoteIP:     "127.0.0.1",
				Method:       "POST",
				Path:         "/api/sites/site-1/cache/purge",
				Action:       "cache.purged",
				ResourceType: "site",
				ResourceID:   "site-1",
				Summary:      "Purged cache for site site-1",
				StatusCode:   200,
			},
		},
	}

	if err := service.flushBatch(batch); err != nil {
		t.Fatalf("flush batch: %v", err)
	}

	report, err := service.Report(context.Background(), ReportParams{SiteID: "site-1", Period: 24 * time.Hour})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.Summary.Requests != 2 {
		t.Fatalf("unexpected request count: %#v", report.Summary)
	}
	if report.Summary.OriginRequests != 1 {
		t.Fatalf("unexpected origin requests: %#v", report.Summary)
	}
	if len(report.TopPaths) != 2 {
		t.Fatalf("expected top paths, got %#v", report.TopPaths)
	}

	requests, err := service.RequestLogs(context.Background(), RequestLogQuery{SiteID: "site-1", Period: 24 * time.Hour, Limit: 10})
	if err != nil {
		t.Fatalf("request logs: %v", err)
	}
	if len(requests.Items) != 2 {
		t.Fatalf("unexpected request log items: %#v", requests.Items)
	}

	audits, err := service.AuditLogs(context.Background(), AuditLogQuery{SiteID: "site-1", Period: 24 * time.Hour, Limit: 10})
	if err != nil {
		t.Fatalf("audit logs: %v", err)
	}
	if len(audits.Items) != 1 {
		t.Fatalf("unexpected audit items: %#v", audits.Items)
	}
}
