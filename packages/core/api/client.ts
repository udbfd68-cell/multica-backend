import type {
  Issue,
  CreateIssueRequest,
  UpdateIssueRequest,
  ListIssuesResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  UpdateMeRequest,
  CreateMemberRequest,
  UpdateMemberRequest,
  ListIssuesParams,
  Agent,
  CreateAgentRequest,
  UpdateAgentRequest,
  AgentTask,
  AgentRuntime,
  InboxItem,
  IssueSubscriber,
  Comment,
  Reaction,
  IssueReaction,
  Workspace,
  WorkspaceRepo,
  MemberWithUser,
  User,
  Skill,
  CreateSkillRequest,
  UpdateSkillRequest,
  SetAgentSkillsRequest,
  PersonalAccessToken,
  CreatePersonalAccessTokenRequest,
  CreatePersonalAccessTokenResponse,
  RuntimeUsage,
  IssueUsageSummary,
  RuntimeHourlyActivity,
  RuntimePing,
  RuntimeUpdate,
  TimelineEntry,
  AssigneeFrequencyEntry,
  TaskMessagePayload,
  Attachment,
  ChatSession,
  ChatMessage,
  ChatPendingTask,
  PendingChatTasksResponse,
  SendChatMessageResponse,
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  ListProjectsResponse,
  PinnedItem,
  CreatePinRequest,
  PinnedItemType,
  ReorderPinsRequest,
  Invitation,
  Autopilot,
  AutopilotTrigger,
  AutopilotRun,
  CreateAutopilotRequest,
  UpdateAutopilotRequest,
  CreateAutopilotTriggerRequest,
  UpdateAutopilotTriggerRequest,
  ListAutopilotsResponse,
  GetAutopilotResponse,
  ListAutopilotRunsResponse,
  ManagedAgent,
  ManagedAgentVersion,
  CreateManagedAgentRequest,
  UpdateManagedAgentRequest,
  ManagedEnvironment,
  CreateEnvironmentRequest,
  ManagedSession,
  CreateManagedSessionRequest,
  SessionEvent,
  StoreEvent,
  SessionCostReport,
  SessionInfo,
  BudgetStatus,
  MemoryStore,
  CreateMemoryStoreRequest,
  MemoryDocument,
  WriteMemoryRequest,
  UpdateMemoryRequest,
  MemoryVersion,
  ManagedVault,
  CreateVaultRequest,
  VaultCredentialSummary,
  AddVaultCredentialRequest,
  SessionThread,
  SendSessionEventsRequest,
  PaginatedResponse,
} from "../types";
import { type Logger, noopLogger } from "../logger";
import { createRequestId } from "../utils";
import { getCurrentSlug } from "../platform/workspace-storage";

export interface ApiClientOptions {
  logger?: Logger;
  onUnauthorized?: () => void;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export class ApiError extends Error {
  readonly status: number;
  readonly statusText: string;

  constructor(message: string, status: number, statusText: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.statusText = statusText;
  }
}

export class ApiClient {
  private baseUrl: string;
  private token: string | null = null;
  private logger: Logger;
  private options: ApiClientOptions;

  constructor(baseUrl: string, options?: ApiClientOptions) {
    this.baseUrl = baseUrl;
    this.options = options ?? {};
    this.logger = options?.logger ?? noopLogger;
  }

  getBaseUrl(): string {
    return this.baseUrl;
  }

  setToken(token: string | null) {
    this.token = token;
  }

  private readCsrfToken(): string | null {
    if (typeof document === "undefined") return null;
    const match = document.cookie
      .split("; ")
      .find((c) => c.startsWith("aurion_csrf="));
    return match ? match.split("=")[1] ?? null : null;
  }

  private authHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    const slug = getCurrentSlug();
    if (slug) headers["X-Workspace-Slug"] = slug;
    const csrf = this.readCsrfToken();
    if (csrf) headers["X-CSRF-Token"] = csrf;
    return headers;
  }

  private handleUnauthorized() {
    this.token = null;
    // Workspace id is owned by the URL-driven workspace-storage singleton
    // (set by [workspaceSlug]/layout.tsx). On 401, the auth flow navigates
    // to /login which leaves the workspace route, and the next workspace
    // entry will overwrite the id. No clear needed here.
    this.options.onUnauthorized?.();
  }

