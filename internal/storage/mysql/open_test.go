package mysql

import (
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// =============================================================================
// Config / DSN tests — exercise the connection-string formatting path that
// every Open call goes through.
// =============================================================================

func TestConfig_ApplyDefaults(t *testing.T) {
	c := &Config{}
	c.applyDefaults()
	if c.Host != defaultHost {
		t.Errorf("host = %q, want %q", c.Host, defaultHost)
	}
	if c.Port != defaultPort {
		t.Errorf("port = %d, want %d", c.Port, defaultPort)
	}
	if c.User != defaultUser {
		t.Errorf("user = %q, want %q", c.User, defaultUser)
	}
	if c.Database != defaultDB {
		t.Errorf("database = %q, want %q", c.Database, defaultDB)
	}
}

func TestConfig_ApplyDefaultsRespectsExisting(t *testing.T) {
	c := &Config{Host: "db.example.com", Port: 6033, User: "alice", Database: "myproj"}
	c.applyDefaults()
	if c.Host != "db.example.com" || c.Port != 6033 || c.User != "alice" || c.Database != "myproj" {
		t.Errorf("applyDefaults clobbered set fields: %+v", c)
	}
}

func TestConfig_FormatDSN_TCP(t *testing.T) {
	c := &Config{Host: "127.0.0.1", Port: 3306, User: "root", Database: "beads"}
	c.applyDefaults()
	dsn, err := c.formatDSN()
	if err != nil {
		t.Fatalf("formatDSN: %v", err)
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("re-parse DSN: %v", err)
	}
	if !parsed.ParseTime {
		t.Error("ParseTime not enabled — DATETIME columns will scan as []byte")
	}
	if !parsed.MultiStatements {
		t.Error("MultiStatements not enabled — migration 0041 needs it")
	}
	if parsed.Addr != "127.0.0.1:3306" {
		t.Errorf("Addr = %q, want 127.0.0.1:3306", parsed.Addr)
	}
	if parsed.Net != "tcp" {
		t.Errorf("Net = %q, want tcp", parsed.Net)
	}
}

func TestConfig_FormatDSN_UnixSocket(t *testing.T) {
	c := &Config{Socket: "/tmp/mysql.sock", User: "root", Database: "beads"}
	c.applyDefaults()
	dsn, err := c.formatDSN()
	if err != nil {
		t.Fatalf("formatDSN: %v", err)
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("re-parse DSN: %v", err)
	}
	if parsed.Net != "unix" {
		t.Errorf("Net = %q, want unix", parsed.Net)
	}
	if parsed.Addr != "/tmp/mysql.sock" {
		t.Errorf("Addr = %q, want /tmp/mysql.sock", parsed.Addr)
	}
}

func TestConfig_FormatDSN_PreservesCallerDSN(t *testing.T) {
	caller := "alice:secret@tcp(db.example.com:3307)/proj?charset=utf8mb4"
	c := &Config{DSN: caller}
	dsn, err := c.formatDSN()
	if err != nil {
		t.Fatalf("formatDSN: %v", err)
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if parsed.User != "alice" || parsed.Passwd != "secret" || parsed.DBName != "proj" {
		t.Errorf("caller DSN fields lost: %+v", parsed)
	}
	// We always force these flags on regardless of caller DSN.
	if !parsed.ParseTime || !parsed.MultiStatements {
		t.Errorf("required flags not enforced on caller DSN: parseTime=%v multiStatements=%v",
			parsed.ParseTime, parsed.MultiStatements)
	}
}

func TestConfig_FormatDSN_RejectsBadCallerDSN(t *testing.T) {
	c := &Config{DSN: "not a dsn"}
	if _, err := c.formatDSN(); err == nil {
		t.Error("expected error for malformed DSN, got nil")
	}
}

// =============================================================================
// Database name validation
// =============================================================================

func TestValidateDatabaseName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "beads", false},
		{"valid with underscore", "beads_v2", false},
		{"empty rejected", "", true},
		{"backtick rejected", "bea`ds", true},
		{"newline rejected", "beads\n", true},
		{"null byte rejected", "beads\x00", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateDatabaseName(c.input)
			if (err != nil) != c.wantErr {
				t.Errorf("validateDatabaseName(%q) err=%v, wantErr=%v", c.input, err, c.wantErr)
			}
		})
	}
}

// =============================================================================
// Pool limits
// =============================================================================

func TestApplyPoolLimits_Defaults(t *testing.T) {
	// We can't easily inspect the *sql.DB after applyPoolLimits without an
	// open connection, so verify the defaults are sensible numbers. This is
	// effectively a smoke test for the constants.
	if defaultMaxOpenConns <= 0 || defaultMaxIdleConns <= 0 {
		t.Errorf("non-positive default pool limits: open=%d idle=%d",
			defaultMaxOpenConns, defaultMaxIdleConns)
	}
	if defaultMaxIdleConns > defaultMaxOpenConns {
		t.Errorf("default idle (%d) > default open (%d)",
			defaultMaxIdleConns, defaultMaxOpenConns)
	}
	if defaultConnMaxLifetime <= 0 {
		t.Errorf("non-positive default lifetime: %v", defaultConnMaxLifetime)
	}
	// Just to use the time import meaningfully.
	if defaultConnMaxLifetime > 24*time.Hour {
		t.Errorf("suspiciously long default lifetime: %v", defaultConnMaxLifetime)
	}
}

// =============================================================================
// Errors helpers
// =============================================================================

func TestWrapDBError_NoRows(t *testing.T) {
	err := wrapDBError("get foo", &mysqldriver.MySQLError{Number: 1146, Message: "Table doesn't exist"})
	if err == nil || !strings.Contains(err.Error(), "get foo") {
		t.Errorf("wrap missing op prefix: %v", err)
	}
	// nil passthrough
	if wrapDBError("ignored", nil) != nil {
		t.Error("nil err should pass through")
	}
}

func TestIsTableNotExistError(t *testing.T) {
	if !isTableNotExistError(&mysqldriver.MySQLError{Number: 1146}) {
		t.Error("expected 1146 to be table-not-exist")
	}
	if isTableNotExistError(&mysqldriver.MySQLError{Number: 1213}) {
		t.Error("1213 (deadlock) should not be table-not-exist")
	}
	if isTableNotExistError(nil) {
		t.Error("nil should not be table-not-exist")
	}
}

func TestIsSerializationError(t *testing.T) {
	if !isSerializationError(&mysqldriver.MySQLError{Number: 1213}) {
		t.Error("1213 (deadlock) should be serialization")
	}
	if !isSerializationError(&mysqldriver.MySQLError{Number: 1205}) {
		t.Error("1205 (lock wait timeout) should be serialization")
	}
	if isSerializationError(&mysqldriver.MySQLError{Number: 1146}) {
		t.Error("1146 (table missing) should not be serialization")
	}
	if isSerializationError(nil) {
		t.Error("nil should not be serialization")
	}
}
