-- Add idx_wisp_dep_type if it doesn't already exist.
SET @needs_idx_type = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisp_dependencies'
      AND INDEX_NAME = 'idx_wisp_dep_type'
);
SET @table_exists = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisp_dependencies'
);
SET @sql = IF(@table_exists > 0 AND @needs_idx_type = 1,
    'CREATE INDEX idx_wisp_dep_type ON wisp_dependencies (type)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Add idx_wisp_dep_type_depends if it doesn't already exist.
SET @needs_idx_compound = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisp_dependencies'
      AND INDEX_NAME = 'idx_wisp_dep_type_depends'
);
SET @sql = IF(@table_exists > 0 AND @needs_idx_compound = 1,
    'CREATE INDEX idx_wisp_dep_type_depends ON wisp_dependencies (type, depends_on_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
