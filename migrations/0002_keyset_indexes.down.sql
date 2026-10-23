DROP INDEX IF EXISTS tasks_assignee_keyset_idx;
DROP INDEX IF EXISTS tasks_status_keyset_idx;

CREATE INDEX tasks_status_idx ON tasks (status);
CREATE INDEX tasks_assignee_idx ON tasks (assignee);
