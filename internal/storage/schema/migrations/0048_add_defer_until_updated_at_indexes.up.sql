-- 0048_add_defer_until_updated_at_indexes.up.sql
--
-- Dolt counterpart of mysql migration 0049: index the columns bd's hot
-- polling queries filter on (issues.defer_until, issues.updated_at,
-- wisps.defer_until). See that file's header for the performance_schema
-- evidence. Dolt supports CREATE INDEX IF NOT EXISTS, so the issues indexes
-- are idempotent on their own.
--
-- wisps is a dolt-ignored table (0019 registers `wisps` / `wisp_%` in
-- dolt_ignore), so its index lives in the local working set only and is
-- created behind a table-existence guard — the same shape as 0031's
-- wisp_events index. Indexing a dolt-ignored wisp table from the main set is
-- the established pattern (0031, 0022).

CREATE INDEX IF NOT EXISTS idx_issues_defer_until ON issues (defer_until);
CREATE INDEX IF NOT EXISTS idx_issues_updated_at ON issues (updated_at);

SET @sql = IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
        WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps') > 0,
    'CREATE INDEX IF NOT EXISTS idx_wisps_defer_until ON wisps (defer_until)',
    'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
