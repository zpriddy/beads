SET FOREIGN_KEY_CHECKS = 0;

SET @needs_migrate = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisp_dependencies'
      AND COLUMN_NAME = 'depends_on_wisp_id'
);

-- Phase 1: add the three split-target columns alongside depends_on_id.

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD COLUMN depends_on_issue_id VARCHAR(255) NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD COLUMN depends_on_wisp_id VARCHAR(255) NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD COLUMN depends_on_external VARCHAR(255) NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Phase 2: backfill split columns from the old depends_on_id.

SET @sql = IF(@needs_migrate = 1,
    'UPDATE wisp_dependencies SET depends_on_external = depends_on_id WHERE depends_on_id LIKE ''external:%''',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'UPDATE wisp_dependencies wd JOIN wisps w ON w.id = wd.depends_on_id SET wd.depends_on_wisp_id = wd.depends_on_id WHERE wd.depends_on_external IS NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'UPDATE wisp_dependencies wd JOIN issues i ON i.id = wd.depends_on_id SET wd.depends_on_issue_id = wd.depends_on_id WHERE wd.depends_on_external IS NULL AND wd.depends_on_wisp_id IS NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'UPDATE wisp_dependencies SET depends_on_external = depends_on_id WHERE depends_on_external IS NULL AND depends_on_wisp_id IS NULL AND depends_on_issue_id IS NULL',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Phase 3: drop the old shape.

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies DROP INDEX idx_wisp_dep_depends',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies DROP INDEX idx_wisp_dep_type_depends',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies DROP PRIMARY KEY',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies DROP COLUMN depends_on_id',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Phase 4: add indexes on the new split columns.
-- (The previously-here `ADD INDEX idx_wisp_dep_type_target (type,
-- depends_on_id)` block was removed: it ran after the DROP COLUMN
-- above and before the generated column re-add below — Error 1072.
-- The intended add is in Phase 5 below, after depends_on_id is
-- restored as a STORED generated column.)

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD INDEX idx_wisp_dep_wisp_target (depends_on_wisp_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD INDEX idx_wisp_dep_issue_target (depends_on_issue_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD INDEX idx_wisp_dep_external_target (depends_on_external)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- Phase 5: re-add depends_on_id as STORED, then the PK/index/CHECK/FK.
--
-- NOTE: FK constraints on the three target base columns
-- (depends_on_issue_id, depends_on_wisp_id, depends_on_external)
-- are ALL intentionally omitted. MySQL 9.x rejects "FOREIGN KEY on
-- a column referenced by a STORED generated column", and
-- depends_on_id is exactly that. The parallel migration 0041
-- (split_dependencies_target for issues) sidesteps this by using
-- VIRTUAL, but VIRTUAL is forbidden in PRIMARY KEY, and we need
-- depends_on_id in the PK. The CHECK constraint below +
-- application-layer validation cover the integrity story that
-- the FKs would otherwise enforce.
--
-- (Previously this section also included
-- `fk_wisp_dep_issue_target FOREIGN KEY (depends_on_issue_id)
-- REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE` —
-- removed because it violates the same rule that already led to
-- fk_wisp_dep_wisp_target being omitted.)

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD COLUMN depends_on_id VARCHAR(255) AS (COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)) STORED',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD PRIMARY KEY (issue_id, depends_on_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD INDEX idx_wisp_dep_type_target (type, depends_on_id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD CONSTRAINT ck_wisp_dep_one_target CHECK ((depends_on_issue_id IS NOT NULL) + (depends_on_wisp_id IS NOT NULL) + (depends_on_external IS NOT NULL) = 1)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- The one remaining FK: issue_id is NOT a base column of any
-- generated column, so this is safe.
-- (A duplicate of this same FK ADD also appeared earlier in the
-- migration body and was removed.)

SET @sql = IF(@needs_migrate = 1,
    'ALTER TABLE wisp_dependencies ADD CONSTRAINT fk_wisp_dep_issue FOREIGN KEY (issue_id) REFERENCES wisps(id) ON DELETE CASCADE ON UPDATE CASCADE',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET FOREIGN_KEY_CHECKS = 1;
