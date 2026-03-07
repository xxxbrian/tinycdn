import type {
  AnalyticsPeriod,
  AnalyticsReport,
  AnalyticsSeriesPoint,
  RequestLogItem,
} from "@/types";

export const analyticsPeriods: { value: AnalyticsPeriod; label: string }[] = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
];

export function formatCompactNumber(value: number) {
  return new Intl.NumberFormat("en-US", {
    notation: "compact",
    maximumFractionDigits: value >= 1000 ? 1 : 0,
  }).format(value);
}

export function formatPercent(value: number) {
  return `${(value * 100).toFixed(value >= 0.1 ? 1 : 2)}%`;
}

export function formatBytes(value: number) {
  if (value === 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size >= 100 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`;
}

export function formatDuration(value: number) {
  if (value >= 1000) {
    return `${(value / 1000).toFixed(value >= 10_000 ? 0 : 1)}s`;
  }
  return `${Math.round(value)}ms`;
}

export function formatDateTime(value: string) {
  return new Date(value).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

export function formatSeriesTick(value: string, period: AnalyticsPeriod) {
  const date = new Date(value);
  if (period === "30d" || period === "90d") {
    return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  }
  return date.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" });
}

export function toTrafficSeries(report: AnalyticsReport) {
  return (report.series ?? []).map((point) => ({
    bucket: point.bucket,
    requests: point.requests,
    cacheHits: point.hit_requests + point.stale_requests,
    originFetches: point.origin_requests,
    edgeBytes: point.edge_bytes,
  }));
}

export function formatPath(item: { path: string; raw_query?: string }) {
  return item.raw_query ? `${item.path}?${item.raw_query}` : item.path;
}

export function formatRequestID(value: string) {
  if (value.length <= 14) {
    return value;
  }
  return `${value.slice(0, 8)}…${value.slice(-4)}`;
}

export function latestPoint(points: AnalyticsSeriesPoint[]) {
  return points[points.length - 1] ?? null;
}

export function summarizeCacheStatus(item: Pick<RequestLogItem, "cache_state" | "cache_status">) {
  const detailMatch = item.cache_status.match(/detail=([^;]+)/);
  const detail = detailMatch?.[1]?.split(",")[0] ?? "";

  switch (item.cache_state) {
    case "HIT":
      return detail === "REVALIDATED"
        ? "Validated with origin metadata."
        : "Served directly from edge cache.";
    case "STALE":
      return "Served stale while the edge refreshes the object.";
    case "MISS":
      if (detail === "STORE_ERROR") {
        return "Fetched from origin, but the cache write failed.";
      }
      return "Fetched from origin and eligible for caching.";
    case "BYPASS":
      switch (detail) {
        case "request":
          return "Skipped cache because the request was private or otherwise uncacheable.";
        case "site-not-found":
          return "No enabled site matched this host.";
        case "object-too-large":
          return "Streamed from origin because the object exceeded the cacheable size limit.";
        default:
          return "Bypassed edge cache for this request.";
      }
    case "ERROR":
      return "The edge could not satisfy the request.";
    default:
      return item.cache_status || "No cache diagnostics recorded.";
  }
}

export function formatOriginStatus(
  item: Pick<RequestLogItem, "origin_requests" | "origin_status_code" | "status_code">,
) {
  if (!item.origin_requests) {
    return null;
  }
  if (!item.origin_status_code || item.origin_status_code === item.status_code) {
    return null;
  }
  return `origin ${item.origin_status_code}`;
}
