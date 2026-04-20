"use client";

import { useState, useEffect, useCallback } from "react";
import { Activity, ArrowLeft, Clock, DollarSign, Play, Send } from "lucide-react";
import type { Agent, ManagedSession } from "@aurion/core/types";
import { Skeleton } from "@aurion/ui/components/ui/skeleton";
import { Badge } from "@aurion/ui/components/ui/badge";
import { Button } from "@aurion/ui/components/ui/button";
import { Input } from "@aurion/ui/components/ui/input";
import { api } from "@aurion/core/api";
import { useRequiredWorkspaceSlug } from "@aurion/core/paths";
import { SessionView } from "../session-view";

const statusStyles: Record<string, { dot: string; color: string; label: string }> = {
  running: { dot: "bg-green-500", color: "text-green-600", label: "Running" },
  idle: { dot: "bg-gray-400", color: "text-muted-foreground", label: "Idle" },
  completed: { dot: "bg-blue-500", color: "text-blue-600", label: "Completed" },
  terminated: { dot: "bg-red-500", color: "text-red-600", label: "Terminated" },
  interrupted: { dot: "bg-amber-500", color: "text-amber-600", label: "Interrupted" },
  paused: { dot: "bg-yellow-500", color: "text-yellow-600", label: "Paused" },
};

function formatRelativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function SessionsTab({ agent }: { agent: Agent }) {
  const [sessions, setSessions] = useState<ManagedSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<ManagedSession | null>(null);
  const [triggerPrompt, setTriggerPrompt] = useState("");
  const [triggering, setTriggering] = useState(false);
  const workspaceSlug = useRequiredWorkspaceSlug();

  const loadSessions = useCallback(() => {
    setLoading(true);
    api
      .listManagedSessions({ agentId: agent.id })
      .then((res) => setSessions(res.data ?? []))
      .catch(() => setSessions([]))
      .finally(() => setLoading(false));
  }, [agent.id]);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  const handleTrigger = async () => {
    if (!triggerPrompt.trim() || triggering) return;
    setTriggering(true);
    try {
      const result = await api.triggerAgent(agent.id, {
        prompt: triggerPrompt.trim(),
        source: "manual",
      });
      setTriggerPrompt("");
      setSelected(result.session);
      loadSessions();
    } catch {
      // Error handled silently — session list will reflect state
    } finally {
      setTriggering(false);
    }
  };

  // Drill-down: show SessionView for selected session
  if (selected) {
    return (
      <SessionView
        session={selected}
        workspaceSlug={workspaceSlug}
        onBack={() => setSelected(null)}
      />
    );
  }

  if (loading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 rounded-lg border px-4 py-3">
            <Skeleton className="h-4 w-4 rounded shrink-0" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    );
  }

  if (sessions.length === 0) {
    return (
      <div className="space-y-4">
        <TriggerBar
          prompt={triggerPrompt}
          setPrompt={setTriggerPrompt}
          onTrigger={handleTrigger}
          triggering={triggering}
        />
        <div className="flex flex-col items-center justify-center py-12 text-center">
          <Activity className="mb-3 h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm text-muted-foreground">No sessions yet</p>
          <p className="mt-1 text-xs text-muted-foreground/70">
            Type a prompt above to trigger the agent
          </p>
        </div>
      </div>
    );
  }

  // Sort: running first, then by created_at desc
  const sorted = [...sessions].sort((a, b) => {
    if (a.status === "running" && b.status !== "running") return -1;
    if (b.status === "running" && a.status !== "running") return 1;
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });

  return (
    <div className="space-y-3">
      <TriggerBar
        prompt={triggerPrompt}
        setPrompt={setTriggerPrompt}
        onTrigger={handleTrigger}
        triggering={triggering}
      />
      <div className="space-y-1.5">
      {sorted.map((s) => {
        const st = statusStyles[s.status] ?? statusStyles.idle;
        return (
          <button
            key={s.id}
            onClick={() => setSelected(s)}
            className="flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors hover:bg-muted/50"
          >
            <Activity className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium truncate">
                  {s.title ?? `Session ${s.id.slice(0, 8)}`}
                </span>
                <span className={`flex items-center gap-1 text-xs ${st.color}`}>
                  <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
                  {st.label}
                </span>
              </div>
              <div className="mt-0.5 flex items-center gap-3 text-xs text-muted-foreground">
                <span className="flex items-center gap-1">
                  <Clock className="h-3 w-3" />
                  {formatRelativeTime(s.created_at)}
                </span>
                {s.total_cost_usd != null && s.total_cost_usd > 0 && (
                  <span className="flex items-center gap-1">
                    <DollarSign className="h-3 w-3" />
                    ${s.total_cost_usd.toFixed(4)}
                  </span>
                )}
                {s.last_event_index != null && (
                  <span>{s.last_event_index + 1} events</span>
                )}
                {(s.wake_count ?? 0) > 0 && (
                  <Badge variant="outline" className="h-4 text-[10px] px-1">
                    {s.wake_count} wake{s.wake_count === 1 ? "" : "s"}
                  </Badge>
                )}
              </div>
            </div>
          </button>
        );
      })}
    </div>
    </div>
  );
}

function TriggerBar({
  prompt,
  setPrompt,
  onTrigger,
  triggering,
}: {
  prompt: string;
  setPrompt: (v: string) => void;
  onTrigger: () => void;
  triggering: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <Input
        placeholder="Enter a prompt to trigger the agent..."
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && onTrigger()}
        disabled={triggering}
        className="flex-1"
      />
      <Button
        size="sm"
        onClick={onTrigger}
        disabled={!prompt.trim() || triggering}
      >
        {triggering ? (
          <Play className="h-4 w-4 animate-pulse" />
        ) : (
          <Send className="h-4 w-4" />
        )}
        <span className="ml-1.5">Run</span>
      </Button>
    </div>
  );
}
