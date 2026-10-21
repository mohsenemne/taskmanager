CREATE TABLE tasks (
    id          uuid PRIMARY KEY,
    title       text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '',
    status      text NOT NULL CHECK (status IN ('todo', 'in_progress', 'done')),
    assignee    text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Filtering by status and assignee, and stable pagination by creation time.
CREATE INDEX tasks_status_idx ON tasks (status);
CREATE INDEX tasks_assignee_idx ON tasks (assignee);
CREATE INDEX tasks_created_at_idx ON tasks (created_at DESC, id DESC);
