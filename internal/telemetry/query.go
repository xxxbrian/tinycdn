package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *Service) Report(ctx context.Context, params ReportParams) (Report, error) {
	period := params.Period
	if period <= 0 {
		period = 24 * time.Hour
	}
	since := time.Now().Add(-period).Unix()
	report := Report{
		GeneratedAt:   time.Now().UTC(),
		Period:        period.String(),
		Series:        []SeriesPoint{},
		CacheStates:   []CountBreakdown{},
		Methods:       []CountBreakdown{},
		StatusClasses: []CountBreakdown{},
		TopPaths:      []PathBreakdown{},
		TopHosts:      []CountBreakdown{},
		TopSites:      []SiteBreakdown{},
		TopRules:      []CountBreakdown{},
	}

	filter := "bucket_unix >= ?"
	args := []any{since}
	if params.SiteID != "" {
		filter += " AND site_id = ?"
		args = append(args, params.SiteID)
	} else {
		filter += " AND site_id != '_unmatched'"
	}

	summaryRow := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(requests), 0),
			COALESCE(SUM(edge_bytes), 0),
			COALESCE(SUM(cached_bytes), 0),
			COALESCE(SUM(origin_bytes), 0),
			COALESCE(SUM(origin_requests), 0),
			COALESCE(SUM(hit_requests), 0),
			COALESCE(SUM(stale_requests), 0),
			COALESCE(SUM(miss_requests), 0),
			COALESCE(SUM(bypass_requests), 0),
			COALESCE(SUM(error_requests), 0),
			COALESCE(SUM(total_duration_ms), 0),
			COALESCE(SUM(total_duration_count), 0),
			COALESCE(SUM(origin_duration_ms), 0),
			COALESCE(SUM(origin_duration_count), 0),
			COALESCE(SUM(edge_le_10), 0),
			COALESCE(SUM(edge_le_25), 0),
			COALESCE(SUM(edge_le_50), 0),
			COALESCE(SUM(edge_le_100), 0),
			COALESCE(SUM(edge_le_250), 0),
			COALESCE(SUM(edge_le_500), 0),
			COALESCE(SUM(edge_le_1000), 0),
			COALESCE(SUM(edge_le_2500), 0),
			COALESCE(SUM(edge_gt_2500), 0),
			COALESCE(SUM(origin_le_10), 0),
			COALESCE(SUM(origin_le_25), 0),
			COALESCE(SUM(origin_le_50), 0),
			COALESCE(SUM(origin_le_100), 0),
			COALESCE(SUM(origin_le_250), 0),
			COALESCE(SUM(origin_le_500), 0),
			COALESCE(SUM(origin_le_1000), 0),
			COALESCE(SUM(origin_le_2500), 0),
			COALESCE(SUM(origin_gt_2500), 0)
		FROM metric_rollups_minute
		WHERE `+filter, args...)

	var (
		summary             ReportSummary
		edgeCounts          [9]int64
		originCounts        [9]int64
		totalDurationMS     int64
		totalDurationCount  int64
		originDurationMS    int64
		originDurationCount int64
	)
	if err := summaryRow.Scan(
		&summary.Requests,
		&summary.EdgeBytes,
		&summary.CachedBytes,
		&summary.OriginBytes,
		&summary.OriginRequests,
		&summary.HitRequests,
		&summary.StaleRequests,
		&summary.MissRequests,
		&summary.BypassRequests,
		&summary.ErrorRequests,
		&totalDurationMS,
		&totalDurationCount,
		&originDurationMS,
		&originDurationCount,
		&edgeCounts[0], &edgeCounts[1], &edgeCounts[2], &edgeCounts[3], &edgeCounts[4], &edgeCounts[5], &edgeCounts[6], &edgeCounts[7], &edgeCounts[8],
		&originCounts[0], &originCounts[1], &originCounts[2], &originCounts[3], &originCounts[4], &originCounts[5], &originCounts[6], &originCounts[7], &originCounts[8],
	); err != nil {
		return Report{}, err
	}

	if summary.Requests > 0 {
		summary.CacheHitRatio = float64(summary.HitRequests+summary.StaleRequests) / float64(summary.Requests)
		summary.ErrorRate = float64(summary.ErrorRequests) / float64(summary.Requests)
	}
	if summary.EdgeBytes > 0 {
		summary.CacheBandwidthRatio = float64(summary.CachedBytes) / float64(summary.EdgeBytes)
	}
	if totalDurationCount > 0 {
		summary.AverageEdgeDurationMS = float64(totalDurationMS) / float64(totalDurationCount)
		summary.P95EdgeDurationMS = estimatePercentile(edgeCounts[:], 0.95)
	}
	if originDurationCount > 0 {
		summary.AverageOriginDurationMS = float64(originDurationMS) / float64(originDurationCount)
		summary.P95OriginDurationMS = estimatePercentile(originCounts[:], 0.95)
	}

	activeFilter := "ts_unix >= ? AND is_internal = 0 AND site_id != ''"
	activeArgs := []any{since}
	if params.SiteID != "" {
		activeFilter += " AND site_id = ?"
		activeArgs = append(activeArgs, params.SiteID)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT site_id) FROM request_logs WHERE `+activeFilter, activeArgs...).Scan(&summary.ActiveSites); err != nil {
		return Report{}, err
	}
	report.Summary = summary

	step := seriesStep(period)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			(bucket_unix / ?) * ? AS grouped_bucket,
			COALESCE(SUM(requests), 0),
			COALESCE(SUM(hit_requests), 0),
			COALESCE(SUM(stale_requests), 0),
			COALESCE(SUM(miss_requests), 0),
			COALESCE(SUM(bypass_requests), 0),
			COALESCE(SUM(error_requests), 0),
			COALESCE(SUM(edge_bytes), 0),
			COALESCE(SUM(cached_bytes), 0),
			COALESCE(SUM(origin_requests), 0),
			COALESCE(SUM(origin_bytes), 0)
		FROM metric_rollups_minute
		WHERE `+filter+`
		GROUP BY grouped_bucket
		ORDER BY grouped_bucket ASC
	`, append([]any{step, step}, args...)...)
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var point SeriesPoint
		var bucket int64
		if err := rows.Scan(
			&bucket,
			&point.Requests,
			&point.HitRequests,
			&point.StaleRequests,
			&point.MissRequests,
			&point.BypassRequests,
			&point.ErrorRequests,
			&point.EdgeBytes,
			&point.CachedBytes,
			&point.OriginRequests,
			&point.OriginBytes,
		); err != nil {
			return Report{}, err
		}
		point.Bucket = time.Unix(bucket, 0).UTC()
		report.Series = append(report.Series, point)
	}
	if err := rows.Err(); err != nil {
		return Report{}, err
	}

	report.CacheStates, err = s.queryCountBreakdowns(ctx, params, `cache_state`, `cache_state`)
	if err != nil {
		return Report{}, err
	}
	report.Methods, err = s.queryCountBreakdowns(ctx, params, `method`, `method`)
	if err != nil {
		return Report{}, err
	}
	report.StatusClasses, err = s.queryStatusBreakdowns(ctx, params)
	if err != nil {
		return Report{}, err
	}
	report.TopPaths, err = s.queryPathBreakdowns(ctx, params)
	if err != nil {
		return Report{}, err
	}
	report.TopHosts, err = s.queryCountBreakdowns(ctx, params, `host`, `host`)
	if err != nil {
		return Report{}, err
	}
	if params.SiteID == "" {
		report.TopSites, err = s.querySiteBreakdowns(ctx, params)
		if err != nil {
			return Report{}, err
		}
	}
	report.TopRules, err = s.queryCountBreakdowns(ctx, params, `rule_id`, `rule_id`)
	if err != nil {
		return Report{}, err
	}

	return report, nil
}

func (s *Service) RequestLogs(ctx context.Context, query RequestLogQuery) (RequestLogPage, error) {
	period := query.Period
	if period <= 0 {
		period = 24 * time.Hour
	}
	limit := clampLimit(query.Limit, 50)
	since := time.Now().Add(-period).Unix()

	clauses := []string{"ts_unix >= ?"}
	args := []any{since}
	if query.SiteID != "" {
		clauses = append(clauses, "site_id = ?")
		args = append(args, query.SiteID)
	}
	if !query.IncludeInternal {
		clauses = append(clauses, "is_internal = 0")
	}
	if query.Method != "" {
		clauses = append(clauses, "method = ?")
		args = append(args, strings.ToUpper(query.Method))
	}
	if query.CacheState != "" {
		clauses = append(clauses, "cache_state = ?")
		args = append(args, strings.ToUpper(query.CacheState))
	}
	if query.StatusClass != "" {
		switch query.StatusClass {
		case "2xx":
			clauses = append(clauses, "status_code BETWEEN 200 AND 299")
		case "3xx":
			clauses = append(clauses, "status_code BETWEEN 300 AND 399")
		case "4xx":
			clauses = append(clauses, "status_code BETWEEN 400 AND 499")
		case "5xx":
			clauses = append(clauses, "status_code >= 500")
		}
	}
	if query.PathPrefix != "" {
		clauses = append(clauses, "path LIKE ?")
		args = append(args, query.PathPrefix+"%")
	}
	if strings.TrimSpace(query.Search) != "" {
		clauses = append(clauses, "(request_id LIKE ? OR host LIKE ? OR path LIKE ? OR raw_query LIKE ? OR remote_ip LIKE ? OR user_agent LIKE ?)")
		pattern := likePattern(query.Search)
		for range 6 {
			args = append(args, pattern)
		}
	}
	if query.Cursor != "" {
		ts, id, err := ParseCursor(query.Cursor)
		if err != nil {
			return RequestLogPage{}, err
		}
		clauses = append(clauses, "(ts_unix < ? OR (ts_unix = ? AND id < ?))")
		args = append(args, ts, ts, id)
	}

	statement := `
		SELECT
			id, ts_unix, request_id, site_id, site_name, rule_id, is_internal,
			method, scheme, host, path, raw_query, remote_ip, user_agent, referer,
			cache_state, cache_status, status_code, response_bytes, total_duration_ms,
			origin_requests, origin_status_code, origin_duration_ms, error_kind, upstream_host, content_type
		FROM request_logs
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY ts_unix DESC, id DESC
		LIMIT ?
	`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return RequestLogPage{}, err
	}
	defer rows.Close()

	page := RequestLogPage{Items: make([]RequestLogItem, 0, limit)}
	for rows.Next() {
		var (
			item     RequestLogItem
			tsUnix   int64
			internal int
		)
		if err := rows.Scan(
			&item.ID,
			&tsUnix,
			&item.RequestID,
			&item.SiteID,
			&item.SiteName,
			&item.RuleID,
			&internal,
			&item.Method,
			&item.Scheme,
			&item.Host,
			&item.Path,
			&item.RawQuery,
			&item.RemoteIP,
			&item.UserAgent,
			&item.Referer,
			&item.CacheState,
			&item.CacheStatus,
			&item.StatusCode,
			&item.ResponseBytes,
			&item.TotalDurationMS,
			&item.OriginRequests,
			&item.OriginStatusCode,
			&item.OriginDurationMS,
			&item.ErrorKind,
			&item.UpstreamHost,
			&item.ContentType,
		); err != nil {
			return RequestLogPage{}, err
		}
		item.IsInternal = internal == 1
		item.Timestamp = time.Unix(tsUnix, 0).UTC()
		if len(page.Items) == limit {
			page.NextCursor = BuildCursor(tsUnix, item.ID)
			break
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return RequestLogPage{}, err
	}
	return page, nil
}

func (s *Service) AuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogPage, error) {
	period := query.Period
	if period <= 0 {
		period = 7 * 24 * time.Hour
	}
	limit := clampLimit(query.Limit, 30)
	since := time.Now().Add(-period).Unix()

	clauses := []string{"ts_unix >= ?"}
	args := []any{since}
	if query.SiteID != "" {
		clauses = append(clauses, "path LIKE ?")
		args = append(args, fmt.Sprintf("/api/sites/%s%%", query.SiteID))
	}
	if query.Cursor != "" {
		ts, id, err := ParseCursor(query.Cursor)
		if err != nil {
			return AuditLogPage{}, err
		}
		clauses = append(clauses, "(ts_unix < ? OR (ts_unix = ? AND id < ?))")
		args = append(args, ts, ts, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ts_unix, request_id, remote_ip, method, path, action, resource_type, resource_id, summary, status_code
		FROM audit_logs
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY ts_unix DESC, id DESC
		LIMIT ?
	`, append(args, limit+1)...)
	if err != nil {
		return AuditLogPage{}, err
	}
	defer rows.Close()

	page := AuditLogPage{Items: make([]AuditLogItem, 0, limit)}
	for rows.Next() {
		var item AuditLogItem
		var tsUnix int64
		if err := rows.Scan(
			&item.ID,
			&tsUnix,
			&item.RequestID,
			&item.RemoteIP,
			&item.Method,
			&item.Path,
			&item.Action,
			&item.ResourceType,
			&item.ResourceID,
			&item.Summary,
			&item.StatusCode,
		); err != nil {
			return AuditLogPage{}, err
		}
		item.Timestamp = time.Unix(tsUnix, 0).UTC()
		if len(page.Items) == limit {
			page.NextCursor = BuildCursor(tsUnix, item.ID)
			break
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AuditLogPage{}, err
	}
	return page, nil
}

