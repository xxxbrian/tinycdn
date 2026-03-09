import { createFileRoute } from "@tanstack/react-router";
import { startTransition, useDeferredValue, useEffect, useState } from "react";

import type { AnalyticsPeriod, AuditLogPage, RequestLogPage } from "@/types";
import { api } from "@/lib/api";
import { requireAuth, withProtectedLoader } from "@/lib/auth";
import { analyticsPeriods } from "@/lib/telemetry";
import { AuditLogList } from "@/components/audit-log-list";
import { PageHeader } from "@/components/page-header";
import { RequestLogsTable } from "@/components/request-logs-table";
import { SiteShell } from "@/components/site-shell";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export const Route = createFileRoute("/sites/$siteId/logs")({
  beforeLoad: ({ location }) => requireAuth(location),
  loader: ({ location, params }) =>
    withProtectedLoader(location, async () => {
      const [site, requests, audit] = await Promise.all([
        api.getSite(params.siteId),
        api.requestLogs({ period: "24h", siteId: params.siteId, limit: 20 }),
        api.auditLogs({ period: "7d", siteId: params.siteId, limit: 12 }),
      ]);
      return { site, requests, audit };
    }),
  component: SiteLogsPage,
});

function SiteLogsPage() {
  const initial = Route.useLoaderData();
  const [period, setPeriod] = useState<AnalyticsPeriod>("24h");
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const [method, setMethod] = useState("all");
  const [cacheState, setCacheState] = useState("all");
  const [statusClass, setStatusClass] = useState("all");
  const [requests, setRequests] = useState<RequestLogPage>(initial.requests);
  const [audit, setAudit] = useState<AuditLogPage>(initial.audit);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      api.requestLogs({
        period,
        siteId: initial.site.id,
        limit: 20,
        search: deferredSearch.trim() || undefined,
        method: method === "all" ? undefined : method,
        cacheState: cacheState === "all" ? undefined : cacheState,
        statusClass: statusClass === "all" ? undefined : statusClass,
      }),
      api.auditLogs({
        period: period === "1h" ? "24h" : period,
        siteId: initial.site.id,
        limit: 12,
      }),
    ]).then(([nextRequests, nextAudit]) => {
      if (cancelled) {
        return;
      }
      startTransition(() => {
        setRequests(nextRequests);
        setAudit(nextAudit);
      });
    });
    return () => {
      cancelled = true;
    };
  }, [cacheState, deferredSearch, initial.site.id, method, period, statusClass]);

  return (
    <SiteShell site={initial.site} section="Logs">
      <PageHeader
        eyebrow="Site workspace"
        title={`${initial.site.name} logs`}
        description="Filter recent requests and review configuration actions that touched this site."
      />

      <div className="grid gap-2 px-4 lg:px-6">
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Search request ID, path, host, IP, or user agent"
        />
        <div className="flex flex-wrap gap-2">
          <Select value={period} onValueChange={(value) => setPeriod(value as AnalyticsPeriod)}>
            <SelectTrigger className="w-[152px]">
              <SelectValue placeholder="Period" />
            </SelectTrigger>
            <SelectContent>
              {analyticsPeriods.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={method} onValueChange={setMethod}>
            <SelectTrigger className="w-[152px]">
              <SelectValue placeholder="Method" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All methods</SelectItem>
              <SelectItem value="GET">GET</SelectItem>
              <SelectItem value="HEAD">HEAD</SelectItem>
              <SelectItem value="POST">POST</SelectItem>
              <SelectItem value="PUT">PUT</SelectItem>
            </SelectContent>
          </Select>
          <Select value={cacheState} onValueChange={setCacheState}>
            <SelectTrigger className="w-[168px]">
              <SelectValue placeholder="Cache state" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All cache states</SelectItem>
              <SelectItem value="HIT">HIT</SelectItem>
              <SelectItem value="MISS">MISS</SelectItem>
              <SelectItem value="STALE">STALE</SelectItem>
              <SelectItem value="BYPASS">BYPASS</SelectItem>
              <SelectItem value="ERROR">ERROR</SelectItem>
            </SelectContent>
          </Select>
          <Select value={statusClass} onValueChange={setStatusClass}>
            <SelectTrigger className="w-[152px]">
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All statuses</SelectItem>
              <SelectItem value="2xx">2xx</SelectItem>
              <SelectItem value="3xx">3xx</SelectItem>
              <SelectItem value="4xx">4xx</SelectItem>
              <SelectItem value="5xx">5xx</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="grid gap-6 px-4 pb-6 2xl:grid-cols-[minmax(0,1.5fr)_20rem] lg:px-6">
        <RequestLogsTable
          title="Site request stream"
          description="Recent requests through this site."
          items={requests.items}
        />
        <AuditLogList
          title="Site audit events"
          description="Recent admin actions scoped to this site."
          items={audit.items}
        />
      </div>
    </SiteShell>
  );
}
