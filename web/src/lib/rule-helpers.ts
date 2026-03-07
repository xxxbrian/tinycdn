import type { MatchClause, MatchField, MatchLogical, MatchOperator, Rule } from "@/types";

export interface RuleConditionState {
  id: string;
  logical?: MatchLogical;
  field: MatchField;
  operator: MatchOperator;
  name: string;
  value: string;
}

export interface RuleFormState {
  name: string;
  enabled: boolean;
  conditions: RuleConditionState[];
  cacheMode: Rule["action"]["cache"]["mode"];
  ttl: string;
  staleIfError: string;
  optimistic: boolean;
}

export const fieldOptions: Array<{ value: MatchField; label: string }> = [
  { value: "hostname", label: "Hostname" },
  { value: "uri_path", label: "URI path" },
  { value: "method", label: "HTTP method" },
  { value: "request_header", label: "Request header" },
];

export const operatorLabels: Record<MatchOperator, string> = {
  equals: "Equals",
  not_equals: "Does not equal",
  contains: "Contains",
  not_contains: "Does not contain",
  starts_with: "Starts with",
  not_starts_with: "Does not start with",
  matches_glob: "Matches glob",
  not_matches_glob: "Does not match glob",
  exists: "Exists",
  not_exists: "Does not exist",
};

export const fieldOperators: Record<MatchField, MatchOperator[]> = {
  hostname: ["equals", "not_equals", "contains", "not_contains", "starts_with", "not_starts_with"],
  uri_path: [
    "starts_with",
    "not_starts_with",
    "matches_glob",
    "not_matches_glob",
    "equals",
    "not_equals",
    "contains",
    "not_contains",
  ],
  method: ["equals", "not_equals"],
  request_header: ["exists", "not_exists", "equals", "not_equals", "contains", "not_contains"],
};

export const allMethods = ["GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE"] as const;

function createConditionId() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }

  return `cond-${Math.random().toString(36).slice(2, 10)}`;
}

function defaultOperatorForField(field: MatchField): MatchOperator {
  return fieldOperators[field][0];
}

export function createBlankCondition(logical?: MatchLogical): RuleConditionState {
  return {
    id: createConditionId(),
    logical,
    field: "hostname",
    operator: defaultOperatorForField("hostname"),
    name: "",
    value: "",
  };
}

export function operatorOptionsForField(field: MatchField) {
  return fieldOperators[field].map((operator) => ({
    value: operator,
    label: operatorLabels[operator],
  }));
}

export function needsHeaderName(condition: Pick<RuleConditionState, "field">) {
  return condition.field === "request_header";
}

export function needsValue(condition: Pick<RuleConditionState, "operator">) {
  return condition.operator !== "exists" && condition.operator !== "not_exists";
}

function clauseToCondition(clause: MatchClause): RuleConditionState {
  return {
    id: createConditionId(),
    logical: clause.logical,
    field: clause.field,
    operator: clause.operator,
    name: clause.name ?? "",
    value: clause.value ?? "",
  };
}

function conditionToClause(condition: RuleConditionState): MatchClause {
  return {
    logical: condition.logical,
    field: condition.field,
    operator: condition.operator,
    name: needsHeaderName(condition) ? condition.name.trim() || undefined : undefined,
    value: needsValue(condition) ? condition.value.trim() || undefined : undefined,
  };
}

function summarizeClause(clause: MatchClause) {
  const operator = operatorLabels[clause.operator];

  switch (clause.field) {
    case "hostname":
      return `Hostname ${operator} ${clause.value || ""}`.trim();
    case "uri_path":
      return `URI path ${operator} ${clause.value ?? ""}`.trim();
    case "method":
      return `Method ${operator} ${clause.value || ""}`.trim();
    case "request_header":
      if (clause.operator === "exists" || clause.operator === "not_exists") {
        return `Header ${clause.name ?? ""} ${operator}`.trim();
      }
      return `Header ${clause.name ?? ""} ${operator} ${clause.value ?? ""}`.trim();
  }
}

export function ruleToFormState(rule: Rule): RuleFormState {
  return {
    name: rule.name,
    enabled: rule.enabled,
    conditions: rule.match.any ? [] : (rule.match.clauses ?? []).map(clauseToCondition),
    cacheMode: rule.action.cache.mode,
    ttl: rule.action.cache.ttl ?? "",
    staleIfError: rule.action.cache.stale_if_error ?? "",
    optimistic: rule.action.cache.optimistic ?? false,
  };
}

export function formStateToRule(rule: Rule, state: RuleFormState): Rule {
  return {
    ...rule,
    name: state.name.trim() || "Untitled Rule",
    enabled: rule.system ? true : state.enabled,
    match: rule.system
      ? { any: true }
      : {
          clauses: state.conditions.map(conditionToClause),
        },
    action: {
      cache: {
        mode: state.cacheMode,
        ttl: state.ttl || undefined,
        stale_if_error: state.staleIfError || undefined,
        optimistic: state.cacheMode === "bypass" ? undefined : state.optimistic || undefined,
      },
    },
  };
}

export function hasMatchConditions(rule: Rule) {
  if (rule.system || rule.match.any) {
    return true;
  }

  return Boolean(rule.match.clauses?.length);
}

export function isConditionComplete(condition: RuleConditionState) {
  if (needsHeaderName(condition) && !condition.name.trim()) {
    return false;
  }

  if (!needsValue(condition)) {
    return true;
  }

  return condition.value.trim().length > 0;
}

export function describeRuleMatch(rule: Rule) {
  if (rule.match.any || rule.system) {
    return "Catch-all fallback";
  }

  const groups: MatchClause[][] = [];
  for (const clause of rule.match.clauses ?? []) {
    if (groups.length === 0 || clause.logical === "or") {
      groups.push([clause]);
      continue;
    }
    groups[groups.length - 1].push(clause);
  }

  return groups
    .map((group) => {
      const summary = group.map(summarizeClause).join(" AND ");
      return group.length > 1 ? `(${summary})` : summary;
    })
    .join(" OR ");
}

export function describeRuleCache(rule: Rule) {
  const bits = [rule.action.cache.mode.replaceAll("_", " ")];
  if (supportsCacheTiming(rule.action.cache.mode) && rule.action.cache.ttl) {
    bits.push(`TTL ${rule.action.cache.ttl}`);
  }
  if (supportsCacheTiming(rule.action.cache.mode) && rule.action.cache.stale_if_error) {
    bits.push(`Stale-if-error ${rule.action.cache.stale_if_error}`);
  }
  if (rule.action.cache.mode !== "bypass" && rule.action.cache.optimistic) {
    bits.push("Optimistic");
  }
  return bits.join(" • ");
}

export function supportsCacheTiming(mode: Rule["action"]["cache"]["mode"]) {
  return mode === "force_cache" || mode === "override_origin";
}

export const blankRule: Rule = {
  id: "",
  name: "New Rule",
  enabled: true,
  match: {
    clauses: [],
  },
  action: {
    cache: {
      mode: "follow_origin",
    },
  },
};
