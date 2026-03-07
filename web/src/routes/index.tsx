import { Link, createFileRoute } from "@tanstack/react-router";
import { Globe, Logs, Workflow } from "lucide-react";
import { startTransition, useEffect, useState } from "react";

import type { AnalyticsPeriod, AnalyticsReport, AuditLogPage } from "@/types";
import { api } from "@/lib/api";
import {
  analyticsPeriods,
  formatBytes,
  formatCompactNumber,
  formatPercent,
  toTrafficSeries,
} from "@/lib/telemetry";
import { ChartAreaInteractive } from "@/components/chart-area-interactive";
import { AuditLogList } from "@/components/audit-log-list";
import { BreakdownCard } from "@/components/breakdown-card";
import { ConsoleShell } from "@/components/console-shell";
import { PageHeader } from "@/components/page-header";
import { TelemetrySummaryCards } from "@/components/telemetry-summary-cards";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export const Route = createFileRoute("/")({
  loader: async () => {
    const [sites, report, audit] = await Promise.all([
      api.listSites(),
      api.analyticsReport("24h"),
      api.auditLogs({ period: "7d", limit: 6 }),
    ]);
    return { sites, report, audit };
  },
  component: OverviewPage,
});

function OverviewPage() {
  const initial = Route.useLoaderData();
  const [period, setPeriod] = useState<AnalyticsPeriod>("24h");
  const [report, setReport] = useState<AnalyticsReport>(initial.report);
  const [audit] = useState<AuditLogPage>(initial.audit);

  useEffect(() => {
    let cancelled = false;
    void api.analyticsReport(period).then((next) => {
      if (cancelled) {
        return;
      }
      startTransition(() => setReport(next));
    });
    return () => {
      cancelled = true;
    };
  }, [period]);

  const topSitesByID = new Map((report.top_sites ?? []).map((site) => [site.site_id, site]));
  const requestShare = (requests: number) =>
    report.summary.requests > 0 ? requests / report.summary.requests : undefined;

  return (
    <ConsoleShell>
      <PageHeader
        eyebrow="Global overview"
        title="Edge control plane"
        description="Global traffic, cache efficiency, origin cost, and configuration activity in one high-density overview."
        actions={
          <>
            <div className="flex items-center gap-2">
              <Select value={period} onValueChange={(value) => setPeriod(value as AnalyticsPeriod)}>
                <SelectTrigger className="w-[140px]">
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
            </div>
            <Button variant="outline" asChild>
              <Link to="/logs">
                <Logs className="size-4" />
                View logs
              </Link>
            </Button>
            <Button asChild>
              <Link to="/sites">
                <Globe className="size-4" />
                Manage sites
              </Link>
            </Button>
          </>
        }
      />

      <TelemetrySummaryCards report={report} />

      <div className="px-4 lg:px-6">
        <ChartAreaInteractive
          title="Traffic and cache activity"
          description="Requests, cache-delivered responses, and origin fetch pressure over the selected period."
          points={toTrafficSeries(report)}
          period={period}
          onPeriodChange={setPeriod}
        />
      </div>

      <div className="grid gap-6 px-4 lg:grid-cols-[1.5fr_0.9fr] lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>Sites at a glance</CardTitle>
            <CardDescription>
              Rank current sites by real traffic while keeping direct access to the site workspace.
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Site</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Requests</TableHead>
                  <TableHead>Hit ratio</TableHead>
                  <TableHead>Bandwidth</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {initial.sites.slice(0, 8).map((site) => {
                  const telemetry = topSitesByID.get(site.id);
                  return (
                    <TableRow key={site.id}>
                      <TableCell>
                        <Link
                          to="/sites/$siteId"
                          params={{ siteId: site.id }}
                          className="font-medium text-foreground no-underline hover:text-primary"
                        >
                          {site.name}
                        </Link>
                        <div className="text-xs text-muted-foreground">{site.upstream.url}</div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={site.enabled ? "outline" : "secondary"}>
                          {site.enabled ? "Enabled" : "Disabled"}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {telemetry ? formatCompactNumber(telemetry.requests) : "0"}
                      </TableCell>
                      <TableCell>{telemetry ? formatPercent(telemetry.hit_ratio) : "0%"}</TableCell>
                      <TableCell>{telemetry ? formatBytes(telemetry.edge_bytes) : "0 B"}</TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <div className="grid gap-6">
          <BreakdownCard
            title="Cache state mix"
            description="How the edge is serving traffic right now."
            items={report.cache_states}
            emptyLabel="Traffic will appear here after the first requests land."
          />
          <AuditLogList
            title="Recent config activity"
            description="Admin-side changes, reorders, and purge actions."
            items={audit.items}
          />
        </div>
      </div>

      <div className="grid gap-6 px-4 pb-6 lg:grid-cols-3 lg:px-6">
        <BreakdownCard
          title="Top paths"
          description="The busiest cached paths over the current period."
          items={(report.top_paths ?? []).map((item) => ({
            key: item.path,
            label: item.path,
            requests: item.requests,
            edge_bytes: item.edge_bytes,
            hit_ratio: requestShare(item.requests),
          }))}
          emptyLabel="Path-level traffic will show up here once requests arrive."
        />
        <BreakdownCard
          title="Top hosts"
          description="Which host bindings currently drive the most volume."
          items={(report.top_hosts ?? []).map((item) => ({
            ...item,
            hit_ratio: requestShare(item.requests),
          }))}
          emptyLabel="Host distribution will appear once traffic is present."
        />
        <Card>
          <CardHeader>
            <CardTitle>Control plane snapshot</CardTitle>
            <CardDescription>
              Keep configuration scale in view next to live traffic metrics.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 text-sm">
            <div className="flex items-center justify-between rounded-xl border p-3">
              <span className="text-muted-foreground">Configured sites</span>
              <span className="font-medium">{initial.sites.length}</span>
            </div>
            <div className="flex items-center justify-between rounded-xl border p-3">
              <span className="text-muted-foreground">Active sites in telemetry</span>
              <span className="font-medium">
                {formatCompactNumber(report.summary.active_sites)}
              </span>
            </div>
            <div className="flex items-center justify-between rounded-xl border p-3">
              <span className="text-muted-foreground">Cached bandwidth served</span>
              <span className="font-medium">{formatBytes(report.summary.cached_bytes)}</span>
            </div>
            <Button variant="outline" asChild className="justify-start">
              <Link to="/analytics">
                <Workflow className="size-4" />
                Open detailed analytics
              </Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    </ConsoleShell>
  );
}
