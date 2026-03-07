package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"tinycdn/internal/observe"
)

const (
	defaultQueueSize           = 8192
	defaultFlushInterval       = 2 * time.Second
	defaultRequestLogRetention = 7 * 24 * time.Hour
	defaultRollupRetention     = 90 * 24 * time.Hour
	defaultAuditLogRetention   = 180 * 24 * time.Hour
)

type Service struct {
	db     *sql.DB
	logger *slog.Logger

	queue chan envelope
	wg    sync.WaitGroup
	stop  chan struct{}
	once  sync.Once
}

type envelope struct {
	request *observe.RequestEvent
	audit   *observe.AuditEvent
}

type ReportParams struct {
	SiteID string
	Period time.Duration
}

type Report struct {
	GeneratedAt    time.Time        `json:"generated_at"`
	Period         string           `json:"period"`
	Summary        ReportSummary    `json:"summary"`
	Series         []SeriesPoint    `json:"series"`
	CacheStates    []CountBreakdown `json:"cache_states"`
	Methods        []CountBreakdown `json:"methods"`
	StatusClasses  []CountBreakdown `json:"status_classes"`
	TopPaths       []PathBreakdown  `json:"top_paths"`
	TopHosts       []CountBreakdown `json:"top_hosts"`
	TopSites       []SiteBreakdown  `json:"top_sites"`
	TopRules       []CountBreakdown `json:"top_rules"`
	CacheInventory CacheInventory   `json:"cache_inventory"`
}

type ReportSummary struct {
	Requests                int64   `json:"requests"`
	EdgeBytes               int64   `json:"edge_bytes"`
	CachedBytes             int64   `json:"cached_bytes"`
	OriginBytes             int64   `json:"origin_bytes"`
	OriginRequests          int64   `json:"origin_requests"`
	HitRequests             int64   `json:"hit_requests"`
	StaleRequests           int64   `json:"stale_requests"`
	MissRequests            int64   `json:"miss_requests"`
	BypassRequests          int64   `json:"bypass_requests"`
	ErrorRequests           int64   `json:"error_requests"`
	CacheHitRatio           float64 `json:"cache_hit_ratio"`
	CacheBandwidthRatio     float64 `json:"cache_bandwidth_ratio"`
	ErrorRate               float64 `json:"error_rate"`
	AverageEdgeDurationMS   float64 `json:"average_edge_duration_ms"`
	P95EdgeDurationMS       int64   `json:"p95_edge_duration_ms"`
	AverageOriginDurationMS float64 `json:"average_origin_duration_ms"`
	P95OriginDurationMS     int64   `json:"p95_origin_duration_ms"`
	ActiveSites             int64   `json:"active_sites"`
}

type SeriesPoint struct {
	Bucket         time.Time `json:"bucket"`
	Requests       int64     `json:"requests"`
	HitRequests    int64     `json:"hit_requests"`
	StaleRequests  int64     `json:"stale_requests"`
	MissRequests   int64     `json:"miss_requests"`
	BypassRequests int64     `json:"bypass_requests"`
	ErrorRequests  int64     `json:"error_requests"`
	EdgeBytes      int64     `json:"edge_bytes"`
	CachedBytes    int64     `json:"cached_bytes"`
	OriginRequests int64     `json:"origin_requests"`
	OriginBytes    int64     `json:"origin_bytes"`
}

type CountBreakdown struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Requests  int64   `json:"requests"`
	EdgeBytes int64   `json:"edge_bytes,omitempty"`
	HitRatio  float64 `json:"hit_ratio,omitempty"`
	SiteID    string  `json:"site_id,omitempty"`
	SiteName  string  `json:"site_name,omitempty"`
}

type PathBreakdown struct {
	Path      string  `json:"path"`
	Requests  int64   `json:"requests"`
	EdgeBytes int64   `json:"edge_bytes"`
	HitRatio  float64 `json:"hit_ratio"`
}

type SiteBreakdown struct {
	SiteID    string  `json:"site_id"`
	SiteName  string  `json:"site_name"`
	Requests  int64   `json:"requests"`
	EdgeBytes int64   `json:"edge_bytes"`
	HitRatio  float64 `json:"hit_ratio"`
}

type CacheInventory struct {
	Objects int64 `json:"objects"`
	Bytes   int64 `json:"bytes"`
}

type RequestLogQuery struct {
	SiteID          string
	Period          time.Duration
	Limit           int
	Cursor          string
	Method          string
	CacheState      string
	StatusClass     string
	PathPrefix      string
	Search          string
	IncludeInternal bool
}

