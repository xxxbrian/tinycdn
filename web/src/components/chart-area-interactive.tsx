"use client";

import { Area, AreaChart, CartesianGrid, XAxis } from "recharts";

import type { AnalyticsPeriod } from "@/types";
import { formatSeriesTick } from "@/lib/telemetry";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

const chartConfig = {
  requests: {
    label: "Requests",
    color: "var(--chart-1)",
  },
  cacheHits: {
    label: "Cache hits",
    color: "var(--chart-2)",
  },
  originFetches: {
    label: "Origin fetches",
    color: "var(--primary)",
  },
} satisfies ChartConfig;

export interface TrafficChartPoint {
  bucket: string;
  requests: number;
  cacheHits: number;
  originFetches: number;
}

export function ChartAreaInteractive({
  title,
  description,
  points,
  period,
  onPeriodChange,
  loading,
  showPeriodControls = true,
}: {
  title: string;
  description: string;
  points: TrafficChartPoint[];
  period: AnalyticsPeriod;
  onPeriodChange: (value: AnalyticsPeriod) => void;
  loading?: boolean;
  showPeriodControls?: boolean;
}) {
  return (
    <Card className="@container/card">
      <CardHeader>
        <div>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </div>
        {showPeriodControls ? (
          <CardAction>
            <ToggleGroup
              type="single"
              value={period}
              onValueChange={(value) => {
                if (value) {
                  onPeriodChange(value as AnalyticsPeriod);
                }
              }}
              variant="outline"
              className="hidden *:data-[slot=toggle-group-item]:px-4! @[767px]/card:flex"
            >
              <ToggleGroupItem value="1h">1h</ToggleGroupItem>
              <ToggleGroupItem value="24h">24h</ToggleGroupItem>
              <ToggleGroupItem value="7d">7d</ToggleGroupItem>
              <ToggleGroupItem value="30d">30d</ToggleGroupItem>
              <ToggleGroupItem value="90d">90d</ToggleGroupItem>
            </ToggleGroup>
            <Select
              value={period}
              onValueChange={(value) => onPeriodChange(value as AnalyticsPeriod)}
            >
              <SelectTrigger
                className="flex w-28 **:data-[slot=select-value]:block **:data-[slot=select-value]:truncate @[767px]/card:hidden"
                size="sm"
                aria-label="Select period"
              >
                <SelectValue placeholder="24h" />
              </SelectTrigger>
              <SelectContent className="rounded-xl">
                <SelectItem value="1h" className="rounded-lg">
                  1h
                </SelectItem>
                <SelectItem value="24h" className="rounded-lg">
                  24h
                </SelectItem>
                <SelectItem value="7d" className="rounded-lg">
                  7d
                </SelectItem>
                <SelectItem value="30d" className="rounded-lg">
                  30d
                </SelectItem>
                <SelectItem value="90d" className="rounded-lg">
                  90d
                </SelectItem>
              </SelectContent>
            </Select>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent className="px-2 pt-4 sm:px-6 sm:pt-6">
        {loading ? (
          <Skeleton className="h-[250px] w-full rounded-xl" />
        ) : (
          <ChartContainer config={chartConfig} className="aspect-auto h-[250px] w-full">
            <AreaChart data={points}>
              <defs>
                <linearGradient id="fillRequests" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="var(--color-requests)" stopOpacity={0.18} />
                  <stop offset="95%" stopColor="var(--color-requests)" stopOpacity={0.03} />
                </linearGradient>
                <linearGradient id="fillCacheHits" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="var(--color-cacheHits)" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="var(--color-cacheHits)" stopOpacity={0.06} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="bucket"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={32}
                tickFormatter={(value) => formatSeriesTick(value, period)}
              />
              <ChartTooltip
                cursor={false}
                content={
                  <ChartTooltipContent
                    labelFormatter={(value) =>
                      new Date(value).toLocaleString("en-US", {
                        month: "short",
                        day: "numeric",
                        hour: "numeric",
                        minute: "2-digit",
                      })
                    }
                    indicator="dot"
                  />
                }
              />
              <Area
                dataKey="requests"
                type="monotone"
                fill="url(#fillRequests)"
                stroke="var(--color-requests)"
                strokeWidth={2}
              />
              <Area
                dataKey="cacheHits"
                type="monotone"
                fill="url(#fillCacheHits)"
                stroke="var(--color-cacheHits)"
                strokeWidth={2}
              />
              <Area
                dataKey="originFetches"
                type="monotone"
                stroke="var(--color-originFetches)"
                strokeWidth={2}
                fillOpacity={0}
              />
            </AreaChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  );
}
