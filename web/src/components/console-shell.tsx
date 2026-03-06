import type { ReactNode } from "react";

import { ConsoleSidebar } from "@/components/console-sidebar";
import { ModeToggle } from "@/components/mode-toggle";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Separator } from "@/components/ui/separator";

export function ConsoleShell({ children, banner }: { children: ReactNode; banner?: ReactNode }) {
  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "20rem",
        } as React.CSSProperties
      }
    >
      <ConsoleSidebar />
      <SidebarInset className="@container/main overflow-hidden">
        <header className="flex h-14 shrink-0 items-center gap-2 rounded-t-xl border-b bg-background px-4 lg:px-6">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="h-4" />
          <div className="text-sm font-medium text-muted-foreground">TinyCDN dashboard</div>
          <div className="ml-auto">
            <ModeToggle />
          </div>
        </header>
        {banner}
        <div className="flex flex-1 flex-col gap-4 md:gap-6">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  );
}
