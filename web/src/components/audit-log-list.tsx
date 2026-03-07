import type { AuditLogItem } from "@/types";

import { formatDateTime } from "@/lib/telemetry";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export function AuditLogList({
  title,
  description,
  items,
}: {
  title: string;
  description: string;
  items: AuditLogItem[];
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3">
        {items.length === 0 ? (
          <div className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
            No audit events yet.
          </div>
        ) : (
          items.map((item) => (
            <div key={`${item.id}-${item.request_id}`} className="rounded-xl border p-3">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <div className="font-medium">{item.summary}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {item.action} • {item.resource_type} • {item.resource_id}
                  </div>
                </div>
                <Badge variant="outline">{item.status_code}</Badge>
              </div>
              <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <span>{formatDateTime(item.timestamp)}</span>
                <span>{item.method}</span>
                <span>{item.remote_ip}</span>
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}
