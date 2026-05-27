-- 0049_add_defer_until_updated_at_indexes.up.sql
--
-- Index the columns bd's hot polling queries filter on. A performance_schema
-- sample of the live gascity_hq instance showed three full-table-scan queries
-- dominating DB time because their filter columns are unindexed:
--   SELECT 1 FROM issues WHERE defer_until ... LIMIT 1   (~39/s, ~4.1 B rows examined)
--   SELECT 1 FROM wisps  WHERE defer_until ... LIMIT 1   (~39/s, ~2.4 B rows examined)
--   SELECT MAX(updated_at) FROM issues                   (bd freshness probe)
-- issues (~8 k rows) and wisps (~6 k rows) are small but scanned in full on
-- every call. Write rate is negligible (~99 % of bd transactions are
-- read-only), so index write-amplification is effectively zero.
--
-- MySQL has no CREATE INDEX IF NOT EXISTS, so each index is made idempotent
-- with an INFORMATION_SCHEMA.STATISTICS existence guard (mirrors 0046/0048).
-- The wisps index additionally guards on table existence (mirrors 0031).

SET @needs_index = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND INDEX_NAME = 'idx_issues_defer_until'
);
SET @sql = IF(@needs_index = 1,
    'CREATE INDEX idx_issues_defer_until ON issues(defer_until)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_index = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND INDEX_NAME = 'idx_issues_updated_at'
);
SET @sql = IF(@needs_index = 1,
    'CREATE INDEX idx_issues_updated_at ON issues(updated_at)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @table_exists = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps'
);
SET @needs_index = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisps'
      AND INDEX_NAME = 'idx_wisps_defer_until'
);
SET @sql = IF(@table_exists > 0 AND @needs_index = 1,
    'CREATE INDEX idx_wisps_defer_until ON wisps(defer_until)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
