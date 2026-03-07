import { BarChart3 } from "lucide-react";

import { formatBytes, formatCompactNumber, formatPercent } from "@/lib/telemetry";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export function BreakdownCard({
  title,
  description,
  items,
  emptyLabel,
}: {
  title: string;
  description: string;
  items: Array<{
    key: string;
    label?: string;
    requests: number;
    edge_bytes?: number;
    hit_ratio?: number;
  }>;
  emptyLabel: string;
}) {
  const total = items.reduce((sum, item) => sum + item.requests, 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3">
        {items.length === 0 ? (
          <div className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
            {emptyLabel}
          </div>
        ) : (
          items.map((item) => (
            <div key={item.key} className="rounded-xl border p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium text-foreground">
                    {item.label ?? item.key}
                  </div>
                  <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    <span>{formatCompactNumber(item.requests)} requests</span>
                    {item.edge_bytes !== undefined ? (
                      <span>{formatBytes(item.edge_bytes)}</span>
                    ) : null}
                    {item.hit_ratio !== undefined ? (
                      <span>{formatPercent(item.hit_ratio)}</span>
                    ) : null}
                  </div>
                </div>
                <BarChart3 className="mt-0.5 size-4 shrink-0 text-primary/70" />
              </div>
              <div className="mt-3 h-2 rounded-full bg-muted/80 ring-1 ring-border/70">
                <div
                  className="h-2 rounded-full bg-primary/75"
                  style={{
                    width: `${total === 0 ? 0 : (item.requests / total) * 100}%`,
                  }}
                />
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}
