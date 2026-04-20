"use client";

import { useState, useEffect } from "react";
import {
  Plus,
  Plug,
  Trash2,
  ExternalLink,
  CheckCircle2,
  AlertCircle,
  Loader2,
  Search,
  Shield,
} from "lucide-react";
import type { Agent } from "@aurion/core/types";
import type { McpCatalogEntry, McpConnector } from "@aurion/core/types/mcp";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@aurion/ui/components/ui/dialog";
import { Button } from "@aurion/ui/components/ui/button";
import { Input } from "@aurion/ui/components/ui/input";
import { toast } from "sonner";
import { api } from "@aurion/core/api";

// Category icons/colors
const categoryColors: Record<string, string> = {
  communication: "bg-blue-500/10 text-blue-500",
  productivity: "bg-purple-500/10 text-purple-500",
  search: "bg-orange-500/10 text-orange-500",
  browser: "bg-cyan-500/10 text-cyan-500",
  database: "bg-green-500/10 text-green-500",
  version_control: "bg-gray-500/10 text-gray-500",
  cloud: "bg-indigo-500/10 text-indigo-500",
  sandbox: "bg-yellow-500/10 text-yellow-500",
  monitoring: "bg-red-500/10 text-red-500",
  utility: "bg-slate-500/10 text-slate-500",
  memory: "bg-pink-500/10 text-pink-500",
  finance: "bg-emerald-500/10 text-emerald-500",
  ai: "bg-violet-500/10 text-violet-500",
};

// Status badge
function ConnectorStatus({ status }: { status: string }) {
  if (status === "connected") {
    return (
      <span className="flex items-center gap-1 text-xs text-green-600">
        <CheckCircle2 className="h-3 w-3" />
        Connected
      </span>
    );
  }
  if (status === "error") {
    return (
      <span className="flex items-center gap-1 text-xs text-red-500">
        <AlertCircle className="h-3 w-3" />
        Error
      </span>
    );
  }
  return (
    <span className="flex items-center gap-1 text-xs text-muted-foreground">
      <Plug className="h-3 w-3" />
      {status || "Pending"}
    </span>
  );
}

