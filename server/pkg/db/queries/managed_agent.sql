-- name: GetManagedAgent :one
SELECT * FROM managed_agent WHERE id = $1;

-- name: GetManagedAgentInWorkspace :one
SELECT * FROM managed_agent WHERE id = $1 AND workspace_id = $2;

-- name: ListManagedAgents :many
SELECT * FROM managed_agent
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: ListAllManagedAgents :many
SELECT * FROM managed_agent
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: CreateManagedAgent :one
INSERT INTO managed_agent (workspace_id, name, description, model, system_prompt, tools, mcp_servers, skills, callable_agents, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpdateManagedAgent :one
UPDATE managed_agent SET
    name = COALESCE(NULLIF($2, ''), name),
    description = COALESCE($3, description),
    model = COALESCE($4, model),
    system_prompt = COALESCE($5, system_prompt),
    tools = COALESCE($6, tools),
    mcp_servers = COALESCE($7, mcp_servers),
    skills = COALESCE($8, skills),
    callable_agents = COALESCE($9, callable_agents),
    metadata = COALESCE($10, metadata),
    version = version + 1,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveManagedAgent :exec
UPDATE managed_agent SET archived_at = now() WHERE id = $1;

-- name: RestoreManagedAgent :exec
UPDATE managed_agent SET archived_at = NULL WHERE id = $1;

-- name: DeleteManagedAgent :exec
DELETE FROM managed_agent WHERE id = $1;

-- name: CreateManagedAgentVersion :one
INSERT INTO managed_agent_version (agent_id, version, snapshot)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListManagedAgentVersions :many
SELECT * FROM managed_agent_version
WHERE agent_id = $1
ORDER BY version DESC;

-- name: GetManagedAgentVersion :one
SELECT * FROM managed_agent_version
WHERE agent_id = $1 AND version = $2;
