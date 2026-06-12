-- +goose Up
CREATE TABLE charges (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  payment_provider_id uuid NOT NULL REFERENCES payment_providers(id) ON DELETE RESTRICT,
  txid                text NOT NULL,
  kind                text NOT NULL CHECK (kind IN ('cob','cobv')),
  status              text NOT NULL,
  amount              bigint NOT NULL CHECK (amount > 0),
  pix_key             text NOT NULL,
  description         text NOT NULL DEFAULT '',
  expiration_seconds  int,
  due_date            date,
  validity_after_days int,
  fine_percent        numeric,
  interest_mode       text,
  interest_percent    numeric,
  discount_mode       text,
  discount_value      numeric,
  abatement_value     numeric,
  location_id         text NOT NULL DEFAULT '',
  qr_code_image       text NOT NULL DEFAULT '',
  pix_payload         text NOT NULL DEFAULT '',
  payer_doc           text NOT NULL DEFAULT '',
  payer_doc_type      text NOT NULL DEFAULT '',
  payer_name          text NOT NULL DEFAULT '',
  payer_email         text NOT NULL DEFAULT '',
  payer_phone         text NOT NULL DEFAULT '',
  external_reference  text NOT NULL DEFAULT '',
  version             int NOT NULL DEFAULT 0,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  deleted_at          timestamptz,
  CONSTRAINT uq_charges_txid UNIQUE (tenant_id, txid),
  CONSTRAINT ck_cob_fields  CHECK (kind <> 'cob'  OR (due_date IS NULL AND fine_percent IS NULL AND interest_percent IS NULL)),
  CONSTRAINT ck_cobv_fields CHECK (kind <> 'cobv' OR expiration_seconds IS NULL)
);
CREATE INDEX idx_charges_status ON charges(tenant_id, status);
CREATE INDEX idx_charges_due    ON charges(tenant_id, due_date) WHERE kind = 'cobv';

CREATE TABLE payments (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  charge_id  uuid NOT NULL REFERENCES charges(id) ON DELETE RESTRICT,
  e2e_id     text NOT NULL,
  amount     bigint NOT NULL,
  paid_at    timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT uq_payments_e2e UNIQUE (tenant_id, e2e_id)
);
CREATE INDEX idx_payments_charge ON payments(charge_id);

CREATE TABLE payment_events (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  charge_id   uuid NOT NULL REFERENCES charges(id) ON DELETE RESTRICT,
  event_type  text NOT NULL,
  payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_payment_events_charge ON payment_events(charge_id, occurred_at);

CREATE TABLE outbox (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  aggregate_id uuid NOT NULL,
  type         text NOT NULL,
  payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
  sent_at      timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_outbox_unsent ON outbox(created_at) WHERE sent_at IS NULL;

CREATE TABLE idempotency_keys (
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  key         text NOT NULL,
  fingerprint text NOT NULL,
  txid        text NOT NULL DEFAULT '',
  status      int  NOT NULL DEFAULT 0,
  response    bytea,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, key)
);

ALTER TABLE charges          ENABLE ROW LEVEL SECURITY; ALTER TABLE charges          FORCE ROW LEVEL SECURITY;
ALTER TABLE payments         ENABLE ROW LEVEL SECURITY; ALTER TABLE payments         FORCE ROW LEVEL SECURITY;
ALTER TABLE payment_events   ENABLE ROW LEVEL SECURITY; ALTER TABLE payment_events   FORCE ROW LEVEL SECURITY;
ALTER TABLE outbox           ENABLE ROW LEVEL SECURITY; ALTER TABLE outbox           FORCE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY; ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;

CREATE POLICY p_charges        ON charges          USING (tenant_id = current_tenant_id());
CREATE POLICY p_payments       ON payments         USING (tenant_id = current_tenant_id());
CREATE POLICY p_payment_events ON payment_events   USING (tenant_id = current_tenant_id());
CREATE POLICY p_outbox         ON outbox           USING (tenant_id = current_tenant_id());
CREATE POLICY p_idem           ON idempotency_keys USING (tenant_id = current_tenant_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON charges, payments, payment_events, outbox, idempotency_keys TO pix_app;

-- +goose Down
DROP TABLE idempotency_keys; DROP TABLE outbox; DROP TABLE payment_events; DROP TABLE payments; DROP TABLE charges;
