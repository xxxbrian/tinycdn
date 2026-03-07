import { useMemo, useState } from "react";
import { LoaderCircle, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { api } from "@/lib/api";
import type { Site } from "@/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";

function uniqueLines(input: string) {
  return Array.from(
    new Set(
      input
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean),
    ),
  );
}

export function CachePurgeSheet({ site }: { site: Site }) {
  const [open, setOpen] = useState(false);
  const [confirmToken, setConfirmToken] = useState("");
  const [armingFullPurge, setArmingFullPurge] = useState(false);
  const [purgingAll, setPurgingAll] = useState(false);
  const [urlText, setURLText] = useState("");
  const [purgingURLs, setPurgingURLs] = useState(false);

  const parsedURLs = useMemo(() => uniqueLines(urlText), [urlText]);
  const canConfirmFullPurge = confirmToken === site.name;

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="outline">
          <Trash2 className="size-4" />
          Purge cache
        </Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-[min(42rem,100vw)] overflow-y-auto sm:max-w-[42rem]">
        <SheetHeader className="border-b px-6 py-5">
          <div className="flex items-center gap-2">
            <SheetTitle>Cache operations</SheetTitle>
            <Badge variant="outline">Destructive</Badge>
          </div>
          <SheetDescription>
            Invalidate cached objects for{" "}
            <span className="font-medium text-foreground">{site.name}</span>. Use URL purge for
            routine updates and reserve full purge for large content shifts or bad cache
            populations.
          </SheetDescription>
        </SheetHeader>

        <div className="grid gap-6 px-6 py-6">
          <section className="grid gap-4 rounded-xl border p-5">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <h2 className="font-medium">Purge by URL</h2>
                <Badge variant="secondary">Recommended</Badge>
              </div>
              <p className="text-sm text-muted-foreground">
                Remove specific cache entries without blowing away the entire site. Enter one
                absolute path or site URL per line.
              </p>
            </div>

            <div className="grid gap-2">
              <Label htmlFor="purge-urls">Paths or URLs</Label>
              <Textarea
                id="purge-urls"
                value={urlText}
                onChange={(event) => setURLText(event.target.value)}
                placeholder={`/thumbnails/hero.jpg\nhttps://${site.hosts[0] ?? "cdn.example.com"}/assets/app.js?rev=42`}
                className="min-h-32 resize-y"
              />
              <p className="text-xs text-muted-foreground">
                Absolute URLs must belong to this site. Accepted hosts: {site.hosts.join(", ")}.
              </p>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="text-sm text-muted-foreground">
                {parsedURLs.length === 0
                  ? "Nothing queued yet."
                  : `${parsedURLs.length} ${parsedURLs.length === 1 ? "target" : "targets"} queued.`}
              </div>
              <Button
                disabled={parsedURLs.length === 0 || purgingURLs}
                onClick={async () => {
                  setPurgingURLs(true);
                  try {
                    const result = await api.purgeCache(site.id, { urls: parsedURLs });
                    toast.success(
                      result.purged === 0
                        ? "No cached objects matched those URLs"
                        : `Purged ${result.purged} cached ${result.purged === 1 ? "record" : "records"}`,
                    );
                    setURLText("");
                  } catch (error) {
                    toast.error(error instanceof Error ? error.message : "Failed to purge cache");
                  } finally {
                    setPurgingURLs(false);
                  }
                }}
              >
                {purgingURLs ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <Trash2 className="size-4" />
                )}
                Purge URLs
              </Button>
            </div>
          </section>

          <section className="grid gap-4 rounded-xl border border-destructive/30 p-5">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <h2 className="font-medium">Purge everything</h2>
                <Badge variant="outline">Use sparingly</Badge>
              </div>
              <p className="text-sm text-muted-foreground">
                Invalidate all currently stored cache objects for this site. New requests will
                refill from the origin and can temporarily increase origin load.
              </p>
            </div>

            {armingFullPurge ? (
              <div className="grid gap-4 rounded-lg bg-destructive/5 p-4">
                <div className="grid gap-2">
                  <Label htmlFor="confirm-full-purge">Type the site name to confirm</Label>
                  <Input
                    id="confirm-full-purge"
                    value={confirmToken}
                    onChange={(event) => setConfirmToken(event.target.value)}
                    placeholder={site.name}
                  />
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    variant="destructive"
                    disabled={!canConfirmFullPurge || purgingAll}
                    onClick={async () => {
                      setPurgingAll(true);
                      try {
                        const result = await api.purgeCache(site.id, { all: true });
                        toast.success(
                          result.purged === 0
                            ? "Site cache was already empty"
                            : `Purged ${result.purged} cached ${result.purged === 1 ? "record" : "records"} for ${site.name}`,
                        );
                        setArmingFullPurge(false);
                        setConfirmToken("");
                      } catch (error) {
                        toast.error(
                          error instanceof Error ? error.message : "Failed to purge cache",
                        );
                      } finally {
                        setPurgingAll(false);
                      }
                    }}
                  >
                    {purgingAll ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : (
                      <Trash2 className="size-4" />
                    )}
                    Confirm full purge
                  </Button>
                  <Button
                    variant="outline"
                    disabled={purgingAll}
                    onClick={() => {
                      setArmingFullPurge(false);
                      setConfirmToken("");
                    }}
                  >
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <Button variant="destructive" onClick={() => setArmingFullPurge(true)}>
                <Trash2 className="size-4" />
                Prepare full purge
              </Button>
            )}
          </section>
        </div>
      </SheetContent>
    </Sheet>
  );
}
