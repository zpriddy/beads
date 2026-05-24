-- 0048_add_is_blocked_to_wisps.up.sql (mysql-only fork patch)
--
-- The shared 0046_add_is_blocked migration only adds is_blocked to the
-- issues table. The blocked_state recompute query at
-- internal/storage/issueops/blocked_state.go:153 also joins
-- `wisps p ... WHERE p.is_blocked = 1`, so wisps needs the column too.
-- On dolt, wisps may inherit from issues via a view or share the column;
-- on mysql wisps is a separate table that needs its own ADD COLUMN.

SET @needs_add = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisps'
      AND COLUMN_NAME = 'is_blocked'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE wisps ADD COLUMN is_blocked TINYINT(1) NOT NULL DEFAULT 0',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_index = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisps'
      AND INDEX_NAME = 'idx_wisps_is_blocked'
);
SET @sql = IF(@needs_index = 1,
    'CREATE INDEX idx_wisps_is_blocked ON wisps(is_blocked, status)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
