import { createFileRoute } from "@tanstack/react-router";

import { api } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { PlaceholderPanel } from "@/components/placeholder-panel";
import { SiteShell } from "@/components/site-shell";

export const Route = createFileRoute("/sites/$siteId/waf")({
  loader: ({ params }) => api.getSite(params.siteId),
  component: SiteWafPage,
});

function SiteWafPage() {
  const site = Route.useLoaderData();

  return (
    <SiteShell site={site} section="WAF">
      <PageHeader
        eyebrow="Site workspace"
        title="WAF"
        description="The navigation is already carved out so future security controls can land without reshaping the site workspace."
      />
      <div className="grid gap-6 p-4 lg:grid-cols-2 lg:p-6">
        <PlaceholderPanel
          title="Managed protections"
          description="Future rule groups, bot mitigation, and request screening controls will live here."
        />
        <PlaceholderPanel
          title="Custom policies"
          description="Site-specific security policies will appear here once the WAF phase exists in the runtime pipeline."
        />
      </div>
    </SiteShell>
  );
}
