import { useState } from "react";
import { createFileRoute, useNavigate, useRouter } from "@tanstack/react-router";
import { toast } from "sonner";

import { api, toSiteInput } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { SiteForm } from "@/components/site-form";
import { SiteShell } from "@/components/site-shell";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export const Route = createFileRoute("/sites/$siteId/settings")({
  loader: ({ params }) => api.getSite(params.siteId),
  component: SiteSettingsPage,
});

function SiteSettingsPage() {
  const site = Route.useLoaderData();
  const router = useRouter();
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  return (
    <SiteShell site={site} section="Settings">
      <PageHeader
        eyebrow="Site workspace"
        title="Settings"
        description="General site metadata and host bindings live here, while cache behavior stays with rules."
      />
      <div className="grid gap-6 px-4 pb-4 lg:grid-cols-[1.2fr_0.8fr] lg:px-6 lg:pb-6">
        <Card>
          <CardHeader>
            <CardTitle>General configuration</CardTitle>
            <CardDescription>
              These settings feed the Admin API model directly and compile into the runtime
              snapshot.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <SiteForm
              initialValue={toSiteInput(site)}
              submitLabel="Save site"
              saving={saving}
              onSubmit={async (value) => {
                setSaving(true);
                try {
                  await api.updateSite(site.id, value);
                  toast.success("Site updated");
                  await router.invalidate();
                } catch (error) {
                  toast.error(error instanceof Error ? error.message : "Failed to update site");
                } finally {
                  setSaving(false);
                }
              }}
            />
          </CardContent>
        </Card>

        <Card className="border-destructive/30">
          <CardHeader>
            <CardTitle>Danger zone</CardTitle>
            <CardDescription>
              Deleting a site removes its rules and host bindings from the runtime snapshot.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Button
              variant="destructive"
              disabled={deleting}
              onClick={async () => {
                setDeleting(true);
                try {
                  await api.deleteSite(site.id);
                  toast.success("Site deleted");
                  await navigate({ to: "/sites" });
                } catch (error) {
                  toast.error(error instanceof Error ? error.message : "Failed to delete site");
                } finally {
                  setDeleting(false);
                }
              }}
            >
              Delete site
            </Button>
          </CardContent>
        </Card>
      </div>
    </SiteShell>
  );
}
