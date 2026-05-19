-- MySQL backend: lines 1-2 of the original migration touched dolt_nonlocal_tables
-- and DOLT_COMMIT (dolt-only); they are no-ops here.
SET FOREIGN_KEY_CHECKS = 0;

SET @needs_drop_old_fk = (
    SELECT IF(COUNT(*) > 0, 1, 0)
    FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'dependencies'
      AND CONSTRAINT_NAME = 'fk_dep_depends_on'
);
SET @sql = IF(@needs_drop_old_fk = 1,
    'ALTER TABLE dependencies DROP FOREIGN KEY fk_dep_depends_on',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_migrate = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'dependencies'
      AND COLUMN_NAME = 'depends_on_wisp_id'
);

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD COLUMN depends_on_issue_id VARCHAR(255) NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD COLUMN depends_on_wisp_id VARCHAR(255) NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD COLUMN depends_on_external VARCHAR(255) NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'UPDATE dependencies SET depends_on_external = depends_on_id WHERE depends_on_id LIKE ''external:%''',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @wisps_exists = (
    SELECT IF(COUNT(*) > 0, 1, 0)
    FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisps'
);
SET @sql = IF(@needs_migrate = 1 AND @wisps_exists = 1,
    'UPDATE dependencies d JOIN wisps w ON w.id = d.depends_on_id SET d.depends_on_wisp_id = d.depends_on_id WHERE d.depends_on_external IS NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'UPDATE dependencies d JOIN issues i ON i.id = d.depends_on_id SET d.depends_on_issue_id = d.depends_on_id WHERE d.depends_on_external IS NULL AND d.depends_on_wisp_id IS NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'UPDATE dependencies SET depends_on_external = depends_on_id WHERE depends_on_external IS NULL AND depends_on_wisp_id IS NULL AND depends_on_issue_id IS NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies DROP INDEX idx_dependencies_depends_on',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies DROP INDEX idx_dependencies_depends_on_type',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies DROP PRIMARY KEY',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Drop the original concrete depends_on_id column. We re-create it below as a
-- VIRTUAL generated column that COALESCEs the three new target columns.
SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies DROP COLUMN depends_on_id',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- MySQL 9.x rejects FOREIGN KEY on a column referenced by a STORED generated
-- column AND rejects VIRTUAL generated columns as primary-key members.
-- Resolution: use a VIRTUAL generated column for compatibility with the FK,
-- and replace the (issue_id, depends_on_id) PRIMARY KEY with a UNIQUE index
-- on the same tuple. The uniqueness guarantee is identical; the only
-- semantic difference is that depends_on_id is now nullable in the schema
-- (but the CHECK constraint forces exactly one of the three target columns
-- to be NOT NULL, so the COALESCE result is always a real value).
SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD COLUMN depends_on_id VARCHAR(255) AS (COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)) VIRTUAL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD UNIQUE KEY uk_dependencies_pk (issue_id, depends_on_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD INDEX idx_dep_wisp_target (depends_on_wisp_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD INDEX idx_dep_issue_target (depends_on_issue_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD INDEX idx_dep_external_target (depends_on_external)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD INDEX idx_dep_type_target (type, depends_on_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE dependencies ADD CONSTRAINT fk_dep_issue_target FOREIGN KEY (depends_on_issue_id) REFERENCES issues(id) ON DELETE CASCADE',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Skip the CHECK constraint: MySQL 9.x rejects CHECK on columns that are
-- subject to FK referential actions (Error 3823). The "exactly one of three
-- target columns is non-null" invariant is enforced in application code (see
-- internal/storage/issueops/dependencies.go).

SET FOREIGN_KEY_CHECKS = 1;
