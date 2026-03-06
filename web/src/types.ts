export type CacheMode = "follow_origin" | "bypass" | "force_cache" | "override_origin";

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
  cache: {
    optimistic_refresh: boolean;
  };
  hosts: string[];
  upstream: {
    url: string;
  };
  rules: Rule[];
}

export interface SiteInput {
  id?: string;
  name: string;
  enabled: boolean;
  optimistic_refresh: boolean;
  hosts: string[];
  upstream_url: string;
}

export interface ReorderPayload {
  rule_ids: string[];
}
