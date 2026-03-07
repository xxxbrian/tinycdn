import { Outlet, Link, createFileRoute, useRouterState } from "@tanstack/react-router";
import { Activity, Globe, Logs, Settings2, Shield, Workflow } from "lucide-react";

import { api } from "@/lib/api";
import { CachePurgeSheet } from "@/components/cache-purge-sheet";
import { ChartAreaInteractive } from "@/components/chart-area-interactive";
import { PageHeader } from "@/components/page-header";
import { RequestLogsTable } from "@/components/request-logs-table";
import { SiteShell } from "@/components/site-shell";
import { getPrimaryCacheMode, getSiteStats } from "@/lib/site-metrics";
import {
  formatBytes,
  formatCompactNumber,
  formatDuration,
  formatPercent,
  toTrafficSeries,
} from "@/lib/telemetry";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export const Route = createFileRoute("/sites/$siteId")({
  loader: async ({ params }) => {
    const [site, report, requests] = await Promise.all([
      api.getSite(params.siteId),
      api.analyticsReport("24h", params.siteId),
      api.requestLogs({ period: "24h", siteId: params.siteId, limit: 8 }),
    ]);
    return { site, report, requests };
  },
  component: SiteOverviewPage,
});

function describeOriginHost(site: ReturnType<typeof Route.useLoaderData>["site"]) {
  switch (site.upstream.host_mode) {
    case "follow_request":
      return {
        label: "Follow incoming request host",
        detail: "The edge forwards the client-facing host to the origin.",
      };
    case "custom":
      return {
        label: site.upstream.host || "Custom host",
        detail: "TinyCDN rewrites the upstream Host header to this explicit value.",
      };
    default:
      return {
        label: "Follow origin URL host",
        detail: "The edge reuses the hostname from the configured upstream URL.",
      };
  }
}

