import { Activity, CloudFog, FolderKanban, Gauge, Logs } from "lucide-react";
import { Link, useRouterState } from "@tanstack/react-router";

import { NavUser } from "@/components/nav-user";
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

const platformItems = [
  { title: "Overview", to: "/", icon: Gauge },
  { title: "Sites", to: "/sites", icon: FolderKanban },
  { title: "Logs", to: "/logs", icon: Logs },
  { title: "Analytics", to: "/analytics", icon: Activity },
] as const;

export function ConsoleSidebar() {
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });

  return (
    <Sidebar collapsible="offcanvas" variant="inset">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg">
              <Link to="/">
                <div className="flex size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                  <CloudFog className="size-4" />
                </div>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="font-semibold">TinyCDN</span>
                  <span className="text-xs text-muted-foreground">Control plane</span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Workspace</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {platformItems.map((item) => {
                const active =
                  item.to === "/" ? pathname === item.to : pathname.startsWith(item.to);
                return (
                  <SidebarMenuItem key={item.title}>
                    <SidebarMenuButton asChild isActive={active} tooltip={item.title}>
                      <Link to={item.to}>
                        <item.icon />
                        <span>{item.title}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <NavUser fallbackUser={{ name: "TinyCDN Admin", email: "owner" }} />
      </SidebarFooter>
    </Sidebar>
  );
}
