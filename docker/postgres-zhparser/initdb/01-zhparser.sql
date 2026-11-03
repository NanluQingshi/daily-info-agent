-- Runs once, as the postgres superuser, when the data volume is first
-- initialised (docker-entrypoint-initdb.d). zhparser is not a trusted
-- extension, so CREATE EXTENSION cannot run from the application's
-- migration path as the non-superuser `dia` role — it must happen here.
-- Migration 006 still carries `CREATE EXTENSION IF NOT EXISTS zhparser`
-- for standalone superuser setups; there it is a no-op.
CREATE EXTENSION IF NOT EXISTS zhparser;
