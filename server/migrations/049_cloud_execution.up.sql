-- Allow agent_task_queue.runtime_id to be NULL for cloud-executed tasks.
ALTER TABLE agent_task_queue ALTER COLUMN runtime_id DROP NOT NULL;

-- Allow agent.runtime_id to be NULL for cloud-mode agents.
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_runtime_id_fkey;
ALTER TABLE agent ALTER COLUMN runtime_id DROP NOT NULL;
ALTER TABLE agent
    ADD CONSTRAINT agent_runtime_id_fkey
    FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE SET NULL;
