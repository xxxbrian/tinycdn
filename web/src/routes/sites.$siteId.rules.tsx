import { useState } from "react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { toast } from "sonner";

import type { Rule } from "@/types";
import { api } from "@/lib/api";
import { requireAuth, withProtectedLoader } from "@/lib/auth";
import { blankRule } from "@/lib/rule-helpers";
import { PageHeader } from "@/components/page-header";
import { RuleFormDrawer } from "@/components/rule-form-drawer";
import { RulesTable } from "@/components/rules-table";
import { SiteShell } from "@/components/site-shell";

export const Route = createFileRoute("/sites/$siteId/rules")({
  beforeLoad: ({ location }) => requireAuth(location),
  loader: ({ location, params }) => withProtectedLoader(location, () => api.getSite(params.siteId)),
  component: SiteRulesPage,
});

function SiteRulesPage() {
  const site = Route.useLoaderData();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editingRule, setEditingRule] = useState<Rule | null>(null);

  async function refresh() {
    await router.invalidate();
  }

  async function saveRule(rule: Rule) {
    setSaving(true);
    try {
      if (editingRule) {
        await api.updateRule(site.id, editingRule.id, rule);
        toast.success("Rule updated");
      } else {
        await api.createRule(site.id, rule);
        toast.success("Rule created");
      }
      setOpen(false);
      setEditingRule(null);
      await refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to save rule");
    } finally {
      setSaving(false);
    }
  }

  async function updateRule(rule: Rule) {
    try {
      await api.updateRule(site.id, rule.id, rule);
      await refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to update rule");
    }
  }

  return (
    <SiteShell site={site} section="Rules">
      <PageHeader
        eyebrow="Site workspace"
        title="Rules"
        description="Rules are evaluated in order. Cache mode, TTL, stale-if-error, and optimistic refresh all live at the rule level."
        badge={`${site.rules.length} total`}
      />
      <div className="px-4 pb-4 lg:px-6 lg:pb-6">
        <RulesTable
          rules={site.rules}
          onCreate={() => {
            setEditingRule(null);
            setOpen(true);
          }}
          onEdit={(rule) => {
            setEditingRule(rule);
            setOpen(true);
          }}
          onDelete={async (rule) => {
            try {
              await api.deleteRule(site.id, rule.id);
              toast.success("Rule deleted");
              await refresh();
            } catch (error) {
              toast.error(error instanceof Error ? error.message : "Failed to delete rule");
            }
          }}
          onMove={async (rule, direction) => {
            const ruleIds = site.rules.map((item) => item.id);
            const index = site.rules.findIndex((item) => item.id === rule.id);
            const targetIndex = direction === "up" ? index - 1 : index + 1;
            if (index < 0 || targetIndex < 0 || targetIndex >= site.rules.length - 0) {
              return;
            }
            [ruleIds[index], ruleIds[targetIndex]] = [ruleIds[targetIndex], ruleIds[index]];
            try {
              await api.reorderRules(site.id, { rule_ids: ruleIds });
              await refresh();
            } catch (error) {
              toast.error(error instanceof Error ? error.message : "Failed to reorder rules");
            }
          }}
          onToggle={async (rule) => {
            await updateRule({ ...rule, enabled: !rule.enabled });
          }}
        />
      </div>
      <RuleFormDrawer
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen);
          if (!nextOpen) {
            setEditingRule(null);
          }
        }}
        mode={editingRule ? "edit" : "create"}
        rule={editingRule ?? { ...blankRule, id: "", name: "New Rule" }}
        saving={saving}
        onSubmit={saveRule}
      />
    </SiteShell>
  );
}