export function McpTab({ agent }: { agent: Agent }) {
  const [connectors, setConnectors] = useState<McpConnector[]>([]);
  const [catalog, setCatalog] = useState<McpCatalogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCatalog, setShowCatalog] = useState(false);
  const [search, setSearch] = useState("");
  const [adding, setAdding] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  // Load connectors and catalog
  useEffect(() => {
    loadData();
  }, [agent.id]);

  async function loadData() {
    setLoading(true);
    try {
      const [connRes, catRes] = await Promise.all([
        api.listAgentMcpConnectors(agent.id),
        api.listMcpCatalog(),
      ]);
      setConnectors(connRes.data || []);
      setCatalog(catRes.data || []);
    } catch (e) {
      toast.error("Failed to load MCP data");
    } finally {
      setLoading(false);
    }
  }

  // Seed + attach from catalog
  async function handleAddFromCatalog(entry: McpCatalogEntry) {
    setAdding(entry.slug);
    try {
      // First seed the registry entry
      const reg = await api.seedMcpRegistry(entry.slug);

      // Then attach to agent
      const connector = await api.addMcpFromRegistry(agent.id, reg.id);
      setConnectors((prev) => [...prev, connector]);
      toast.success(`${entry.name} added`);

      // If it needs OAuth, start the flow
      if (entry.auth_type === "mcp_oauth") {
        await startOAuthFlow(entry, connector);
      }

      setShowCatalog(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add server");
    } finally {
      setAdding(null);
    }
  }

  // Start OAuth flow for a connector
  async function startOAuthFlow(entry: McpCatalogEntry, connector: McpConnector) {
    try {
      // Map catalog slug to OAuth provider name
      const providerMap: Record<string, string> = {
        gmail: "gmail",
        "google-workspace": "google-workspace",
        "google-calendar": "calendar",
        "google-drive": "drive",
        "google-sheets": "sheets",
        "google-docs": "docs",
      };
      const provider = providerMap[entry.slug] || entry.slug;

      const result = await api.initMcpOAuth({
        provider,
        agent_id: agent.id,
        connector_id: connector.id,
        vault_id: "", // Will auto-create if needed
        redirect_url: window.location.href,
      });

      // Redirect to Google OAuth consent
      window.location.href = result.auth_url;
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to start OAuth flow");
    }
  }

  // Delete connector
  async function handleDelete(connectorId: string) {
    setDeleting(connectorId);
    try {
      await api.deleteMcpConnector(agent.id, connectorId);
      setConnectors((prev) => prev.filter((c) => c.id !== connectorId));
      toast.success("Server removed");
    } catch (e) {
      toast.error("Failed to remove server");
    } finally {
      setDeleting(null);
    }
  }

  // Filter catalog by search
  const filteredCatalog = catalog.filter(
    (entry) =>
      entry.name.toLowerCase().includes(search.toLowerCase()) ||
      entry.description.toLowerCase().includes(search.toLowerCase()) ||
      entry.category.toLowerCase().includes(search.toLowerCase()),
  );

  // Group by category
  const grouped = filteredCatalog.reduce(
    (acc, entry) => {
      const cat = entry.category || "other";
      if (!acc[cat]) acc[cat] = [];
      acc[cat].push(entry);
      return acc;
    },
    {} as Record<string, McpCatalogEntry[]>,
  );

  // Check URL for OAuth success
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get("mcp_oauth") === "success") {
      const provider = params.get("provider") || "service";
      const email = params.get("email") || "";
      toast.success(`${provider} connected${email ? ` as ${email}` : ""}`);
      // Clean URL
      const url = new URL(window.location.href);
      url.searchParams.delete("mcp_oauth");
      url.searchParams.delete("provider");
      url.searchParams.delete("email");
      window.history.replaceState({}, "", url.toString());
      // Reload connectors
      loadData();
    }
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">MCP Servers</h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            Connect external services — Gmail, Calendar, GitHub, Slack, and more.
            OAuth servers handle authentication automatically.
          </p>
        </div>
        <Button size="sm" onClick={() => setShowCatalog(true)}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          Add Server
        </Button>
      </div>

      {/* Connected servers */}
      {connectors.length === 0 ? (
        <div className="rounded-lg border border-dashed p-8 text-center">
          <Plug className="h-8 w-8 mx-auto text-muted-foreground/50 mb-3" />
          <p className="text-sm text-muted-foreground">No MCP servers connected</p>
          <p className="text-xs text-muted-foreground mt-1">
            Add servers from the catalog to give this agent access to external services
          </p>
          <Button
            variant="outline"
            size="sm"
            className="mt-4"
            onClick={() => setShowCatalog(true)}
          >
            Browse Catalog
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          {connectors.map((c) => (
            <div
              key={c.id}
              className="flex items-center gap-3 rounded-lg border p-3 hover:bg-muted/30 transition-colors"
            >
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
                <Plug className="h-4 w-4 text-muted-foreground" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium truncate">{c.name}</span>
                  <ConnectorStatus status={c.status} />
                </div>
                {c.status_message && (
                  <p className="text-xs text-muted-foreground truncate mt-0.5">
                    {c.status_message}
                  </p>
                )}
                {c.discovered_tools && c.discovered_tools.length > 0 && (
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {c.discovered_tools.length} tools available
                  </p>
                )}
              </div>
              <div className="flex items-center gap-1">
                {c.auth_type === "mcp_oauth" && c.status !== "connected" && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => {
                      const entry = catalog.find(
                        (e) => e.name === c.name || e.slug === c.name.toLowerCase().replace(/\s+/g, "-"),
                      );
                      if (entry) startOAuthFlow(entry, c);
                    }}
                  >
                    <Shield className="h-3 w-3 mr-1" />
                    Connect
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="h-7 w-7 text-muted-foreground hover:text-destructive"
                  disabled={deleting === c.id}
                  onClick={() => handleDelete(c.id)}
                >
                  {deleting === c.id ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Trash2 className="h-3.5 w-3.5" />
                  )}
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Catalog Dialog */}
      {showCatalog && (
        <Dialog open onOpenChange={(v) => { if (!v) setShowCatalog(false); }}>
          <DialogContent className="max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
            <DialogHeader>
              <DialogTitle>MCP Server Catalog</DialogTitle>
              <DialogDescription>
                Browse available servers. OAuth-enabled servers (marked with 🔐) will
                prompt you to sign in after adding.
              </DialogDescription>
            </DialogHeader>

            {/* Search */}
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search servers..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9"
              />
            </div>

            {/* Scrollable list */}
            <div className="flex-1 overflow-y-auto space-y-4 pr-1">
              {Object.entries(grouped)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([category, entries]) => (
                  <div key={category}>
                    <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                      {category.replace(/_/g, " ")}
                    </h4>
                    <div className="space-y-1">
                      {entries.map((entry) => {
                        const alreadyAdded = connectors.some(
                          (c) => c.name === entry.name,
                        );
                        return (
                          <div
                            key={entry.slug}
                            className="flex items-center gap-3 rounded-md border p-2.5 hover:bg-muted/30 transition-colors"
                          >
                            <div
                              className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-xs font-bold ${
                                categoryColors[entry.category] || "bg-muted text-muted-foreground"
                              }`}
                            >
                              {entry.name.charAt(0)}
                            </div>
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-1.5">
                                <span className="text-sm font-medium">{entry.name}</span>
                                {entry.auth_type === "mcp_oauth" && (
                                  <span className="text-xs" title="Requires OAuth">🔐</span>
                                )}
                              </div>
                              <p className="text-xs text-muted-foreground truncate">
                                {entry.description}
                              </p>
                            </div>
                            <Button
                              variant={alreadyAdded ? "ghost" : "outline"}
                              size="sm"
                              className="h-7 text-xs shrink-0"
                              disabled={alreadyAdded || adding === entry.slug}
                              onClick={() => handleAddFromCatalog(entry)}
                            >
                              {adding === entry.slug ? (
                                <Loader2 className="h-3 w-3 animate-spin" />
                              ) : alreadyAdded ? (
                                <CheckCircle2 className="h-3 w-3 text-green-500" />
                              ) : (
                                <>
                                  <Plus className="h-3 w-3 mr-1" />
                                  Add
                                </>
                              )}
                            </Button>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ))}
              {filteredCatalog.length === 0 && (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No servers match &ldquo;{search}&rdquo;
                </p>
              )}
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={() => setShowCatalog(false)}>
                Close
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
