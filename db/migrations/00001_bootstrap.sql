-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Helper used by every tenant-scoped policy.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS uuid AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS current_tenant_id();
DROP EXTENSION IF EXISTS pgcrypto;
