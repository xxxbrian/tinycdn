"use client";

import { ShieldCheck, LogOut, UserCircle2 } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { useState } from "react";

import { api } from "@/lib/api";
import { clearAuthSession, useAuthSession } from "@/lib/auth";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

type FallbackUser = {
  name: string;
  email: string;
};

export function NavUser({ fallbackUser }: { fallbackUser?: FallbackUser }) {
  const session = useAuthSession();
  const navigate = useNavigate();
  const { isMobile } = useSidebar();
  const [loggingOut, setLoggingOut] = useState(false);

  const displayName = session?.user.username || fallbackUser?.name || "Admin";
  const displayEmail = session?.user.role || fallbackUser?.email || "owner";
  const initials = displayName.slice(0, 2).toUpperCase();

  async function handleLogout() {
    setLoggingOut(true);
    try {
      await api.logout();
    } catch {
      // Token revocation is stateless for now; local cleanup is the important part.
    } finally {
      clearAuthSession();
      toast.success("Signed out");
      await navigate({ to: "/login" });
      setLoggingOut(false);
    }
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <Avatar className="h-8 w-8 rounded-lg">
                <AvatarFallback className="rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                  {initials}
                </AvatarFallback>
              </Avatar>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">{displayName}</span>
                <span className="truncate text-xs text-muted-foreground">{displayEmail}</span>
              </div>
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuLabel className="p-0 font-normal">
              <div className="flex items-center gap-2 px-2 py-2 text-left text-sm">
                <Avatar className="h-8 w-8 rounded-lg">
                  <AvatarFallback className="rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                    {initials}
                  </AvatarFallback>
                </Avatar>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="truncate font-medium">{displayName}</span>
                  <span className="truncate text-xs text-muted-foreground">{displayEmail}</span>
                </div>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem disabled>
              <UserCircle2 className="size-4" />
              Single-user admin mode
            </DropdownMenuItem>
            <DropdownMenuItem disabled>
              <ShieldCheck className="size-4" />
              Role: {session?.user.role || "owner"}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem disabled={loggingOut} onClick={handleLogout}>
              <LogOut className="size-4" />
              {loggingOut ? "Signing out..." : "Log out"}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
