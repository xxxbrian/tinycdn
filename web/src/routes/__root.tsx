import { Outlet, createRootRoute } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { Toaster } from "@/components/ui/sonner";

import "../styles.css";

export const Route = createRootRoute({
  component: RootComponent,
  errorComponent: ({ error }) => {
    const message = error.message || "An unexpected error interrupted this route.";
    const hint =
      message.includes("Expected JSON response") ||
      message.includes("Expected JSON but received") ||
      message.includes("Failed to fetch")
        ? "The frontend could not reach the Admin API cleanly. If you are using `pnpm --dir web dev`, keep TinyCDN running on :8787 so the Vite proxy can forward `/api` requests."
        : null;

    return (
      <div className="flex min-h-screen items-center justify-center bg-background p-6">
        <div className="w-full max-w-xl rounded-2xl border bg-card p-6 shadow-sm">
          <p className="text-xs font-medium uppercase tracking-[0.24em] text-muted-foreground">
            TinyCDN
          </p>
          <h1 className="mt-3 text-2xl font-semibold tracking-[-0.04em]">Dashboard error</h1>
          <p className="mt-2 text-sm text-muted-foreground">{message}</p>
          {hint ? (
            <div className="mt-4 rounded-xl border border-dashed bg-muted/40 p-4 text-sm text-muted-foreground">
              {hint}
            </div>
          ) : null}
        </div>
      </div>
    );
  },
});

function RootComponent() {
  return (
    <>
      <Outlet />
      <Toaster richColors position="top-right" />
      {import.meta.env.DEV ? (
        <TanStackDevtools
          config={{
            position: "bottom-right",
          }}
          plugins={[
            {
              name: "TanStack Router",
              render: <TanStackRouterDevtoolsPanel />,
            },
          ]}
        />
      ) : null}
    </>
  );
}
