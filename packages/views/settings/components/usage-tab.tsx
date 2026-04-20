"use client";

import { useState, useEffect } from "react";
import { DollarSign, TrendingUp, AlertTriangle, Save, Loader2 } from "lucide-react";
import { Button } from "@aurion/ui/components/ui/button";
import { Input } from "@aurion/ui/components/ui/input";
import { Label } from "@aurion/ui/components/ui/label";
import { Badge } from "@aurion/ui/components/ui/badge";
import { api } from "@aurion/core/api";
import type { BudgetStatus } from "@aurion/core/types";

export function UsageTab() {
  const [budget, setBudget] = useState<BudgetStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [dailyLimit, setDailyLimit] = useState("");
  const [monthlyLimit, setMonthlyLimit] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api
      .getWorkspaceBudget()
      .then((b) => {
        setBudget(b);
        setDailyLimit(b.daily_limit > 0 ? String(b.daily_limit) : "");
        setMonthlyLimit(b.monthly_limit > 0 ? String(b.monthly_limit) : "");
      })
      .catch(() => setBudget(null))
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await api.updateWorkspaceBudget({
        daily_budget_usd: dailyLimit ? parseFloat(dailyLimit) : 0,
        monthly_budget_usd: monthlyLimit ? parseFloat(monthlyLimit) : 0,
      });
      // Reload budget
      const b = await api.getWorkspaceBudget();
      setBudget(b);
    } catch {
      // fail silently
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-lg font-semibold">Usage &amp; Budget</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Monitor agent spending and set budget limits to prevent unexpected costs.
        </p>
      </div>

      {/* Current spending */}
      {budget && (
        <div className="grid gap-4 sm:grid-cols-2">
          <SpendingCard
            label="Today"
            spent={budget.daily_spent}
            limit={budget.daily_limit}
          />
          <SpendingCard
            label="This Month"
            spent={budget.monthly_spent}
            limit={budget.monthly_limit}
          />
        </div>
      )}

      {/* Budget limits */}
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center gap-2">
          <DollarSign className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-medium">Budget Limits</h3>
        </div>
        <p className="text-xs text-muted-foreground">
          Set daily and monthly spending caps. Agent sessions that exceed the
          limit will be rejected with a 402 error.
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="daily-limit">Daily limit (USD)</Label>
            <Input
              id="daily-limit"
              type="number"
              min="0"
              step="0.01"
              placeholder="No limit"
              value={dailyLimit}
              onChange={(e) => setDailyLimit(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="monthly-limit">Monthly limit (USD)</Label>
            <Input
              id="monthly-limit"
              type="number"
              min="0"
              step="0.01"
              placeholder="No limit"
              value={monthlyLimit}
              onChange={(e) => setMonthlyLimit(e.target.value)}
            />
          </div>
        </div>
        <div className="flex justify-end">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? (
              <Loader2 className="h-4 w-4 animate-spin mr-1.5" />
            ) : (
              <Save className="h-4 w-4 mr-1.5" />
            )}
            Save limits
          </Button>
        </div>
      </div>
    </div>
  );
}

function SpendingCard({
  label,
  spent,
  limit,
}: {
  label: string;
  spent: number;
  limit: number;
}) {
  const hasLimit = limit > 0;
  const pct = hasLimit ? Math.min((spent / limit) * 100, 100) : 0;
  const isOver = hasLimit && spent >= limit;
  const isWarning = hasLimit && pct >= 80 && !isOver;

  return (
    <div className="rounded-lg border p-4 space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        {isOver && (
          <Badge variant="destructive" className="text-[10px] h-4 px-1.5">
            <AlertTriangle className="h-3 w-3 mr-0.5" />
            Over limit
          </Badge>
        )}
        {isWarning && (
          <Badge variant="outline" className="text-[10px] h-4 px-1.5 border-amber-500 text-amber-600">
            <TrendingUp className="h-3 w-3 mr-0.5" />
            {pct.toFixed(0)}% used
          </Badge>
        )}
      </div>
      <div className="flex items-baseline gap-1">
        <span className="text-2xl font-bold">${spent.toFixed(2)}</span>
        {hasLimit && (
          <span className="text-sm text-muted-foreground">/ ${limit.toFixed(2)}</span>
        )}
      </div>
      {hasLimit && (
        <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
          <div
            className={`h-full rounded-full transition-all ${
              isOver ? "bg-destructive" : isWarning ? "bg-amber-500" : "bg-primary"
            }`}
            style={{ width: `${pct}%` }}
          />
        </div>
      )}
    </div>
  );
}
