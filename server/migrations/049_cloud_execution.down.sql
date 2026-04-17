-- Restore NOT NULL constraints on runtime_id.
ALTER TABLE agent_task_queue ALTER COLUMN runtime_id SET NOT NULL;

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_runtime_id_fkey;
ALTER TABLE agent ALTER COLUMN runtime_id SET NOT NULL;
ALTER TABLE agent
    ADD CONSTRAINT agent_runtime_id_fkey
    FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE RESTRICT;