type RequestLogPage struct {
	Items      []RequestLogItem `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type RequestLogItem struct {
	ID               int64     `json:"id"`
	Timestamp        time.Time `json:"timestamp"`
	RequestID        string    `json:"request_id"`
	SiteID           string    `json:"site_id"`
	SiteName         string    `json:"site_name"`
	RuleID           string    `json:"rule_id"`
	IsInternal       bool      `json:"is_internal"`
	Method           string    `json:"method"`
	Scheme           string    `json:"scheme"`
	Host             string    `json:"host"`
	Path             string    `json:"path"`
	RawQuery         string    `json:"raw_query"`
	RemoteIP         string    `json:"remote_ip"`
	UserAgent        string    `json:"user_agent"`
	Referer          string    `json:"referer"`
	CacheState       string    `json:"cache_state"`
	CacheStatus      string    `json:"cache_status"`
	StatusCode       int       `json:"status_code"`
	ResponseBytes    int64     `json:"response_bytes"`
	TotalDurationMS  int64     `json:"total_duration_ms"`
	OriginRequests   int       `json:"origin_requests"`
	OriginStatusCode int       `json:"origin_status_code"`
	OriginDurationMS int64     `json:"origin_duration_ms"`
	ErrorKind        string    `json:"error_kind"`
	UpstreamHost     string    `json:"upstream_host"`
	ContentType      string    `json:"content_type"`
}

type AuditLogQuery struct {
	SiteID string
	Period time.Duration
	Limit  int
	Cursor string
}

type AuditLogPage struct {
	Items      []AuditLogItem `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type AuditLogItem struct {
	ID           int64     `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	RequestID    string    `json:"request_id"`
	RemoteIP     string    `json:"remote_ip"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Summary      string    `json:"summary"`
	StatusCode   int       `json:"status_code"`
}

func NewService(path string, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	service := &Service{
		db:     db,
		logger: logger,
		queue:  make(chan envelope, defaultQueueSize),
		stop:   make(chan struct{}),
	}
	if err := service.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	service.wg.Add(1)
	go service.run()
	return service, nil
}

func (s *Service) RecordRequest(event observe.RequestEvent) {
	copy := event
	select {
	case s.queue <- envelope{request: &copy}:
	default:
		s.logger.Warn("telemetry queue full, dropping request event", "request_id", event.RequestID)
	}
}

func (s *Service) RecordAudit(event observe.AuditEvent) {
	copy := event
	select {
	case s.queue <- envelope{audit: &copy}:
	default:
		s.logger.Warn("telemetry queue full, dropping audit event", "request_id", event.RequestID)
	}
}

func (s *Service) Close() error {
	var err error
	s.once.Do(func() {
		close(s.stop)
		s.wg.Wait()
		err = s.db.Close()
	})
	return err
}

func (s *Service) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(defaultFlushInterval)
	defer ticker.Stop()

	retentionTicker := time.NewTicker(30 * time.Minute)
	defer retentionTicker.Stop()

	batch := make([]envelope, 0, 256)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.flushBatch(batch); err != nil {
			s.logger.Error("flush telemetry batch failed", "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-s.stop:
			drain := true
			for drain {
				select {
				case item := <-s.queue:
					batch = append(batch, item)
				default:
					drain = false
				}
			}
			flush()
			return
		case item := <-s.queue:
			batch = append(batch, item)
			if len(batch) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-retentionTicker.C:
			if err := s.prune(context.Background()); err != nil {
				s.logger.Error("telemetry retention prune failed", "error", err)
			}
		}
	}
}

