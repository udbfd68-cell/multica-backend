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
  // Session Store fields (Managed Agents architecture)
  last_event_index?: number;
  context_strategy?: ContextStrategy | null;
  total_cost_usd?: number;
  wake_count?: number;
  last_wake_at?: string | null;
}

export interface ContextStrategy {
  type: "sliding_window" | "smart_summary" | "full_replay";
  max_tokens: number;
  summary_model?: string;
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

// Session Store Events (Managed Agents architecture)
export type StoreEventType =
  | "user_message"
  | "assistant_message"
  | "tool_call"
  | "tool_result"
  | "context_reset"
  | "system_event"
  | "cost_event"
  | "thinking";

export interface StoreEvent {
  id: string;
  session_id: string;
  type: StoreEventType;
  index: number;
  timestamp: string;
  data: StoreEventData;
  metadata?: StoreEventMeta;
}

export interface StoreEventData {
  role?: string;
  content?: string;
  tool_name?: string;
  call_id?: string;
  input?: Record<string, unknown>;
  output?: string;
  is_error?: boolean;
  summary?: string;
  compacted_range?: [number, number];
  event_name?: string;
  details?: string;
  thinking?: string;
}

export interface StoreEventMeta {
  tokens_input?: number;
  tokens_output?: number;
  tokens_cached?: number;
  cost_usd?: number;
  provider?: string;
  model?: string;
  duration_ms?: number;
}

export interface SessionCostReport {
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cached_tokens: number;
  event_count: number;
  by_operation: OperationCost[];
  by_tool: ToolCost[];
}

export interface OperationCost {
  operation: string;
  total_cost_usd: number;
  call_count: number;
  total_input_tokens: number;
  total_output_tokens: number;
}

export interface ToolCost {
  tool_name: string;
  total_cost_usd: number;
  call_count: number;
  total_duration_ms: number;
}

export interface SessionInfo {
  id: string;
  workspace_id: string;
  agent_id: string;
  status: string;
  last_event_index: number;
  wake_count: number;
  total_cost_usd: number;
  context_strategy: ContextStrategy | null;
  created_at: string;
  updated_at: string;
}

export interface BudgetStatus {
  allowed: boolean;
  daily_spent_usd: number;
  monthly_spent_usd: number;
  daily_limit_usd?: number;
  monthly_limit_usd?: number;
  reason?: string;
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