  private async parseErrorMessage(res: Response, fallback: string): Promise<string> {
    try {
      const data = await res.json() as { error?: string };
      if (typeof data.error === "string" && data.error) return data.error;
    } catch {
      // Ignore non-JSON error bodies.
    }
    return fallback;
  }

  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const rid = createRequestId();
    const start = Date.now();
    const method = init?.method ?? "GET";

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Request-ID": rid,
      ...this.authHeaders(),
      ...((init?.headers as Record<string, string>) ?? {}),
    };

    this.logger.info(`→ ${method} ${path}`, { rid });

    const res = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers,
      credentials: "include",
    });

    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized();
      const message = await this.parseErrorMessage(res, `API error: ${res.status} ${res.statusText}`);
      const logLevel = res.status === 404 ? "warn" : "error";
      this.logger[logLevel](`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new ApiError(message, res.status, res.statusText);
    }

    this.logger.info(`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms` });

    // Handle 204 No Content
    if (res.status === 204) {
      return undefined as T;
    }

    return res.json() as Promise<T>;
  }

  // Auth
  async sendCode(email: string): Promise<void> {
    await this.fetch("/auth/send-code", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  }

  async verifyCode(email: string, code: string): Promise<LoginResponse> {
    return this.fetch("/auth/verify-code", {
      method: "POST",
      body: JSON.stringify({ email, code }),
    });
  }

  async googleLogin(code: string, redirectUri: string): Promise<LoginResponse> {
    return this.fetch("/auth/google", {
      method: "POST",
      body: JSON.stringify({ code, redirect_uri: redirectUri }),
    });
  }

  async logout(): Promise<void> {
    await this.fetch("/auth/logout", { method: "POST" });
  }

  async issueCliToken(): Promise<{ token: string }> {
    return this.fetch("/api/cli-token", { method: "POST" });
  }

  async getMe(): Promise<User> {
    return this.fetch("/api/me");
  }

  async updateMe(data: UpdateMeRequest): Promise<User> {
    return this.fetch("/api/me", {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  // Issues
  async listIssues(params?: ListIssuesParams): Promise<ListIssuesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", String(params.limit));
    if (params?.offset) search.set("offset", String(params.offset));
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.status) search.set("status", params.status);
    if (params?.priority) search.set("priority", params.priority);
    if (params?.assignee_id) search.set("assignee_id", params.assignee_id);
    if (params?.assignee_ids?.length) search.set("assignee_ids", params.assignee_ids.join(","));
    if (params?.creator_id) search.set("creator_id", params.creator_id);
    if (params?.open_only) search.set("open_only", "true");
    return this.fetch(`/api/issues?${search}`);
  }

  async searchIssues(params: { q: string; limit?: number; offset?: number; include_closed?: boolean; signal?: AbortSignal }): Promise<SearchIssuesResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    if (params.offset !== undefined) search.set("offset", String(params.offset));
    if (params.include_closed) search.set("include_closed", "true");
    return this.fetch(`/api/issues/search?${search}`, params.signal ? { signal: params.signal } : undefined);
  }

  async searchProjects(params: { q: string; limit?: number; offset?: number; include_closed?: boolean; signal?: AbortSignal }): Promise<SearchProjectsResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    if (params.offset !== undefined) search.set("offset", String(params.offset));
    if (params.include_closed) search.set("include_closed", "true");
    return this.fetch(`/api/projects/search?${search}`, params.signal ? { signal: params.signal } : undefined);
  }

  async getIssue(id: string): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`);
  }

  async createIssue(data: CreateIssueRequest): Promise<Issue> {
    return this.fetch("/api/issues", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateIssue(id: string, data: UpdateIssueRequest): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async listChildIssues(id: string): Promise<{ issues: Issue[] }> {
    return this.fetch(`/api/issues/${id}/children`);
  }

  async getChildIssueProgress(): Promise<{ progress: { parent_issue_id: string; total: number; done: number }[] }> {
    return this.fetch("/api/issues/child-progress");
  }

  async deleteIssue(id: string): Promise<void> {
    await this.fetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  async batchUpdateIssues(issueIds: string[], updates: UpdateIssueRequest): Promise<{ updated: number }> {
    return this.fetch("/api/issues/batch-update", {
      method: "POST",
      body: JSON.stringify({ issue_ids: issueIds, updates }),
    });
  }

  async batchDeleteIssues(issueIds: string[]): Promise<{ deleted: number }> {
    return this.fetch("/api/issues/batch-delete", {
      method: "POST",
      body: JSON.stringify({ issue_ids: issueIds }),
    });
  }

  // Comments
  async listComments(issueId: string): Promise<Comment[]> {
    return this.fetch(`/api/issues/${issueId}/comments`);
  }

  async createComment(issueId: string, content: string, type?: string, parentId?: string, attachmentIds?: string[]): Promise<Comment> {
    return this.fetch(`/api/issues/${issueId}/comments`, {
      method: "POST",
      body: JSON.stringify({
        content,
        type: type ?? "comment",
        ...(parentId ? { parent_id: parentId } : {}),
        ...(attachmentIds?.length ? { attachment_ids: attachmentIds } : {}),
      }),
    });
  }

  async listTimeline(issueId: string): Promise<TimelineEntry[]> {
    return this.fetch(`/api/issues/${issueId}/timeline`);
  }

  async getAssigneeFrequency(): Promise<AssigneeFrequencyEntry[]> {
    return this.fetch("/api/assignee-frequency");
  }

  async updateComment(commentId: string, content: string): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}`, {
      method: "PUT",
      body: JSON.stringify({ content }),
    });
  }

  async deleteComment(commentId: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}`, { method: "DELETE" });
  }

  async addReaction(commentId: string, emoji: string): Promise<Reaction> {
    return this.fetch(`/api/comments/${commentId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeReaction(commentId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  async addIssueReaction(issueId: string, emoji: string): Promise<IssueReaction> {
    return this.fetch(`/api/issues/${issueId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeIssueReaction(issueId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/issues/${issueId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  // Subscribers
  async listIssueSubscribers(issueId: string): Promise<IssueSubscriber[]> {
    return this.fetch(`/api/issues/${issueId}/subscribers`);
  }

  async subscribeToIssue(issueId: string, userId?: string, userType?: string): Promise<void> {
    const body: Record<string, string> = {};
    if (userId) body.user_id = userId;
    if (userType) body.user_type = userType;
    await this.fetch(`/api/issues/${issueId}/subscribe`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async unsubscribeFromIssue(issueId: string, userId?: string, userType?: string): Promise<void> {
    const body: Record<string, string> = {};
    if (userId) body.user_id = userId;
    if (userType) body.user_type = userType;
    await this.fetch(`/api/issues/${issueId}/unsubscribe`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  // Agents
  async listAgents(params?: { workspace_id?: string; include_archived?: boolean }): Promise<Agent[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.include_archived) search.set("include_archived", "true");
    return this.fetch(`/api/agents?${search}`);
  }

  async getAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`);
  }

  async createAgent(data: CreateAgentRequest): Promise<Agent> {
    return this.fetch("/api/agents", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async archiveAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/archive`, { method: "POST" });
  }

  async restoreAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/restore`, { method: "POST" });
  }

  async listRuntimes(params?: { workspace_id?: string; owner?: "me" }): Promise<AgentRuntime[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
    if (params?.owner) search.set("owner", params.owner);
    return this.fetch(`/api/runtimes?${search}`);
  }

  async deleteRuntime(runtimeId: string): Promise<void> {
    await this.fetch(`/api/runtimes/${runtimeId}`, { method: "DELETE" });
  }

  async getRuntimeUsage(runtimeId: string, params?: { days?: number }): Promise<RuntimeUsage[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set("days", String(params.days));
    return this.fetch(`/api/runtimes/${runtimeId}/usage?${search}`);
  }

  async getRuntimeTaskActivity(runtimeId: string): Promise<RuntimeHourlyActivity[]> {
    return this.fetch(`/api/runtimes/${runtimeId}/activity`);
  }

  async pingRuntime(runtimeId: string): Promise<RuntimePing> {
    return this.fetch(`/api/runtimes/${runtimeId}/ping`, { method: "POST" });
  }

  async getPingResult(runtimeId: string, pingId: string): Promise<RuntimePing> {
    return this.fetch(`/api/runtimes/${runtimeId}/ping/${pingId}`);
  }

  async initiateUpdate(
    runtimeId: string,
    targetVersion: string,
  ): Promise<RuntimeUpdate> {
    return this.fetch(`/api/runtimes/${runtimeId}/update`, {
      method: "POST",
      body: JSON.stringify({ target_version: targetVersion }),
    });
  }

  async getUpdateResult(
    runtimeId: string,
    updateId: string,
  ): Promise<RuntimeUpdate> {
    return this.fetch(`/api/runtimes/${runtimeId}/update/${updateId}`);
  }

  async listAgentTasks(agentId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/agents/${agentId}/tasks`);
  }

  async getActiveTasksForIssue(issueId: string): Promise<{ tasks: AgentTask[] }> {
    return this.fetch(`/api/issues/${issueId}/active-task`);
  }

  async listTaskMessages(taskId: string): Promise<TaskMessagePayload[]> {
    return this.fetch(`/api/tasks/${taskId}/messages`);
  }

  async listTasksByIssue(issueId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/issues/${issueId}/task-runs`);
  }

  async getIssueUsage(issueId: string): Promise<IssueUsageSummary> {
    return this.fetch(`/api/issues/${issueId}/usage`);
  }

  async cancelTask(issueId: string, taskId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/tasks/${taskId}/cancel`, {
      method: "POST",
    });
  }

  // Inbox
  async listInbox(): Promise<InboxItem[]> {
    return this.fetch("/api/inbox");
  }

  async markInboxRead(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/read`, { method: "POST" });
  }

  async archiveInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/archive`, { method: "POST" });
  }

  async getUnreadInboxCount(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/unread-count");
  }

  async markAllInboxRead(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/mark-all-read", { method: "POST" });
  }

  async archiveAllInbox(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/archive-all", { method: "POST" });
  }

  async archiveAllReadInbox(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/archive-all-read", { method: "POST" });
  }

  async archiveCompletedInbox(): Promise<{ count: number }> {
    return this.fetch("/api/inbox/archive-completed", { method: "POST" });
  }

  // App Config
  async getConfig(): Promise<{ cdn_domain: string }> {
    return this.fetch("/api/config");
  }

  // Workspaces
  async listWorkspaces(): Promise<Workspace[]> {
    return this.fetch("/api/workspaces");
  }

  async getWorkspace(id: string): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`);
  }

  async createWorkspace(data: { name: string; slug: string; description?: string; context?: string }): Promise<Workspace> {
    return this.fetch("/api/workspaces", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateWorkspace(id: string, data: { name?: string; description?: string; context?: string; settings?: Record<string, unknown>; repos?: WorkspaceRepo[] }): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  // Members
  async listMembers(workspaceId: string): Promise<MemberWithUser[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/members`);
  }

  async createMember(workspaceId: string, data: CreateMemberRequest): Promise<Invitation> {
    return this.fetch(`/api/workspaces/${workspaceId}/members`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateMember(workspaceId: string, memberId: string, data: UpdateMemberRequest): Promise<MemberWithUser> {
    return this.fetch(`/api/workspaces/${workspaceId}/members/${memberId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteMember(workspaceId: string, memberId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/members/${memberId}`, {
      method: "DELETE",
    });
  }

  async leaveWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/leave`, {
      method: "POST",
    });
  }

  // Invitations
  async listWorkspaceInvitations(workspaceId: string): Promise<Invitation[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/invitations`);
  }

  async revokeInvitation(workspaceId: string, invitationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/invitations/${invitationId}`, {
      method: "DELETE",
    });
  }

  async listMyInvitations(): Promise<Invitation[]> {
    return this.fetch("/api/invitations");
  }

  async getInvitation(invitationId: string): Promise<Invitation> {
    return this.fetch(`/api/invitations/${invitationId}`);
  }

  async acceptInvitation(invitationId: string): Promise<MemberWithUser> {
    return this.fetch(`/api/invitations/${invitationId}/accept`, {
      method: "POST",
    });
  }

  async declineInvitation(invitationId: string): Promise<void> {
    await this.fetch(`/api/invitations/${invitationId}/decline`, {
      method: "POST",
    });
  }

  async deleteWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}`, {
      method: "DELETE",
    });
  }

  // Skills
  async listSkills(): Promise<Skill[]> {
    return this.fetch("/api/skills");
  }

  async getSkill(id: string): Promise<Skill> {
    return this.fetch(`/api/skills/${id}`);
  }

  async createSkill(data: CreateSkillRequest): Promise<Skill> {
    return this.fetch("/api/skills", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateSkill(id: string, data: UpdateSkillRequest): Promise<Skill> {
    return this.fetch(`/api/skills/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteSkill(id: string): Promise<void> {
    await this.fetch(`/api/skills/${id}`, { method: "DELETE" });
  }

  async importSkill(data: { url: string }): Promise<Skill> {
    return this.fetch("/api/skills/import", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async listAgentSkills(agentId: string): Promise<Skill[]> {
    return this.fetch(`/api/agents/${agentId}/skills`);
  }

  async setAgentSkills(agentId: string, data: SetAgentSkillsRequest): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/skills`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Personal Access Tokens
  async listPersonalAccessTokens(): Promise<PersonalAccessToken[]> {
    return this.fetch("/api/tokens");
  }

  async createPersonalAccessToken(data: CreatePersonalAccessTokenRequest): Promise<CreatePersonalAccessTokenResponse> {
    return this.fetch("/api/tokens", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async revokePersonalAccessToken(id: string): Promise<void> {
    await this.fetch(`/api/tokens/${id}`, { method: "DELETE" });
  }

  // File Upload & Attachments
  async uploadFile(file: File, opts?: { issueId?: string; commentId?: string }): Promise<Attachment> {
    const formData = new FormData();
    formData.append("file", file);
    if (opts?.issueId) formData.append("issue_id", opts.issueId);
    if (opts?.commentId) formData.append("comment_id", opts.commentId);

    const rid = createRequestId();
    const start = Date.now();
    this.logger.info("→ POST /api/upload-file", { rid });

    const res = await fetch(`${this.baseUrl}/api/upload-file`, {
      method: "POST",
      headers: this.authHeaders(),
      body: formData,
      credentials: "include",
    });

    if (!res.ok) {
      if (res.status === 401) this.handleUnauthorized();
      const message = await this.parseErrorMessage(res, `Upload failed: ${res.status}`);
      this.logger.error(`← ${res.status} /api/upload-file`, { rid, duration: `${Date.now() - start}ms`, error: message });
      throw new Error(message);
    }

    this.logger.info(`← ${res.status} /api/upload-file`, { rid, duration: `${Date.now() - start}ms` });
    return res.json() as Promise<Attachment>;
  }

  // Chat Sessions
  async listChatSessions(params?: { status?: string }): Promise<ChatSession[]> {
    const query = params?.status ? `?status=${params.status}` : "";
    return this.fetch(`/api/chat/sessions${query}`);
  }

  async getChatSession(id: string): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}`);
  }

  async createChatSession(data: { agent_id: string; title?: string }): Promise<ChatSession> {
    return this.fetch("/api/chat/sessions", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async archiveChatSession(id: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${id}`, { method: "DELETE" });
  }

  async listChatMessages(sessionId: string): Promise<ChatMessage[]> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`);
  }

  async sendChatMessage(sessionId: string, content: string): Promise<SendChatMessageResponse> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify({ content }),
    });
  }

  async getPendingChatTask(sessionId: string): Promise<ChatPendingTask> {
    return this.fetch(`/api/chat/sessions/${sessionId}/pending-task`);
  }

  async listPendingChatTasks(): Promise<PendingChatTasksResponse> {
    return this.fetch(`/api/chat/pending-tasks`);
  }

  async markChatSessionRead(sessionId: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${sessionId}/read`, { method: "POST" });
  }

  async cancelTaskById(taskId: string): Promise<void> {
    await this.fetch(`/api/tasks/${taskId}/cancel`, { method: "POST" });
  }

  async listAttachments(issueId: string): Promise<Attachment[]> {
    return this.fetch(`/api/issues/${issueId}/attachments`);
  }

  async deleteAttachment(id: string): Promise<void> {
    await this.fetch(`/api/attachments/${id}`, { method: "DELETE" });
  }

  // Projects
  async listProjects(params?: { status?: string }): Promise<ListProjectsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    return this.fetch(`/api/projects?${search}`);
  }

  async getProject(id: string): Promise<Project> {
    return this.fetch(`/api/projects/${id}`);
  }

  async createProject(data: CreateProjectRequest): Promise<Project> {
    return this.fetch("/api/projects", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateProject(id: string, data: UpdateProjectRequest): Promise<Project> {
    return this.fetch(`/api/projects/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteProject(id: string): Promise<void> {
    await this.fetch(`/api/projects/${id}`, { method: "DELETE" });
  }

  // Pins
  async listPins(): Promise<PinnedItem[]> {
    return this.fetch("/api/pins");
  }

  async createPin(data: CreatePinRequest): Promise<PinnedItem> {
    return this.fetch("/api/pins", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async deletePin(itemType: PinnedItemType, itemId: string): Promise<void> {
    await this.fetch(`/api/pins/${itemType}/${itemId}`, { method: "DELETE" });
  }

  async reorderPins(data: ReorderPinsRequest): Promise<void> {
    await this.fetch("/api/pins/reorder", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Autopilots
  async listAutopilots(params?: { status?: string }): Promise<ListAutopilotsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set("status", params.status);
    return this.fetch(`/api/autopilots?${search}`);
  }

  async getAutopilot(id: string): Promise<GetAutopilotResponse> {
    return this.fetch(`/api/autopilots/${id}`);
  }

  async createAutopilot(data: CreateAutopilotRequest): Promise<Autopilot> {
    return this.fetch("/api/autopilots", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateAutopilot(id: string, data: UpdateAutopilotRequest): Promise<Autopilot> {
    return this.fetch(`/api/autopilots/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteAutopilot(id: string): Promise<void> {
    await this.fetch(`/api/autopilots/${id}`, { method: "DELETE" });
  }

  async triggerAutopilot(id: string): Promise<AutopilotRun> {
    return this.fetch(`/api/autopilots/${id}/trigger`, { method: "POST" });
  }

  async listAutopilotRuns(id: string, params?: { limit?: number; offset?: number }): Promise<ListAutopilotRunsResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set("limit", params.limit.toString());
    if (params?.offset) search.set("offset", params.offset.toString());
    return this.fetch(`/api/autopilots/${id}/runs?${search}`);
  }

  async createAutopilotTrigger(autopilotId: string, data: CreateAutopilotTriggerRequest): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateAutopilotTrigger(autopilotId: string, triggerId: string, data: UpdateAutopilotTriggerRequest): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteAutopilotTrigger(autopilotId: string, triggerId: string): Promise<void> {
    await this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}`, { method: "DELETE" });
  }

  // -------------------------------------------------------------------------
  // v1 Managed Agents API
  // -------------------------------------------------------------------------

  // Agents
  async listManagedAgents(): Promise<PaginatedResponse<ManagedAgent>> {
    return this.fetch("/api/v1/agents");
  }

  async createManagedAgent(data: CreateManagedAgentRequest): Promise<ManagedAgent> {
    return this.fetch("/api/v1/agents", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getManagedAgent(agentId: string): Promise<ManagedAgent> {
    return this.fetch(`/api/v1/agents/${agentId}`);
  }

  async updateManagedAgent(agentId: string, data: UpdateManagedAgentRequest): Promise<ManagedAgent> {
    return this.fetch(`/api/v1/agents/${agentId}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async archiveManagedAgent(agentId: string): Promise<void> {
    await this.fetch(`/api/v1/agents/${agentId}/archive`, { method: "POST" });
  }

  async triggerAgent(agentId: string, data: {
    prompt: string;
    title?: string;
    environment_id?: string;
    vault_ids?: string[];
    source?: string;
  }): Promise<{ session: ManagedSession; source: string }> {
    return this.fetch(`/api/v1/agents/${agentId}/trigger`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async listManagedAgentVersions(agentId: string): Promise<PaginatedResponse<ManagedAgentVersion>> {
    return this.fetch(`/api/v1/agents/${agentId}/versions`);
  }

  // Environments
  async listEnvironments(): Promise<PaginatedResponse<ManagedEnvironment>> {
    return this.fetch("/api/v1/environments");
  }

  async createEnvironment(data: CreateEnvironmentRequest): Promise<ManagedEnvironment> {
    return this.fetch("/api/v1/environments", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getEnvironment(envId: string): Promise<ManagedEnvironment> {
    return this.fetch(`/api/v1/environments/${envId}`);
  }

  async archiveEnvironment(envId: string): Promise<void> {
    await this.fetch(`/api/v1/environments/${envId}/archive`, { method: "POST" });
  }

  async deleteEnvironment(envId: string): Promise<void> {
    await this.fetch(`/api/v1/environments/${envId}`, { method: "DELETE" });
  }

  // Sessions
  async listManagedSessions(opts?: { agentId?: string }): Promise<PaginatedResponse<ManagedSession>> {
    const params = new URLSearchParams();
    if (opts?.agentId) params.set("agent_id", opts.agentId);
    const qs = params.toString();
    return this.fetch(`/api/v1/sessions${qs ? `?${qs}` : ""}`);
  }

  async createManagedSession(data: CreateManagedSessionRequest): Promise<ManagedSession> {
    return this.fetch("/api/v1/sessions", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getManagedSession(sessionId: string): Promise<ManagedSession> {
    return this.fetch(`/api/v1/sessions/${sessionId}`);
  }

  async archiveManagedSession(sessionId: string): Promise<void> {
    await this.fetch(`/api/v1/sessions/${sessionId}/archive`, { method: "POST" });
  }

  async deleteManagedSession(sessionId: string): Promise<void> {
    await this.fetch(`/api/v1/sessions/${sessionId}`, { method: "DELETE" });
  }

  async listSessionEvents(sessionId: string, opts?: { limit?: number; offset?: number }): Promise<PaginatedResponse<SessionEvent>> {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return this.fetch(`/api/v1/sessions/${sessionId}/events${qs ? `?${qs}` : ""}`);
  }

  async sendSessionEvents(sessionId: string, data: SendSessionEventsRequest): Promise<{ status: string; events: Array<{ id: string; type: string; created_at: string }> }> {
    return this.fetch(`/api/v1/sessions/${sessionId}/events`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  streamSessionEvents(sessionId: string): EventSource {
    return new EventSource(`${this.baseUrl}/api/v1/sessions/${sessionId}/stream`);
  }

  // Session Store API (Managed Agents architecture)
  async getSessionStoreEvents(
    sessionId: string,
    opts?: { from?: number; to?: number; types?: string[] },
  ): Promise<{ events: StoreEvent[]; count: number }> {
    const params = new URLSearchParams();
    if (opts?.from !== undefined) params.set("from", String(opts.from));
    if (opts?.to !== undefined) params.set("to", String(opts.to));
    if (opts?.types?.length) params.set("types", opts.types.join(","));
    const qs = params.toString();
    return this.fetch(`/api/v1/sessions/${sessionId}/store/events${qs ? `?${qs}` : ""}`);
  }

  async getSessionCost(sessionId: string): Promise<SessionCostReport> {
    return this.fetch(`/api/v1/sessions/${sessionId}/store/cost`);
  }

  async wakeSession(sessionId: string): Promise<SessionInfo> {
    return this.fetch(`/api/v1/sessions/${sessionId}/store/wake`, { method: "POST" });
  }

  async getWorkspaceBudget(): Promise<BudgetStatus> {
    return this.fetch("/api/v1/sessions/budget");
  }

  async updateWorkspaceBudget(data: {
    daily_budget_usd: number;
    monthly_budget_usd: number;
  }): Promise<void> {
    await this.fetch("/api/v1/sessions/budget", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // Session Threads
  async listSessionThreads(sessionId: string): Promise<PaginatedResponse<SessionThread>> {
    return this.fetch(`/api/v1/sessions/${sessionId}/threads`);
  }

  async listThreadEvents(sessionId: string, threadId: string): Promise<PaginatedResponse<SessionEvent>> {
    return this.fetch(`/api/v1/sessions/${sessionId}/threads/${threadId}/events`);
  }

  streamSessionThread(sessionId: string, threadId: string): EventSource {
    return new EventSource(`${this.baseUrl}/api/v1/sessions/${sessionId}/threads/${threadId}/stream`);
  }

  // Memory Stores
  async listMemoryStores(): Promise<PaginatedResponse<MemoryStore>> {
    return this.fetch("/api/v1/memory-stores");
  }

  async createMemoryStore(data: CreateMemoryStoreRequest): Promise<MemoryStore> {
    return this.fetch("/api/v1/memory-stores", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getMemoryStore(storeId: string): Promise<MemoryStore> {
    return this.fetch(`/api/v1/memory-stores/${storeId}`);
  }

  async archiveMemoryStore(storeId: string): Promise<void> {
    await this.fetch(`/api/v1/memory-stores/${storeId}/archive`, { method: "POST" });
  }

  // Memories
  async listMemories(storeId: string, opts?: { limit?: number; offset?: number }): Promise<PaginatedResponse<MemoryDocument>> {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return this.fetch(`/api/v1/memory-stores/${storeId}/memories${qs ? `?${qs}` : ""}`);
  }

  async writeMemory(storeId: string, data: WriteMemoryRequest): Promise<MemoryDocument> {
    return this.fetch(`/api/v1/memory-stores/${storeId}/memories`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async readMemory(storeId: string, memId: string): Promise<MemoryDocument> {
    return this.fetch(`/api/v1/memory-stores/${storeId}/memories/${memId}`);
  }

  async updateMemory(storeId: string, memId: string, data: UpdateMemoryRequest): Promise<MemoryDocument> {
    return this.fetch(`/api/v1/memory-stores/${storeId}/memories/${memId}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteMemory(storeId: string, memId: string): Promise<void> {
    await this.fetch(`/api/v1/memory-stores/${storeId}/memories/${memId}`, { method: "DELETE" });
  }

  // Memory Versions
  async listMemoryVersions(storeId: string, opts?: { limit?: number; offset?: number }): Promise<PaginatedResponse<MemoryVersion>> {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return this.fetch(`/api/v1/memory-stores/${storeId}/versions${qs ? `?${qs}` : ""}`);
  }

  async getMemoryVersion(storeId: string, versionId: string): Promise<MemoryVersion> {
    return this.fetch(`/api/v1/memory-stores/${storeId}/versions/${versionId}`);
  }

  async redactMemoryVersion(storeId: string, versionId: string): Promise<void> {
    await this.fetch(`/api/v1/memory-stores/${storeId}/versions/${versionId}/redact`, { method: "POST" });
  }

  // Vaults
  async listVaults(): Promise<PaginatedResponse<ManagedVault>> {
    return this.fetch("/api/v1/vaults");
  }

  async createVault(data: CreateVaultRequest): Promise<ManagedVault> {
    return this.fetch("/api/v1/vaults", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async getVault(vaultId: string): Promise<ManagedVault> {
    return this.fetch(`/api/v1/vaults/${vaultId}`);
  }

  async archiveVault(vaultId: string): Promise<void> {
    await this.fetch(`/api/v1/vaults/${vaultId}/archive`, { method: "POST" });
  }

  async deleteVault(vaultId: string): Promise<void> {
    await this.fetch(`/api/v1/vaults/${vaultId}`, { method: "DELETE" });
  }

  // Vault Credentials
  async listVaultCredentials(vaultId: string): Promise<PaginatedResponse<VaultCredentialSummary>> {
    return this.fetch(`/api/v1/vaults/${vaultId}/credentials`);
  }

  async addVaultCredential(vaultId: string, data: AddVaultCredentialRequest): Promise<VaultCredentialSummary> {
    return this.fetch(`/api/v1/vaults/${vaultId}/credentials`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async archiveVaultCredential(vaultId: string, credId: string): Promise<void> {
    await this.fetch(`/api/v1/vaults/${vaultId}/credentials/${credId}/archive`, { method: "POST" });
  }

  // ===== MCP Registry & Connectors =====

  async listMcpCatalog(category?: string): Promise<{ data: import("../types/mcp").McpCatalogEntry[] }> {
    const qs = category ? `?category=${category}` : "";
    return this.fetch(`/api/v1/mcp/catalog${qs}`);
  }

  async listMcpRegistry(): Promise<{ data: import("../types/mcp").McpRegistryEntry[] }> {
    return this.fetch("/api/v1/mcp/registry");
  }

  async seedMcpRegistry(slug: string): Promise<import("../types/mcp").McpRegistryEntry> {
    return this.fetch("/api/v1/mcp/registry/seed", {
      method: "POST",
      body: JSON.stringify({ slug }),
    });
  }

  async seedAllMcpRegistry(): Promise<{ seeded: number; total_catalog: number }> {
    return this.fetch("/api/v1/mcp/registry/seed-all", { method: "POST" });
  }

  async listAgentMcpConnectors(agentId: string): Promise<{ data: import("../types/mcp").McpConnector[] }> {
    return this.fetch(`/api/v1/agents/${agentId}/mcp`);
  }

  async createAgentMcpConnector(agentId: string, data: import("../types/mcp").CreateMcpConnectorRequest): Promise<import("../types/mcp").McpConnector> {
    return this.fetch(`/api/v1/agents/${agentId}/mcp`, {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async addMcpFromRegistry(agentId: string, registryId: string, vaultCredentialId?: string): Promise<import("../types/mcp").McpConnector> {
    return this.fetch(`/api/v1/agents/${agentId}/mcp/from-registry`, {
      method: "POST",
      body: JSON.stringify({ registry_id: registryId, vault_credential_id: vaultCredentialId }),
    });
  }

  async autoAttachBrowserMcp(agentId: string): Promise<{ attached: number; no_auth_servers: number }> {
    return this.fetch(`/api/v1/agents/${agentId}/mcp/auto-attach-browser`, { method: "POST" });
  }

  async validateMcpConnector(agentId: string, connectorId: string): Promise<import("../types/mcp").McpValidationResult> {
    return this.fetch(`/api/v1/agents/${agentId}/mcp/${connectorId}/validate`, { method: "POST" });
  }

  async discoverMcpTools(agentId: string, connectorId: string): Promise<{ tools: import("../types/mcp").McpDiscoveredTool[] }> {
    return this.fetch(`/api/v1/agents/${agentId}/mcp/${connectorId}/discover`, { method: "POST" });
  }

  async deleteMcpConnector(agentId: string, connectorId: string): Promise<void> {
    await this.fetch(`/api/v1/agents/${agentId}/mcp/${connectorId}`, { method: "DELETE" });
  }

  async initMcpOAuth(params: {
    provider: string;
    agent_id: string;
    connector_id: string;
    vault_id: string;
    redirect_url: string;
  }): Promise<{ auth_url: string; state: string }> {
    return this.fetch("/api/v1/mcp/oauth/init", {
      method: "POST",
      body: JSON.stringify(params),
    });
  }
}
