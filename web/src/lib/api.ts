import type { ReorderPayload, Rule, Site, SiteInput } from "@/types";

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
