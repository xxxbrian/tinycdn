import type {
  AnalyticsPeriod,
  AnalyticsReport,
  AuditLogPage,
  PurgeCachePayload,
  PurgeCacheResult,
  ReorderPayload,
  RequestLogPage,
  Rule,
  Site,
  SiteInput,
} from "@/types";

async function request<T>(input: string, init?: RequestInit): Promise<T> {
  const response = await fetch(input, {
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
    ...init,
  });

  if (!response.ok) {
    const contentType = response.headers.get("content-type") ?? "";
    if (contentType.includes("application/json")) {
      const payload = (await response.json().catch(() => null)) as { error?: string } | null;
      throw new Error(payload?.error ?? `Request failed: ${response.status}`);
    }

    const body = await response.text().catch(() => "");
    throw new Error(
      `Request failed: ${response.status}. Expected JSON but received ${contentType || "unknown content"}${body.includes("<!doctype") ? " (HTML response)" : ""}`,
    );
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    const body = await response.text().catch(() => "");
    throw new Error(
      `Expected JSON response from ${input.toString()}, received ${contentType || "unknown content"}${body.includes("<!doctype") ? " (HTML response)" : ""}`,
    );
  }

  return (await response.json()) as T;
}

export const api = {
  listSites: () => request<Site[]>("/api/sites"),
  getSite: (siteId: string) => request<Site>(`/api/sites/${siteId}`),
  createSite: (payload: SiteInput) =>
    request<Site>("/api/sites", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateSite: (siteId: string, payload: SiteInput) =>
    request<Site>(`/api/sites/${siteId}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    }),
  deleteSite: (siteId: string) => request<void>(`/api/sites/${siteId}`, { method: "DELETE" }),
  createRule: (siteId: string, payload: Rule) =>
    request<Rule>(`/api/sites/${siteId}/rules`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateRule: (siteId: string, ruleId: string, payload: Rule) =>
    request<Rule>(`/api/sites/${siteId}/rules/${ruleId}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    }),
  deleteRule: (siteId: string, ruleId: string) =>
    request<void>(`/api/sites/${siteId}/rules/${ruleId}`, {
      method: "DELETE",
    }),
  reorderRules: (siteId: string, payload: ReorderPayload) =>
    request<Rule[]>(`/api/sites/${siteId}/rules/reorder`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  purgeCache: (siteId: string, payload: PurgeCachePayload) =>
    request<PurgeCacheResult>(`/api/sites/${siteId}/cache/purge`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  analyticsReport: (period: AnalyticsPeriod, siteId?: string) =>
    request<AnalyticsReport>(
      withQuery(siteId ? `/api/sites/${siteId}/analytics/report` : "/api/analytics/report", {
        period,
      }),
    ),
  requestLogs: (params: {
    period: AnalyticsPeriod;
    siteId?: string;
    limit?: number;
    cursor?: string;
    method?: string;
    cacheState?: string;
    statusClass?: string;
    pathPrefix?: string;
    search?: string;
    includeInternal?: boolean;
  }) =>
    request<RequestLogPage>(
      withQuery(
        params.siteId ? `/api/sites/${params.siteId}/logs/requests` : "/api/logs/requests",
        {
          period: params.period,
          limit: params.limit,
          cursor: params.cursor,
          method: params.method,
          cache_state: params.cacheState,
          status_class: params.statusClass,
          path_prefix: params.pathPrefix,
          search: params.search,
          include_internal: params.includeInternal ? "true" : undefined,
        },
      ),
    ),
  auditLogs: (params: {
    period: AnalyticsPeriod;
    siteId?: string;
    limit?: number;
    cursor?: string;
  }) =>
    request<AuditLogPage>(
      withQuery(params.siteId ? `/api/sites/${params.siteId}/logs/audit` : "/api/logs/audit", {
        period: params.period,
        limit: params.limit,
        cursor: params.cursor,
      }),
    ),
};

export function toSiteInput(site: Site, overrides: Partial<SiteInput> = {}): SiteInput {
  return {
    id: site.id,
    name: site.name,
    enabled: site.enabled,
    hosts: [...site.hosts],
    upstream_url: site.upstream.url,
    upstream_host_mode: site.upstream.host_mode ?? "follow_origin",
    upstream_host: site.upstream.host ?? "",
    ...overrides,
  };
}

function withQuery(path: string, params: Record<string, string | number | undefined>) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") {
      continue;
    }
    query.set(key, String(value));
  }
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}
