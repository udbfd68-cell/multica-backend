-- name: GetVault :one
SELECT * FROM vault WHERE id = $1;

-- name: GetVaultInWorkspace :one
SELECT * FROM vault WHERE id = $1 AND workspace_id = $2;

-- name: ListVaults :many
SELECT * FROM vault
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: CreateVault :one
INSERT INTO vault (workspace_id, display_name, metadata)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateVault :one
UPDATE vault SET
    display_name = COALESCE(NULLIF($2, ''), display_name),
    metadata = COALESCE($3, metadata),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveVault :exec
UPDATE vault SET archived_at = now(), updated_at = now() WHERE id = $1;

-- name: CreateVaultCredential :one
INSERT INTO vault_credential (vault_id, mcp_server_url, auth_type, encrypted_payload, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListVaultCredentials :many
SELECT id, vault_id, mcp_server_url, auth_type, expires_at, created_at, archived_at
FROM vault_credential
WHERE vault_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: GetVaultCredential :one
SELECT * FROM vault_credential WHERE id = $1 AND archived_at IS NULL;

-- name: ArchiveVaultCredential :exec
UPDATE vault_credential SET archived_at = now() WHERE id = $1;

-- name: CountActiveVaultCredentials :one
SELECT COUNT(*) FROM vault_credential
WHERE vault_id = $1 AND archived_at IS NULL;
