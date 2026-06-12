INSERT INTO tenants (id, name) VALUES
  ('11111111-1111-1111-1111-111111111111', 'Dev Tenant')
ON CONFLICT (id) DO NOTHING;

INSERT INTO payment_providers (id, tenant_id, provider, account_label, is_default) VALUES
  ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'efi', 'dev-efi', true)
ON CONFLICT (id) DO NOTHING;

INSERT INTO pix_keys (id, tenant_id, payment_provider_id, key, key_type) VALUES
  ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111',
   '22222222-2222-2222-2222-222222222222', 'dev-pix-key@example.com', 'email')
ON CONFLICT (id) DO NOTHING;

INSERT INTO api_keys (tenant_id, key_hash, name) VALUES
  ('11111111-1111-1111-1111-111111111111', encode(digest('pk_dev_secret', 'sha256'), 'hex'), 'dev')
ON CONFLICT (key_hash) DO NOTHING;
