import { Link, createFileRoute } from "@tanstack/react-router";
import { Globe, Logs, Sparkles, Workflow } from "lucide-react";

import { api } from "@/lib/api";
import { getGlobalStats } from "@/lib/site-metrics";
import { ChartAreaInteractive } from "@/components/chart-area-interactive";
import { ConsoleShell } from "@/components/console-shell";
import { PageHeader } from "@/components/page-header";
import { PlaceholderPanel } from "@/components/placeholder-panel";
import { SectionCards } from "@/components/section-cards";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export const Route = createFileRoute("/")({
  loader: () => api.listSites(),
  component: OverviewPage,
});

function OverviewPage() {
  const sites = Route.useLoaderData();
  const stats = getGlobalStats(sites);

  return (
    <ConsoleShell>
      <PageHeader
        eyebrow="Global overview"
        title="Edge control plane"
        description="A single-node CDN dashboard for site management, rule orchestration, and future telemetry. Configuration metrics are live; traffic analytics stay reserved for later."
        actions={
          <>
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

      <SectionCards stats={stats} />

      <div className="px-4 lg:px-6">
        <ChartAreaInteractive />
      </div>

      <div className="grid gap-6 px-4 lg:grid-cols-[1.45fr_0.95fr] lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>Sites at a glance</CardTitle>
            <CardDescription>
              Jump straight into a site workspace from the global overview.
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Site</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Hosts</TableHead>
                  <TableHead>Rules</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sites.slice(0, 6).map((site) => (
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
                    <TableCell>{site.hosts.length}</TableCell>
                    <TableCell>{site.rules.length}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <div className="grid gap-6">
          <PlaceholderPanel
            title="Telemetry reserved"
            description="Global logs, cache analytics, and origin performance charts will slot into this column once request metrics exist."
          >
            <div className="grid gap-3 text-sm text-muted-foreground">
              <div className="flex items-center gap-2 rounded-lg border p-3">
                <Logs className="size-4" />
                <span>Edge request stream placeholder</span>
              </div>
              <div className="flex items-center gap-2 rounded-lg border p-3">
                <Sparkles className="size-4" />
                <span>Cache efficiency timeline placeholder</span>
              </div>
              <div className="flex items-center gap-2 rounded-lg border p-3">
                <Workflow className="size-4" />
                <span>Runtime publish history placeholder</span>
              </div>
            </div>
          </PlaceholderPanel>
          <Card>
            <CardHeader>
              <CardTitle>How this dashboard is organized</CardTitle>
              <CardDescription>
                The UI is intentionally split into global and site workspaces instead of a single
                editing page.
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm text-muted-foreground">
              <div className="rounded-lg border p-3">
                <p className="font-medium text-foreground">Global overview</p>
                <p className="mt-1">
                  Summary metrics, sites entrypoint, and future logs/analytics.
                </p>
              </div>
              <div className="rounded-lg border p-3">
                <p className="font-medium text-foreground">Site workspace</p>
                <p className="mt-1">Per-site overview, rules, origin, settings, and future WAF.</p>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </ConsoleShell>
  );
}
