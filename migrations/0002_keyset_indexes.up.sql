-- Keyset pagination orders by (created_at DESC, id DESC) and filters with a
-- row comparison, so each filter needs an index whose leading columns match.
CREATE INDEX tasks_status_keyset_idx ON tasks (status, created_at DESC, id DESC);
CREATE INDEX tasks_assignee_keyset_idx ON tasks (assignee, created_at DESC, id DESC);

-- The single-column indexes are prefixes of the ones above and no longer earn
-- their write cost.
DROP INDEX IF EXISTS tasks_status_idx;
DROP INDEX IF EXISTS tasks_assignee_idx;
