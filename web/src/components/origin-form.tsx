import { useEffect, useState } from "react";

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

type UpstreamHostMode = "follow_origin" | "follow_request" | "custom";

export function OriginForm({
  initialUrl,
  initialHostMode,
  initialHost,
  saving,
  onSubmit,
}: {
  initialUrl: string;
  initialHostMode: UpstreamHostMode;
  initialHost: string;
  saving: boolean;
  onSubmit: (value: {
    url: string;
    hostMode: UpstreamHostMode;
    host: string;
  }) => Promise<void> | void;
}) {
  const [url, setUrl] = useState(initialUrl);
  const [hostMode, setHostMode] = useState<UpstreamHostMode>(initialHostMode);
  const [host, setHost] = useState(initialHost);

  useEffect(() => {
    setUrl(initialUrl);
    setHostMode(initialHostMode);
    setHost(initialHost);
  }, [initialHost, initialHostMode, initialUrl]);

  return (
    <form
      className="grid gap-4"
      onSubmit={async (event) => {
        event.preventDefault();
        await onSubmit({
          url: url.trim(),
          hostMode,
          host: host.trim(),
        });
      }}
    >
      <div className="grid gap-2">
        <Label htmlFor="origin-url">Upstream URL</Label>
        <Input
          id="origin-url"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          placeholder="https://origin.internal"
        />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="origin-host-mode">Origin request host</Label>
        <Select
          value={hostMode}
          onValueChange={(value) => {
            setHostMode(value as UpstreamHostMode);
            if (value !== "custom") {
              setHost("");
            }
          }}
        >
          <SelectTrigger id="origin-host-mode" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="follow_origin">Follow origin URL host</SelectItem>
            <SelectItem value="follow_request">Follow incoming request host</SelectItem>
            <SelectItem value="custom">Custom host</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {hostMode === "custom" ? (
        <div className="grid gap-2">
          <Label htmlFor="origin-host">Custom origin host</Label>
          <Input
            id="origin-host"
            value={host}
            onChange={(event) => setHost(event.target.value)}
            placeholder="a.com"
          />
        </div>
      ) : null}
      <div className="flex justify-end">
        <Button type="submit" disabled={saving}>
          Save origin
        </Button>
      </div>
    </form>
  );
}
