-- Initial items schema. The starter ships one resource end-to-end so
-- the wiring of every SDK module is visible. Replace `items` with
-- your module's primary resource.

CREATE TABLE items (
    id          text        PRIMARY KEY,
    name        text        NOT NULL,
    status      text        NOT NULL CHECK (status IN ('active', 'archived')),
    owner_id    text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX items_owner_idx   ON items (owner_id);
CREATE INDEX items_status_idx  ON items (status);
CREATE INDEX items_created_idx ON items (created_at DESC);
