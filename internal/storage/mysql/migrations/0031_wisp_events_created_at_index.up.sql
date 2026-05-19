-- Add idx_wisp_events_created_at if missing.
SET @needs_idx = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisp_events'
      AND INDEX_NAME = 'idx_wisp_events_created_at'
);
SET @table_exists = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisp_events'
);
SET @sql = IF(@table_exists > 0 AND @needs_idx = 1,
    'CREATE INDEX idx_wisp_events_created_at ON wisp_events (created_at)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
