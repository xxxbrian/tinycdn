import { createFileRoute } from "@tanstack/react-router";
import { startTransition, useEffect, useState } from "react";

import type { AnalyticsPeriod, AnalyticsReport } from "@/types";
import { api } from "@/lib/api";
import { analyticsPeriods, toTrafficSeries } from "@/lib/telemetry";
import { BreakdownCard } from "@/components/breakdown-card";
import { ChartAreaInteractive } from "@/components/chart-area-interactive";
import { PageHeader } from "@/components/page-header";
import { SiteShell } from "@/components/site-shell";
import { TelemetrySummaryCards } from "@/components/telemetry-summary-cards";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export const Route = createFileRoute("/sites/$siteId/analytics")({
  loader: async ({ params }) => {
    const [site, report] = await Promise.all([
      api.getSite(params.siteId),
      api.analyticsReport("24h", params.siteId),
    ]);
    return { site, report };
  },
  component: SiteAnalyticsPage,
});

function SiteAnalyticsPage() {
  const initial = Route.useLoaderData();
  const [period, setPeriod] = useState<AnalyticsPeriod>("24h");
  const [report, setReport] = useState<AnalyticsReport>(initial.report);

  useEffect(() => {
    let cancelled = false;
    void api.analyticsReport(period, initial.site.id).then((next) => {
      if (cancelled) {
        return;
      }
      startTransition(() => setReport(next));
    });
    return () => {
      cancelled = true;
    };
  }, [initial.site.id, period]);

  return (
    <SiteShell site={initial.site} section="Analytics">
      <PageHeader
        eyebrow="Site workspace"
        title={`${initial.site.name} analytics`}
        description="Demand, cache efficiency, and origin behavior for this site alone."
        actions={
          <Select value={period} onValueChange={(value) => setPeriod(value as AnalyticsPeriod)}>
            <SelectTrigger className="w-[150px]">
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
        }
      />

      <TelemetrySummaryCards report={report} />

      <div className="px-4 lg:px-6">
        <ChartAreaInteractive
          title="Site traffic"
          description="Requests, cache-delivered responses, and origin fetch pressure."
          points={toTrafficSeries(report)}
          period={period}
          onPeriodChange={setPeriod}
        />
      </div>

      <div className="grid gap-6 px-4 pb-6 lg:grid-cols-2 lg:px-6">
        <BreakdownCard
          title="Cache state mix"
          description="How this site is being served."
          items={report.cache_states}
          emptyLabel="Cache states will appear once requests are captured."
        />
        <BreakdownCard
          title="Status classes"
          description="HTTP outcomes returned through this site."
          items={report.status_classes}
          emptyLabel="Status classes will appear after the first traffic arrives."
        />
        <BreakdownCard
          title="Top paths"
          description="The most active content paths on this site."
          items={report.top_paths.map((item) => ({
            key: item.path,
            label: item.path,
            requests: item.requests,
            edge_bytes: item.edge_bytes,
            hit_ratio: item.hit_ratio,
          }))}
          emptyLabel="No path traffic has been recorded yet."
        />
        <BreakdownCard
          title="Top hosts and rules"
          description="Host bindings and rule pressure for this site."
          items={[
            ...report.top_hosts,
            ...report.top_rules.map((item) => ({
              ...item,
              key: `rule:${item.key}`,
              label: item.key,
            })),
          ].slice(0, 8)}
          emptyLabel="Host and rule breakdowns will appear once requests match this site."
        />
      </div>
    </SiteShell>
  );
}
