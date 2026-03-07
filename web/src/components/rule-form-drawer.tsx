import { useEffect, useMemo, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import type { MatchField, MatchLogical, Rule } from "@/types";
import {
  allMethods,
  blankRule,
  createBlankCondition,
  fieldOptions,
  formStateToRule,
  hasMatchConditions,
  isConditionComplete,
  needsHeaderName,
  needsValue,
  operatorOptionsForField,
  ruleToFormState,
  supportsCacheTiming,
  type RuleConditionState,
  type RuleFormState,
} from "@/lib/rule-helpers";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";

const defaultState = ruleToFormState(blankRule);

function defaultOperator(field: MatchField) {
  return operatorOptionsForField(field)[0].value;
}

function splitConditionGroups(conditions: RuleConditionState[]) {
  const groups: RuleConditionState[][] = [];
  for (const condition of conditions) {
    if (groups.length === 0 || condition.logical === "or") {
      groups.push([condition]);
      continue;
    }
    groups[groups.length - 1].push(condition);
  }
  return groups;
}

export function RuleFormDrawer({
  open,
  onOpenChange,
  mode,
  rule,
  saving,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mode: "create" | "edit";
  rule?: Rule | null;
  saving: boolean;
  onSubmit: (rule: Rule) => Promise<void> | void;
}) {
  const [state, setState] = useState<RuleFormState>(defaultState);

  useEffect(() => {
    if (rule) {
      const nextState = ruleToFormState(rule);
      if (!rule.system && nextState.conditions.length === 0) {
        nextState.conditions = [createBlankCondition()];
      }
      setState(nextState);
      return;
    }
    setState({
      ...defaultState,
      conditions: [createBlankCondition()],
    });
  }, [rule, open]);

  const currentRule = rule ?? blankRule;
  const hasNonSafeMethods = useMemo(
    () =>
      state.conditions.some(
        (condition) =>
          condition.field === "method" &&
          ["OPTIONS", "POST", "PUT", "PATCH", "DELETE"].includes(condition.value.toUpperCase()),
      ),
    [state.conditions],
  );
  const conditionGroups = useMemo(() => splitConditionGroups(state.conditions), [state.conditions]);

  function setCondition(
    conditionId: string,
    updater: (condition: RuleConditionState) => RuleConditionState,
  ) {
    setState((current) => ({
      ...current,
      conditions: current.conditions.map((condition) =>
        condition.id === conditionId ? updater(condition) : condition,
      ),
    }));
  }

  function changeField(conditionId: string, field: MatchField) {
    setCondition(conditionId, (condition) => ({
      ...condition,
      field,
      operator: defaultOperator(field),
      name: field === "request_header" ? condition.name : "",
      value: "",
    }));
  }

  function insertCondition(afterConditionId: string, logical: MatchLogical) {
    setState((current) => {
      const index = current.conditions.findIndex((condition) => condition.id === afterConditionId);
      if (index < 0) {
        return current;
      }

      const nextConditions = [...current.conditions];
      nextConditions.splice(index + 1, 0, createBlankCondition(logical));
      return {
        ...current,
        conditions: nextConditions,
      };
    });
  }

  function removeCondition(conditionId: string) {
    setState((current) => {
      const index = current.conditions.findIndex((condition) => condition.id === conditionId);
      if (index < 0) {
        return current;
      }

      const nextConditions = current.conditions.filter((condition) => condition.id !== conditionId);

      if (nextConditions.length > 0) {
        nextConditions[0] = { ...nextConditions[0], logical: undefined };
        if (index > 0 && current.conditions[index]?.logical === "or" && nextConditions[index]) {
          nextConditions[index] = {
            ...nextConditions[index],
            logical: "or",
          };
        }
      }

      return {
        ...current,
        conditions: nextConditions.length > 0 ? nextConditions : [createBlankCondition()],
      };
    });
  }

  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction="right">
      <DrawerContent className="ml-auto h-full rounded-l-2xl rounded-r-none data-[vaul-drawer-direction=right]:w-[min(80rem,100vw)] data-[vaul-drawer-direction=right]:sm:max-w-5xl">
        <DrawerHeader className="text-left">
          <DrawerTitle>{mode === "create" ? "Create rule" : "Edit rule"}</DrawerTitle>
          <DrawerDescription>
            Build request match conditions first, then describe how TinyCDN should handle matching
            traffic.
          </DrawerDescription>
        </DrawerHeader>

        <div className="grid gap-6 overflow-y-auto px-4 pb-4 lg:px-6 lg:pb-6">
          <section className="grid gap-4 rounded-xl border p-4 lg:p-5">
            <div className="grid gap-1">
              <div className="flex items-center gap-2">
                <h3 className="font-semibold">Rule basics</h3>
                {currentRule.system ? <Badge variant="secondary">System</Badge> : null}
              </div>
              <p className="text-sm text-muted-foreground">
                Name the rule and control whether it participates in the ordered pipeline.
              </p>
            </div>

            <div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
              <div className="grid gap-2">
                <Label htmlFor="rule-name">Rule name</Label>
                <Input
                  id="rule-name"
                  value={state.name}
                  onChange={(event) =>
                    setState((current) => ({ ...current, name: event.target.value }))
                  }
                />
              </div>
              <label className="flex items-start justify-between gap-4 rounded-lg border p-3 text-sm">
                <div>
                  <p className="font-medium">Enable rule</p>
                  <p className="text-muted-foreground">
                    Disabled custom rules stay in the ordered pipeline but do not execute.
                  </p>
                </div>
                <Switch
                  checked={currentRule.system ? true : state.enabled}
                  disabled={currentRule.system}
                  onCheckedChange={(checked) =>
                    setState((current) => ({
                      ...current,
                      enabled: checked,
                    }))
                  }
                  aria-label="Enable rule"
                />
              </label>
            </div>
          </section>

          <section className="grid gap-4 rounded-xl border p-4 lg:p-5">
            <div className="grid gap-1">
              <h3 className="font-semibold">Match requests</h3>
              <p className="text-sm text-muted-foreground">
                Add only the conditions you need. Conditions inside the same block are combined with
                AND; separate blocks are combined with OR.
              </p>
            </div>

            {currentRule.system ? (
              <div className="rounded-lg border border-dashed bg-muted/30 px-4 py-3 text-sm text-muted-foreground">
                The system catch-all rule always matches every request and therefore does not use
                custom conditions.
              </div>
            ) : (
              <div className="grid gap-4">
                <div className="hidden grid-cols-[180px_180px_minmax(0,1fr)_auto] gap-3 px-1 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground lg:grid">
                  <span>Field</span>
                  <span>Operator</span>
                  <span>Value</span>
                  <span />
                </div>

                {conditionGroups.map((group, groupIndex) => (
                  <div key={group[0]?.id ?? groupIndex} className="grid gap-3">
                    {groupIndex > 0 ? (
                      <div className="flex items-center justify-center">
                        <div className="rounded-full border bg-background px-3 py-1 text-xs font-semibold tracking-[0.2em] text-muted-foreground">
                          OR
                        </div>
                      </div>
                    ) : null}

                    <div className="grid gap-3 rounded-xl border p-3">
                      {group.map((condition, index) => {
                        const operatorOptions = operatorOptionsForField(condition.field);

                        return (
                          <div key={condition.id} className="grid gap-3">
                            {index > 0 ? (
                              <div className="flex items-center gap-2 pl-1">
                                <div className="rounded-full border bg-muted/30 px-3 py-1 text-xs font-semibold tracking-[0.2em] text-muted-foreground">
                                  AND
                                </div>
                              </div>
                            ) : null}

                            <div className="grid gap-3 lg:grid-cols-[180px_180px_minmax(0,1fr)_auto] lg:items-start">
                              <div className="grid gap-2">
                                <Label className="lg:sr-only">Field</Label>
                                <Select
                                  value={condition.field}
                                  onValueChange={(value) =>
                                    changeField(condition.id, value as MatchField)
                                  }
                                >
                                  <SelectTrigger>
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {fieldOptions.map((option) => (
                                      <SelectItem key={option.value} value={option.value}>
                                        {option.label}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                              </div>

                              <div className="grid gap-2">
                                <Label className="lg:sr-only">Operator</Label>
                                <Select
                                  value={condition.operator}
                                  onValueChange={(value) =>
                                    setCondition(condition.id, (current) => ({
                                      ...current,
                                      operator: value as RuleConditionState["operator"],
                                      value: "",
                                    }))
                                  }
                                >
                                  <SelectTrigger>
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {operatorOptions.map((option) => (
                                      <SelectItem key={option.value} value={option.value}>
                                        {option.label}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                              </div>

                              <div className="grid gap-2">
                                <Label className="lg:sr-only">Value</Label>
                                {needsHeaderName(condition) ? (
                                  <div className="grid gap-2 md:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
                                    <Input
                                      value={condition.name}
                                      onChange={(event) =>
                                        setCondition(condition.id, (current) => ({
                                          ...current,
                                          name: event.target.value,
                                        }))
                                      }
                                      placeholder="Header name"
                                    />
                                    {needsValue(condition) ? (
                                      <Input
                                        value={condition.value}
                                        onChange={(event) =>
                                          setCondition(condition.id, (current) => ({
                                            ...current,
                                            value: event.target.value,
                                          }))
                                        }
                                        placeholder="Header value"
                                      />
                                    ) : (
                                      <div className="flex h-10 items-center rounded-md border bg-muted/30 px-3 text-sm text-muted-foreground">
                                        No value needed
                                      </div>
                                    )}
                                  </div>
                                ) : condition.field === "method" ? (
                                  <Select
                                    value={condition.value}
                                    onValueChange={(value) =>
                                      setCondition(condition.id, (current) => ({
                                        ...current,
                                        value,
                                      }))
                                    }
                                  >
                                    <SelectTrigger>
                                      <SelectValue placeholder="Select method" />
                                    </SelectTrigger>
                                    <SelectContent>
                                      {allMethods.map((method) => (
                                        <SelectItem key={method} value={method}>
                                          {method}
                                        </SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                ) : (
                                  <Input
                                    value={condition.value}
                                    onChange={(event) =>
                                      setCondition(condition.id, (current) => ({
                                        ...current,
                                        value: event.target.value,
                                      }))
                                    }
                                    placeholder={
                                      condition.field === "hostname"
                                        ? "example.com"
                                        : condition.field === "uri_path"
                                          ? condition.operator.includes("glob")
                                            ? "/assets/**"
                                            : "/api/"
                                          : "Value"
                                    }
                                  />
                                )}

                                {condition.field === "method" ? (
                                  <p className="text-xs text-muted-foreground">
                                    Method is a request matcher, not a cache-method allowlist.
                                  </p>
                                ) : null}
                              </div>

                              <div className="flex items-start justify-end">
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon"
                                  onClick={() => removeCondition(condition.id)}
                                  aria-label="Remove condition"
                                >
                                  <Trash2 className="size-4" />
                                </Button>
                              </div>
                            </div>
                          </div>
                        );
                      })}

                      <div className="flex flex-wrap items-center gap-2 border-t pt-3">
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() => insertCondition(group[group.length - 1].id, "and")}
                        >
                          <Plus className="size-4" />
                          Add AND condition
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() => insertCondition(group[group.length - 1].id, "or")}
                        >
                          <Plus className="size-4" />
                          Add OR group
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className="grid gap-4 rounded-xl border p-4 lg:p-5">
            <div className="grid gap-1">
              <h3 className="font-semibold">Cache behavior</h3>
              <p className="text-sm text-muted-foreground">
                Configure how TinyCDN caches matching responses. Normal and optimistic behavior now
                live directly on the rule.
              </p>
            </div>
            <div className="grid gap-4 lg:grid-cols-2">
              <div className="grid gap-2">
                <Label>Cache mode</Label>
                <Select
                  value={state.cacheMode}
                  onValueChange={(value) =>
                    setState((current) => ({
                      ...current,
                      cacheMode: value as Rule["action"]["cache"]["mode"],
                      ttl:
                        value === "force_cache" || value === "override_origin" ? current.ttl : "",
                      staleIfError:
                        value === "force_cache" || value === "override_origin"
                          ? current.staleIfError
                          : "",
                      optimistic: value === "bypass" ? false : current.optimistic,
                    }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="follow_origin">Follow origin</SelectItem>
                    <SelectItem value="bypass">Bypass</SelectItem>
                    <SelectItem value="force_cache">Force cache</SelectItem>
                    <SelectItem value="override_origin">Override origin</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {supportsCacheTiming(state.cacheMode) ? (
                <>
                  <div className="grid gap-2">
                    <Label htmlFor="rule-ttl">TTL</Label>
                    <Input
                      id="rule-ttl"
                      value={state.ttl}
                      onChange={(event) =>
                        setState((current) => ({ ...current, ttl: event.target.value }))
                      }
                      placeholder="1h"
                    />
                  </div>
                  <div className="grid gap-2 lg:col-span-2">
                    <Label htmlFor="stale-if-error">Stale if error</Label>
                    <Input
                      id="stale-if-error"
                      value={state.staleIfError}
                      onChange={(event) =>
                        setState((current) => ({
                          ...current,
                          staleIfError: event.target.value,
                        }))
                      }
                      placeholder="10m"
                    />
                  </div>
                  <label className="flex items-start justify-between gap-4 rounded-lg border p-3 text-sm lg:col-span-2">
                    <div>
                      <p className="font-medium">Optimistic refresh</p>
                      <p className="text-muted-foreground">
                        Serve stale cache on expiry and refresh it in the background.
                      </p>
                    </div>
                    <Switch
                      checked={state.optimistic}
                      onCheckedChange={(checked) =>
                        setState((current) => ({
                          ...current,
                          optimistic: checked,
                        }))
                      }
                      aria-label="Optimistic refresh"
                    />
                  </label>
                </>
              ) : (
                <>
                  <div className="rounded-lg border border-dashed bg-muted/30 px-4 py-3 text-sm text-muted-foreground lg:col-span-2">
                    {state.cacheMode === "follow_origin"
                      ? "Origin cache headers control freshness in follow-origin mode, while optimistic refresh can still tell TinyCDN to serve stale cache during background refresh."
                      : "Bypass mode skips edge caching entirely, so TTL, stale-if-error, and optimistic refresh do not apply here."}
                  </div>
                  {state.cacheMode === "follow_origin" ? (
                    <label className="flex items-start justify-between gap-4 rounded-lg border p-3 text-sm lg:col-span-2">
                      <div>
                        <p className="font-medium">Optimistic refresh</p>
                        <p className="text-muted-foreground">
                          Serve stale cache on expiry and refresh it in the background.
                        </p>
                      </div>
                      <Switch
                        checked={state.optimistic}
                        onCheckedChange={(checked) =>
                          setState((current) => ({
                            ...current,
                            optimistic: checked,
                          }))
                        }
                        aria-label="Optimistic refresh"
                      />
                    </label>
                  ) : null}
                </>
              )}
            </div>
            {hasNonSafeMethods ? (
              <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
                Non-GET/HEAD methods are selected in at least one clause. That is valid for request
                matching, but it is usually only appropriate when the rule is intentionally handling
                non-cacheable traffic.
              </div>
            ) : null}
          </section>
        </div>

        <DrawerFooter className="border-t">
          <div className="flex w-full items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">Rules evaluate in order, top to bottom.</p>
            <div className="flex items-center gap-2">
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button
                disabled={saving}
                onClick={async () => {
                  if (
                    !currentRule.system &&
                    state.conditions.some((condition) => !isConditionComplete(condition))
                  ) {
                    toast.error(
                      "Each condition needs a complete field, operator, and value before saving",
                    );
                    return;
                  }

                  const nextRule = formStateToRule(currentRule, state);
                  if (!hasMatchConditions(nextRule)) {
                    toast.error("Rule needs at least one match condition before saving");
                    return;
                  }
                  await onSubmit(nextRule);
                }}
              >
                {mode === "create" ? "Create rule" : "Save rule"}
              </Button>
            </div>
          </div>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  );
}
