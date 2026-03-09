import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { LoaderCircle, LockKeyhole, ShieldCheck } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { getAuthSession, setAuthSession } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type LoginSearch = {
  redirect?: string;
};

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  beforeLoad: () => {
    if (getAuthSession()) {
      throw redirect({ to: "/" });
    }
  },
  component: LoginPage,
});

function LoginPage() {
  const search = Route.useSearch();
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    try {
      const session = await api.login(username, password);
      setAuthSession(session);
      toast.success("Signed in");
      await navigate({
        to: search.redirect || "/",
      });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to sign in");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="grid min-h-screen bg-muted/30 lg:grid-cols-[1.15fr_0.85fr]">
      <div className="hidden border-r bg-gradient-to-br from-sidebar via-sidebar/95 to-background lg:flex lg:flex-col lg:justify-between lg:p-10">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-sm">
            <ShieldCheck className="size-5" />
          </div>
          <div>
            <p className="text-sm font-semibold tracking-[0.18em] text-muted-foreground uppercase">
              TinyCDN
            </p>
            <h1 className="text-xl font-semibold tracking-[-0.04em]">Admin control plane</h1>
          </div>
        </div>
        <div className="max-w-lg space-y-5">
          <p className="text-xs font-medium uppercase tracking-[0.26em] text-muted-foreground">
            Secure access
          </p>
          <h2 className="text-4xl font-semibold tracking-[-0.05em] text-foreground">
            Operate edge caching, rules, purge, logs, and analytics from one protected workspace.
          </h2>
          <p className="max-w-md text-base leading-7 text-muted-foreground">
            Sign in with the configured admin credentials to manage sites, inspect traffic, and
            operate the TinyCDN edge safely.
          </p>
          <div className="grid gap-3 pt-4 text-sm text-muted-foreground">
            <div className="rounded-2xl border bg-background/80 p-4">
              Bearer JWT protects the Admin API without adding a user database.
            </div>
            <div className="rounded-2xl border bg-background/80 p-4">
              Analytics, logs, purge, and rules all require an authenticated operator.
            </div>
          </div>
        </div>
      </div>

      <div className="flex items-center justify-center p-6 lg:p-10">
        <Card className="w-full max-w-md border-border/70 shadow-sm">
          <CardHeader className="space-y-2">
            <div className="flex items-center gap-3 lg:hidden">
              <div className="flex size-10 items-center justify-center rounded-2xl bg-primary text-primary-foreground">
                <ShieldCheck className="size-5" />
              </div>
              <div>
                <p className="text-sm font-semibold tracking-[0.18em] text-muted-foreground uppercase">
                  TinyCDN
                </p>
                <p className="font-medium">Admin control plane</p>
              </div>
            </div>
            <CardTitle className="text-2xl tracking-[-0.04em]">Sign in</CardTitle>
            <CardDescription>
              Use the configured administrator credentials to unlock the dashboard.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="grid gap-5" onSubmit={handleSubmit}>
              <div className="grid gap-2">
                <Label htmlFor="username">Username</Label>
                <Input
                  id="username"
                  autoComplete="username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  placeholder="admin"
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="Enter your password"
                  required
                />
              </div>
              <Button type="submit" className="w-full" disabled={submitting}>
                {submitting ? (
                  <>
                    <LoaderCircle className="size-4 animate-spin" />
                    Signing in
                  </>
                ) : (
                  <>
                    <LockKeyhole className="size-4" />
                    Sign in
                  </>
                )}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
