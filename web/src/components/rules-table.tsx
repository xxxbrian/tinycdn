import { useMemo, useState } from "react";
import { ChevronDown, ChevronUp, MoreHorizontal, Pencil, Plus, Search, Trash2 } from "lucide-react";

import type { Rule } from "@/types";
import { describeRuleCache, describeRuleMatch } from "@/lib/rule-helpers";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function RulesTable({
  rules,
  onCreate,
  onEdit,
  onDelete,
  onMove,
  onToggle,
}: {
  rules: Rule[];
  onCreate: () => void;
  onEdit: (rule: Rule) => void;
  onDelete: (rule: Rule) => Promise<void> | void;
  onMove: (rule: Rule, direction: "up" | "down") => Promise<void> | void;
  onToggle: (rule: Rule) => Promise<void> | void;
}) {
  const [query, setQuery] = useState("");
  const filteredRules = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) {
      return rules;
    }

    return rules.filter((rule) =>
      [rule.name, describeRuleMatch(rule), describeRuleCache(rule)]
        .join(" ")
        .toLowerCase()
        .includes(needle),
    );
  }, [query, rules]);

  return (
    <div className="rounded-xl border bg-card">
      <div className="flex flex-col gap-4 border-b px-4 py-4 lg:flex-row lg:items-end lg:justify-between lg:px-6">
        <div className="space-y-1">
          <h2 className="font-semibold">Rules</h2>
          <p className="text-sm text-muted-foreground">
            Ordered top to bottom. The last system rule remains the catch-all.
          </p>
        </div>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <div className="relative w-full sm:w-72">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search rules"
              className="pl-9"
            />
          </div>
          <Button onClick={onCreate}>
            <Plus className="size-4" />
            Add rule
          </Button>
        </div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-20">Order</TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Match</TableHead>
            <TableHead>Cache</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="w-28 text-right">Enabled</TableHead>
            <TableHead className="w-24 text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filteredRules.map((rule) => {
            const index = rules.findIndex((item) => item.id === rule.id);
            return (
              <TableRow key={rule.id}>
                <TableCell>
                  <div className="flex items-center gap-1">
                    <span className="font-mono text-xs">{index + 1}</span>
                    {!rule.system ? (
                      <div className="flex items-center">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7"
                          onClick={() => onMove(rule, "up")}
                          disabled={index === 0}
                        >
                          <ChevronUp className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7"
                          onClick={() => onMove(rule, "down")}
                          disabled={index === rules.length - 2}
                        >
                          <ChevronDown className="size-4" />
                        </Button>
                      </div>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{rule.name}</span>
                    {rule.system ? <Badge variant="secondary">System</Badge> : null}
                  </div>
                </TableCell>
                <TableCell className="max-w-sm text-sm text-muted-foreground">
                  {describeRuleMatch(rule)}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {describeRuleCache(rule)}
                </TableCell>
                <TableCell>
                  <Badge variant={rule.enabled ? "outline" : "secondary"}>
                    {rule.enabled ? "Enabled" : "Disabled"}
                  </Badge>
                </TableCell>
                <TableCell>
                  <div className="flex justify-end">
                    {!rule.system ? (
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={() => onToggle(rule)}
                        aria-label={`Toggle ${rule.name}`}
                      />
                    ) : (
                      <Badge variant="secondary">Locked</Badge>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex items-center justify-end gap-1">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon">
                          <MoreHorizontal className="size-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => onEdit(rule)}>
                          <Pencil className="size-4" />
                          Edit
                        </DropdownMenuItem>
                        {!rule.system ? (
                          <DropdownMenuItem
                            onClick={() => onDelete(rule)}
                            className="text-destructive"
                          >
                            <Trash2 className="size-4" />
                            Delete
                          </DropdownMenuItem>
                        ) : null}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
          {filteredRules.length === 0 ? (
            <TableRow>
              <TableCell colSpan={7} className="py-10 text-center text-sm text-muted-foreground">
                No rules match this search.
              </TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>
    </div>
  );
}
