"use client";

import { useState, useCallback, useRef, useEffect, useMemo } from "react";
import {
  Sparkles,
  Send,
  Square,
  Loader2,
  ChevronRight,
  ArrowLeft,
  MessageSquarePlus,
  Globe,
  Zap,
  Brain,
} from "lucide-react";
import { Button } from "@aurion/ui/components/ui/button";
import { Textarea } from "@aurion/ui/components/ui/textarea";
import { Badge } from "@aurion/ui/components/ui/badge";
import { toast } from "sonner";
import { Skeleton } from "@aurion/ui/components/ui/skeleton";
import { cn } from "@aurion/ui/lib/utils";
import { api } from "@aurion/core/api";
import { useWorkspaceId } from "@aurion/core/hooks";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { PageHeader } from "../../layout/page-header";
import type {
  ManagedSession,
  StoreEvent,
} from "@aurion/core/types/managed-agents";

// ÔöÇÔöÇ Heuristic auto-selection ÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇ
// Given a natural-language description, pick the best execution mode,
// model, tools and MCP servers. The backend still injects Playwright MCP
// automatically for browser/hybrid modes, so these hints are additive.

type ExecutionMode = "browser" | "routine" | "hybrid";

interface AutoConfig {
  mode: ExecutionMode;
  model: string;
  modelLabel: string;
  mcpServers: string[];
  reasoning: string;
}

