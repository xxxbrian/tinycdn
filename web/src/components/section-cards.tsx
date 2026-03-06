import {
  IconArrowUpRight,
  IconCircleCheckFilled,
  IconRefresh,
  IconRoute,
} from "@tabler/icons-react";

import { Badge } from "#/components/ui/badge";
import {
  Card,
  CardAction,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "#/components/ui/card";

export function SectionCards({
  stats,
}: {
  stats: {
    totalSites: number;
    activeSites: number;
    totalHosts: number;
    totalRules: number;
    optimisticSites: number;
  };
}) {
  return (
    <div className="grid grid-cols-1 gap-4 px-4 *:data-[slot=card]:bg-gradient-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card *:data-[slot=card]:shadow-xs lg:px-6 @xl/main:grid-cols-2 @5xl/main:grid-cols-4 dark:*:data-[slot=card]:bg-card">
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>Configured Sites</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {stats.totalSites}
          </CardTitle>
          <CardAction>
            <Badge variant="outline">
              <IconCircleCheckFilled className="fill-emerald-500 text-emerald-500" />
              {stats.activeSites} active
            </Badge>
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">
            Host ownership compiled cleanly <IconArrowUpRight className="size-4" />
          </div>
          <div className="text-muted-foreground">Each site owns one or more host bindings</div>
        </CardFooter>
      </Card>
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>Host Bindings</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {stats.totalHosts}
          </CardTitle>
          <CardAction>
            <Badge variant="outline">
              <IconRoute />
              routed
            </Badge>
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">
            Matched before rule evaluation <IconArrowUpRight className="size-4" />
          </div>
          <div className="text-muted-foreground">Unknown hosts fall through before proxying</div>
        </CardFooter>
      </Card>
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>Ordered Rules</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {stats.totalRules}
          </CardTitle>
          <CardAction>
            <Badge variant="outline">
              <IconRoute />
              pipeline
            </Badge>
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">
            Catch-all defaults stay last <IconArrowUpRight className="size-4" />
          </div>
          <div className="text-muted-foreground">Custom rules remain top-down and explicit</div>
        </CardFooter>
      </Card>
      <Card className="@container/card">
        <CardHeader>
          <CardDescription>Optimistic Refresh</CardDescription>
          <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
            {stats.optimisticSites}
          </CardTitle>
          <CardAction>
            <Badge variant="outline">
              <IconRefresh />
              site-level
            </Badge>
          </CardAction>
        </CardHeader>
        <CardFooter className="flex-col items-start gap-1.5 text-sm">
          <div className="line-clamp-1 flex gap-2 font-medium">
            Stale refresh stays coarse on purpose <IconArrowUpRight className="size-4" />
          </div>
          <div className="text-muted-foreground">Rule-level SWR was removed from the MVP model</div>
        </CardFooter>
      </Card>
    </div>
  );
}