func (s *Service) queryCountBreakdowns(ctx context.Context, params ReportParams, selectExpr string, labelExpr string) ([]CountBreakdown, error) {
	rows, err := s.runRequestBreakdownQuery(ctx, params, fmt.Sprintf(`
		SELECT %s, %s, COUNT(*), COALESCE(SUM(response_bytes), 0), COALESCE(AVG(CASE WHEN cache_state IN ('HIT', 'STALE') THEN 1.0 ELSE 0.0 END), 0)
		FROM request_logs
		WHERE %%s
		GROUP BY %s, %s
		HAVING %s != ''
		ORDER BY COUNT(*) DESC, COALESCE(SUM(response_bytes), 0) DESC
		LIMIT 8
	`, selectExpr, labelExpr, selectExpr, labelExpr, selectExpr))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	breakdowns := make([]CountBreakdown, 0, 8)
	for rows.Next() {
		var item CountBreakdown
		if err := rows.Scan(&item.Key, &item.Label, &item.Requests, &item.EdgeBytes, &item.HitRatio); err != nil {
			return nil, err
		}
		breakdowns = append(breakdowns, item)
	}
	return breakdowns, rows.Err()
}

func (s *Service) queryStatusBreakdowns(ctx context.Context, params ReportParams) ([]CountBreakdown, error) {
	rows, err := s.runRequestBreakdownQuery(ctx, params, `
		SELECT
			CASE
				WHEN status_code >= 500 THEN '5xx'
				WHEN status_code >= 400 THEN '4xx'
				WHEN status_code >= 300 THEN '3xx'
				ELSE '2xx'
			END AS key,
			CASE
				WHEN status_code >= 500 THEN '5xx'
				WHEN status_code >= 400 THEN '4xx'
				WHEN status_code >= 300 THEN '3xx'
				ELSE '2xx'
			END AS label,
			COUNT(*),
			COALESCE(SUM(response_bytes), 0),
			COALESCE(AVG(CASE WHEN cache_state IN ('HIT', 'STALE') THEN 1.0 ELSE 0.0 END), 0)
		FROM request_logs
		WHERE %s
		GROUP BY key, label
		ORDER BY key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CountBreakdown, 0, 4)
	for rows.Next() {
		var item CountBreakdown
		if err := rows.Scan(&item.Key, &item.Label, &item.Requests, &item.EdgeBytes, &item.HitRatio); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) queryPathBreakdowns(ctx context.Context, params ReportParams) ([]PathBreakdown, error) {
	rows, err := s.runRequestBreakdownQuery(ctx, params, `
		SELECT
			path,
			COUNT(*),
			COALESCE(SUM(response_bytes), 0),
			COALESCE(AVG(CASE WHEN cache_state IN ('HIT', 'STALE') THEN 1.0 ELSE 0.0 END), 0)
		FROM request_logs
		WHERE %s
		GROUP BY path
		ORDER BY COUNT(*) DESC, COALESCE(SUM(response_bytes), 0) DESC
		LIMIT 8
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PathBreakdown, 0, 8)
	for rows.Next() {
		var item PathBreakdown
		if err := rows.Scan(&item.Path, &item.Requests, &item.EdgeBytes, &item.HitRatio); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) querySiteBreakdowns(ctx context.Context, params ReportParams) ([]SiteBreakdown, error) {
	rows, err := s.runRequestBreakdownQuery(ctx, params, `
		SELECT
			site_id,
			MAX(site_name),
			COUNT(*),
			COALESCE(SUM(response_bytes), 0),
			COALESCE(AVG(CASE WHEN cache_state IN ('HIT', 'STALE') THEN 1.0 ELSE 0.0 END), 0)
		FROM request_logs
		WHERE %s AND site_id != ''
		GROUP BY site_id
		ORDER BY COUNT(*) DESC, COALESCE(SUM(response_bytes), 0) DESC
		LIMIT 8
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]SiteBreakdown, 0, 8)
	for rows.Next() {
		var item SiteBreakdown
		if err := rows.Scan(&item.SiteID, &item.SiteName, &item.Requests, &item.EdgeBytes, &item.HitRatio); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) runRequestBreakdownQuery(ctx context.Context, params ReportParams, statement string) (*sql.Rows, error) {
	period := params.Period
	if period <= 0 {
		period = 24 * time.Hour
	}
	since := time.Now().Add(-period).Unix()
	clauses := []string{"ts_unix >= ?", "is_internal = 0"}
	args := []any{since}
	if params.SiteID != "" {
		clauses = append(clauses, "site_id = ?")
		args = append(args, params.SiteID)
	} else {
		clauses = append(clauses, "site_id != ''")
	}
	return s.db.QueryContext(ctx, fmt.Sprintf(statement, strings.Join(clauses, " AND ")), args...)
}

func seriesStep(period time.Duration) int64 {
	switch {
	case period <= 24*time.Hour:
		return 60
	case period <= 7*24*time.Hour:
		return int64(15 * time.Minute / time.Second)
	case period <= 30*24*time.Hour:
		return int64(time.Hour / time.Second)
	default:
		return int64(6 * time.Hour / time.Second)
	}
}
