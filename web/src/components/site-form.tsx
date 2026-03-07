import { useEffect, useState } from "react";

import type { SiteInput } from "@/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
          upstream_host: form.upstream_host.trim(),
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

      <div className="grid gap-2">
        <Label htmlFor="site-upstream-host-mode">Origin request host</Label>
        <Select
          value={form.upstream_host_mode}
          onValueChange={(value) =>
            setForm((current) => ({
              ...current,
              upstream_host_mode: value as SiteInput["upstream_host_mode"],
              upstream_host: value === "custom" ? current.upstream_host : "",
            }))
          }
        >
          <SelectTrigger id="site-upstream-host-mode" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="follow_origin">Follow origin URL host</SelectItem>
            <SelectItem value="follow_request">Follow incoming request host</SelectItem>
            <SelectItem value="custom">Custom host</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {form.upstream_host_mode === "custom" ? (
        <div className="grid gap-2">
          <Label htmlFor="site-upstream-host">Custom origin host</Label>
          <Input
            id="site-upstream-host"
            value={form.upstream_host}
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                upstream_host: event.target.value,
              }))
            }
            placeholder="a.com"
          />
        </div>
      ) : null}

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

      <div className="flex justify-end">
        <Button type="submit" disabled={saving}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
