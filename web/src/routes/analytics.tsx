import { createFileRoute } from "@tanstack/react-router";

import { ConsoleShell } from "@/components/console-shell";
import { PageHeader } from "@/components/page-header";
import { PlaceholderPanel } from "@/components/placeholder-panel";

export const Route = createFileRoute("/analytics")({
  component: AnalyticsPage,
});

function AnalyticsPage() {
  return (
    <ConsoleShell>
      <PageHeader
        eyebrow="Control plane"
        title="Analytics"
        description="Traffic analytics and cache efficiency dashboards are reserved here. For now, configuration metrics live in the overview while request telemetry remains a future slice."
      />
      <div className="grid gap-6 p-4 lg:grid-cols-3 lg:p-6">
        <PlaceholderPanel
          title="Traffic series"
          description="Aggregate requests, origin fetches, and hit ratio charts will appear here."
        />
        <PlaceholderPanel
          title="Top sites"
          description="A ranked list of the busiest sites will replace this placeholder when live telemetry exists."
        />
        <PlaceholderPanel
          title="Latency views"
          description="Edge timing and origin timing distributions will be exposed here."
        />
      </div>
    </ConsoleShell>
  );
}