func (s *Service) flushBatch(batch []envelope) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	requestStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO request_logs (
			ts_unix, request_id, site_id, site_name, rule_id, is_internal,
			method, scheme, host, path, raw_query, remote_ip, user_agent, referer,
			cache_state, cache_status, status_code, response_bytes, total_duration_ms,
			origin_requests, origin_status_code, origin_duration_ms, error_kind, upstream_host, content_type
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer requestStmt.Close()

	auditStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO audit_logs (
			ts_unix, request_id, remote_ip, method, path, action, resource_type, resource_id, summary, status_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer auditStmt.Close()

	rollupStmt, err := tx.PrepareContext(ctx, buildRollupUpsertSQL())
	if err != nil {
		return err
	}
	defer rollupStmt.Close()

	for _, item := range batch {
		if item.request != nil {
			event := item.request
			if _, err = requestStmt.ExecContext(ctx,
				event.Timestamp.UTC().Unix(),
				event.RequestID,
				event.SiteID,
				event.SiteName,
				event.RuleID,
				boolToInt(event.IsInternal),
				event.Method,
				event.Scheme,
				event.Host,
				event.Path,
				event.RawQuery,
				event.RemoteIP,
				event.UserAgent,
				event.Referer,
				event.CacheState,
				event.CacheStatus,
				event.StatusCode,
				event.ResponseBytes,
				event.TotalDurationMS,
				event.OriginRequests,
				event.OriginStatusCode,
				event.OriginDurationMS,
				event.ErrorKind,
				event.UpstreamHost,
				event.ContentType,
			); err != nil {
				return err
			}
			if err = s.upsertRollup(ctx, rollupStmt, *event); err != nil {
				return err
			}
		}
		if item.audit != nil {
			event := item.audit
			if _, err = auditStmt.ExecContext(ctx,
				event.Timestamp.UTC().Unix(),
				event.RequestID,
				event.RemoteIP,
				event.Method,
				event.Path,
				event.Action,
				event.ResourceType,
				event.ResourceID,
				event.Summary,
				event.StatusCode,
			); err != nil {
				return err
			}
		}
	}

	err = tx.Commit()
	return err
}

func (s *Service) upsertRollup(ctx context.Context, stmt *sql.Stmt, event observe.RequestEvent) error {
	bucket := event.Timestamp.UTC().Truncate(time.Minute).Unix()
	method := strings.ToUpper(event.Method)
	statusClass := statusClass(event.StatusCode)
	cacheState := strings.ToUpper(event.CacheState)
	siteID := event.SiteID
	if siteID == "" {
		siteID = "_unmatched"
	}

	edgeCounts := histogramCounts(event.TotalDurationMS)
	originCounts := histogramCounts(event.OriginDurationMS)

	_, err := stmt.ExecContext(ctx,
		bucket,
		siteID,
		boolToInt(!event.IsInternal),
		intForState(!event.IsInternal && cacheState == "HIT"),
		intForState(!event.IsInternal && cacheState == "STALE"),
		intForState(!event.IsInternal && cacheState == "MISS"),
		intForState(!event.IsInternal && cacheState == "BYPASS"),
		intForState(!event.IsInternal && cacheState == "ERROR"),
		int64If(!event.IsInternal, event.ResponseBytes),
		int64If(!event.IsInternal && (cacheState == "HIT" || cacheState == "STALE"), event.ResponseBytes),
		event.OriginRequests,
		int64(event.OriginRequests)*event.ResponseBytes,
		intForState(!event.IsInternal && statusClass == "2xx"),
		intForState(!event.IsInternal && statusClass == "3xx"),
		intForState(!event.IsInternal && statusClass == "4xx"),
		intForState(!event.IsInternal && statusClass == "5xx"),
		int64If(!event.IsInternal, event.TotalDurationMS),
		boolToInt(!event.IsInternal),
		int64(event.OriginRequests)*event.OriginDurationMS,
		event.OriginRequests,
		edgeCounts[0], edgeCounts[1], edgeCounts[2], edgeCounts[3], edgeCounts[4], edgeCounts[5], edgeCounts[6], edgeCounts[7], edgeCounts[8],
		originCounts[0], originCounts[1], originCounts[2], originCounts[3], originCounts[4], originCounts[5], originCounts[6], originCounts[7], originCounts[8],
		methodCount(method, httpMethodKeyGet),
		methodCount(method, httpMethodKeyHead),
		methodCount(method, httpMethodKeyPost),
		methodCount(method, httpMethodKeyPut),
		methodCount(method, httpMethodKeyDelete),
		methodCount(method, httpMethodKeyPatch),
		methodCount(method, httpMethodKeyOptions),
		methodCount(method, httpMethodKeyOther),
	)
	return err
}

func (s *Service) prune(ctx context.Context) error {
	requestCutoff := time.Now().Add(-defaultRequestLogRetention).Unix()
	rollupCutoff := time.Now().Add(-defaultRollupRetention).Unix()
	auditCutoff := time.Now().Add(-defaultAuditLogRetention).Unix()

	for _, statement := range []struct {
		query  string
		cutoff int64
	}{
		{query: `DELETE FROM request_logs WHERE ts_unix < ?`, cutoff: requestCutoff},
		{query: `DELETE FROM metric_rollups_minute WHERE bucket_unix < ?`, cutoff: rollupCutoff},
		{query: `DELETE FROM audit_logs WHERE ts_unix < ?`, cutoff: auditCutoff},
	} {
		if _, err := s.db.ExecContext(ctx, statement.query, statement.cutoff); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) initSchema(ctx context.Context) error {
	for _, statement := range schemaStatements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA temp_store=MEMORY`,
	} {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	return nil
}

func ParsePeriod(raw string) (time.Duration, error) {
	switch strings.TrimSpace(raw) {
	case "", "24h":
		return 24 * time.Hour, nil
	case "1h":
		return time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	case "90d":
		return 90 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported period %q", raw)
	}
}

func ParseCursor(raw string) (int64, int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, 0, nil
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid cursor")
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, errors.New("invalid cursor timestamp")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, errors.New("invalid cursor id")
	}
	return ts, id, nil
}

func BuildCursor(ts int64, id int64) string {
	if ts == 0 || id == 0 {
		return ""
	}
	return strconv.FormatInt(ts, 10) + ":" + strconv.FormatInt(id, 10)
}

func clampLimit(limit int, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > 200 {
		return 200
	}
	return limit
}
