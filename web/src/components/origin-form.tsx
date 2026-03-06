import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function OriginForm({
  initialValue,
  saving,
  onSubmit,
}: {
  initialValue: string;
  saving: boolean;
  onSubmit: (value: string) => Promise<void> | void;
}) {
  const [value, setValue] = useState(initialValue);

  useEffect(() => {
    setValue(initialValue);
  }, [initialValue]);

  return (
    <form
      className="grid gap-4"
      onSubmit={async (event) => {
        event.preventDefault();
        await onSubmit(value.trim());
      }}
    >
      <div className="grid gap-2">
        <Label htmlFor="origin-url">Upstream URL</Label>
        <Input
          id="origin-url"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          placeholder="https://origin.internal"
        />
      </div>
      <div className="flex justify-end">
        <Button type="submit" disabled={saving}>
          Save origin
        </Button>
      </div>
    </form>
  );
}
