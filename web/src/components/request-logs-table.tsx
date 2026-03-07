import type { RequestLogItem } from "@/types";

import { formatBytes, formatDateTime, formatDuration, formatPath } from "@/lib/telemetry";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function RequestLogsTable({
  title,
  description,
  items,
}: {
  title: string;
  description: string;
  items: RequestLogItem[];
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>When</TableHead>
              <TableHead>Request</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Cache</TableHead>
              <TableHead>Latency</TableHead>
              <TableHead>Bytes</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                  No requests matched the current filters.
                </TableCell>
              </TableRow>
            ) : (
              items.map((item) => (
                <TableRow key={`${item.id}-${item.request_id}`}>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    {formatDateTime(item.timestamp)}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Badge variant="outline">{item.method}</Badge>
                      <span className="truncate font-medium">{item.host}</span>
                    </div>
                    <div className="mt-1 truncate text-xs text-muted-foreground">
                      {formatPath(item)}
                    </div>
                    <div className="mt-1 truncate text-[11px] text-muted-foreground">
                      {item.request_id}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="font-medium">{item.status_code}</div>
                    <div className="text-xs text-muted-foreground">
                      {item.origin_status_code || "no origin"}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={item.cache_state === "HIT" ? "outline" : "secondary"}>
                      {item.cache_state || "n/a"}
                    </Badge>
                    <div className="mt-1 truncate text-xs text-muted-foreground">
                      {item.cache_status}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="font-medium">{formatDuration(item.total_duration_ms)}</div>
                    <div className="text-xs text-muted-foreground">
                      origin{" "}
                      {item.origin_duration_ms ? formatDuration(item.origin_duration_ms) : "n/a"}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-sm">
                    {formatBytes(item.response_bytes)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
