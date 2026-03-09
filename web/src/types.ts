export type CacheMode = "follow_origin" | "bypass" | "force_cache" | "override_origin";
export type UpstreamHostMode = "follow_origin" | "follow_request" | "custom";

export type MatchLogical = "and" | "or";

export type MatchField = "hostname" | "uri_path" | "method" | "request_header";

export type MatchOperator =
  | "equals"
  | "not_equals"
  | "contains"
  | "not_contains"
  | "starts_with"
  | "not_starts_with"
  | "matches_glob"
  | "not_matches_glob"
  | "exists"
  | "not_exists";

export interface MatchClause {
  logical?: MatchLogical;
  field: MatchField;
  operator: MatchOperator;
  name?: string;
  value?: string;
}

export interface MatchSpec {
  clauses?: MatchClause[];
  any?: boolean;
}

export interface CacheAction {
  mode: CacheMode;
  ttl?: string;
  stale_if_error?: string;
  optimistic?: boolean;
}

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  system?: boolean;
  match: MatchSpec;
  action: {
    cache: CacheAction;
  };
}

export interface Site {
  id: string;
  name: string;
  enabled: boolean;
  hosts: string[];
  upstream: {
    url: string;
    host_mode?: UpstreamHostMode;
    host?: string;
  };
  rules: Rule[];
}

export interface SiteInput {
  id?: string;
  name: string;
  enabled: boolean;
  hosts: string[];
  upstream_url: string;
  upstream_host_mode: UpstreamHostMode;
  upstream_host: string;
}

export interface ReorderPayload {
  rule_ids: string[];
}

export interface PurgeCachePayload {
  all?: boolean;
  urls?: string[];
}

export interface PurgeCacheResult {
  purged: number;
  scope: "site" | "url";
  urls?: string[];
}

export interface AuthIdentity {
  username: string;
  role: string;
}

export interface LoginResponse {
  token: string;
  user: AuthIdentity;
  expires_at: string;
  token_ttl: number;
}

export type AnalyticsPeriod = "1h" | "24h" | "7d" | "30d" | "90d";

export interface AnalyticsSummary {
  requests: number;
  edge_bytes: number;
  cached_bytes: number;
  origin_bytes: number;
  origin_requests: number;
  hit_requests: number;
  stale_requests: number;
  miss_requests: number;
  bypass_requests: number;
  error_requests: number;
  cache_hit_ratio: number;
  cache_bandwidth_ratio: number;
  error_rate: number;
  average_edge_duration_ms: number;
  p95_edge_duration_ms: number;
  average_origin_duration_ms: number;
  p95_origin_duration_ms: number;
  active_sites: number;
}

export interface AnalyticsSeriesPoint {
  bucket: string;
  requests: number;
  hit_requests: number;
  stale_requests: number;
  miss_requests: number;
  bypass_requests: number;
  error_requests: number;
  edge_bytes: number;
  cached_bytes: number;
  origin_requests: number;
  origin_bytes: number;
}

export interface CountBreakdown {
  key: string;
  label: string;
  requests: number;
  edge_bytes?: number;
  hit_ratio?: number;
  site_id?: string;
  site_name?: string;
}

export interface PathBreakdown {
  path: string;
  requests: number;
  edge_bytes: number;
  hit_ratio: number;
}

export interface SiteBreakdown {
  site_id: string;
  site_name: string;
  requests: number;
  edge_bytes: number;
  hit_ratio: number;
}

export interface CacheInventory {
  objects: number;
  bytes: number;
}

export interface AnalyticsReport {
  generated_at: string;
  period: string;
  summary: AnalyticsSummary;
  series: AnalyticsSeriesPoint[];
  cache_states: CountBreakdown[];
  methods: CountBreakdown[];
  status_classes: CountBreakdown[];
  top_paths: PathBreakdown[];
  top_hosts: CountBreakdown[];
  top_sites: SiteBreakdown[];
  top_rules: CountBreakdown[];
  cache_inventory: CacheInventory;
}

export interface RequestLogItem {
  id: number;
  timestamp: string;
  request_id: string;
  site_id: string;
  site_name: string;
  rule_id: string;
  is_internal: boolean;
  method: string;
  scheme: string;
  host: string;
  path: string;
  raw_query: string;
  remote_ip: string;
  user_agent: string;
  referer: string;
  cache_state: string;
  cache_status: string;
  status_code: number;
  response_bytes: number;
  total_duration_ms: number;
  origin_requests: number;
  origin_status_code: number;
  origin_duration_ms: number;
  error_kind: string;
  upstream_host: string;
  content_type: string;
}

export interface RequestLogPage {
  items: RequestLogItem[];
  next_cursor?: string;
}

export interface AuditLogItem {
  id: number;
  timestamp: string;
  request_id: string;
  remote_ip: string;
  method: string;
  path: string;
  action: string;
  resource_type: string;
  resource_id: string;
  summary: string;
  status_code: number;
}

export interface AuditLogPage {
  items: AuditLogItem[];
  next_cursor?: string;
}
