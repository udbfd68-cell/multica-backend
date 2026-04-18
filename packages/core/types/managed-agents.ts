// Managed Agents v1 API types — Claude Managed Agents compatible

export interface ManagedAgent {
  id: string;
  name: string;
  description: string | null;
  model: ManagedAgentModel;
  system_prompt: string | null;
  tools: ManagedAgentTool[];
  mcp_servers: McpServer[];
  skills: ManagedAgentSkillRef[];
  callable_agents: string[];
  metadata: Record<string, unknown>;
  version: number;
  created_at: string;
  updated_at: string;
  archived_at: string | null;
}

export interface ManagedAgentModel {
  id: string;
  speed: "standard" | "fast";
}

export interface ManagedAgentTool {
  type: "agent_toolset_20260401" | "custom";
  name?: string;
  default_config?: {
    enabled: boolean;
  };
}

export interface McpServer {
  url: string;
  name?: string;
  auth_type?: "mcp_oauth" | "bearer";
}

export interface ManagedAgentSkillRef {
  id: string;
  name: string;
}

export interface ManagedAgentVersion {
  id: string;
  version: number;
  snapshot: ManagedAgent;
  created_at: string;
}

export interface CreateManagedAgentRequest {
  name: string;
  description?: string;
  model?: ManagedAgentModel;
  system_prompt?: string;
  tools?: ManagedAgentTool[];
  mcp_servers?: McpServer[];
  callable_agents?: string[];
  metadata?: Record<string, unknown>;
}

export interface UpdateManagedAgentRequest {
  name?: string;
  description?: string;
  model?: ManagedAgentModel;
  system_prompt?: string;
  tools?: ManagedAgentTool[];
  mcp_servers?: McpServer[];
  callable_agents?: string[];
  metadata?: Record<string, unknown>;
}

// Environments
export interface ManagedEnvironment {
  id: string;
  name: string;
  config: EnvironmentConfig;
  created_at: string;
  archived_at: string | null;
}

export interface EnvironmentConfig {
  type: "cloud";
  packages?: Record<string, string>;
  networking?: { type: "unrestricted" | "restricted" };
}

export interface CreateEnvironmentRequest {
  name: string;
  config?: EnvironmentConfig;
}

// Sessions
export type ManagedSessionStatus = "idle" | "running" | "rescheduling" | "terminated";

export interface ManagedSession {
  id: string;
  agent_id: string;
  agent_version: number;
  status: ManagedSessionStatus;
  vault_ids: string[];
  resources: SessionResource[];
  usage: SessionUsage;
  title: string | null;
  stop_reason: SessionStopReason | null;
  created_at: string;
  updated_at: string;
}

export interface SessionResource {
  type: string;
  uri: string;
}

export interface SessionUsage {
  input_tokens: number;
  output_tokens: number;
  cache_creation_tokens: number;
  cache_read_tokens: number;
}

export interface SessionStopReason {
  type: "end_turn" | "max_tokens" | "stop_sequence" | "tool_use" | "paused" | "terminated";
  message?: string;
}

export interface CreateManagedSessionRequest {
  agent_id: string;
  environment_id?: string;
  vault_ids?: string[];
  title?: string;
}

// Session Events
export type SessionEventType =
  | "message_start"
  | "content_block_start"
  | "content_block_delta"
  | "content_block_stop"
  | "message_delta"
  | "message_stop"
  | "tool_use"
  | "tool_result"
  | "error"
  | "done";

export interface SessionEvent {
  id: string;
  type: SessionEventType;
  payload: Record<string, unknown>;
  created_at: string;
}

// Memory Stores
export interface MemoryStore {
  id: string;
  name: string;
  description: string | null;
  created_at: string;
}

export interface MemoryDocument {
  id: string;
  path: string;
  content: string;
  content_sha256: string;
  content_size_bytes: number;
  created_at: string;
  updated_at: string;
}

export interface CreateMemoryStoreRequest {
  name: string;
  description?: string;
}

// Vaults
export interface ManagedVault {
  id: string;
  display_name: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface VaultCredentialSummary {
  id: string;
  mcp_server_url: string;
  auth_type: "mcp_oauth" | "bearer";
  expires_at: string | null;
  created_at: string;
}

export interface CreateVaultRequest {
  display_name: string;
  metadata?: Record<string, unknown>;
}

// Memory Versions
export interface MemoryVersion {
  id: string;
  memory_id: string | null;
  store_id: string;
  operation: "created" | "modified" | "deleted";
  content: string | null;
  content_sha256: string | null;
  content_size_bytes: number | null;
  path: string;
  session_id: string | null;
  created_at: string;
  redacted_at: string | null;
}

export interface WriteMemoryRequest {
  path: string;
  content: string;
  precondition?: {
    type: "not_exists" | "content_sha256";
    content_sha256?: string;
  };
}

export interface UpdateMemoryRequest {
  content?: string;
  precondition?: {
    type: "content_sha256";
    content_sha256: string;
  };
}

// Session Threads
export interface SessionThread {
  id: string;
  session_id: string;
  agent_id: string;
  agent_name: string;
  status: string;
  created_at: string;
}

export interface SendSessionEventsRequest {
  events: Array<{
    type: string;
    session_thread_id?: string;
    content?: Record<string, unknown>;
    [key: string]: unknown;
  }>;
}

// Vault Credential
export interface AddVaultCredentialRequest {
  mcp_server_url: string;
  auth: {
    type: "mcp_oauth" | "bearer";
    token?: string;
    [key: string]: unknown;
  };
}

// Paginated list response
export interface PaginatedResponse<T> {
  data: T[];
}
