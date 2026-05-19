package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// Config carries the connection settings for the mysql backend. It is a
// strict subset of the dolt Config — there is no committer identity, no
// remote, no version-control plumbing.
type Config struct {
	// DSN is a fully-formed go-sql-driver/mysql DSN. When set, it takes
	// precedence over the discrete Host/Port/User/Password/Database fields.
	DSN string

	// Discrete connection settings used when DSN is empty.
	Host     string
	Port     int
	Socket   string // unix domain socket; overrides Host/Port when set
	User     string
	Password string
	Database string
	TLS      bool

	// BeadsDir is the .beads directory; required for the closed-bead JSONL
	// export. Empty string disables the export (in-process / test setups).
	BeadsDir string

	// CreateIfMissing creates the target database when it does not yet exist
	// on the server. Only init paths should set this.
	CreateIfMissing bool

	// ReadOnly opens the pool without running migrations. Useful for
	// diagnostics/inspection paths.
	ReadOnly bool

	// MaxOpenConns / MaxIdleConns / ConnMaxLifetime override the pool
	// limits. Zero values mean "use defaults".
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Pool defaults — match the dolt backend's defaults so behavior is
// predictable when callers swap one backend for the other.
const (
	defaultMaxOpenConns    = 10
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = time.Hour

	defaultHost = "127.0.0.1"
	defaultPort = 3306 // standard MySQL port (vs dolt's 3307)
	defaultUser = "root"
	defaultDB   = "beads"
)

// Open opens a connection pool to a MySQL server, validates connectivity,
// optionally creates the target database, and runs migrations. The returned
// *MySQLStore is ready to satisfy storage.Storage.
//
// Open is the primary entry point used by the cmd/bd store factory and by
// tests. Callers that already have a hand-built DSN can set Config.DSN and
// leave the discrete fields blank.
func Open(ctx context.Context, cfg *Config) (*MySQLStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mysql: Open: nil config")
	}

	cfg.applyDefaults()

	dsn, err := cfg.formatDSN()
	if err != nil {
		return nil, err
	}

	// Step 1: optionally create the database. We do this through a
	// "no-database" connection so we don't fail when the target DB does
	// not yet exist.
	if cfg.CreateIfMissing {
		if err := ensureDatabaseExists(ctx, cfg); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql: open pool: %w", err)
	}

	applyPoolLimits(db, cfg)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mysql: ping %q on server: %w", cfg.Database, err)
	}

	store := &MySQLStore{
		db:       db,
		dsn:      dsn,
		database: cfg.Database,
		beadsDir: cfg.BeadsDir,
	}

	if !cfg.ReadOnly {
		if err := store.runMigrations(ctx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("mysql: migrate: %w", err)
		}
	}

	return store, nil
}

func (c *Config) applyDefaults() {
	if c.Host == "" {
		c.Host = defaultHost
	}
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.User == "" {
		c.User = defaultUser
	}
	if c.Database == "" {
		c.Database = defaultDB
	}
}

// formatDSN builds a connection string when an explicit DSN was not provided.
// We intentionally append the parseTime/multiStatements flags that the bd
// schema migrations require: ALTER ... PREPARE / EXECUTE statements in
// migration 0041 need multiStatements; DATETIME columns need parseTime.
func (c *Config) formatDSN() (string, error) {
	if c.DSN != "" {
		// Caller-supplied DSN. Parse + re-format to enforce the flags we
		// need without overwriting their other choices.
		parsed, err := mysqldriver.ParseDSN(c.DSN)
		if err != nil {
			return "", fmt.Errorf("mysql: parse caller DSN: %w", err)
		}
		ensureRequiredDSNFlags(parsed)
		return parsed.FormatDSN(), nil
	}

	cfg := mysqldriver.NewConfig()
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.DBName = c.Database
	if c.Socket != "" {
		cfg.Net = "unix"
		cfg.Addr = c.Socket
	} else {
		cfg.Net = "tcp"
		cfg.Addr = fmt.Sprintf("%s:%d", c.Host, c.Port)
	}
	if c.TLS {
		cfg.TLSConfig = "true"
	}
	ensureRequiredDSNFlags(cfg)
	return cfg.FormatDSN(), nil
}

// ensureRequiredDSNFlags sets the driver flags this package depends on.
func ensureRequiredDSNFlags(cfg *mysqldriver.Config) {
	cfg.ParseTime = true
	cfg.MultiStatements = true
	if cfg.Loc == nil {
		cfg.Loc = time.UTC
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
}

// ensureDatabaseExists connects without selecting a database, checks for the
// target's existence, and creates it if missing. The check uses an exact
// SHOW DATABASES match (rather than SHOW DATABASES LIKE) to avoid
// underscore-as-wildcard false positives on names like "beads_vulcan".
func ensureDatabaseExists(ctx context.Context, cfg *Config) error {
	if err := validateDatabaseName(cfg.Database); err != nil {
		return err
	}

	noDBCfg := *cfg
	noDBCfg.Database = ""
	dsn, err := noDBCfg.formatDSN()
	if err != nil {
		return err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("mysql: open admin connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql: ping admin connection: %w", err)
	}

	exists, err := databaseExists(ctx, db, cfg.Database)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// #nosec G201 — cfg.Database has been validated by validateDatabaseName.
	stmt := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", cfg.Database)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "database exists") || strings.Contains(errLower, "1007") {
			return nil
		}
		return fmt.Errorf("mysql: create database %q: %w", cfg.Database, err)
	}
	return nil
}

func databaseExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return false, fmt.Errorf("mysql: SHOW DATABASES: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return false, err
		}
		if dbName == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

// validateDatabaseName rejects identifiers that contain backticks or
// non-printable characters. The check is intentionally narrow — bd's own
// database names are conservative — but it prevents the obvious
// CREATE-DATABASE injection vector when a name flows through configfile.
func validateDatabaseName(name string) error {
	if name == "" {
		return fmt.Errorf("mysql: database name is empty")
	}
	if strings.ContainsAny(name, "`\x00\n\r") {
		return fmt.Errorf("mysql: database name %q contains forbidden characters", name)
	}
	return nil
}

func applyPoolLimits(db *sql.DB, cfg *Config) {
	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = defaultMaxOpenConns
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = defaultMaxIdleConns
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = defaultConnMaxLifetime
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)
}
