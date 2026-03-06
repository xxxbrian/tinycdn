import { Outlet, Link, createFileRoute, useRouterState } from "@tanstack/react-router";
import { Globe, Settings2, Shield, Workflow } from "lucide-react";

import { api } from "@/lib/api";
import { getPrimaryCacheMode, getSiteStats } from "@/lib/site-metrics";
import { PageHeader } from "@/components/page-header";
import { SiteShell } from "@/components/site-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export const Route = createFileRoute("/sites/$siteId")({
  loader: ({ params }) => api.getSite(params.siteId),
  component: SiteOverviewPage,
});

function describeOriginHost(site: ReturnType<typeof Route.useLoaderData>) {
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
  const site = Route.useLoaderData();
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
        description="Review the current host bindings, cache posture, and downstream origin before drilling into rules and settings."
        badge={site.enabled ? "Serving traffic" : "Disabled"}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/sites/$siteId/origin" params={{ siteId: site.id }}>
                <Globe className="size-4" />
                Origin
              </Link>
            </Button>
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
            <CardDescription>Hosts</CardDescription>
            <CardTitle className="text-3xl">{stats.hostCount}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>Custom rules</CardDescription>
            <CardTitle className="text-3xl">{stats.customRuleCount}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>Enabled rules</CardDescription>
            <CardTitle className="text-3xl">{stats.enabledRules}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>Default cache mode</CardDescription>
            <CardTitle className="text-xl capitalize">
              {getPrimaryCacheMode(stats.defaultRule)}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <div className="grid gap-6 px-4 pb-4 lg:grid-cols-[1.4fr_1fr] lg:px-6 lg:pb-6">
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
              Site-level defaults that shape how the rule pipeline behaves.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 text-sm">
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Optimistic refresh</span>
              <Badge variant={site.cache.optimistic_refresh ? "outline" : "secondary"}>
                {site.cache.optimistic_refresh ? "Enabled" : "Off"}
              </Badge>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <span className="text-muted-foreground">Origin</span>
              <code className="max-w-[16rem] truncate">{site.upstream.url}</code>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Rule pipeline</CardTitle>
            <CardDescription>
              Requests evaluate custom rules first, then fall through to the system catch-all.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
            {site.rules.map((rule, index) => (
              <div
                key={rule.id}
                className="flex items-center justify-between rounded-lg border p-3"
              >
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{rule.name}</span>
                    {rule.system ? <Badge variant="secondary">System</Badge> : null}
                  </div>
                  <p className="text-sm text-muted-foreground">
                    Order {index + 1} • {rule.action.cache.mode.replaceAll("_", " ")}
                  </p>
                </div>
                <Badge variant={rule.enabled ? "outline" : "secondary"}>
                  {rule.enabled ? "Enabled" : "Disabled"}
                </Badge>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Jump to section</CardTitle>
            <CardDescription>
              The site workspace is split into focused pages instead of a single editing canvas.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3">
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
