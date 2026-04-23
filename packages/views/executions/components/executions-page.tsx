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

// ── Heuristic auto-selection ────────────────────────────────────────
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
  if (isComplex) reasons.push("complex task → using most powerful model");
  if (reasons.length === 0) reasons.push("general-purpose agent");

  return {
    mode,
    model,
    modelLabel,
    mcpServers: mcps,
    reasoning: reasons.join(" · "),
  };
}

// ── Session Event Viewer ────────────────────────────────────────────

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

// ── Main Page ───────────────────────────────────────────────────────

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
                    ⌘↵
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
                          ? ` • $${session.total_cost_usd.toFixed(4)}`
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
"use client";

import { useState, useCallback, useRef, useEffect, useMemo } from "react";
import {
  Rocket,
  Play,
  Square,
  Loader2,
  ChevronRight,
  Globe,
  Terminal,
  FileCode,
  Search,
  Shield,
  Eye,
  Settings2,
} from "lucide-react";
import { Button } from "@aurion/ui/components/ui/button";
import { Textarea } from "@aurion/ui/components/ui/textarea";
import { Input } from "@aurion/ui/components/ui/input";
import { Badge } from "@aurion/ui/components/ui/badge";
import { toast } from "sonner";
import { Skeleton } from "@aurion/ui/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@aurion/ui/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@aurion/ui/components/ui/tooltip";
import { Switch } from "@aurion/ui/components/ui/switch";
import { Label } from "@aurion/ui/components/ui/label";
import { cn } from "@aurion/ui/lib/utils";
import { api } from "@aurion/core/api";
import { useWorkspaceId } from "@aurion/core/hooks";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { PageHeader } from "../../layout/page-header";
import type {
  ManagedAgent,
  ManagedSession,
  StoreEvent,
} from "@aurion/core/types/managed-agents";

// ── Execution Templates ─────────────────────────────────────────────

interface ExecutionTemplate {
  id: string;
  name: string;
  description: string;
  icon: typeof Globe;
  category: "web" | "code" | "research" | "automation";
  defaultPrompt: string;
  tools: string[];
  mcpServers: string[];
  stealthMode: boolean;
  /**
   * Execution mode:
   * - "browser" → agent drives a real Chromium via Playwright MCP (default for any web task)
   * - "routine" → headless / direct API calls only (for repetitive / background tasks)
   * - "hybrid"  → prefers APIs, falls back to browser
   */
  executionMode: "browser" | "routine" | "hybrid";
}

const TEMPLATES: ExecutionTemplate[] = [
  {
    id: "web-scraper",
    name: "Web Scraper",
    description: "Browse websites, extract data, handle anti-bot protections",
    icon: Globe,
    category: "web",
    defaultPrompt:
      "Navigate to the target website, handle any anti-bot challenges, and extract the requested data. Use stealth browsing techniques.",
    tools: ["bash", "browse_page", "stealth_browse", "screenshot_page", "extract_links", "http_request", "solve_captcha"],
    mcpServers: ["playwright", "brave-search"],
    stealthMode: true,
    executionMode: "browser",
  },
  {
    id: "linkedin-prospector",
    name: "LinkedIn Prospector",
    description:
      "Find prospects on LinkedIn using stealth techniques to bypass robot detection",
    icon: Search,
    category: "research",
    defaultPrompt:
      "Find prospects matching the criteria on LinkedIn. Use rotating proxies, human-like delays, and browser fingerprint randomization to avoid detection. Extract: name, title, company, profile URL.",
    tools: [
      "bash",
      "browse_page",
      "stealth_browse",
      "screenshot_page",
      "fill_form",
      "extract_links",
      "http_request",
      "solve_captcha",
    ],
    mcpServers: ["playwright", "brave-search"],
    stealthMode: true,
    executionMode: "browser",
  },
  {
    id: "code-generator",
    name: "Code Generator",
    description: "Generate, test, and deploy code from a description",
    icon: FileCode,
    category: "code",
    defaultPrompt:
      "Generate the requested code, write tests, ensure all tests pass, then provide the final implementation.",
    tools: ["bash", "read_file", "write_file", "edit", "list_directory", "web_search"],
    mcpServers: ["github"],
    stealthMode: false,
    executionMode: "routine",
  },
  {
    id: "research-agent",
    name: "Deep Research",
    description: "Research any topic across the web, compile findings into a report",
    icon: Eye,
    category: "research",
    defaultPrompt:
      "Research the given topic thoroughly. Browse multiple sources, cross-reference information, and compile a comprehensive report with citations.",
    tools: [
      "bash",
      "browse_page",
      "screenshot_page",
      "extract_links",
      "http_request",
      "web_search",
      "write_file",
    ],
    mcpServers: ["brave-search", "playwright"],
    stealthMode: false,
    executionMode: "hybrid",
  },
  {
    id: "form-automator",
    name: "Form Automator",
    description: "Fill forms, submit applications, automate web workflows",
    icon: Terminal,
    category: "automation",
    defaultPrompt:
      "Navigate to the target website, fill in the required forms with the provided data, handle CAPTCHAs if possible, and submit.",
    tools: [
      "bash",
      "browse_page",
      "stealth_browse",
      "fill_form",
      "screenshot_page",
      "extract_links",
      "http_request",
      "solve_captcha",
    ],
    mcpServers: ["playwright"],
    stealthMode: true,
    executionMode: "browser",
  },
  {
    id: "custom",
    name: "Custom Agent",
    description: "Describe exactly what you want — the AI builds and executes it",
    icon: Settings2,
    category: "automation",
    defaultPrompt: "",
    tools: [],
    mcpServers: [],
    stealthMode: false,
    executionMode: "browser",
  },
];

