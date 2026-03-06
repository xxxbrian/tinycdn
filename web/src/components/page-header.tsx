import type { ReactNode } from "react";

import { Badge } from "@/components/ui/badge";

export function PageHeader(props: {
  eyebrow?: string;
  title: string;
  description: string;
  badge?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 border-b px-4 py-5 lg:px-6 lg:py-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          {props.eyebrow ? (
            <p className="text-xs font-medium uppercase tracking-[0.24em] text-muted-foreground">
              {props.eyebrow}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-3xl font-semibold tracking-[-0.04em]">{props.title}</h1>
            {props.badge ? <Badge variant="outline">{props.badge}</Badge> : null}
          </div>
          <p className="max-w-3xl text-sm text-muted-foreground lg:text-base">
            {props.description}
          </p>
        </div>
        {props.actions ? (
          <div className="flex flex-wrap items-center gap-2">{props.actions}</div>
        ) : null}
      </div>
    </div>
  );
}