function analyzeDescription(desc: string): AutoConfig {
  const text = desc.toLowerCase();

  const webSignals = /\b(browse|navigate|scrape|linkedin|twitter|instagram|facebook|reddit|google|site:|website|login|sign in|form|captcha|cloudflare)\b/;
  const codeSignals = /\b(code|function|test|bug|fix|refactor|typescript|python|golang|react|api endpoint|sql|migration|deploy|ci|build|compile)\b/;
  const emailSignals = /\b(email|mail|prospect|cold|outreach|gmail|outlook|smtp)\b/;
  const dataSignals = /\b(database|postgres|mysql|sql|analyze|report|metrics|dashboard|csv|spreadsheet)\b/;
  const researchSignals = /\b(research|compare|study|analyze|report|find info|summarize|investigate)\b/;
  const automationSignals = /\b(schedule|cron|every|daily|monitor|watch|notify|alert|workflow)\b/;
  const complexSignals = /\b(multi[- ]step|complex|sophisticated|strategy|plan|design|architect|reason)\b/;

  const isWeb = webSignals.test(text);
  const isCode = codeSignals.test(text);
  const isEmail = emailSignals.test(text);
  const isData = dataSignals.test(text);
  const isResearch = researchSignals.test(text);
  const isAutomation = automationSignals.test(text);
  const isComplex = complexSignals.test(text) || desc.length > 400;

  let mode: ExecutionMode = "hybrid";
  if (isWeb && !isCode) mode = "browser";
  else if (isCode && !isWeb) mode = "routine";
  else if (isData && !isWeb) mode = "routine";

  let model = "anthropic/claude-opus-4.7";
  let modelLabel = "Claude Opus 4.7 (most powerful)";
  if (!isComplex && (mode === "routine" || desc.length < 120)) {
    model = "anthropic/claude-sonnet-4-5";
    modelLabel = "Claude Sonnet 4.5 (fast)";
  }

  const mcps: string[] = [];
  if (mode === "browser" || mode === "hybrid") mcps.push("playwright");
  if (/\b(github|repo|pull request|pr|issue|commit)\b/.test(text)) mcps.push("github");
  if (/\b(slack|channel|#\w+)\b/.test(text)) mcps.push("slack");
  if (/\b(notion|page|database)\b/.test(text)) mcps.push("notion");
  if (/\b(postgres|postgresql|pg)\b/.test(text)) mcps.push("postgres");
  if (/\b(linear|ticket)\b/.test(text)) mcps.push("linear");
  if (/\b(search|look up|find)\b/.test(text) && !mcps.includes("brave-search")) {
    mcps.push("brave-search");
  }
  if (/\b(scrape|extract|fetch)\b/.test(text) && !mcps.includes("firecrawl")) {
    mcps.push("firecrawl");
  }

  const reasons: string[] = [];
  if (isWeb) reasons.push("detected web browsing needs");
  if (isCode) reasons.push("detected code tasks");
  if (isEmail) reasons.push("detected email tasks");
  if (isData) reasons.push("detected data/analytics tasks");
  if (isResearch) reasons.push("detected research tasks");
  if (isAutomation) reasons.push("detected automation/scheduling");
  if (isComplex) reasons.push("complex task ÔåÆ using most powerful model");
  if (reasons.length === 0) reasons.push("general-purpose agent");

  return {
    mode,
    model,
    modelLabel,
    mcpServers: mcps,
    reasoning: reasons.join(" ┬À "),
  };
}

// ÔöÇÔöÇ Session Event Viewer ÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇ

function EventCard({ event }: { event: StoreEvent }) {
  const typeColors: Record<string, string> = {
    user_message: "border-l-white",
    assistant_message: "border-l-white/60",
    tool_call: "border-l-white/80",
    tool_result: "border-l-white/50",
    system_event: "border-l-white/30",
    cost_event: "border-l-white/30",
    thinking: "border-l-white/70",
  };
  return (
    <div
      className={cn(
        "border-l-2 pl-3 py-2 text-xs font-mono",
        typeColors[event.type] ?? "border-l-white/20"
      )}
    >
      <div className="flex items-center gap-2 mb-1">
        <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-foreground border-foreground/30">
          {event.type}
        </Badge>
        <span className="text-foreground/60">#{event.index}</span>
      </div>
      <div className="text-foreground/80 whitespace-pre-wrap line-clamp-6">
        {event.data.content ??
          event.data.thinking ??
          event.data.summary ??
          (event.data.tool_name
            ? `${event.data.tool_name}(${JSON.stringify(event.data.input ?? {})})`
            : event.data.details ?? JSON.stringify(event.data, null, 2))}
      </div>
    </div>
  );
}

function SessionPanel({
  session,
  onStop,
  onBack,
}: {
  session: ManagedSession;
  onStop: () => void;
  onBack: () => void;
}) {
  const [events, setEvents] = useState<StoreEvent[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!session?.id) return;
    let cancelled = false;
    let lastIndex = 0;
    const poll = async () => {
      while (!cancelled) {
        try {
          const data = await api.getSessionStoreEvents(session.id, { from: lastIndex });
          if (data.events?.length) {
            setEvents((prev) => [...prev, ...data.events]);
            lastIndex = data.events[data.events.length - 1]!.index;
          }
        } catch {
          /* ignore */
        }
        await new Promise((r) => setTimeout(r, 1500));
      }
    };
    poll();
    return () => {
      cancelled = true;
    };
  }, [session?.id]);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [events.length]);

  const isRunning = session.status === "running";

  return (
    <div className="flex flex-col h-full">
      <PageHeader className="justify-between">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onBack}>
            <ArrowLeft className="h-3.5 w-3.5 mr-1" />
            New
          </Button>
          <span className="text-sm font-medium truncate max-w-[60ch]">
            {session.title ?? `Session ${session.id.slice(0, 8)}`}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Badge
            variant={isRunning ? "default" : "secondary"}
            className={cn(isRunning && "animate-pulse")}
          >
            {isRunning ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin mr-1" />
                Running
              </>
            ) : (
              session.status
            )}
          </Badge>
          {session.total_cost_usd != null && (
            <span className="text-xs text-foreground/60">
              ${session.total_cost_usd.toFixed(4)}
            </span>
          )}
          {isRunning && (
            <Button variant="destructive" size="xs" onClick={onStop}>
              <Square className="h-3 w-3 mr-1" />
              Stop
            </Button>
          )}
        </div>
      </PageHeader>
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4 space-y-2">
        {events.length === 0 && isRunning && (
          <div className="flex items-center justify-center py-12 text-foreground/60 text-sm">
            <Loader2 className="h-4 w-4 animate-spin mr-2" />
            Agent starting...
          </div>
        )}
        {events.map((event) => (
          <EventCard key={event.id} event={event} />
        ))}
      </div>
    </div>
  );
}

