import { createFileRoute } from "@tanstack/react-router";
import { startTransition, useEffect, useState } from "react";

import type { AnalyticsPeriod, AnalyticsReport } from "@/types";
import { api } from "@/lib/api";
import { analyticsPeriods, toTrafficSeries } from "@/lib/telemetry";
import { BreakdownCard } from "@/components/breakdown-card";
import { ChartAreaInteractive } from "@/components/chart-area-interactive";
import { ConsoleShell } from "@/components/console-shell";
import { PageHeader } from "@/components/page-header";
import { TelemetrySummaryCards } from "@/components/telemetry-summary-cards";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export const Route = createFileRoute("/analytics")({
  loader: () => api.analyticsReport("24h"),
  component: AnalyticsPage,
});

function AnalyticsPage() {
  const initialReport = Route.useLoaderData();
  const [period, setPeriod] = useState<AnalyticsPeriod>("24h");
  const [report, setReport] = useState<AnalyticsReport>(initialReport);
  const requestShare = (requests: number) =>
    report.summary.requests > 0 ? requests / report.summary.requests : undefined;

  useEffect(() => {
    let cancelled = false;
    void api.analyticsReport(period).then((next) => {
      if (cancelled) {
        return;
      }
      startTransition(() => setReport(next));
    });
    return () => {
      cancelled = true;
    };
  }, [period]);

  return (
    <ConsoleShell>
      <PageHeader
        eyebrow="Control plane"
        title="Analytics"
        description="Traffic volume, cache efficiency, origin pressure, and content distribution across the whole platform."
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
          title="Platform traffic"
          description="Requests, cache-served volume, and origin fetch pressure for the selected period."
          points={toTrafficSeries(report)}
          period={period}
          onPeriodChange={setPeriod}
        />
      </div>

      <div className="grid gap-6 px-4 pb-6 lg:grid-cols-2 lg:px-6">
        <BreakdownCard
          title="HTTP methods"
          description="Method mix at the edge."
          items={report.methods}
          emptyLabel="Method distribution will appear once requests are recorded."
        />
        <BreakdownCard
          title="Status classes"
          description="Which response families dominate current traffic."
          items={report.status_classes}
          emptyLabel="Status code distribution will appear after requests arrive."
        />
        <BreakdownCard
          title="Top hosts"
          description="Host bindings ranked by request volume."
          items={(report.top_hosts ?? []).map((item) => ({
            ...item,
            hit_ratio: requestShare(item.requests),
          }))}
          emptyLabel="No host traffic has been recorded yet."
        />
        <BreakdownCard
          title="Top sites"
          description="Which sites currently own the most demand."
          items={(report.top_sites ?? []).map((item) => ({
            key: item.site_id,
            label: item.site_name,
            requests: item.requests,
            edge_bytes: item.edge_bytes,
            hit_ratio: requestShare(item.requests),
          }))}
          emptyLabel="Site traffic ranking will appear once requests land."
        />
        <BreakdownCard
          title="Top paths"
          description="Busy content surfaces over the current period."
          items={(report.top_paths ?? []).map((item) => ({
            key: item.path,
            label: item.path,
            requests: item.requests,
            edge_bytes: item.edge_bytes,
            hit_ratio: requestShare(item.requests),
          }))}
          emptyLabel="No path traffic has been recorded yet."
        />
        <BreakdownCard
          title="Rule pressure"
          description="Which cache rules are seeing the most requests."
          items={(report.top_rules ?? []).map((item) => ({
            ...item,
            hit_ratio: requestShare(item.requests),
          }))}
          emptyLabel="Rule-level analytics will appear after matching traffic arrives."
        />
      </div>
    </ConsoleShell>
  );
}