// ── Category Colors ─────────────────────────────────────────────────

const CATEGORY_COLORS: Record<string, string> = {
  web: "bg-blue-500/10 text-blue-600 border-blue-500/20",
  code: "bg-green-500/10 text-green-600 border-green-500/20",
  research: "bg-purple-500/10 text-purple-600 border-purple-500/20",
  automation: "bg-orange-500/10 text-orange-600 border-orange-500/20",
};

// ── Stealth Anti-Detection Tools ────────────────────────────────────

const STEALTH_TOOLS = [
  {
    name: "puppeteer-extra-stealth",
    url: "https://github.com/AhmedElywa/puppeteer-extra-stealth",
    description: "Puppeteer plugin to pass all bot detection tests",
  },
  {
    name: "undetected-chromedriver",
    url: "https://github.com/ultrafunkamsterdam/undetected-chromedriver",
    description: "Chrome driver that bypasses Cloudflare, Distil, Datadome",
  },
  {
    name: "playwright-stealth",
    url: "https://github.com/nichmarch/playwright-stealth",
    description: "Stealth plugin for Playwright to avoid detection",
  },
  {
    name: "nodriver",
    url: "https://github.com/nichmarch/nodriver",
    description: "Next-gen undetected browser automation (successor to undetected-chromedriver)",
  },
  {
    name: "botasaurus",
    url: "https://github.com/nichmarch/botasaurus",
    description: "All-in-one web scraping framework with anti-detection",
  },
  {
    name: "curl-impersonate",
    url: "https://github.com/lwthiker/curl-impersonate",
    description: "Curl that impersonates Chrome/Firefox TLS fingerprints",
  },
  {
    name: "FlareSolverr",
    url: "https://github.com/FlareSolverr/FlareSolverr",
    description: "Proxy server to bypass Cloudflare and DDoS-Guard",
  },
  {
    name: "2captcha",
    url: "https://github.com/2captcha/2captcha-python",
    description: "CAPTCHA solving API — reCAPTCHA, hCaptcha, Turnstile",
  },
  {
    name: "capsolver",
    url: "https://github.com/capsolver/capsolver-python",
    description: "AI-powered CAPTCHA solving — faster and cheaper than 2captcha",
  },
  {
    name: "scrapfly",
    url: "https://github.com/scrapfly/python-scrapfly",
    description: "Web scraping API with anti-bot bypass, proxy rotation, JS rendering",
  },
];

// ── Session Event Viewer ────────────────────────────────────────────

