import { createFileRoute } from "@tanstack/react-router";

import { ConsoleShell } from "@/components/console-shell";
import { PageHeader } from "@/components/page-header";
import { PlaceholderPanel } from "@/components/placeholder-panel";

export const Route = createFileRoute("/logs")({
  component: LogsPage,
});

function LogsPage() {
  return (
    <ConsoleShell>
      <PageHeader
        eyebrow="Control plane"
        title="Logs"
        description="Request logs, cache traces, and operational audit feeds will land here. The route exists now so the dashboard structure stays stable as we add telemetry."
      />
      <div className="grid gap-6 p-4 lg:grid-cols-2 lg:p-6">
        <PlaceholderPanel
          title="Request stream"
          description="A live tail of edge traffic will appear here once the data plane starts exporting structured logs."
        />
        <PlaceholderPanel
          title="Audit events"
          description="Admin API changes, config publishes, and runtime reloads will appear here."
        />
      </div>
    </ConsoleShell>
  );
}