// ÔöÇÔöÇ Main Page ÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇÔöÇ

const EXAMPLES = [
  "Find 20 CTOs of French SaaS startups on LinkedIn and draft a personalized cold email for each.",
  "Every morning at 8am, scrape crypto prices from CoinGecko and open an issue if BTC moved more than 5%.",
  "Research the top 5 open-source alternatives to Salesforce, compile a comparison report with pricing.",
  "Read my GitHub repo saasorchids-stack/multica, find all TODO comments, and create a prioritized issue list.",
  "Write a Python script that generates fizzbuzz with unit tests, run the tests, and save the result.",
];

export function ExecutionsPage() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [description, setDescription] = useState("");
  const [activeSession, setActiveSession] = useState<ManagedSession | null>(null);
  const [showComposer, setShowComposer] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const auto = useMemo(() => analyzeDescription(description), [description]);

  const { data: sessions = [], isLoading: sessionsLoading } = useQuery({
    queryKey: ["managed-sessions", wsId],
    queryFn: async () => (await api.listManagedSessions({})).data,
    refetchInterval: 5000,
  });

  const recentSessions = useMemo(
    () =>
      [...sessions]
        .sort(
          (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        )
        .slice(0, 20),
    [sessions]
  );

  const triggerMutation = useMutation({
    mutationFn: async (desc: string) => {
      const cfg = analyzeDescription(desc);
      const titleLine = desc.split("\n")[0]!.slice(0, 80);

      const systemPrompt = [
        "You are an autonomous AI agent running on the Aurion platform.",
        "Execute the user's task completely and autonomously without asking for confirmation.",
        "Use every tool and MCP server available to you.",
        "Reason step-by-step, act, verify results, and report concisely.",
        "",
        `Execution mode: ${cfg.mode}`,
        `Auto-selected MCP servers: ${cfg.mcpServers.join(", ") || "none (built-in tools only)"}`,
        "",
        "When the task is complete, provide a clear summary of what was done and what was found.",
      ].join("\n");

      const agent = await api.createManagedAgent({
        name: `Chat: ${titleLine}`,
        description: desc.slice(0, 200),
        model: { id: cfg.model, speed: "standard" },
        system_prompt: systemPrompt,
        tools: [{ type: "agent_toolset_20260401" as const }],
        metadata: {
          source: "chat-executions",
          auto_mcp_servers: cfg.mcpServers,
          auto_reasoning: cfg.reasoning,
          execution_mode: cfg.mode,
        },
      });

      // Auto-attach suggested MCP servers from the registry.
      // We try to match registry entries by slug/name against cfg.mcpServers.
      // Fails silently per-connector so one missing registry entry doesn't block launch.
      if (cfg.mcpServers.length > 0) {
        try {
          const { data: registry } = await api.listMcpRegistry();
          const byKey: Record<string, { id: string }> = {};
          for (const entry of registry as Array<{
            id: string;
            slug?: string;
            name?: string;
          }>) {
            if (entry.slug) byKey[entry.slug.toLowerCase()] = entry;
            if (entry.name) byKey[entry.name.toLowerCase()] = entry;
          }
          await Promise.all(
            cfg.mcpServers.map(async (server) => {
              const entry = byKey[server.toLowerCase()];
              if (!entry) return;
              try {
                await api.addMcpFromRegistry(agent.id, entry.id);
              } catch (e) {
                console.warn(`[auto-mcp] failed to attach ${server}:`, e);
              }
            })
          );
        } catch (e) {
          console.warn("[auto-mcp] registry lookup failed:", e);
        }
      }

      const { session } = await api.triggerAgent(agent.id, {
        prompt: desc,
        title: titleLine,
        source: "manual",
        model: cfg.model,
        execution_mode: cfg.mode,
      });

      return session;
    },
    onSuccess: (session) => {
      setActiveSession(session);
      setDescription("");
      setShowComposer(false);
      toast.success("Agent launched");
      qc.invalidateQueries({ queryKey: ["managed-sessions"] });
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to launch agent");
    },
  });

  const handleLaunch = useCallback(() => {
    const trimmed = description.trim();
    if (!trimmed) {
      toast.error("Describe what your agent should do");
      return;
    }
    triggerMutation.mutate(trimmed);
  }, [description, triggerMutation]);

  const handleStop = useCallback(async () => {
    if (!activeSession) return;
    try {
      await api.sendSessionEvents(activeSession.id, {
        events: [{ type: "user.interrupt", content: { message: "Stopped by user" } }],
      });
      toast.success("Agent stopped");
      setActiveSession(null);
    } catch {
      toast.error("Failed to stop");
    }
  }, [activeSession]);

  if (activeSession) {
    return (
      <SessionPanel
        session={activeSession}
        onStop={handleStop}
        onBack={() => setActiveSession(null)}
      />
    );
  }

  if (showComposer) {
    return (
      <div className="flex flex-col h-full bg-background text-foreground">
        <PageHeader className="justify-between">
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => setShowComposer(false)}>
              <ArrowLeft className="h-3.5 w-3.5 mr-1" />
              Back
            </Button>
            <Sparkles className="h-4 w-4" />
            <h1 className="text-sm font-semibold">New Agent</h1>
          </div>
        </PageHeader>

        <div className="flex-1 overflow-y-auto">
          <div className="max-w-3xl mx-auto w-full px-4 py-8 space-y-6">
            <div className="space-y-2">
              <h2 className="text-2xl font-semibold">Describe your agent</h2>
              <p className="text-sm text-foreground/70">
                Tell it what to do in natural language. Aurion automatically
                picks the best execution mode, model, and MCP servers.
              </p>
            </div>

            <div className="rounded-xl border border-foreground/20 bg-card p-1 focus-within:border-foreground/60 transition-colors">
              <Textarea
                ref={textareaRef}
                autoFocus
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="e.g. Find 20 CTOs of French SaaS startups on LinkedIn and draft a personalized cold email for each."
                className="min-h-[160px] border-0 bg-transparent text-base resize-y focus-visible:ring-0 placeholder:text-foreground/40"
                onKeyDown={(e) => {
                  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                    e.preventDefault();
                    handleLaunch();
                  }
                }}
              />
              <div className="flex items-center justify-between px-3 pb-2 pt-1">
                <div className="flex items-center gap-2 text-[11px] text-foreground/60">
                  <kbd className="rounded border border-foreground/20 px-1.5 py-0.5 font-mono">
                    ÔîÿÔåÁ
                  </kbd>
                  to launch
                </div>
                <Button
                  size="sm"
                  onClick={handleLaunch}
                  disabled={!description.trim() || triggerMutation.isPending}
                >
                  {triggerMutation.isPending ? (
                    <>
                      <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />
                      Launching...
                    </>
                  ) : (
                    <>
                      <Send className="h-3.5 w-3.5 mr-1.5" />
                      Launch
                    </>
                  )}
                </Button>
              </div>
            </div>

            {description.trim().length > 10 && (
              <div className="rounded-xl border border-foreground/15 p-4 space-y-3">
                <div className="flex items-center gap-2 text-[11px] uppercase tracking-wider text-foreground/60">
                  <Brain className="h-3 w-3" />
                  Auto-configured for this task
                </div>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                  <div>
                    <div className="flex items-center gap-1.5 text-xs text-foreground/60 mb-1">
                      {auto.mode === "browser" ? (
                        <Globe className="h-3 w-3" />
                      ) : auto.mode === "routine" ? (
                        <Zap className="h-3 w-3" />
                      ) : (
                        <Sparkles className="h-3 w-3" />
                      )}
                      Execution mode
                    </div>
                    <div className="text-sm font-medium capitalize">{auto.mode}</div>
                  </div>
                  <div>
                    <div className="text-xs text-foreground/60 mb-1">Model</div>
                    <div className="text-sm font-medium">{auto.modelLabel}</div>
                  </div>
                  <div>
                    <div className="text-xs text-foreground/60 mb-1">MCP servers</div>
                    <div className="flex flex-wrap gap-1">
                      {auto.mcpServers.length > 0 ? (
                        auto.mcpServers.map((m) => (
                          <Badge
                            key={m}
                            variant="outline"
                            className="text-[10px] border-foreground/30"
                          >
                            {m}
                          </Badge>
                        ))
                      ) : (
                        <span className="text-xs text-foreground/50">built-in only</span>
                      )}
                    </div>
                  </div>
                </div>
                <div className="text-[11px] text-foreground/60">{auto.reasoning}</div>
              </div>
            )}

            <div className="space-y-2">
              <div className="text-[11px] uppercase tracking-wider text-foreground/60">
                Examples
              </div>
              <div className="grid gap-2">
                {EXAMPLES.map((ex) => (
                  <button
                    key={ex}
                    onClick={() => {
                      setDescription(ex);
                      textareaRef.current?.focus();
                    }}
                    className="text-left rounded-lg border border-foreground/15 p-3 text-sm hover:border-foreground/40 hover:bg-foreground/5 transition-colors"
                  >
                    {ex}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full bg-background text-foreground">
      <PageHeader className="justify-between">
        <div className="flex items-center gap-2">
          <Sparkles className="h-4 w-4" />
          <h1 className="text-sm font-semibold">Executions</h1>
        </div>
        <Button size="sm" onClick={() => setShowComposer(true)}>
          <MessageSquarePlus className="h-3.5 w-3.5 mr-1.5" />
          New agent
        </Button>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="max-w-3xl mx-auto w-full px-4 py-10">
          <button
            onClick={() => setShowComposer(true)}
            className="w-full rounded-2xl border border-foreground/20 bg-card p-8 text-left hover:border-foreground/50 transition-colors group"
          >
            <div className="flex items-start gap-4">
              <div className="rounded-xl border border-foreground/30 p-3 group-hover:border-foreground/60 transition-colors">
                <Sparkles className="h-5 w-5" />
              </div>
              <div className="flex-1">
                <div className="text-lg font-semibold">Describe a new agent</div>
                <div className="text-sm text-foreground/70 mt-1">
                  Tell Aurion what to do. MCPs, tools, and the best model are
                  selected automatically.
                </div>
              </div>
              <ChevronRight className="h-5 w-5 text-foreground/40 group-hover:text-foreground transition-colors" />
            </div>
          </button>

          <div className="mt-10">
            <h2 className="text-xs font-medium uppercase tracking-wider text-foreground/60 mb-3">
              Recent runs
            </h2>
            {sessionsLoading ? (
              <div className="space-y-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : recentSessions.length === 0 ? (
              <div className="flex flex-col items-center py-12 text-foreground/50">
                <Sparkles className="h-8 w-8 opacity-30" />
                <p className="mt-2 text-sm">No runs yet</p>
                <p className="text-xs">Click &quot;New agent&quot; to describe one</p>
              </div>
            ) : (
              <div className="space-y-1.5">
                {recentSessions.map((session) => (
                  <button
                    key={session.id}
                    onClick={() => setActiveSession(session)}
                    className="w-full flex items-center gap-3 rounded-lg border border-foreground/15 p-3 text-left hover:border-foreground/40 hover:bg-foreground/5 transition-colors"
                  >
                    <Badge
                      variant={session.status === "running" ? "default" : "secondary"}
                      className={cn(
                        "text-[10px] shrink-0",
                        session.status === "running" && "animate-pulse"
                      )}
                    >
                      {session.status}
                    </Badge>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate">
                        {session.title ?? `Session ${session.id.slice(0, 8)}`}
                      </p>
                      <p className="text-[11px] text-foreground/50">
                        {new Date(session.created_at).toLocaleString()}
                        {session.total_cost_usd
                          ? ` ÔÇó $${session.total_cost_usd.toFixed(4)}`
                          : ""}
                      </p>
                    </div>
                    <ChevronRight className="h-3.5 w-3.5 text-foreground/40 shrink-0" />
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
