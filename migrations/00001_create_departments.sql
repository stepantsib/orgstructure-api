-- +goose Up
CREATE TABLE departments (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(200) NOT NULL,
    parent_id   BIGINT REFERENCES departments(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT departments_no_self_parent CHECK (parent_id IS NULL OR parent_id <> id),
    CONSTRAINT departments_name_not_blank CHECK (length(btrim(name)) > 0)
);

CREATE INDEX idx_departments_parent_id ON departments(parent_id);

-- Unique name within the same parent (two indexes: one for non-null parent, one for root)
CREATE UNIQUE INDEX idx_departments_name_parent
    ON departments(parent_id, name)
    WHERE parent_id IS NOT NULL;

CREATE UNIQUE INDEX idx_departments_name_root
    ON departments(name)
    WHERE parent_id IS NULL;

-- +goose Down
DROP TABLE IF EXISTS departments;
