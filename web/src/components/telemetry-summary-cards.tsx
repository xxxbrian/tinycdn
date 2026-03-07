import type { AnalyticsReport } from "@/types";
import {
  Activity,
  DatabaseZap,
  Gauge,
  HardDriveDownload,
  PackageOpen,
  ServerCog,
} from "lucide-react";

import { formatBytes, formatCompactNumber, formatDuration, formatPercent } from "@/lib/telemetry";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

const cardConfig = [
  {
    key: "requests",
    label: "Requests",
    icon: Activity,
    value: (report: AnalyticsReport) => formatCompactNumber(report.summary.requests),
    detail: (report: AnalyticsReport) =>
      `${formatCompactNumber(report.summary.origin_requests)} origin fetches`,
  },
  {
    key: "hit_ratio",
    label: "Cache hit ratio",
    icon: DatabaseZap,
    value: (report: AnalyticsReport) => formatPercent(report.summary.cache_hit_ratio),
    detail: (report: AnalyticsReport) =>
      `${formatPercent(report.summary.cache_bandwidth_ratio)} bandwidth served from cache`,
  },
  {
    key: "edge_bytes",
    label: "Edge bandwidth",
    icon: HardDriveDownload,
    value: (report: AnalyticsReport) => formatBytes(report.summary.edge_bytes),
    detail: (report: AnalyticsReport) =>
      `${formatBytes(report.summary.origin_bytes)} pulled from origin`,
  },
  {
    key: "edge_p95",
    label: "Edge p95",
    icon: Gauge,
    value: (report: AnalyticsReport) => formatDuration(report.summary.p95_edge_duration_ms),
    detail: (report: AnalyticsReport) =>
      `${formatDuration(report.summary.average_edge_duration_ms)} average request time`,
  },
  {
    key: "origin_p95",
    label: "Origin p95",
    icon: ServerCog,
    value: (report: AnalyticsReport) => formatDuration(report.summary.p95_origin_duration_ms),
    detail: (report: AnalyticsReport) =>
      `${formatDuration(report.summary.average_origin_duration_ms)} average fetch time`,
  },
  {
    key: "cache_inventory",
    label: "Cached objects",
    icon: PackageOpen,
    value: (report: AnalyticsReport) => formatCompactNumber(report.cache_inventory.objects),
    detail: (report: AnalyticsReport) =>
      `${formatBytes(report.cache_inventory.bytes)} stored right now`,
  },
] as const;

export function TelemetrySummaryCards({ report }: { report: AnalyticsReport }) {
  return (
    <div className="grid gap-4 px-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 lg:px-6">
      {cardConfig.map((item) => (
        <Card key={item.key}>
          <CardHeader className="gap-3">
            <div className="flex items-center justify-between">
              <CardDescription>{item.label}</CardDescription>
              <item.icon className="size-4 text-muted-foreground" />
            </div>
            <CardTitle className="text-2xl">{item.value(report)}</CardTitle>
            <p className="text-xs text-muted-foreground">{item.detail(report)}</p>
          </CardHeader>
        </Card>
      ))}
    </div>
  );
}
