-- +goose Up
CREATE TABLE tenants (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text NOT NULL,
  status      text NOT NULL DEFAULT 'active',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);

CREATE TABLE api_keys (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  key_hash    text NOT NULL UNIQUE,           -- sha256 hex of the raw key
  name        text NOT NULL DEFAULT '',
  status      text NOT NULL DEFAULT 'active',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);

CREATE TABLE payment_providers (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  provider      text NOT NULL DEFAULT 'efi',
  account_label text NOT NULL DEFAULT '',
  status        text NOT NULL DEFAULT 'active',
  is_default    boolean NOT NULL DEFAULT false,
  webhook_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_provider_default ON payment_providers(tenant_id) WHERE is_default;
CREATE INDEX idx_providers_tenant ON payment_providers(tenant_id);

CREATE TABLE pix_keys (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  payment_provider_id uuid NOT NULL REFERENCES payment_providers(id) ON DELETE RESTRICT,
  key                 text NOT NULL,
  key_type            text NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_pix_keys_provider ON pix_keys(payment_provider_id);

-- RLS
ALTER TABLE tenants            ENABLE ROW LEVEL SECURITY; ALTER TABLE tenants            FORCE ROW LEVEL SECURITY;
ALTER TABLE api_keys           ENABLE ROW LEVEL SECURITY; ALTER TABLE api_keys           FORCE ROW LEVEL SECURITY;
ALTER TABLE payment_providers  ENABLE ROW LEVEL SECURITY; ALTER TABLE payment_providers  FORCE ROW LEVEL SECURITY;
ALTER TABLE pix_keys           ENABLE ROW LEVEL SECURITY; ALTER TABLE pix_keys           FORCE ROW LEVEL SECURITY;

CREATE POLICY p_tenants  ON tenants           USING (id = current_tenant_id());
CREATE POLICY p_apikeys  ON api_keys          USING (tenant_id = current_tenant_id());
CREATE POLICY p_providers ON payment_providers USING (tenant_id = current_tenant_id());
CREATE POLICY p_pixkeys  ON pix_keys          USING (tenant_id = current_tenant_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, api_keys, payment_providers, pix_keys TO pix_app;

-- +goose Down
DROP TABLE pix_keys; DROP TABLE payment_providers; DROP TABLE api_keys; DROP TABLE tenants;
