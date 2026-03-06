import type { Rule, Site } from "@/types";

export function getGlobalStats(sites: Site[]) {
  const totalHosts = sites.reduce((sum, site) => sum + site.hosts.length, 0);
  const totalRules = sites.reduce((sum, site) => sum + site.rules.length, 0);
  const activeSites = sites.filter((site) => site.enabled).length;
  const optimisticSites = sites.filter((site) => site.cache.optimistic_refresh).length;

  return {
    totalSites: sites.length,
    activeSites,
    totalHosts,
    totalRules,
    optimisticSites,
  };
}

export function getSiteStats(site: Site) {
  const customRules = site.rules.filter((rule) => !rule.system);
  const enabledRules = customRules.filter((rule) => rule.enabled).length;

  return {
    hostCount: site.hosts.length,
    totalRules: site.rules.length,
    customRuleCount: customRules.length,
    enabledRules,
    optimisticRefresh: site.cache.optimistic_refresh,
    defaultRule: site.rules[site.rules.length - 1] ?? null,
  };
}

export function getPrimaryCacheMode(rule: Rule | null) {
  if (!rule) {
    return "none";
  }

  return rule.action.cache.mode.replaceAll("_", " ");
}
