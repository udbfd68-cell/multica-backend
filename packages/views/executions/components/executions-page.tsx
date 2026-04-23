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
  Target,
  ShieldCheck,
  Users,
  Check,
  Ban,
  BookOpen,
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

  // ── MCP auto-selection ──────────────────────────────────────────────────
  // Each rule maps a keyword regex → catalog slug. All slugs below exist in
  // server/internal/mcp/catalog.go. Keep alphabetized within each section.
  const mcps: string[] = [];
  const add = (slug: string) => {
    if (!mcps.includes(slug)) mcps.push(slug);
  };

  // Browser automation (default for web/hybrid modes)
  if (mode === "browser" || mode === "hybrid") add("playwright");
  if (/\b(headed|visible browser|with cookies|my account|my gmail|my drive|logged in)\b/.test(text)) add("playwright-headed");
  if (/\b(puppeteer|headless chrome)\b/.test(text)) add("puppeteer");
  if (/\b(devtools|inspect|network tab|console log|debug browser)\b/.test(text)) add("chrome-devtools");

  // Version control & code hosting
  if (/\b(github|repo|repository|pull request|\bpr\b|merge|issue|commit|branch)\b/.test(text)) add("github");
  if (/\b(gitlab|merge request|\bmr\b)\b/.test(text)) add("gitlab");
  if (/\b(azure devops|ado pipeline|work item)\b/.test(text)) add("azure-devops");
  if (/\b(git log|git diff|git clone|local repo)\b/.test(text)) add("git");
  if (/\b(linear|ticket|cycle)\b/.test(text)) add("linear");
  if (/\b(jira|sprint|epic|backlog)\b/.test(text)) add("jira");
  if (/\b(shortcut|story|iteration)\b/.test(text)) add("shortcut");

  // Databases
  if (/\b(postgres|postgresql|\bpg\b|psql)\b/.test(text)) add("postgres");
  if (/\b(mysql|mariadb)\b/.test(text)) add("mysql");
  if (/\b(sqlite|\.db\b|embedded db)\b/.test(text)) add("sqlite");
  if (/\b(mongo|mongodb|document db)\b/.test(text)) add("mongodb");
  if (/\b(redis|cache|key[- ]value)\b/.test(text)) add("redis");
  if (/\b(supabase|edge function)\b/.test(text)) add("supabase");
  if (/\b(neon)\b/.test(text)) add("neon");
  if (/\b(qdrant|vector|embedding|semantic search)\b/.test(text)) add("qdrant");

  // Communication
  if (/\b(slack|channel|#\w+)\b/.test(text)) add("slack");
  if (/\b(discord|guild|voice channel)\b/.test(text)) add("discord");
  if (/\b(telegram|tg bot)\b/.test(text)) add("telegram");
  if (/\b(whatsapp|wa message)\b/.test(text)) add("whatsapp");
  if (/\b(twitter|\bx\.com|tweet|\bpost on x\b)\b/.test(text)) add("twitter");
  if (/\b(notion|wiki|knowledge base)\b/.test(text)) add("notion");
  if (/\b(gmail|send email|inbox|outlook|mail merge)\b/.test(text)) add("gmail");

  // Google Workspace all-in-one (Drive, Docs, Sheets, Calendar, Forms)
  if (/\b(google drive|\bgdrive\b|google docs?|google sheets?|google calendar|google form|spreadsheet|calendar event|meeting)\b/.test(text)) add("google-workspace");

  // Search / research / scraping
  if (/\b(search|look up|find info|\bgoogle\b|duckduckgo|bing)\b/.test(text)) add("brave-search");
  if (/\b(research|deep research|study|investigate|literature)\b/.test(text)) add("tavily");
  if (/\b(semantic search|neural search|\bexa\b)\b/.test(text)) add("exa");
  if (/\b(scrape|crawl|extract|firecrawl)\b/.test(text)) add("firecrawl");
  if (/\b(apify|mass scrape|actor)\b/.test(text)) add("apify");
  if (/\b(fetch url|download page|web page|html)\b/.test(text)) add("fetch");
  if (/\b(maps?|directions|geocode|place|address)\b/.test(text)) add("google-maps");
  if (/\b(mapbox|tile)\b/.test(text)) add("mapbox");

  // Sandbox / code execution
  if (/\b(sandbox|run code|execute|python script|shell command)\b/.test(text)) add("e2b");
  if (/\b(docker|container|image)\b/.test(text)) add("docker");

  // Cloud & DevOps
  if (/\b(aws|s3|lambda|ec2|cloudformation|dynamodb)\b/.test(text)) add("aws");
  if (/\b(azure|microsoft cloud|\bakv\b)\b/.test(text)) add("azure");
  if (/\b(cloudflare|workers?|\bkv\b|\br2\b|\bd1\b)\b/.test(text)) add("cloudflare");
  if (/\b(vercel|deploy frontend)\b/.test(text)) add("vercel");
  if (/\b(railway|rails app deploy)\b/.test(text)) add("railway");
  if (/\b(heroku|dyno)\b/.test(text)) add("heroku");
  if (/\b(kubernetes|\bk8s\b|kubectl|helm)\b/.test(text)) add("kubernetes");
  if (/\b(terraform|\bhcl\b|\biac\b)\b/.test(text)) add("terraform");
  if (/\b(pulumi)\b/.test(text)) add("pulumi");
  if (/\b(circleci|circle ci)\b/.test(text)) add("circleci");

  // Monitoring / observability
  if (/\b(sentry|error tracking|stacktrace)\b/.test(text)) add("sentry");
  if (/\b(datadog|\bddog\b|apm)\b/.test(text)) add("datadog");
  if (/\b(grafana|dashboard|metrics)\b/.test(text)) add("grafana");

  // Productivity / project management
  if (/\b(asana)\b/.test(text)) add("asana");
  if (/\b(todoist|todo list)\b/.test(text)) add("todoist");
  if (/\b(clickup)\b/.test(text)) add("clickup");
  if (/\b(figma|design file)\b/.test(text)) add("figma");

  // CRM / business
  if (/\b(salesforce|\bsfdc\b|soql)\b/.test(text)) add("salesforce");
  if (/\b(hubspot|crm contact|deal pipeline)\b/.test(text)) add("hubspot");
  if (/\b(stripe|payment|subscription|invoice|checkout)\b/.test(text)) add("stripe");

  // Utilities / local / files
  if (/\b(file|filesystem|read file|write file|folder|directory)\b/.test(text)) add("filesystem");
  if (/\b(time|timezone|convert date|schedule for)\b/.test(text)) add("time");
  if (/\b(translate|translation|deepl|french|english|spanish|japanese|chinese)\b/.test(text)) add("deepl");

  // Automation
  if (/\b(n8n|workflow automation|trigger webhook)\b/.test(text)) add("n8n");

  // Memory / knowledge / reasoning
  if (/\b(remember|persistent memory|knowledge graph)\b/.test(text)) add("memory");
  if (/\b(library docs?|api reference|\bnpm package\b|how to use)\b/.test(text)) add("context7");
  if (isComplex || /\b(step by step|reasoning|plan carefully|think through)\b/.test(text)) add("sequential-thinking");

  // UI / design generation
  if (/\b(ui component|react component|shadcn|magic ui)\b/.test(text)) add("21st-dev");
  if (/\b(storybook|story file)\b/.test(text)) add("storybook");
  if (/\b(eslint|lint)\b/.test(text)) add("eslint");

  // New verified GitHub MCPs (catalog ≥ 2026-04-23)
  if (/\b(perplexity|cited answer)\b/.test(text)) add("perplexity");
  if (/\b(youtube transcript|video transcript|caption)\b/.test(text)) add("youtube-transcript");
  if (/\b(youtube video|yt channel|playlist)\b/.test(text)) add("youtube-data");
  if (/\b(airtable|base|\brecord\b)\b/.test(text)) add("airtable");
  if (/\b(obsidian|vault|\.md note)\b/.test(text)) add("obsidian");
  if (/\b(shopify|ecommerce|product listing)\b/.test(text)) add("shopify");
  if (/\b(sendgrid)\b/.test(text)) add("sendgrid");
  if (/\b(resend)\b/.test(text)) add("resend");
  if (/\b(twilio|\bsms\b|text message|phone call)\b/.test(text)) add("twilio");
  if (/\b(intercom)\b/.test(text)) add("intercom");
  if (/\b(zendesk|helpdesk|support ticket)\b/.test(text)) add("zendesk");
  if (/\b(spotify|playlist|song|music)\b/.test(text)) add("spotify");
  if (/\b(reddit|subreddit|\br\/)\b/.test(text)) add("reddit");
  if (/\b(elasticsearch|elastic|\belk\b)\b/.test(text)) add("elasticsearch");
  if (/\b(clickhouse|olap)\b/.test(text)) add("clickhouse");
  if (/\b(bigquery|big query|gcp data)\b/.test(text)) add("bigquery");
  if (/\b(snowflake)\b/.test(text)) add("snowflake");
  if (/\b(pagerduty|on[- ]call|incident)\b/.test(text)) add("pagerduty");
  if (/\b(opensearch)\b/.test(text)) add("opensearch");
  if (/\b(arxiv|paper|academic|preprint)\b/.test(text)) add("arxiv");
  if (/\b(wikipedia|wiki article)\b/.test(text)) add("wikipedia");
  if (/\b(hacker news|\bhn\b|ycombinator)\b/.test(text)) add("hackernews");
  if (/\b(trello|kanban board)\b/.test(text)) add("trello");
  if (/\b(jupyter|\.ipynb|notebook)\b/.test(text)) add("jupyter");
  if (/\b(excel|xlsx|spreadsheet)\b/.test(text)) add("excel");
  if (/\b(pdf|\.pdf\b)\b/.test(text)) add("pdf");
  if (/\b(calendly|book meeting|schedule call)\b/.test(text)) add("calendly");

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

function EventCard({
  event,
  onConfirm,
}: {
  event: StoreEvent;
  onConfirm?: (allow: boolean, toolCallId?: string) => void;
}) {
  const typeColors: Record<string, string> = {
    user_message: "border-l-white",
    assistant_message: "border-l-white/60",
    tool_call: "border-l-white/80",
    tool_result: "border-l-white/50",
    system_event: "border-l-white/30",
    cost_event: "border-l-white/30",
    thinking: "border-l-white/70",
  };

  // Claude Managed Agents "requires_action" pattern — surfaced by the backend
  // as a system_event named tool_confirmation, or as a tool_call flagged with
  // requires_confirmation=true (always_ask policy or MCP toolset default).
  const data = event.data as typeof event.data & {
    requires_confirmation?: boolean;
    event_name?: string;
  };
  const isConfirmation =
    (event.type === "system_event" && data.event_name === "tool_confirmation") ||
    (event.type === "tool_call" && data.requires_confirmation === true);

  // Outcome evaluation spans (managed-agents-2026-04-01-research-preview).
  const isOutcome =
    event.type === "system_event" &&
    (data.event_name === "outcome_evaluation_end" ||
      data.event_name === "outcome_evaluation_ongoing" ||
      data.event_name === "outcome_evaluation_start");

  return (
    <div
      className={cn(
        "border-l-2 pl-3 py-2 text-xs font-mono",
        isConfirmation && "border-l-amber-400 bg-amber-500/5",
        isOutcome && "border-l-emerald-400 bg-emerald-500/5",
        !isConfirmation && !isOutcome && (typeColors[event.type] ?? "border-l-white/20"),
      )}
    >
      <div className="flex items-center gap-2 mb-1">
        <Badge
          variant="outline"
          className="text-[10px] px-1.5 py-0 text-foreground border-foreground/30"
        >
          {isConfirmation ? "tool_confirmation" : isOutcome ? "outcome" : event.type}
        </Badge>
        <span className="text-foreground/60">#{event.index}</span>
      </div>
      <div className="text-foreground/80 whitespace-pre-wrap line-clamp-6">
        {data.content ??
          data.thinking ??
          data.summary ??
          (data.tool_name
            ? `${data.tool_name}(${JSON.stringify(data.input ?? {})})`
            : data.details ?? JSON.stringify(data, null, 2))}
      </div>
      {isConfirmation && onConfirm && (
        <div className="flex items-center gap-2 mt-2">
          <Button size="xs" variant="default" onClick={() => onConfirm(true, data.call_id)}>
            <Check className="h-3 w-3 mr-1" />
            Allow
          </Button>
          <Button size="xs" variant="destructive" onClick={() => onConfirm(false, data.call_id)}>
            <Ban className="h-3 w-3 mr-1" />
            Deny
          </Button>
        </div>
      )}
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

  const handleConfirm = useCallback(
    async (allow: boolean, toolCallId?: string) => {
      try {
        await api.sendSessionEvents(session.id, {
          events: [
            {
              type: "user.tool_confirmation",
              content: {
                decision: allow ? "allow" : "deny",
                tool_call_id: toolCallId,
              },
            },
          ],
        });
        toast.success(allow ? "Tool allowed" : "Tool denied");
      } catch {
        toast.error("Failed to send confirmation");
      }
    },
    [session.id],
  );

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
          <EventCard key={event.id} event={event} onConfirm={handleConfirm} />
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

  // Claude Managed Agents advanced features (all optional, sensible defaults).
  const [outcomeEnabled, setOutcomeEnabled] = useState(false);
  const [outcomeRubric, setOutcomeRubric] = useState("");
  const [maxIterations, setMaxIterations] = useState(3);
  const [precisionMode, setPrecisionMode] = useState(false); // always_ask on MCP
  const [delegationMode, setDelegationMode] = useState(false); // spawn sub-agents
  const [guardrailsEnabled, setGuardrailsEnabled] = useState(true);

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

      // ── Claude Managed Agents parity ──────────────────────────────────
      // 1. MEMORY: workspace-scoped persistent memory store (auto-provisioned
      //    once, reused forever). See docs/managed-agents/memory.
      let memoryStoreId: string | null = null;
      try {
        const { data: stores } = await api.listMemoryStores();
        const existing = stores.find((s) => s.name === "Workspace Default Memory");
        if (existing) {
          memoryStoreId = existing.id;
        } else {
          const created = await api.createMemoryStore({
            name: "Workspace Default Memory",
            description:
              "Persistent cross-session memory for this workspace. Agents read learnings before starting and write durable insights when finished.",
          });
          memoryStoreId = created.id;
        }
      } catch (e) {
        console.warn("[memory] auto-provision failed:", e);
      }

      // 2. VAULT: workspace-scoped credential store for MCP OAuth. Auto-
      //    provisioned once and passed via session.vault_ids so Anthropic can
      //    refresh OAuth tokens transparently. See docs/managed-agents/vaults.
      let defaultVaultId: string | null = null;
      try {
        const { data: vaults } = await api.listVaults();
        const existing = vaults.find(
          (v) => v.display_name === "Workspace Default Vault",
        );
        if (existing) {
          defaultVaultId = existing.id;
        } else {
          const created = await api.createVault({
            display_name: "Workspace Default Vault",
            metadata: {
              purpose: "default_mcp_oauth",
              external_user_id: wsId ?? "workspace",
            },
          });
          defaultVaultId = created.id;
        }
      } catch (e) {
        console.warn("[vault] auto-provision failed:", e);
      }

      // 3. SKILLS: auto-attach all workspace skills to the agent. Each skill
      //    contributes metadata (~100 tokens) at load time and is fully loaded
      //    on trigger-match. See docs/agent-skills/overview.
      let workspaceSkillIds: string[] = [];
      try {
        const skills = await api.listSkills();
        workspaceSkillIds = skills.map((s) => s.id);
      } catch (e) {
        console.warn("[skills] listing failed:", e);
      }

      // 4. GUARDRAILS: safety-by-default system-prompt additions aligned with
      //    docs/test-and-evaluate/strengthen-guardrails/*.
      const guardrails = guardrailsEnabled
        ? [
            "",
            "SAFETY GUARDRAILS:",
            "- If unsure or information is missing, say so explicitly instead of fabricating.",
            "- Cite sources when making factual claims from web_fetch / web_search.",
            "- Never execute destructive commands (rm -rf, DROP TABLE, force-push) without explicit user confirmation.",
            "- Never expose, log, or transmit credentials, tokens, or secrets.",
            "- Refuse requests for disallowed content; redirect to safe alternatives.",
          ].join("\n")
        : "";

      const precisionHint = precisionMode
        ? "\nPRECISION MODE: each MCP tool invocation will require user confirmation (always_ask policy). Plan efficiently — batch related steps."
        : "";

      const delegationHint = delegationMode
        ? "\nDELEGATION MODE: you may delegate sub-tasks to specialist callable agents (reviewer, tester) using the multi-agent protocol."
        : "";

      const outcomeHint =
        outcomeEnabled && outcomeRubric.trim()
          ? `\nOUTCOME RUBRIC:\n${outcomeRubric.trim()}\nIterate up to ${maxIterations} times until the rubric is satisfied. Write final deliverables to /mnt/session/outputs/.`
          : "";

      const systemPrompt = [
        "You are an autonomous AI agent running on the Aurion platform.",
        "Execute the user's task completely and autonomously without asking for confirmation.",
        "Use every tool and MCP server available to you.",
        "Reason step-by-step, act, verify results, and report concisely.",
        "",
        `Execution mode: ${cfg.mode}`,
        `Auto-selected MCP servers: ${cfg.mcpServers.join(", ") || "none (built-in tools only)"}`,
        memoryStoreId
          ? `Persistent memory store attached: ${memoryStoreId}. Use memory_list/memory_search/memory_read before starting; memory_write durable learnings when done.`
          : "",
        workspaceSkillIds.length > 0
          ? `${workspaceSkillIds.length} workspace skill(s) attached. Consult their SKILL.md metadata and load on match.`
          : "",
        precisionHint,
        delegationHint,
        outcomeHint,
        guardrails,
        "",
        "When the task is complete, provide a clear summary of what was done and what was found.",
      ]
        .filter(Boolean)
        .join("\n");

      // 5. CALLABLE AGENTS: create specialist sub-agents the orchestrator can
      //    delegate to. One level of delegation only, per docs/managed-agents/
      //    multi-agent. We spin up two reusable reviewers scoped to the
      //    workspace (idempotent — reused across sessions by name).
      let callableAgentIds: string[] = [];
      if (delegationMode) {
        try {
          const { data: existingAgents } = await api.listManagedAgents();
          const findOrCreate = async (
            name: string,
            roleSystem: string,
          ): Promise<string> => {
            const found = existingAgents.find((a) => a.name === name);
            if (found) return found.id;
            const created = await api.createManagedAgent({
              name,
              description: `Specialist sub-agent: ${name}`,
              model: { id: "anthropic/claude-haiku-4-5", speed: "fast" },
              system_prompt: roleSystem,
              tools: [{ type: "agent_toolset_20260401" }],
              metadata: { source: "executions-delegation" },
            });
            return created.id;
          };
          const reviewerId = await findOrCreate(
            "Delegation: Reviewer",
            "You are a strict code/content reviewer. Verify correctness, security, and alignment with the requested outcome. Report issues tersely with file:line citations.",
          );
          const testerId = await findOrCreate(
            "Delegation: Tester",
            "You are a test-writer. Given a piece of code or a spec, produce runnable tests covering the critical paths and edge cases. Return test code only.",
          );
          callableAgentIds = [reviewerId, testerId];
        } catch (e) {
          console.warn("[delegation] sub-agent provisioning failed:", e);
        }
      }

      const agent = await api.createManagedAgent({
        name: `Chat: ${titleLine}`,
        description: desc.slice(0, 200),
        model: { id: cfg.model, speed: "standard" },
        system_prompt: systemPrompt,
        tools: [{ type: "agent_toolset_20260401" as const }],
        callable_agents: callableAgentIds,
        metadata: {
          source: "chat-executions",
          auto_mcp_servers: cfg.mcpServers,
          auto_reasoning: cfg.reasoning,
          execution_mode: cfg.mode,
          memory_store_ids: memoryStoreId ? [memoryStoreId] : [],
          default_vault_id: defaultVaultId,
          skill_ids: workspaceSkillIds,
          precision_mode: precisionMode,
          delegation_mode: delegationMode,
          guardrails_enabled: guardrailsEnabled,
          outcome_enabled: outcomeEnabled,
          outcome_max_iterations: outcomeEnabled ? maxIterations : null,
        },
      });

      // Skills are referenced in the system prompt (setAgentSkills targets the
      // legacy /api/agents endpoint, not /api/v1/agents — so we skip the attach
      // call and rely on metadata.skill_ids + system prompt mention).

      // 6. MCP auto-attach (registry lookup).
      if (cfg.mcpServers.length > 0) {
        try {
          // Ensure every built-in catalog entry is materialized in the
          // workspace registry (idempotent, no-op if already seeded).
          try { await api.seedAllMcpRegistry(); } catch { /* non-fatal */ }
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
            }),
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
        vault_ids: defaultVaultId ? [defaultVaultId] : undefined,
      });

      // Outcome rubric is injected into the system prompt via outcomeHint —
      // the backend's SendSessionEvents does not yet accept user.define_outcome,
      // so prompt-based iteration is our current fallback.

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
                <div className="flex items-center gap-1.5 text-[11px] text-foreground/70 pt-1 border-t border-foreground/10">
                  <Brain className="h-3 w-3" />
                  Persistent memory: <span className="font-medium">Workspace Default</span>
                  <span className="text-foreground/40">— learnings persist across sessions</span>
                </div>
                <div className="text-[11px] text-foreground/60">{auto.reasoning}</div>
              </div>
            )}

            {/* Claude Managed Agents — advanced toggles */}
            <div className="rounded-xl border border-foreground/15 p-4 space-y-3">
              <div className="flex items-center gap-2 text-[11px] uppercase tracking-wider text-foreground/60">
                <Sparkles className="h-3 w-3" />
                Claude advanced controls
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <label className="flex items-start gap-2 rounded-lg border border-foreground/15 p-2.5 cursor-pointer hover:border-foreground/40 transition-colors">
                  <input
                    type="checkbox"
                    className="mt-0.5 accent-foreground"
                    checked={guardrailsEnabled}
                    onChange={(e) => setGuardrailsEnabled(e.target.checked)}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 text-xs font-medium">
                      <ShieldCheck className="h-3 w-3" />
                      Safety guardrails
                    </div>
                    <div className="text-[11px] text-foreground/60">
                      Harm screens, citation requirement, no destructive ops
                    </div>
                  </div>
                </label>
                <label className="flex items-start gap-2 rounded-lg border border-foreground/15 p-2.5 cursor-pointer hover:border-foreground/40 transition-colors">
                  <input
                    type="checkbox"
                    className="mt-0.5 accent-foreground"
                    checked={precisionMode}
                    onChange={(e) => setPrecisionMode(e.target.checked)}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 text-xs font-medium">
                      <BookOpen className="h-3 w-3" />
                      Precision mode
                    </div>
                    <div className="text-[11px] text-foreground/60">
                      Confirm each MCP call (always_ask policy)
                    </div>
                  </div>
                </label>
                <label className="flex items-start gap-2 rounded-lg border border-foreground/15 p-2.5 cursor-pointer hover:border-foreground/40 transition-colors">
                  <input
                    type="checkbox"
                    className="mt-0.5 accent-foreground"
                    checked={delegationMode}
                    onChange={(e) => setDelegationMode(e.target.checked)}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 text-xs font-medium">
                      <Users className="h-3 w-3" />
                      Delegation mode
                    </div>
                    <div className="text-[11px] text-foreground/60">
                      Spawn reviewer + tester callable sub-agents
                    </div>
                  </div>
                </label>
                <label className="flex items-start gap-2 rounded-lg border border-foreground/15 p-2.5 cursor-pointer hover:border-foreground/40 transition-colors">
                  <input
                    type="checkbox"
                    className="mt-0.5 accent-foreground"
                    checked={outcomeEnabled}
                    onChange={(e) => setOutcomeEnabled(e.target.checked)}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5 text-xs font-medium">
                      <Target className="h-3 w-3" />
                      Outcome rubric
                    </div>
                    <div className="text-[11px] text-foreground/60">
                      Iterate until rubric satisfied (max {maxIterations})
                    </div>
                  </div>
                </label>
              </div>

              {outcomeEnabled && (
                <div className="space-y-2 pt-2 border-t border-foreground/10">
                  <div className="text-[11px] text-foreground/60">
                    Rubric (markdown) — evaluator checks each criterion before marking satisfied
                  </div>
                  <Textarea
                    value={outcomeRubric}
                    onChange={(e) => setOutcomeRubric(e.target.value)}
                    placeholder={`# Success Criteria\n- [ ] All tests pass\n- [ ] No lint errors\n- [ ] Deliverable in /mnt/session/outputs/`}
                    className="min-h-[100px] text-xs font-mono"
                  />
                  <div className="flex items-center gap-2">
                    <span className="text-[11px] text-foreground/60">Max iterations</span>
                    <input
                      type="range"
                      min={1}
                      max={20}
                      value={maxIterations}
                      onChange={(e) => setMaxIterations(Number(e.target.value))}
                      className="flex-1 accent-foreground"
                    />
                    <span className="text-xs font-medium w-8 text-right">{maxIterations}</span>
                  </div>
                </div>
              )}
            </div>

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
