-- +goose Up
CREATE TABLE employees (
    id              BIGSERIAL PRIMARY KEY,
    department_id   BIGINT NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    full_name       VARCHAR(200) NOT NULL,
    position        VARCHAR(200) NOT NULL,
    hired_at        DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT employees_full_name_not_blank CHECK (length(btrim(full_name)) > 0),
    CONSTRAINT employees_position_not_blank  CHECK (length(btrim(position))  > 0)
);

CREATE INDEX idx_employees_department_id ON employees(department_id);
CREATE INDEX idx_employees_created_at    ON employees(created_at);
CREATE INDEX idx_employees_full_name     ON employees(full_name);

-- +goose Down
DROP TABLE IF EXISTS employees;
