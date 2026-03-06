import type { ReactNode } from "react";

import type { Site } from "@/types";
import { ModeToggle } from "@/components/mode-toggle";
import { SiteSidebar } from "@/components/site-sidebar";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Badge } from "@/components/ui/badge";
import { Link } from "@tanstack/react-router";

export function SiteShell({
  site,
  section,
  children,
}: {
  site: Site;
  section: string;
  children: ReactNode;
}) {
  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "18rem",
        } as React.CSSProperties
      }
    >
      <SiteSidebar site={site} />
      <SidebarInset className="@container/main overflow-hidden">
        <header className="flex h-14 shrink-0 items-center gap-2 rounded-t-xl border-b bg-background px-4 lg:px-6">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="h-4" />
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink asChild>
                  <Link to="/sites">Sites</Link>
                </BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbLink asChild>
                  <Link to="/sites/$siteId" params={{ siteId: site.id }}>
                    {site.name}
                  </Link>
                </BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>{section}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          <div className="ml-auto flex items-center gap-2">
            <Badge variant={site.enabled ? "outline" : "secondary"}>
              {site.enabled ? "Enabled" : "Disabled"}
            </Badge>
            <ModeToggle />
          </div>
        </header>
        <div className="flex flex-1 flex-col gap-4 md:gap-6">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  );
}
