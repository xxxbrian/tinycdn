import { Outlet, Link, createFileRoute, useRouter, useRouterState } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { Globe, Plus, Search } from "lucide-react";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { ConsoleShell } from "@/components/console-shell";
import { PageHeader } from "@/components/page-header";
import { SiteForm } from "@/components/site-form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from "@/components/ui/drawer";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export const Route = createFileRoute("/sites")({
  loader: () => api.listSites(),
  component: SitesPage,
});

function SitesPage() {
  const sites = Route.useLoaderData();
  const router = useRouter();
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const isIndexRoute = pathname === "/sites";

  const filteredSites = useMemo(() => {
    if (!query.trim()) {
      return sites;
    }

    const needle = query.trim().toLowerCase();
    return sites.filter((site) =>
      [site.name, site.upstream.url, ...site.hosts].join(" ").toLowerCase().includes(needle),
    );
  }, [query, sites]);

  if (!isIndexRoute) {
    return <Outlet />;
  }

  return (
    <ConsoleShell>
      <PageHeader
        eyebrow="Inventory"
        title="Sites"
        description="All configured host groups live here. Select a site to enter its dedicated workspace for rules, origin, settings, and future security features."
        badge={`${sites.length} total`}
        actions={
          <Drawer open={open} onOpenChange={setOpen} direction="right">
            <DrawerTrigger asChild>
              <Button>
                <Plus className="size-4" />
                Add site
              </Button>
            </DrawerTrigger>
            <DrawerContent className="ml-auto h-full max-w-2xl rounded-l-2xl rounded-r-none">
              <DrawerHeader className="text-left">
                <DrawerTitle>Create site</DrawerTitle>
                <DrawerDescription>
                  Sites define host ownership, origin target, and site-level cache posture.
                </DrawerDescription>
              </DrawerHeader>
              <div className="px-4 pb-4">
                <SiteForm
                  initialValue={{
                    name: "",
                    enabled: true,
                    optimistic_refresh: false,
                    hosts: [],
                    upstream_url: "",
                    upstream_host_mode: "follow_origin",
                    upstream_host: "",
                  }}
                  submitLabel="Create site"
                  saving={saving}
                  onSubmit={async (value) => {
                    setSaving(true);
                    try {
                      const site = await api.createSite(value);
                      toast.success("Site created");
                      setOpen(false);
                      await router.invalidate();
                      await router.navigate({
                        to: "/sites/$siteId",
                        params: { siteId: site.id },
                      });
                    } catch (error) {
                      toast.error(error instanceof Error ? error.message : "Failed to create site");
                    } finally {
                      setSaving(false);
                    }
                  }}
                />
              </div>
            </DrawerContent>
          </Drawer>
        }
      />

      <div className="grid gap-6 px-4 pb-4 lg:grid-cols-[1.4fr_0.6fr] lg:px-6 lg:pb-6">
        <Card>
          <CardHeader className="gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <CardTitle>All sites</CardTitle>
              <CardDescription>Search by site name, host, or origin URL.</CardDescription>
            </div>
            <div className="relative w-full max-w-sm">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search sites"
                className="pl-9"
              />
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Site</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Hosts</TableHead>
                  <TableHead>Rules</TableHead>
                  <TableHead>Cache</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredSites.map((site) => (
                  <TableRow key={site.id}>
                    <TableCell>
                      <Link
                        to="/sites/$siteId"
                        params={{ siteId: site.id }}
                        className="font-medium text-foreground no-underline hover:text-primary"
                      >
                        {site.name}
                      </Link>
                      <div className="truncate text-xs text-muted-foreground">
                        {site.upstream.url}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={site.enabled ? "outline" : "secondary"}>
                        {site.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </TableCell>
                    <TableCell>{site.hosts.length}</TableCell>
                    <TableCell>{site.rules.length}</TableCell>
                    <TableCell>
                      <Badge variant={site.cache.optimistic_refresh ? "outline" : "secondary"}>
                        {site.cache.optimistic_refresh ? "Optimistic" : "Normal"}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <div className="grid gap-6">
          <Card>
            <CardHeader>
              <CardTitle>Workspace model</CardTitle>
              <CardDescription>
                The dashboard mirrors Cloudflare-style flow without collapsing everything into one
                page.
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm text-muted-foreground">
              <div className="rounded-lg border p-3">
                <p className="font-medium text-foreground">Global level</p>
                <p className="mt-1">Overview, sites, logs, analytics.</p>
              </div>
              <div className="rounded-lg border p-3">
                <p className="font-medium text-foreground">Site level</p>
                <p className="mt-1">Overview, rules, origin, settings, WAF.</p>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>No single-page sprawl</CardTitle>
              <CardDescription>
                Editing is split into focused sections so future features slot in naturally.
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm text-muted-foreground">
              <div className="flex items-center gap-2 rounded-lg border p-3">
                <Globe className="size-4" />
                <span>Origin is isolated from general site settings.</span>
              </div>
              <div className="flex items-center gap-2 rounded-lg border p-3">
                <Plus className="size-4" />
                <span>Rules have their own ordered workspace with create/edit flows.</span>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </ConsoleShell>
  );
}
