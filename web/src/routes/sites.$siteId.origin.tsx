import { useState } from "react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { toast } from "sonner";

import { api, toSiteInput } from "@/lib/api";
import { requireAuth, withProtectedLoader } from "@/lib/auth";
import { OriginForm } from "@/components/origin-form";
import { PageHeader } from "@/components/page-header";
import { SiteShell } from "@/components/site-shell";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export const Route = createFileRoute("/sites/$siteId/origin")({
  beforeLoad: ({ location }) => requireAuth(location),
  loader: ({ location, params }) => withProtectedLoader(location, () => api.getSite(params.siteId)),
  component: SiteOriginPage,
});

function SiteOriginPage() {
  const site = Route.useLoaderData();
  const router = useRouter();
  const [saving, setSaving] = useState(false);

  return (
    <SiteShell site={site} section="Origin">
      <PageHeader
        eyebrow="Site workspace"
        title="Origin"
        description="Keep upstream configuration separate from general site settings so origin behavior can grow independently later."
      />
      <div className="grid gap-6 p-4 lg:grid-cols-[1.2fr_0.8fr] lg:p-6">
        <Card>
          <CardHeader>
            <CardTitle>Upstream endpoint</CardTitle>
            <CardDescription>
              TinyCDN proxies misses and uncached requests to this upstream URL.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <OriginForm
              initialUrl={site.upstream.url}
              initialHostMode={site.upstream.host_mode ?? "follow_origin"}
              initialHost={site.upstream.host ?? ""}
              saving={saving}
              onSubmit={async (value) => {
                setSaving(true);
                try {
                  await api.updateSite(
                    site.id,
                    toSiteInput(site, {
                      upstream_url: value.url,
                      upstream_host_mode: value.hostMode,
                      upstream_host: value.host,
                    }),
                  );
                  toast.success("Origin updated");
                  await router.invalidate();
                } catch (error) {
                  toast.error(error instanceof Error ? error.message : "Failed to update origin");
                } finally {
                  setSaving(false);
                }
              }}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Routing context</CardTitle>
            <CardDescription>
              Host bindings still live at the site level, while this page stays focused on the
              upstream endpoint.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <div className="rounded-lg border p-3">
              <div className="font-medium">Hosts</div>
              <div className="mt-1 text-muted-foreground">{site.hosts.join(", ")}</div>
            </div>
            <div className="rounded-lg border p-3">
              <div className="font-medium">Current origin</div>
              <code className="mt-1 block max-w-full overflow-hidden text-ellipsis whitespace-nowrap">
                {site.upstream.url}
              </code>
            </div>
            <div className="rounded-lg border p-3">
              <div className="font-medium">Request host mode</div>
              <div className="mt-1 text-muted-foreground">
                {site.upstream.host_mode === "follow_request"
                  ? "Follow incoming request host"
                  : site.upstream.host_mode === "custom"
                    ? `Custom: ${site.upstream.host}`
                    : "Follow origin URL host"}
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </SiteShell>
  );
}