function EventCard({ event }: { event: StoreEvent }) {
  const typeColors: Record<string, string> = {
    user_message: "border-l-blue-500",
    assistant_message: "border-l-green-500",
    tool_call: "border-l-yellow-500",
    tool_result: "border-l-orange-500",
    system_event: "border-l-gray-500",
    cost_event: "border-l-red-500",
    thinking: "border-l-purple-500",
  };

  return (
    <div
      className={cn(
        "border-l-2 pl-3 py-2 text-xs font-mono",
        typeColors[event.type] ?? "border-l-gray-300"
      )}
    >
      <div className="flex items-center gap-2 mb-1">
        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
          {event.type}
        </Badge>
        <span className="text-muted-foreground">#{event.index}</span>
      </div>
      <div className="text-muted-foreground whitespace-pre-wrap line-clamp-4">
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

// ── Active Session Panel ────────────────────────────────────────────

function SessionPanel({
  session,
  onStop,
}: {
  session: ManagedSession;
  onStop: () => void;
}) {
  const [events, setEvents] = useState<StoreEvent[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Poll for events
  useEffect(() => {
    if (!session?.id) return;
    let cancelled = false;
    let lastIndex = 0;

    const poll = async () => {
      while (!cancelled) {
        try {
          const data = await api.getSessionStoreEvents(session.id, {
            from: lastIndex,
          });
          if (data.events?.length) {
            setEvents((prev) => [...prev, ...data.events]);
            lastIndex = data.events[data.events.length - 1]!.index;
          }
        } catch {
          // ignore polling errors
        }
        // Wait before next poll
        await new Promise((r) => setTimeout(r, 1500));
      }
    };

    poll();
    return () => {
      cancelled = true;
    };
  }, [session?.id]);

  // Auto-scroll
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [events.length]);

  const isRunning = session.status === "running";

  return (
    <div className="flex flex-col h-full">
      {/* Session Header */}
      <div className="flex items-center justify-between border-b px-4 py-2">
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
          <span className="text-xs text-muted-foreground font-mono">
            {session.id.slice(0, 8)}
          </span>
        </div>
        <div className="flex items-center gap-2">
          {session.usage && (
            <span className="text-xs text-muted-foreground">
              ${(session.total_cost_usd ?? 0).toFixed(4)}
            </span>
          )}
          {isRunning && (
            <Button variant="destructive" size="xs" onClick={onStop}>
              <Square className="h-3 w-3 mr-1" />
              Stop
            </Button>
          )}
        </div>
      </div>

      {/* Events Stream */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-3 space-y-2">
        {events.length === 0 && isRunning && (
          <div className="flex items-center justify-center py-12 text-muted-foreground text-sm">
            <Loader2 className="h-4 w-4 animate-spin mr-2" />
            Waiting for agent output...
          </div>
        )}
        {events.map((event) => (
          <EventCard key={event.id} event={event} />
        ))}
      </div>
    </div>
  );
}

// ── Main Executions Page ────────────────────────────────────────────

export function ExecutionsPage() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [selectedTemplate, setSelectedTemplate] = useState<ExecutionTemplate | null>(null);
  const [prompt, setPrompt] = useState("");
  const [agentName, setAgentName] = useState("");
  const [stealthEnabled, setStealthEnabled] = useState(false);
  const [executionMode, setExecutionMode] = useState<"browser" | "routine" | "hybrid">("browser");
  const [activeSession, setActiveSession] = useState<ManagedSession | null>(null);
  const [showStealthTools, setShowStealthTools] = useState(true);
  const [model, setModel] = useState("anthropic/claude-sonnet-4-20250514");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Fetch existing sessions
  const { data: sessions = [], isLoading: sessionsLoading } = useQuery({
    queryKey: ["managed-sessions", wsId],
    queryFn: async () => {
      const res = await api.listManagedSessions({});
      return res.data;
    },
    refetchInterval: 5000,
  });

  const recentSessions = useMemo(
    () =>
      [...sessions]
        .sort(
          (a, b) =>
            new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        )
        .slice(0, 20),
    [sessions]
  );

  // Create and trigger
  const triggerMutation = useMutation({
    mutationFn: async ({
      template,
      userPrompt,
      name,
    }: {
      template: ExecutionTemplate;
      userPrompt: string;
      name: string;
    }) => {
      // Step 1: Create a managed agent configured for this execution
      const systemPrompt = buildSystemPrompt(template, userPrompt, stealthEnabled);

      const agent = await api.createManagedAgent({
        name: name || `Execution: ${template.name}`,
        description: `Auto-created execution agent — ${template.description}`,
        model: { id: model, speed: "standard" },
        system_prompt: systemPrompt,
        tools:
          template.id === "custom"
            ? [{ type: "agent_toolset_20260401" as const }]
            : template.tools.map((t) => ({
                type: "custom" as const,
                name: t,
                default_config: { enabled: true },
              })),
        metadata: {
          execution_template: template.id,
          stealth_mode: stealthEnabled,
          execution_mode: executionMode,
        },
      });

      // Step 2: Trigger execution with the user prompt + stealth config
      const { session } = await api.triggerAgent(agent.id, {
        prompt: userPrompt,
        title: name || `${template.name} execution`,
        source: "manual",
        stealth_mode: stealthEnabled,
        model: model,
        execution_mode: executionMode,
      });

      return session;
    },
    onSuccess: (session) => {
      setActiveSession(session);
      toast.success("Execution started!");
      qc.invalidateQueries({ queryKey: ["managed-sessions"] });
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to start execution");
    },
  });

  const handleLaunch = useCallback(() => {
    if (!selectedTemplate || !prompt.trim()) {
      toast.error("Select a template and enter a prompt");
      return;
    }
    triggerMutation.mutate({
      template: selectedTemplate,
      userPrompt: prompt.trim(),
      name: agentName.trim(),
    });
  }, [selectedTemplate, prompt, agentName, triggerMutation]);

  const handleStop = useCallback(async () => {
    if (!activeSession) return;
    try {
      await api.sendSessionEvents(activeSession.id, {
        events: [{ type: "user.interrupt", content: { message: "Stopped by user" } }],
      });
      toast.success("Execution stopped");
      setActiveSession(null);
    } catch {
      toast.error("Failed to stop execution");
    }
  }, [activeSession]);

  const handleSelectTemplate = useCallback((t: ExecutionTemplate) => {
    setSelectedTemplate(t);
    setPrompt(t.defaultPrompt);
    setStealthEnabled(t.stealthMode);
    setExecutionMode(t.executionMode);
    setAgentName("");
    if (t.id === "custom") {
      setTimeout(() => textareaRef.current?.focus(), 100);
    }
  }, []);

  // ── If there's an active session, show it ─────────────────────────

  if (activeSession) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader className="justify-between">
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setActiveSession(null)}
            >
              ← Back
            </Button>
            <h1 className="text-sm font-semibold">
              Execution: {selectedTemplate?.name ?? "Session"}
            </h1>
          </div>
        </PageHeader>
        <SessionPanel session={activeSession} onStop={handleStop} />
      </div>
    );
  }

  // ── Main Builder UI ───────────────────────────────────────────────

  return (
    <div className="flex flex-col h-full">
      <PageHeader className="justify-between">
        <div className="flex items-center gap-2">
          <Rocket className="h-4 w-4" />
          <h1 className="text-sm font-semibold">Execution Agents</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="xs"
            onClick={() => setShowStealthTools(!showStealthTools)}
          >
            <Shield className="h-3 w-3 mr-1" />
            Anti-Detection Tools
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        {/* Anti-Detection Tools Panel */}
        {showStealthTools && (
          <div className="mx-4 mt-4 rounded-lg border bg-card p-4">
            <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
              <Shield className="h-4 w-4 text-orange-500" />
              Open-Source Anti-Detection & Stealth Tools
            </h3>
            <p className="text-xs text-muted-foreground mb-3">
              These tools help bypass anti-bot protections (Cloudflare,
              DataDome, PerimeterX, reCAPTCHA). They are auto-configured when
              stealth mode is enabled.
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
              {STEALTH_TOOLS.map((tool) => (
                <a
                  key={tool.name}
                  href={tool.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-start gap-2 rounded-md border p-2.5 hover:bg-muted/50 transition-colors"
                >
                  <Globe className="h-3.5 w-3.5 mt-0.5 text-muted-foreground shrink-0" />
                  <div>
                    <span className="text-xs font-medium">{tool.name}</span>
                    <p className="text-[10px] text-muted-foreground leading-tight">
                      {tool.description}
                    </p>
                  </div>
                </a>
              ))}
            </div>
          </div>
        )}

        {/* Template Selection */}
        <div className="p-4">
          <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-3">
            Choose a Template
          </h2>
          <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
            {TEMPLATES.map((t) => {
              const Icon = t.icon;
              const isSelected = selectedTemplate?.id === t.id;
              return (
                <button
                  key={t.id}
                  onClick={() => handleSelectTemplate(t)}
                  className={cn(
                    "flex flex-col items-start gap-1.5 rounded-lg border p-3 text-left transition-all hover:shadow-sm",
                    isSelected
                      ? "border-brand bg-brand/5 ring-1 ring-brand"
                      : "hover:border-muted-foreground/30"
                  )}
                >
                  <div className="flex items-center gap-2 w-full">
                    <div
                      className={cn(
                        "rounded-md p-1.5 border",
                        CATEGORY_COLORS[t.category]
                      )}
                    >
                      <Icon className="h-3.5 w-3.5" />
                    </div>
                    <span className="text-xs font-medium flex-1">{t.name}</span>
                    {t.stealthMode && (
                      <Shield className="h-3 w-3 text-orange-500" />
                    )}
                  </div>
                  <p className="text-[10px] text-muted-foreground leading-tight">
                    {t.description}
                  </p>
                </button>
              );
            })}
          </div>
        </div>

        {/* Prompt Builder */}
        {selectedTemplate && (
          <div className="px-4 pb-4 space-y-3">
            <div className="rounded-lg border bg-card p-4 space-y-3">
              <div className="flex items-center justify-between">
                <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  Configure Execution
                </h2>
                <Badge
                  variant="outline"
                  className={cn("text-[10px]", CATEGORY_COLORS[selectedTemplate.category])}
                >
                  {selectedTemplate.category}
                </Badge>
              </div>

              {/* Agent Name */}
              <div>
                <Label className="text-xs" htmlFor="agent-name">
                  Name (optional)
                </Label>
                <Input
                  id="agent-name"
                  placeholder={`e.g. "Find SaaS founders in Paris"`}
                  value={agentName}
                  onChange={(e) => setAgentName(e.target.value)}
                  className="text-sm h-8"
                />
              </div>

              {/* Model Selection */}
              <div>
                <Label className="text-xs">Model</Label>
                <Select value={model} onValueChange={(v) => { if (v) setModel(v); }}>
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="anthropic/claude-sonnet-4-20250514">
                      Claude Sonnet 4 (fast, cheap)
                    </SelectItem>
                    <SelectItem value="anthropic/claude-opus-4-20250514">
                      Claude Opus 4 (powerful)
                    </SelectItem>
                    <SelectItem value="anthropic/claude-haiku-3-5-20241022">
                      Claude Haiku 3.5 (fastest)
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {/* Prompt */}
              <div>
                <Label className="text-xs" htmlFor="execution-prompt">
                  What should the agent do?
                </Label>
                <Textarea
                  ref={textareaRef}
                  id="execution-prompt"
                  placeholder="Describe exactly what the agent should accomplish..."
                  value={prompt}
                  onChange={(e) => setPrompt(e.target.value)}
                  className="min-h-[120px] text-sm resize-y"
                />
              </div>

              {/* Execution Mode Selector */}
              <div className="rounded-md border p-2.5 space-y-2">
                <div>
                  <p className="text-xs font-medium">Execution mode</p>
                  <p className="text-[10px] text-muted-foreground">
                    How the agent interacts with the world
                  </p>
                </div>
                <div className="grid grid-cols-3 gap-1">
                  {([
                    { id: "browser", label: "Browser", hint: "Drives Chromium like a human (Playwright MCP)" },
                    { id: "routine", label: "Routine", hint: "Headless / direct APIs — fast & cheap" },
                    { id: "hybrid", label: "Hybrid", hint: "APIs first, browser when needed" },
                  ] as const).map((m) => (
                    <button
                      key={m.id}
                      type="button"
                      onClick={() => setExecutionMode(m.id)}
                      title={m.hint}
                      className={`rounded border px-2 py-1.5 text-[11px] transition ${
                        executionMode === m.id
                          ? "border-primary bg-primary/10 font-medium text-primary"
                          : "border-border hover:bg-accent"
                      }`}
                    >
                      {m.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Stealth Toggle */}
              <div className="flex items-center justify-between rounded-md border p-2.5">
                <div className="flex items-center gap-2">
                  <Shield className="h-3.5 w-3.5 text-orange-500" />
                  <div>
                    <p className="text-xs font-medium">Stealth Mode</p>
                    <p className="text-[10px] text-muted-foreground">
                      Anti-bot bypass, rotating fingerprints, human-like delays
                    </p>
                  </div>
                </div>
                <Switch
                  checked={stealthEnabled}
                  onCheckedChange={setStealthEnabled}
                />
              </div>

              {/* Tools Preview */}
              {selectedTemplate.tools.length > 0 && (
                <div>
                  <Label className="text-xs">Tools</Label>
                  <div className="flex flex-wrap gap-1 mt-1">
                    {selectedTemplate.tools.map((tool) => (
                      <Badge key={tool} variant="secondary" className="text-[10px]">
                        {tool}
                      </Badge>
                    ))}
                    {stealthEnabled && (
                      <>
                        <Badge variant="outline" className="text-[10px] border-orange-500/30 text-orange-600">
                          stealth-browser
                        </Badge>
                        <Badge variant="outline" className="text-[10px] border-orange-500/30 text-orange-600">
                          captcha-solver
                        </Badge>
                        <Badge variant="outline" className="text-[10px] border-orange-500/30 text-orange-600">
                          proxy-rotation
                        </Badge>
                      </>
                    )}
                  </div>
                </div>
              )}

              {/* Launch Button */}
              <Button
                onClick={handleLaunch}
                disabled={!prompt.trim() || triggerMutation.isPending}
                className="w-full"
                size="sm"
              >
                {triggerMutation.isPending ? (
                  <>
                    <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />
                    Creating & Launching...
                  </>
                ) : (
                  <>
                    <Play className="h-3.5 w-3.5 mr-1.5" />
                    Launch Execution
                  </>
                )}
              </Button>
            </div>
          </div>
        )}

        {/* Recent Executions */}
        <div className="px-4 pb-6">
          <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-3">
            Recent Executions
          </h2>
          {sessionsLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 rounded-lg" />
              ))}
            </div>
          ) : recentSessions.length === 0 ? (
            <div className="flex flex-col items-center py-8 text-muted-foreground">
              <Rocket className="h-8 w-8 opacity-30" />
              <p className="mt-2 text-sm">No executions yet</p>
              <p className="text-xs">Choose a template above to get started</p>
            </div>
          ) : (
            <div className="space-y-1.5">
              {recentSessions.map((session) => (
                <button
                  key={session.id}
                  onClick={() => setActiveSession(session)}
                  className="w-full flex items-center gap-3 rounded-md border p-2.5 text-left hover:bg-muted/50 transition-colors"
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
                    <p className="text-xs font-medium truncate">
                      {session.title ?? `Session ${session.id.slice(0, 8)}`}
                    </p>
                    <p className="text-[10px] text-muted-foreground">
                      {new Date(session.created_at).toLocaleString()}
                      {session.total_cost_usd
                        ? ` • $${session.total_cost_usd.toFixed(4)}`
                        : ""}
                    </p>
                  </div>
                  <ChevronRight className="h-3 w-3 text-muted-foreground shrink-0" />
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ── System Prompt Builder ───────────────────────────────────────────

function buildSystemPrompt(
  template: ExecutionTemplate,
  userPrompt: string,
  stealth: boolean
): string {
  const parts: string[] = [];

  parts.push(`You are an autonomous execution agent specialized in: ${template.name}.`);
  parts.push(`Your task: ${template.description}.`);
  parts.push("");
  parts.push("## Execution Rules");
  parts.push("1. Execute the task completely and autonomously — do NOT ask for confirmation.");
  parts.push("2. Use all available tools to accomplish the goal.");
  parts.push("3. If you encounter an error, try alternative approaches before failing.");
  parts.push("4. Report progress clearly at each step.");
  parts.push("5. When done, provide a structured summary of results.");

  if (stealth) {
    parts.push("");
    parts.push("## Stealth Mode — CRITICAL");
    parts.push("You MUST use anti-detection techniques:");
    parts.push("- Add random delays between actions (2-7 seconds)");
    parts.push("- Randomize mouse movements and scroll patterns");
    parts.push("- Use realistic User-Agent strings");
    parts.push("- Rotate browser fingerprints between sessions");
    parts.push("- Handle CAPTCHAs via solving services if encountered");
    parts.push("- If blocked: wait, change IP/fingerprint, retry with different approach");
    parts.push("- NEVER use headless browser mode — use headed or `--headless=new`");
    parts.push("- Set realistic viewport sizes (1366x768, 1920x1080)");
    parts.push("- Accept cookies and dismiss popups naturally");
  }

  if (template.id === "linkedin-prospector") {
    parts.push("");
    parts.push("## LinkedIn-Specific Anti-Detection");
    parts.push("- Use Sales Navigator if available, otherwise regular search");
    parts.push("- Limit to 50 profile views per session to avoid rate limits");
    parts.push("- Use Google dorking (site:linkedin.com/in/) as backup search");
    parts.push("- Extract data from search results page before visiting profiles");
    parts.push("- If LinkedIn blocks: switch to Apollo.io, ZoomInfo, or Lusha alternatives");
    parts.push("- Use browser cookies from a logged-in session if available");
  }

  return parts.join("\n");
}
