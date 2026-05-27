-- Roll back 0048: drop the defer_until / updated_at indexes.
-- Mirrors the bare DROP INDEX convention of 0031/0046. Down migrations are
-- not embedded or auto-applied by the runtime (only *.up.sql is); this file
-- documents the manual rollback.
DROP INDEX idx_issues_defer_until ON issues;
DROP INDEX idx_issues_updated_at ON issues;
DROP INDEX idx_wisps_defer_until ON wisps;
