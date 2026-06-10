-- Owner/migrator role is the compose superuser "pix" (POSTGRES_USER).
-- Create the least-privilege application login role.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'pix_app') THEN
    CREATE ROLE pix_app LOGIN PASSWORD 'pix_app_pw';
  END IF;
END$$;
GRANT USAGE ON SCHEMA public TO pix_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pix_app;