function SiteOverviewPage() {
  const { site, report, requests } = Route.useLoaderData();
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const stats = getSiteStats(site);
  const originHost = describeOriginHost(site);
  const isOverviewRoute = pathname === `/sites/${site.id}`;

  if (!isOverviewRoute) {
    return <Outlet />;
  }

  return (
    <SiteShell site={site} section="Overview">
      <PageHeader
        eyebrow="Site workspace"
        title={site.name}
        description="Current traffic, cache posture, host binding behavior, and the newest request activity for this site."
        badge={site.enabled ? "Serving traffic" : "Disabled"}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/sites/$siteId/analytics" params={{ siteId: site.id }}>
                <Activity className="size-4" />
                Analytics
              </Link>
            </Button>
            <Button variant="outline" asChild>
              <Link to="/sites/$siteId/logs" params={{ siteId: site.id }}>
                <Logs className="size-4" />
                Logs
              </Link>
            </Button>
            <CachePurgeSheet site={site} />
            <Button asChild>
              <Link to="/sites/$siteId/rules" params={{ siteId: site.id }}>
                <Workflow className="size-4" />
                Rules
              </Link>
            </Button>
          </>
        }
      />

      <div className="grid gap-4 px-4 md:grid-cols-2 lg:px-6 xl:grid-cols-4">
        <Card>
          <CardHeader>
            <CardDescription>Requests (24h)</CardDescription>
            <CardTitle className="text-3xl">
              {formatCompactNumber(report.summary.requests)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>Cache hit ratio</CardDescription>
            <CardTitle className="text-3xl">
              {formatPercent(report.summary.cache_hit_ratio)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>Edge p95</CardDescription>
            <CardTitle className="text-3xl">
              {formatDuration(report.summary.p95_edge_duration_ms)}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>Cached bandwidth</CardDescription>
            <CardTitle className="text-2xl">{formatBytes(report.summary.cached_bytes)}</CardTitle>
          </CardHeader>
        </Card>
      </div>

      <div className="px-4 lg:px-6">
        <ChartAreaInteractive
          title="Site traffic"
          description="Requests, cache-served responses, and origin fetches for the last 24 hours."
          points={toTrafficSeries(report)}
          period="24h"
          onPeriodChange={() => {}}
          showPeriodControls={false}
        />
      </div>

      <div className="grid gap-6 px-4 pb-4 lg:grid-cols-[1.35fr_1fr] lg:px-6 lg:pb-6">
        <Card>
          <CardHeader>
            <CardTitle>Host bindings</CardTitle>
            <CardDescription>
              Review both the public hostnames bound to this site and the host TinyCDN presents when
              it talks to the origin.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4">
            <div className="rounded-lg border p-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <div className="font-medium">Incoming host bindings</div>
                  <p className="mt-1 text-sm text-muted-foreground">
                    Requests matching any of these hosts enter this site before rules run.
                  </p>
                </div>
                <Badge variant="outline">{site.hosts.length}</Badge>
              </div>
              <div className="mt-4 flex flex-wrap gap-2">
                {site.hosts.map((host) => (
                  <Badge key={host} variant="secondary">
                    {host}
                  </Badge>
                ))}
              </div>
            </div>
            <div className="rounded-lg border p-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <div className="font-medium">Origin request host</div>
                  <p className="mt-1 text-sm text-muted-foreground">{originHost.detail}</p>
                </div>
                <Badge variant={site.upstream.host_mode === "custom" ? "outline" : "secondary"}>
                  {site.upstream.host_mode === "custom"
                    ? "Custom"
                    : site.upstream.host_mode === "follow_request"
                      ? "Request"
                      : "Origin"}
                </Badge>
              </div>
              <code className="mt-4 block max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded-md bg-muted px-3 py-2 text-sm">
                {originHost.label}
              </code>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Cache posture</CardTitle>
            <CardDescription>
              Rule-level behavior, optimistic settings, and currently stored cache size.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 text-sm">
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Default cache mode</span>
              <Badge variant="outline" className="capitalize">
                {getPrimaryCacheMode(stats.defaultRule)}
              </Badge>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Optimistic rules</span>
              <Badge variant={stats.optimisticRuleCount > 0 ? "outline" : "secondary"}>
                {stats.optimisticRuleCount}
              </Badge>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Stored cache</span>
              <span className="font-medium">{formatBytes(report.cache_inventory.bytes)}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Stored objects</span>
              <span className="font-medium">
                {formatCompactNumber(report.cache_inventory.objects)}
              </span>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Origin</span>
              <code className="max-w-[16rem] truncate">{site.upstream.url}</code>
            </div>
          </CardContent>
        </Card>

        <RequestLogsTable
          title="Recent site requests"
          description="The newest requests served through this site workspace."
          items={requests.items}
        />

        <Card>
          <CardHeader>
            <CardTitle>Jump to section</CardTitle>
            <CardDescription>
              Drill into analytics, request history, rules, and site configuration from here.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
            <Button variant="outline" asChild className="justify-start">
              <Link to="/sites/$siteId/analytics" params={{ siteId: site.id }}>
                <Activity className="size-4" />
                Open analytics
              </Link>
            </Button>
            <Button variant="outline" asChild className="justify-start">
              <Link to="/sites/$siteId/logs" params={{ siteId: site.id }}>
                <Logs className="size-4" />
                Inspect logs
              </Link>
            </Button>
            <Button variant="outline" asChild className="justify-start">
              <Link to="/sites/$siteId/rules" params={{ siteId: site.id }}>
                <Workflow className="size-4" />
                Manage rules
              </Link>
            </Button>
            <Button variant="outline" asChild className="justify-start">
              <Link to="/sites/$siteId/origin" params={{ siteId: site.id }}>
                <Globe className="size-4" />
                Configure origin
              </Link>
            </Button>
            <Button variant="outline" asChild className="justify-start">
              <Link to="/sites/$siteId/settings" params={{ siteId: site.id }}>
                <Settings2 className="size-4" />
                Site settings
              </Link>
            </Button>
            <Button variant="outline" asChild className="justify-start">
              <Link to="/sites/$siteId/waf" params={{ siteId: site.id }}>
                <Shield className="size-4" />
                WAF roadmap
              </Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    </SiteShell>
  );
}
