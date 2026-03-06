import { useEffect, useState } from "react";

import type { SiteInput } from "@/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

export function SiteForm({
  initialValue,
  submitLabel,
  saving,
  onSubmit,
}: {
  initialValue: SiteInput;
  submitLabel: string;
  saving: boolean;
  onSubmit: (value: SiteInput) => Promise<void> | void;
}) {
  const [form, setForm] = useState(initialValue);

  useEffect(() => {
    setForm(initialValue);
  }, [initialValue]);

  return (
    <form
      className="grid gap-5"
      onSubmit={async (event) => {
        event.preventDefault();
        await onSubmit({
          ...form,
          name: form.name.trim(),
          upstream_url: form.upstream_url.trim(),
          hosts: form.hosts.flatMap((value) =>
            value
              .split(/[\n,]/)
              .map((item) => item.trim())
              .filter(Boolean),
          ),
        });
      }}
    >
      <div className="grid gap-2">
        <Label htmlFor="site-name">Display name</Label>
        <Input
          id="site-name"
          value={form.name}
          onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
        />
      </div>

      <div className="grid gap-2">
        <Label htmlFor="site-hosts">Hosts</Label>
        <Textarea
          id="site-hosts"
          value={form.hosts.join("\n")}
          onChange={(event) =>
            setForm((current) => ({
              ...current,
              hosts: event.target.value.split("\n"),
            }))
          }
          placeholder={"example.com\nwww.example.com"}
        />
      </div>

      <div className="grid gap-2">
        <Label htmlFor="site-upstream">Upstream URL</Label>
        <Input
          id="site-upstream"
          value={form.upstream_url}
          onChange={(event) =>
            setForm((current) => ({
              ...current,
              upstream_url: event.target.value,
            }))
          }
          placeholder="https://origin.internal"
        />
      </div>

      <label className="flex items-start justify-between gap-4 rounded-lg border p-3 text-sm">
        <div>
          <p className="font-medium">Enable site</p>
          <p className="text-muted-foreground">
            Disabled sites remain configured but stop serving traffic.
          </p>
        </div>
        <Switch
          checked={form.enabled}
          onCheckedChange={(checked) =>
            setForm((current) => ({
              ...current,
              enabled: checked,
            }))
          }
          aria-label="Enable site"
        />
      </label>

      <label className="flex items-start justify-between gap-4 rounded-lg border p-3 text-sm">
        <div>
          <p className="font-medium">Optimistic refresh</p>
          <p className="text-muted-foreground">
            Serve stale edge cache on expiry and refresh in the background.
          </p>
        </div>
        <Switch
          checked={form.optimistic_refresh}
          onCheckedChange={(checked) =>
            setForm((current) => ({
              ...current,
              optimistic_refresh: checked,
            }))
          }
          aria-label="Optimistic refresh"
        />
      </label>

      <div className="flex justify-end">
        <Button type="submit" disabled={saving}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
