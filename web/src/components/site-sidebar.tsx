import { Globe, Radar, Shield, SlidersHorizontal, Undo, Workflow } from "lucide-react";
import { Link, useRouterState } from "@tanstack/react-router";

import type { Site } from "@/types";
import { NavUser } from "@/components/site-nav-user";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";

export function SiteSidebar({ site }: { site: Site }) {
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });

  const items = [
    {
      title: "Overview",
      to: "/sites/$siteId",
      href: `/sites/${site.id}`,
      icon: Radar,
    },
    {
      title: "Rules",
      to: "/sites/$siteId/rules",
      href: `/sites/${site.id}/rules`,
      icon: Workflow,
    },
    {
      title: "Origin",
      to: "/sites/$siteId/origin",
      href: `/sites/${site.id}/origin`,
      icon: Globe,
    },
    {
      title: "Settings",
      to: "/sites/$siteId/settings",
      href: `/sites/${site.id}/settings`,
      icon: SlidersHorizontal,
    },
    {
      title: "WAF",
      to: "/sites/$siteId/waf",
      href: `/sites/${site.id}/waf`,
      icon: Shield,
    },
  ] as const;

  return (
    <Sidebar variant="inset" collapsible="offcanvas">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg">
              <Link to="/sites">
                <div className="flex size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                  <Undo className="size-4" />
                </div>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="font-medium">Back to sites</span>
                  <span className="text-xs text-muted-foreground">All zones</span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <div className="px-2 pt-2">
          <div className="rounded-xl border bg-background/70 p-3">
            <p className="truncate text-sm font-semibold">{site.name}</p>
            <p className="truncate text-xs text-muted-foreground">{site.hosts.join(", ")}</p>
          </div>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Site</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {items.map((item) => (
                <SidebarMenuItem key={item.title}>
                  <SidebarMenuButton asChild isActive={pathname === item.href} tooltip={item.title}>
                    <Link to={item.to} params={{ siteId: site.id }}>
                      <item.icon />
                      <span>{item.title}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <NavUser
          user={{
            name: site.name,
            email: site.upstream.url,
            avatar: "",
          }}
        />
      </SidebarFooter>
    </Sidebar>
  );
}
