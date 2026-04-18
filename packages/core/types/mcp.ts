// MCP Server Registry & Connector types

export type McpTransport = "stdio" | "sse" | "streamable-http";
export type McpAuthType = "none" | "bearer" | "mcp_oauth" | "api_key" | "env_var";
export type McpConnectorStatus = "pending" | "connected" | "error" | "disabled";

export type McpCategory =
  | "version_control"
  | "database"
  | "communication"
  | "search"
  | "sandbox"
  | "cloud"
  | "monitoring"
  | "productivity"
  | "browser"
  | "memory"
  | "finance"
  | "utility"
  | "ai"
  | "other";

// Built-in catalog entry (no DB, static)
export interface McpCatalogEntry {
  slug: string;
  name: string;
  description: string;
  category: McpCategory;
  repo_url: string;
  transport: McpTransport;
  command: string;
  auth_type: McpAuthType;
  env_vars: McpEnvVar[];
  tags: string[];
}

export interface McpEnvVar {
  name: string;
  description: string;
  required: boolean;
}

// Registry entry (DB-backed, workspace-scoped)
export interface McpRegistryEntry {
  id: string;
  is_builtin: boolean;
  slug: string;
  name: string;
  description: string;
  category: McpCategory;
  icon_url: string;
  repo_url: string;
  server_url: string;
  transport: McpTransport;
  command: string;
  args: unknown[];
  env_vars: McpEnvVar[];
  auth_type: McpAuthType;
  oauth_config?: Record<string, unknown>;
  tags: string[];
  created_at: string;
  updated_at: string;
}

export interface CreateMcpRegistryRequest {
  slug: string;
  name: string;
  description?: string;
  category?: McpCategory;
  repo_url?: string;
  server_url?: string;
  transport?: McpTransport;
  command?: string;
  args?: unknown[];
  env_vars?: McpEnvVar[];
  auth_type?: McpAuthType;
  oauth_config?: Record<string, unknown>;
  tags?: string[];
}

// Agent MCP Connector
export interface McpConnector {
  id: string;
  agent_id: string;
  registry_id?: string;
  name: string;
  server_url: string;
  transport: McpTransport;
  command: string;
  args: unknown[];
  auth_type: McpAuthType;
  vault_credential_id?: string;
  enabled: boolean;
  status: McpConnectorStatus;
  status_message?: string;
  last_validated_at?: string;
  discovered_tools: McpDiscoveredTool[];
  tools_discovered_at?: string;
  created_at: string;
  updated_at: string;
}

export interface McpDiscoveredTool {
  name: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
}

export interface CreateMcpConnectorRequest {
  registry_id?: string;
  name: string;
  server_url?: string;
  transport?: McpTransport;
  command?: string;
  args?: unknown[];
  env_config?: Record<string, string>;
  auth_type?: McpAuthType;
  vault_credential_id?: string;
  enabled?: boolean;
}

export interface UpdateMcpConnectorRequest {
  name?: string;
  server_url?: string;
  transport?: McpTransport;
  command?: string;
  args?: unknown[];
  env_config?: Record<string, string>;
  auth_type?: McpAuthType;
  vault_credential_id?: string;
  enabled?: boolean;
}

export interface AddFromRegistryRequest {
  registry_id: string;
  vault_credential_id?: string;
}

export interface McpValidationResult {
  id: string;
  status: McpConnectorStatus;
  status_message?: string;
  transport: McpTransport;
}

export interface McpToolDiscoveryResult {
  id: string;
  name: string;
  tools: McpDiscoveredTool[];
}
